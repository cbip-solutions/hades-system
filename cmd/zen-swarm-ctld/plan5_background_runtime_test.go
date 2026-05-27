// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	daemonorch "github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	core "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestPlan5BudgetSnapshotReaderUsesDoctrineCapAndLiveCounters(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()

	if err := built.CostCounters.Record(daemonorch.CostLedgerRow{
		IdempotencyKey: "plan5-budget-reader",
		TS:             time.Now().Add(-time.Hour),
		Project:        plan5DaemonProjectID,
		Profile:        "worker-code",
		Tier:           "tier2",
		Provider:       "provider-a",
		Model:          "model-a",
		CostUSD:        42.50,
	}); err != nil {
		t.Fatalf("CostCounters.Record: %v", err)
	}

	reader := plan5BudgetSnapshotReader{
		counters:   built.CostCounters,
		repoRoot:   t.TempDir(),
		projectID:  plan5DaemonProjectID,
		doctrine:   "max-scope",
		paygActive: false,
	}
	snap, err := reader.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.DailyCapUSD != 100 {
		t.Fatalf("DailyCapUSD = %.2f, want doctrine max-scope project cap 100.00", snap.DailyCapUSD)
	}
	if snap.CumulativeUSD != 42.50 {
		t.Fatalf("CumulativeUSD = %.2f, want live counter total 42.50", snap.CumulativeUSD)
	}
	if snap.ProjectID != plan5DaemonProjectID {
		t.Fatalf("ProjectID = %q, want %q", snap.ProjectID, plan5DaemonProjectID)
	}
	if snap.DoctrineName != "max-scope" {
		t.Fatalf("DoctrineName = %q, want max-scope", snap.DoctrineName)
	}
}

func TestPlan5EventLogHeartbeatProbeReadsWorkerBeats(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)

	dispatchedAt := clk.Now()
	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: plan5DaemonSessionID,
		ProjectID: plan5DaemonProjectID,
		Timestamp: dispatchedAt,
		Payload: map[string]any{
			"worker_id": "worker-1",
			"task_id":   "task-1",
			"tier":      "tier2",
		},
	}); err != nil {
		t.Fatalf("append dispatched: %v", err)
	}
	checkpointAt := dispatchedAt.Add(2 * time.Minute)
	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: plan5DaemonSessionID,
		ProjectID: plan5DaemonProjectID,
		Timestamp: checkpointAt,
		Payload: map[string]any{
			"worker_id":      "worker-1",
			"task_id":        "task-1",
			"checkpoint_sha": "abc123",
		},
	}); err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}

	probe := plan5EventLogHeartbeatProbe{
		log:       log,
		sessionID: plan5DaemonSessionID,
	}
	beats, err := probe.LastBeats(context.Background())
	if err != nil {
		t.Fatalf("LastBeats: %v", err)
	}
	if got := beats["worker-1"]; !got.Equal(checkpointAt) {
		t.Fatalf("worker-1 beat = %s, want checkpoint timestamp %s", got, checkpointAt)
	}
	firstCursor := probe.lastSeen
	if firstCursor == 0 {
		t.Fatal("probe.lastSeen = 0, want incremental cursor after first read")
	}

	worker2At := checkpointAt.Add(time.Minute)
	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: plan5DaemonSessionID,
		ProjectID: plan5DaemonProjectID,
		Timestamp: worker2At,
		Payload: map[string]any{
			"worker_id":      "worker-2",
			"task_id":        "task-2",
			"checkpoint_sha": "def456",
		},
	}); err != nil {
		t.Fatalf("append worker-2 checkpoint: %v", err)
	}
	beats, err = probe.LastBeats(context.Background())
	if err != nil {
		t.Fatalf("LastBeats after incremental append: %v", err)
	}
	if !beats["worker-1"].Equal(checkpointAt) {
		t.Fatalf("worker-1 beat was not retained across incremental read")
	}
	if !beats["worker-2"].Equal(worker2At) {
		t.Fatalf("worker-2 beat = %s, want %s", beats["worker-2"], worker2At)
	}
	if probe.lastSeen <= firstCursor {
		t.Fatalf("probe.lastSeen = %d, want > first cursor %d", probe.lastSeen, firstCursor)
	}
}

func TestStartPlan5BackgroundSupervisorCountsContractTopology(t *testing.T) {
	st := openTestStore(t)
	svc := buildTestPlan5ServiceWithDoctrine(t, st, "max-scope")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supervisor, err := startPlan5BackgroundSupervisor(ctx, plan5BackgroundRuntimeConfig{
		Service: svc,
		Gate:    newPlan5MemoryGate(),
		Budget:  plan5StaticBudgetReader{capUSD: 100, doctrine: "max-scope", projectID: plan5DaemonProjectID},
	})
	if err != nil {
		t.Fatalf("startPlan5BackgroundSupervisor: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := supervisor.Stop(stopCtx); err != nil {
			t.Fatalf("background supervisor Stop: %v", err)
		}
	})

	info, err := svc.Session()
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if info.BackgroundGoroutines != 9 {
		t.Fatalf("BackgroundGoroutines = %d, want 9 daemon-owned Plan 5 contract slots", info.BackgroundGoroutines)
	}
	names := supervisor.Names()
	sort.Strings(names)
	for _, want := range []string{
		"amendment-pattern-detector",
		"cost-gating-evaluator",
		"eventlog-subscriber-dispatcher",
		"hra-cadence",
		"recovery-heartbeat-monitor",
		"safetynet-drift-detector",
		"safetynet-regression-updater",
	} {
		if !containsString(names, want) {
			t.Fatalf("supervisor names = %v, missing %q", names, want)
		}
	}
}

func TestStartPlan5BackgroundSupervisorReportsFullDoctorTopologyWithWorktreePool(t *testing.T) {
	st := openTestStore(t)
	svc := buildTestPlan5ServiceWithDoctrine(t, st, "max-scope")
	p, err := worktreepool.NewPool(worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   time.Hour,
		Clock:       clock.Real{},
	}, daemonPlan5RuntimeTestAppender{}, daemonPlan5RuntimeTestExecutor{})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Close(ctx)
	})
	svc.SetWorktreePool(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	supervisor, err := startPlan5BackgroundSupervisor(ctx, plan5BackgroundRuntimeConfig{
		Service: svc,
		Gate:    newPlan5MemoryGate(),
		Budget:  plan5StaticBudgetReader{capUSD: 100, doctrine: "max-scope", projectID: plan5DaemonProjectID},
	})
	if err != nil {
		t.Fatalf("startPlan5BackgroundSupervisor: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := supervisor.Stop(stopCtx); err != nil {
			t.Fatalf("background supervisor Stop: %v", err)
		}
	})

	info, err := svc.Session()
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if info.BackgroundGoroutines != 11 {
		t.Fatalf("BackgroundGoroutines = %d, want 11 (2 worktree + 9 daemon runners)", info.BackgroundGoroutines)
	}
}

func buildTestPlan5ServiceWithDoctrine(t *testing.T, st *store.Store, doctrine string) *daemon.Plan5OrchestratorService {
	t.Helper()
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("orchestratoradapter.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := daemon.NewPlan5OrchestratorService(daemon.Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: t.TempDir(),
		Doctrine: doctrine,
	})
	if err != nil {
		t.Fatalf("NewPlan5OrchestratorService: %v", err)
	}
	return svc
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

type daemonPlan5RuntimeTestAppender struct{}

func (daemonPlan5RuntimeTestAppender) Append(context.Context, eventlog.Event) (int64, error) {
	return 0, nil
}

type daemonPlan5RuntimeTestExecutor struct{}

func (daemonPlan5RuntimeTestExecutor) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

type plan5StaticBudgetReader struct {
	capUSD    float64
	doctrine  string
	projectID string
}

func (r plan5StaticBudgetReader) Snapshot(context.Context) (core.BudgetSnapshot, error) {
	return core.BudgetSnapshot{
		DailyCapUSD:  r.capUSD,
		ProjectID:    r.projectID,
		DoctrineName: r.doctrine,
	}, nil
}
