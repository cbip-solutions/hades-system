package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

func newTestPlan5Service(t *testing.T) *Plan5OrchestratorService {
	t.Helper()
	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter: a,
	})
	if err != nil {
		t.Fatalf("NewPlan5OrchestratorService: %v", err)
	}
	return svc
}

func TestPlan5Service_RejectsNilAdapter(t *testing.T) {
	if _, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{}); err == nil {
		t.Fatal("expected error on nil Adapter")
	}
}

func TestPlan5Service_SessionAndPoolReturnTruthfulIdleSnapshot(t *testing.T) {
	svc := newTestPlan5Service(t)

	info, err := svc.Session()
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if info.State != "idle" {
		t.Errorf("State: got %q, want %q (no live build)", info.State, "idle")
	}
	if info.BackgroundGoroutines != 0 {
		t.Errorf("BackgroundGoroutines: got %d, want 0 (idle)", info.BackgroundGoroutines)
	}

	pool, err := svc.Pool()
	if err != nil {
		t.Fatalf("Pool: %v", err)
	}
	if !pool.HealthOK {
		t.Errorf("HealthOK: got false, want true (idle pool)")
	}
	if pool.CurrentLeased != 0 || pool.ElasticInUse != 0 {
		t.Errorf("expected idle pool counters zero, got %+v", pool)
	}

	pruned, err := svc.PrunePool()
	if err != nil {
		t.Fatalf("PrunePool: %v", err)
	}
	if pruned != 0 {
		t.Errorf("PrunePool: got %d, want 0 (no pool to prune)", pruned)
	}
}

func TestPlan5Service_SetDepthReturnsConfiguredError(t *testing.T) {
	svc := newTestPlan5Service(t)
	err := svc.SetDepth(client.DepthOverride{ProjectID: "p", Depth: 2})
	if err == nil {
		t.Fatal("expected ErrDepthOverridesUnconfigured (Plan 8 persistence not wired)")
	}
}

func TestPlan5Service_AutonomyModeRoundTrip(t *testing.T) {
	svc := newTestPlan5Service(t)

	show, err := svc.AutonomyShow()
	if err != nil {
		t.Fatalf("AutonomyShow: %v", err)
	}
	if show.FlagMode != "" {
		t.Errorf("default FlagMode: got %q, want empty", show.FlagMode)
	}

	if err := svc.AutonomyMode(client.AutonomyModeRequest{Mode: "semi"}); err != nil {
		t.Fatalf("AutonomyMode: %v", err)
	}
	show2, _ := svc.AutonomyShow()
	if show2.FlagMode != "semi" {
		t.Errorf("after Set: FlagMode = %q, want semi", show2.FlagMode)
	}
	if show2.EffectiveMode != "semi" {
		t.Errorf("after Set: EffectiveMode = %q, want semi", show2.EffectiveMode)
	}

	if err := svc.AutonomyMode(client.AutonomyModeRequest{Reset: true}); err != nil {
		t.Fatalf("AutonomyMode reset: %v", err)
	}
	show3, _ := svc.AutonomyShow()
	if show3.FlagMode != "" {
		t.Errorf("after reset: FlagMode = %q, want empty", show3.FlagMode)
	}
}

func TestPlan5Service_AutonomyModeRejectsUnknownToken(t *testing.T) {
	svc := newTestPlan5Service(t)
	err := svc.AutonomyMode(client.AutonomyModeRequest{Mode: "bogus"})
	if err == nil {
		t.Fatal("expected error on unknown mode token")
	}
}

func TestPlan5Service_AutonomyModeRequiresModeOrReset(t *testing.T) {
	svc := newTestPlan5Service(t)
	err := svc.AutonomyMode(client.AutonomyModeRequest{})
	if err == nil {
		t.Fatal("expected error when neither mode nor reset is set")
	}
}

func TestPlan5Service_DoctrineProposeListEmptyWhenNoRepo(t *testing.T) {
	svc := newTestPlan5Service(t)
	list, err := svc.DoctrineProposeList()
	if err != nil {
		t.Fatalf("DoctrineProposeList: %v", err)
	}
	if len(list.Proposals) != 0 {
		t.Errorf("expected empty list (no RepoRoot), got %+v", list)
	}
}

func TestPlan5Service_DoctrineProposeListScansFilesystem(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions", "proposed"))
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions", "rejected"))
	mustWriteFile(t, filepath.Join(dir, "docs", "decisions", "proposed", "0020-test-amendment.md"),
		"# Test Amendment\n\nProposal body.")
	mustWriteFile(t, filepath.Join(dir, "docs", "decisions", "0019-applied-amendment.md"),
		"# Applied Amendment\n\nApplied body.")
	mustWriteFile(t, filepath.Join(dir, "docs", "decisions", "rejected", "0021-rejected-amendment.md"),
		"# Rejected Amendment\n\nRejected body.")

	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}

	list, err := svc.DoctrineProposeList()
	if err != nil {
		t.Fatalf("DoctrineProposeList: %v", err)
	}
	if len(list.Proposals) != 3 {
		t.Fatalf("expected 3 proposals, got %d: %+v", len(list.Proposals), list.Proposals)
	}
	statuses := map[string]string{}
	for _, p := range list.Proposals {
		statuses[p.ID] = p.Status
	}
	if statuses["ADR-0019"] != "applied" {
		t.Errorf("ADR-0019: got %q, want applied", statuses["ADR-0019"])
	}
	if statuses["ADR-0020"] != "proposed" {
		t.Errorf("ADR-0020: got %q, want proposed", statuses["ADR-0020"])
	}
	if statuses["ADR-0021"] != "denied" {
		t.Errorf("ADR-0021: got %q, want denied", statuses["ADR-0021"])
	}

	prop, err := svc.DoctrineProposeShow("ADR-0020")
	if err != nil {
		t.Fatalf("DoctrineProposeShow: %v", err)
	}
	if prop.Title != "Test Amendment" {
		t.Errorf("title: got %q, want %q", prop.Title, "Test Amendment")
	}
}

func TestPlan5Service_DoctrineProposeShowReturnsNotFound(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.DoctrineProposeShow("ADR-9999"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestPlan5Service_DoctrineAckEmitsEvent(t *testing.T) {
	svc := newTestPlan5Service(t)
	if err := svc.DoctrineAck(client.DoctrineDecision{
		ID:     "ADR-0020",
		Reason: "approved by operator",
	}); err != nil {
		t.Fatalf("DoctrineAck: %v", err)
	}

	rows, err := svc.adapter.QueryRaw(context.Background(), "operator-action", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 ack row, got %d", len(rows))
	}
	if rows[0].EventType != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Errorf("event type: got %v, want EvtDoctrineAmendmentSuppressed", rows[0].EventType)
	}
}

func TestPlan5Service_DoctrineDenyEmitsEvent(t *testing.T) {
	svc := newTestPlan5Service(t)
	if err := svc.DoctrineDeny(client.DoctrineDecision{ID: "ADR-0020"}); err != nil {
		t.Fatalf("DoctrineDeny: %v", err)
	}
	rows, _ := svc.adapter.QueryRaw(context.Background(), "operator-action", 0)
	if len(rows) != 1 {
		t.Fatalf("expected 1 deny row, got %d", len(rows))
	}
}

func TestPlan5Service_DoctrineAckRejectsBadID(t *testing.T) {
	svc := newTestPlan5Service(t)
	if err := svc.DoctrineAck(client.DoctrineDecision{ID: ""}); err == nil {
		t.Fatal("expected error on empty ID")
	}
	if err := svc.DoctrineAck(client.DoctrineDecision{ID: "BOGUS"}); err == nil {
		t.Fatal("expected error on non-numeric ID")
	}
}

func TestPlan5Service_DoctrineRevertWithoutRepoErrors(t *testing.T) {
	svc := newTestPlan5Service(t)
	err := svc.DoctrineRevert(client.DoctrineDecision{ID: "ADR-0020"})
	if err == nil {
		t.Fatal("expected error when RepoRoot not configured")
	}
}

func TestPlan5Service_DoctrineProposeAllocatesADRInPlan8Range(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions"))

	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}

	resp, err := svc.DoctrinePropose(client.DoctrineProposeRequest{
		RulePath:      "amendment.cooldown_hours",
		NewValue:      "12",
		Justification: "reduce cooldown",
		Category:      "merge",
	})
	if err != nil {
		t.Fatalf("DoctrinePropose: %v", err)
	}
	if resp.ID != "ADR-0050" {
		t.Errorf("ID: got %q, want ADR-0050 (first free in Plan 8 range)", resp.ID)
	}
	if resp.Status != "proposed" {
		t.Errorf("Status: got %q, want proposed", resp.Status)
	}
	if resp.RulePath != "amendment.cooldown_hours" {
		t.Errorf("RulePath: got %q", resp.RulePath)
	}
	if resp.Proposer != "operator" {
		t.Errorf("Proposer: got %q, want operator", resp.Proposer)
	}
	if resp.AdrMarkdownPath == "" {
		t.Errorf("AdrMarkdownPath should be populated")
	}

	absPath := filepath.Join(dir, "docs", "decisions", "proposed", "0050-amendment-cooldown-hours.md")
	body, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read ADR markdown: %v", err)
	}
	for _, want := range []string{"ADR-0050", "amendment.cooldown_hours", "12", "merge", "reduce cooldown", "inv-zen-103"} {
		if !contains(string(body), want) {
			t.Errorf("ADR body missing %q; got: %s", want, string(body))
		}
	}

	rows, err := svc.adapter.QueryRaw(context.Background(), "operator-action", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(rows))
	}
	if rows[0].EventType != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Errorf("event type: got %v", rows[0].EventType)
	}
	var payload map[string]any
	_ = json.Unmarshal(rows[0].Payload, &payload)
	if payload["decision"] != "propose" {
		t.Errorf("payload.decision: got %v, want propose", payload["decision"])
	}
}

func TestPlan5Service_DoctrineProposeSkipsUsedSlots(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions", "proposed"))
	mustWriteFile(t, filepath.Join(dir, "docs", "decisions", "proposed", "0050-existing.md"), "# old\n")

	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	resp, err := svc.DoctrinePropose(client.DoctrineProposeRequest{
		RulePath:      "x.y",
		NewValue:      "z",
		Justification: "j",
		Category:      "cost",
	})
	if err != nil {
		t.Fatalf("DoctrinePropose: %v", err)
	}
	if resp.ID != "ADR-0051" {
		t.Errorf("ID: got %q, want ADR-0051 (0050 already allocated)", resp.ID)
	}
}

func TestPlan5Service_DoctrineProposeRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions"))

	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}

	cases := []struct {
		name    string
		req     client.DoctrineProposeRequest
		errPart string
	}{
		{"empty rule_path", client.DoctrineProposeRequest{NewValue: "z", Justification: "j", Category: "cost"}, "missing_rule_path"},
		{"empty new_value", client.DoctrineProposeRequest{RulePath: "x", Justification: "j", Category: "cost"}, "missing_new_value"},
		{"empty justification", client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Category: "cost"}, "missing_justification"},
		{"empty category", client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Justification: "j"}, "missing_category"},
		{"invalid category", client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Justification: "j", Category: "garbage"}, "invalid_category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.DoctrinePropose(tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tc.errPart) {
				t.Errorf("error should contain %q; got: %v", tc.errPart, err)
			}
		})
	}
}

func TestPlan5Service_DoctrineProposeWithoutRepoErrors(t *testing.T) {
	svc := newTestPlan5Service(t)
	_, err := svc.DoctrinePropose(client.DoctrineProposeRequest{
		RulePath:      "x.y",
		NewValue:      "z",
		Justification: "j",
		Category:      "cost",
	})
	if err == nil {
		t.Fatal("expected error when RepoRoot not configured")
	}
	if !contains(err.Error(), "invalid_rule_path") {
		t.Errorf("expected invalid_rule_path; got: %v", err)
	}
}

func TestPlan5Service_DoctrineProposeRangeExhausted(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "docs", "decisions", "proposed"))
	for i := 50; i <= 59; i++ {
		mustWriteFile(t, filepath.Join(dir, "docs", "decisions", "proposed", fmt.Sprintf("%04d-x.md", i)), "# x\n")
	}

	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	_, err = svc.DoctrinePropose(client.DoctrineProposeRequest{
		RulePath:      "x.y",
		NewValue:      "z",
		Justification: "j",
		Category:      "cost",
	})
	if err == nil {
		t.Fatal("expected error when range exhausted")
	}
	if !contains(err.Error(), "exhausted") {
		t.Errorf("expected 'exhausted' in error; got: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringIndex(haystack, needle) != -1
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestPlan5Service_SafetynetStatusReportsZeroDriftWhenEmpty(t *testing.T) {
	svc := newTestPlan5Service(t)
	st, err := svc.SafetynetStatus()
	if err != nil {
		t.Fatalf("SafetynetStatus: %v", err)
	}
	if st.DriftIncidents24h != 0 {
		t.Errorf("DriftIncidents24h: got %d, want 0", st.DriftIncidents24h)
	}
	if !st.LastDivergenceClean {
		t.Errorf("LastDivergenceClean: got false, want true (no divergence events recorded)")
	}
}

func TestPlan5Service_RegressionQueryRoundTrip(t *testing.T) {
	svc := newTestPlan5Service(t)
	rec := safetynet.HealthRecord{
		CommitSHA:        "deadbeef00000000",
		AuthoredBy:       "substrate",
		TestPassRate:     0.95,
		TestTotal:        100,
		TestPassed:       95,
		DoctrineLintPass: true,
		RecordedAt:       time.Now().Unix(),
	}
	if err := svc.regression.Record(context.Background(), rec); err != nil {
		t.Fatalf("regression.Record: %v", err)
	}

	rows, err := svc.SafetynetRegressionQuery("substrate", "24h")
	if err != nil {
		t.Fatalf("SafetynetRegressionQuery: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].CommitSHA != "deadbeef00000000" {
		t.Errorf("CommitSHA: got %q", rows[0].CommitSHA)
	}
}

func TestPlan5Service_RegressionQueryRejectsBadDuration(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.SafetynetRegressionQuery("substrate", "not-a-duration"); err == nil {
		t.Fatal("expected error on bad duration")
	}
}

func TestPlan5Service_RegressionQueryAggregatesAuthorsWhenEmpty(t *testing.T) {
	svc := newTestPlan5Service(t)
	for _, author := range []string{"substrate", "operator", "manual"} {
		_ = svc.regression.Record(context.Background(), safetynet.HealthRecord{
			CommitSHA: "sha-" + author, AuthoredBy: author,
			TestPassRate: 1.0, TestTotal: 1, TestPassed: 1,
			DoctrineLintPass: true, RecordedAt: time.Now().Unix(),
		})
	}
	rows, err := svc.SafetynetRegressionQuery("", "1h")
	if err != nil {
		t.Fatalf("SafetynetRegressionQuery empty author: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows merged across authors, got %d", len(rows))
	}
}

func TestPlan5Service_PrevShowReportsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	a, _ := orchestratoradapter.New(st)
	t.Cleanup(func() { _ = a.Close() })

	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	out, err := svc.SafetynetPrevShow()
	if err != nil {
		t.Fatalf("SafetynetPrevShow: %v", err)
	}
	if out["installed"] != "false" {
		t.Errorf("installed: got %q, want false", out["installed"])
	}
}

func TestPlan5Service_PrevExecRequiresArgv(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	a, _ := orchestratoradapter.New(st)
	t.Cleanup(func() { _ = a.Close() })
	svc, _ := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter:  a,
		RepoRoot: dir,
	})
	if _, err := svc.SafetynetPrevExec(nil); err == nil {
		t.Fatal("expected error on empty argv")
	}
}

func TestPlan5Service_DriftRunWithoutRepoErrors(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.SafetynetDriftRun(); err == nil {
		t.Fatal("expected error when RepoRoot not configured")
	}
}

func TestPlan5Service_HealthEventLogWritableProbes(t *testing.T) {

	svc := newTestPlan5Service(t)
	writable, _, err := svc.HealthEventLogWritable()
	if err != nil {
		t.Fatalf("HealthEventLogWritable (no sampler): unexpected error: %v", err)
	}
	if writable {
		t.Errorf("HealthEventLogWritable (no sampler): expected false (no snapshot primed)")
	}

	svc.SetHealthSampler(staticSampler(orchestrator.HealthSnapshot{
		Deps: map[string]orchestrator.DepHealth{"event_log_writable": {Up: true}},
	}))
	writable, _, err = svc.HealthEventLogWritable()
	if err != nil {
		t.Fatalf("HealthEventLogWritable (sampler primed): unexpected error: %v", err)
	}
	if !writable {
		t.Errorf("HealthEventLogWritable (sampler primed): expected true")
	}
}

func TestPlan5Service_HealthAdaptersCleanInitiallyTrue(t *testing.T) {
	svc := newTestPlan5Service(t)
	clean, err := svc.HealthAdaptersClean()
	if err != nil {
		t.Fatalf("HealthAdaptersClean: %v", err)
	}
	if !clean {
		t.Errorf("expected adaptersClean=true on construction")
	}
}

func TestPlan5Service_HealthLastSessionCleanWhenNoEvents(t *testing.T) {
	svc := newTestPlan5Service(t)
	clean, err := svc.HealthLastSessionClean()
	if err != nil {
		t.Fatalf("HealthLastSessionClean: %v", err)
	}
	if !clean {
		t.Errorf("expected clean=true on idle daemon (no recorded sessions)")
	}
}

func TestPlan5Service_CaptureRequiresFields(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.Capture(client.CaptureRequest{}); err == nil {
		t.Fatal("expected error on empty session_id + output_path")
	}
	if _, err := svc.Capture(client.CaptureRequest{SessionID: "s"}); err == nil {
		t.Fatal("expected error on empty output_path")
	}
}

func TestPlan5Service_CaptureAndReplayRoundTrip(t *testing.T) {
	svc := newTestPlan5Service(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_, err := svc.adapter.EmitRaw(ctx, "p", "session-cap",
			int(eventlog.EvtOrchestratorStarted),
			[]byte(`{"i":`+itoa(i)+`}`),
			time.Now().UnixNano())
		if err != nil {
			t.Fatalf("EmitRaw: %v", err)
		}
	}
	out := filepath.Join(t.TempDir(), "capture.jsonl")
	cap, err := svc.Capture(client.CaptureRequest{
		SessionID:  "session-cap",
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if cap.EventCount != 4 {
		t.Errorf("EventCount: got %d, want 4", cap.EventCount)
	}
	if cap.BytesWritten <= 0 {
		t.Errorf("BytesWritten: got %d, want >0", cap.BytesWritten)
	}

	rep, err := svc.Replay(client.ReplayRequest{InputPath: out})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if rep.EventsReplayed != 4 {
		t.Errorf("EventsReplayed: got %d, want 4", rep.EventsReplayed)
	}
	if !rep.Deterministic {
		t.Errorf("Deterministic: got false, want true; divergences=%v", rep.Divergences)
	}
}

func TestPlan5Service_ReplayDetectsBackwardsEventID(t *testing.T) {
	svc := newTestPlan5Service(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "bad.jsonl")

	rows := []string{
		`{"event_id":3,"session_id":"s","event_type":1,"payload":{}}`,
		`{"event_id":2,"session_id":"s","event_type":1,"payload":{}}`,
	}
	if err := os.WriteFile(out, []byte(rows[0]+"\n"+rows[1]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := svc.Replay(client.ReplayRequest{InputPath: out})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if rep.Deterministic {
		t.Errorf("expected Deterministic=false on backwards event_id")
	}
	if len(rep.Divergences) == 0 {
		t.Errorf("expected at least one divergence row")
	}
}

func TestPlan5Service_ReplayRequiresInputPath(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.Replay(client.ReplayRequest{}); err == nil {
		t.Fatal("expected error on empty input_path")
	}
}

func TestPlan5Service_DivergenceRunWithoutRepoErrors(t *testing.T) {
	svc := newTestPlan5Service(t)
	if _, err := svc.SafetynetDivergenceRun(); err == nil {
		t.Fatal("expected error when RepoRoot not configured")
	}
}

func TestPlan5Service_DivergenceHistoryEmptyWhenNoEvents(t *testing.T) {
	svc := newTestPlan5Service(t)
	rows, err := svc.SafetynetDivergenceHistory("24h")
	if err != nil {
		t.Fatalf("SafetynetDivergenceHistory: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty history, got %d rows", len(rows))
	}
}

func TestPlan5Service_DriftHistoryEmptyWhenNoEvents(t *testing.T) {
	svc := newTestPlan5Service(t)
	rows, err := svc.SafetynetDriftHistory("7d")
	if err != nil {
		t.Fatalf("SafetynetDriftHistory: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty history, got %d rows", len(rows))
	}
}

func TestPlan5Service_ParseSinceDurationAcceptsDays(t *testing.T) {
	d, err := parseSinceDuration("3d", 0)
	if err != nil {
		t.Fatalf("parseSinceDuration: %v", err)
	}
	if d != 3*24*time.Hour {
		t.Errorf("3d: got %v, want 72h", d)
	}
}

func TestPlan5Service_ParseSinceDurationDefaultsOnEmpty(t *testing.T) {
	d, err := parseSinceDuration("", 5*time.Minute)
	if err != nil {
		t.Fatalf("parseSinceDuration: %v", err)
	}
	if d != 5*time.Minute {
		t.Errorf("default: got %v, want 5m", d)
	}
}

func TestPlan5Service_ParseSinceDurationRejectsBogus(t *testing.T) {
	if _, err := parseSinceDuration("zz", 0); err == nil {
		t.Fatal("expected error on bogus duration")
	}
	if _, err := parseSinceDuration("nopd", 0); err == nil {
		t.Fatal("expected error on non-numeric Nd")
	}
}

func TestPlan5Service_AdrIDFromFilenameVariants(t *testing.T) {
	cases := map[string]string{
		"0020-test.md":    "ADR-0020",
		"0001-foo-bar.md": "ADR-0001",
		"not-a-match.md":  "",
		"NNNN-bad.md":     "",
	}
	for in, want := range cases {
		if got := adrIDFromFilename(in); got != want {
			t.Errorf("adrIDFromFilename(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestPlan5Service_AdrTitleFromBodyFallsBackToFilename(t *testing.T) {
	if got := adrTitleFromBody("no h1 here\n", "0020-some-title.md"); got != "some title" {
		t.Errorf("title fallback: got %q, want %q", got, "some title")
	}
	if got := adrTitleFromBody("# Real Title\n\nbody", "0020-x.md"); got != "Real Title" {
		t.Errorf("h1 extract: got %q", got)
	}
}

func TestPlan5Service_ExtractHoursFromReason(t *testing.T) {
	if extractHoursFromReason("wiki age 36h exceeds threshold 24h") != 36 {
		t.Errorf("expected 36 from first match")
	}
	if extractHoursFromReason("nothing matches") != 0 {
		t.Errorf("expected 0 on no match")
	}
}

func TestPlan5Service_ParseADRIDAcceptsBoth(t *testing.T) {
	if id, err := parseADRID("ADR-0020"); err != nil || id != 20 {
		t.Errorf("parseADRID(\"ADR-0020\") = %d, %v; want 20, nil", id, err)
	}
	if id, err := parseADRID("0020"); err != nil || id != 20 {
		t.Errorf("parseADRID(\"0020\") = %d, %v; want 20, nil", id, err)
	}
	if _, err := parseADRID("bogus"); err == nil {
		t.Errorf("expected error on bogus")
	}
}

func mustMkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", p, err)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func itoa(i int) string {
	switch {
	case i == 0:
		return "0"
	case i < 0:
		return "-" + itoa(-i)
	default:
		var b [20]byte
		n := len(b)
		for i > 0 {
			n--
			b[n] = byte('0' + i%10)
			i /= 10
		}
		return string(b[n:])
	}
}

var _ = json.Marshal
