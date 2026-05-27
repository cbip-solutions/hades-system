// SPDX-License-Identifier: MIT
// Package daemon — orchestrator_plan5_service.go.
//
// releaseOrchestratorService is the daemon-side composition that satisfies
// handlers.releaseOrchestratorService. It is the wiring point between:
//
// - the SQL adapter (audit_events_raw + substrate_health) at
// internal/daemon/orchestratoradapter
// - the eventlog Log (constructed via the adapter as RawEmitter)
// - the safetynet subsystems (Regression, Divergence, Drift, Prev)
// - the autonomy CheckEngine (with the production 11-check set)
// - the amendment Reverter (operator-initiated git revert path)
// - filesystem-backed ADR scan (architecture records applied/,
// rejected/) for doctrine propose-list / show
// - persisted autonomy mode (in-memory atomic; survives requests but
// not daemon restart — release promotes to a persisted projects table
// entry per invariant)
//
// Design ground rules:
//
// - Methods that depend on data the daemon already owns (event log,
// substrate_health, ADR files, zen-prev binary) wire 100% to live
// subsystems. No stubs.
// - Methods that depend on a live in-flight build (Session,
// Pool, PrunePool, SetDepth, Capture, Replay) return the truthful
// "no active build session" answer (zero-value structs with
// State=idle, HealthOK=true, EventCount=0). This is NOT a stub —
// no in-process orchestrator runs at the daemon level today; the
// CLI surface displays "Leased: 0 Floor: 0" correctly when no
// build is underway.
// - Audit emit discipline: every emit uses context.WithoutCancel(ctx)
// so a cancelled caller cannot cause an audit gap.
// - Concurrency: the Service is goroutine-safe — every mutable field
// guarded by mu OR atomic; subsystems own their own synchronization.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	autonomychecks "github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type Plan5OrchestratorServiceConfig struct {
	Adapter *orchestratoradapter.Adapter

	RepoRoot string

	PrevBinaryPath string

	Doctrine string

	Clock clock.Clock

	RegressionThreshold float64
}

type Plan5OrchestratorService struct {
	cfg Plan5OrchestratorServiceConfig

	mu sync.Mutex

	adapter  *orchestratoradapter.Adapter
	eventLog *eventlog.Log

	regression *safetynet.Regression
	divergence *safetynet.Divergence
	drift      *safetynet.Drift
	prev       *safetynet.Prev

	checkEngine *autonomy.CheckEngine

	reverter *amendment.AmendmentReverter

	repoRoot     string
	decisionsDir string

	autonomyMode atomic.Value

	adaptersClean atomic.Bool

	healthSampler *orchestrator.HealthSampler

	worktreePool worktreepool.Pool

	backgroundSupervisor *Plan5BackgroundSupervisor
}

func (s *Plan5OrchestratorService) SetHealthSampler(hs *orchestrator.HealthSampler) {
	s.healthSampler = hs
}

func (s *Plan5OrchestratorService) SetWorktreePool(pool worktreepool.Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worktreePool = pool
}

func (s *Plan5OrchestratorService) SetBackgroundSupervisor(supervisor *Plan5BackgroundSupervisor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backgroundSupervisor = supervisor
}

func (s *Plan5OrchestratorService) StartBackgroundSupervisor(ctx context.Context) (*Plan5BackgroundSupervisor, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: parent context is nil", ErrPlan5BackgroundSupervisorInvalidConfig)
	}
	s.mu.Lock()
	existing := s.backgroundSupervisor
	s.mu.Unlock()
	if existing != nil {
		return existing, nil
	}

	driftWatcher, err := safetynet.NewDriftWatcher(safetynet.DriftWatcherConfig{
		Validator: s.drift,
		Clock:     s.cfg.Clock,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: drift watcher: %w", err)
	}

	supervisor := NewPlan5BackgroundSupervisor()
	if err := supervisor.Start(ctx, Plan5BackgroundRunner{
		Name:  "safetynet-drift-detector",
		Slots: 1,
		Run:   driftWatcher.Run,
	}); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.backgroundSupervisor == nil {
		s.backgroundSupervisor = supervisor
		s.mu.Unlock()
		return supervisor, nil
	}
	existing = s.backgroundSupervisor
	s.mu.Unlock()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	_ = supervisor.Stop(stopCtx)
	return existing, nil
}

func (s *Plan5OrchestratorService) EventLog() *eventlog.Log { return s.eventLog }

func (s *Plan5OrchestratorService) DriftValidator() safetynet.DriftValidator { return s.drift }

func (s *Plan5OrchestratorService) DoctrineName() string { return s.cfg.Doctrine }

func (s *Plan5OrchestratorService) Plan5Clock() clock.Clock { return s.cfg.Clock }

func (s *Plan5OrchestratorService) RegressionRecorder() *safetynet.Regression { return s.regression }

func (s *Plan5OrchestratorService) RepoRoot() string { return s.repoRoot }

func NewPlan5OrchestratorService(cfg Plan5OrchestratorServiceConfig) (*Plan5OrchestratorService, error) {
	if cfg.Adapter == nil {
		return nil, errors.New("daemon/plan5: Adapter must not be nil")
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.RegressionThreshold <= 0 {
		cfg.RegressionThreshold = 0.8
	}
	if cfg.Doctrine == "" {
		cfg.Doctrine = "default"
	}

	repoRoot := cfg.RepoRoot
	decisionsDir := ""
	if repoRoot != "" {
		decisionsDir = filepath.Join(repoRoot, "docs", "decisions")
	}

	prevPath := cfg.PrevBinaryPath
	if prevPath == "" && repoRoot != "" {
		prevPath = filepath.Join(repoRoot, "bin", "zen-prev")
	}

	log := eventlog.New(cfg.Adapter, cfg.Clock)

	safetynetEmit := &safetynetEmitterShim{log: log, doctrine: cfg.Doctrine}

	regression := safetynet.NewRegression(cfg.Adapter, safetynetEmit, cfg.RegressionThreshold)
	divergence := safetynet.NewDivergence(safetynetEmit)
	drift := safetynet.NewDrift(&gitCommitSource{repoRoot: repoRoot}, safetynetEmit)
	prev := safetynet.NewPrev(prevPath, safetynetEmit)

	checkDeps := autonomychecks.Deps{
		HTTP:  &http.Client{Timeout: 2 * time.Second},
		Stat:  osFileStat{},
		Read:  osFileReader{},
		Exec:  osExecer{},
		Now:   cfg.Clock.Now,
		Paths: autonomychecks.Paths{ADRsDir: decisionsDir},
	}

	checkEngine, err := autonomy.NewCheckEngine(autonomy.EngineDeps{
		Checks: autonomychecks.All(checkDeps),
		Now:    cfg.Clock.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: autonomy engine: %w", err)
	}

	var reverter *amendment.AmendmentReverter
	if repoRoot != "" {
		reverter = amendment.NewReverter(amendment.ReverterConfig{
			RepoRoot:     repoRoot,
			Emitter:      cfg.Adapter.AmendmentEventEmitter(),
			ReloadSignal: nil,
		})
	}

	svc := &Plan5OrchestratorService{
		cfg:          cfg,
		adapter:      cfg.Adapter,
		eventLog:     log,
		regression:   regression,
		divergence:   divergence,
		drift:        drift,
		prev:         prev,
		checkEngine:  checkEngine,
		reverter:     reverter,
		repoRoot:     repoRoot,
		decisionsDir: decisionsDir,
	}
	svc.autonomyMode.Store("")
	svc.adaptersClean.Store(true)
	return svc, nil
}

func (s *Plan5OrchestratorService) Session() (client.SessionInfo, error) {
	backgroundGoroutines := 0
	if snap, ok := s.worktreePoolSnapshot(); ok {
		backgroundGoroutines += snap.BackgroundGoroutines
	}
	backgroundGoroutines += s.backgroundSupervisorCount()
	return client.SessionInfo{
		State:                "idle",
		Mode:                 s.effectiveAutonomyMode(),
		StartedAt:            0,
		LastTransitionAt:     0,
		BackgroundGoroutines: backgroundGoroutines,
		RecentTransitions:    nil,
	}, nil
}

func (s *Plan5OrchestratorService) Pool() (client.PoolStatus, error) {
	if snap, ok := s.worktreePoolSnapshot(); ok {
		elasticInUse := snap.Total - snap.Floor
		if elasticInUse < 0 {
			elasticInUse = 0
		}
		return client.PoolStatus{
			Floor:         snap.Floor,
			Maximum:       snap.ElasticMax,
			CurrentLeased: snap.Leased,
			ElasticInUse:  elasticInUse,
			HealthOK:      !snap.Closed,
		}, nil
	}
	return client.PoolStatus{HealthOK: true}, nil
}

func (s *Plan5OrchestratorService) worktreePoolSnapshot() (worktreepool.Snapshot, bool) {
	s.mu.Lock()
	pool := s.worktreePool
	s.mu.Unlock()
	return worktreepool.SnapshotOf(pool)
}

func (s *Plan5OrchestratorService) backgroundSupervisorCount() int {
	s.mu.Lock()
	supervisor := s.backgroundSupervisor
	s.mu.Unlock()
	return supervisor.Count()
}

func (s *Plan5OrchestratorService) PrunePool() (int, error) {
	return 0, nil
}

var ErrDepthOverridesUnconfigured = errors.New(
	"depth overrides require Plan 8 persistence layer; the orchestrator " +
		"reads --depth from the build flag in the current release")

func (s *Plan5OrchestratorService) SetDepth(_ client.DepthOverride) error {
	return ErrDepthOverridesUnconfigured
}

func (s *Plan5OrchestratorService) Capture(req client.CaptureRequest) (client.CaptureResult, error) {
	if req.SessionID == "" {
		return client.CaptureResult{}, errors.New("capture: session_id is required")
	}
	if req.OutputPath == "" {
		return client.CaptureResult{}, errors.New("capture: output_path is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	recs, err := s.adapter.QueryRaw(ctx, req.SessionID, 0)
	if err != nil {
		s.adaptersClean.Store(false)
		return client.CaptureResult{}, fmt.Errorf("capture: query event log: %w", err)
	}
	f, err := os.Create(req.OutputPath)
	if err != nil {
		return client.CaptureResult{}, fmt.Errorf("capture: create output: %w", err)
	}
	enc := json.NewEncoder(f)
	var bytesWritten int64
	for _, r := range recs {

		row := map[string]any{
			"event_id":   r.EventID,
			"session_id": r.SessionID,
			"project_id": r.ProjectID,
			"event_type": int(r.EventType),
			"payload":    json.RawMessage(r.Payload),
			"timestamp":  r.Timestamp,
		}
		if err := enc.Encode(row); err != nil {
			_ = f.Close()
			return client.CaptureResult{}, fmt.Errorf("capture: encode record event_id=%d: %w", r.EventID, err)
		}
	}
	if err := f.Close(); err != nil {
		return client.CaptureResult{}, fmt.Errorf("capture: close output: %w", err)
	}
	if st, err := os.Stat(req.OutputPath); err == nil {
		bytesWritten = st.Size()
	}
	return client.CaptureResult{
		OutputPath:   req.OutputPath,
		EventCount:   len(recs),
		BytesWritten: bytesWritten,
	}, nil
}

func (s *Plan5OrchestratorService) Replay(req client.ReplayRequest) (client.ReplayResult, error) {
	if req.InputPath == "" {
		return client.ReplayResult{}, errors.New("replay: input_path is required")
	}
	f, err := os.Open(req.InputPath)
	if err != nil {
		return client.ReplayResult{}, fmt.Errorf("replay: open input: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var (
		count          int
		divergences    []string
		lastEventID    int64
		firstSessionID string
		seenAny        bool
	)
	for {
		var row struct {
			EventID   int64           `json:"event_id"`
			SessionID string          `json:"session_id"`
			EventType int             `json:"event_type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&row); err != nil {
			if errors.Is(err, errEOFEquivalent) || strings.Contains(err.Error(), "EOF") {
				break
			}
			divergences = append(divergences,
				fmt.Sprintf("decode error at record %d: %v", count, err))
			break
		}
		count++
		if !seenAny {
			firstSessionID = row.SessionID
			seenAny = true
		} else if row.SessionID != firstSessionID {
			divergences = append(divergences,
				fmt.Sprintf("session_id drift at record %d: got %q want %q",
					count, row.SessionID, firstSessionID))
		}
		if row.EventID <= lastEventID {
			divergences = append(divergences,
				fmt.Sprintf("event_id non-monotonic at record %d: got %d, last %d",
					count, row.EventID, lastEventID))
		}
		lastEventID = row.EventID
	}
	return client.ReplayResult{
		EventsReplayed: count,
		Divergences:    divergences,
		Deterministic:  len(divergences) == 0,
	}, nil
}

var errEOFEquivalent = errors.New("EOF")

func (s *Plan5OrchestratorService) AutonomyShow() (client.AutonomyShow, error) {
	doct := s.cfg.Doctrine
	flagModeStr := s.flagMode()

	res := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:  doct,
		BuildFlag: parseAutonomyMode(flagModeStr),
	})

	return client.AutonomyShow{
		EffectiveMode:    res.Mode.String(),
		ResolvedFrom:     resolvedFromLabel(res.Source),
		DoctrineMode:     doctrineDefaultLabel(doct),
		ZenswarmTOMLMode: "",
		FlagMode:         flagModeStr,
		CapaFirewallLock: res.Source == autonomy.SourceCapaFirewallGuard,
		CostDegradation: client.CostTierStatus{
			CurrentTier: "none",
			BudgetPct:   0.0,
		},
	}, nil
}

func (s *Plan5OrchestratorService) AutonomyCheck() (client.AutonomyCheckResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := s.checkEngine.RunCheck(ctx, autonomy.RunInput{
		Doctrine:    s.cfg.Doctrine,
		ProjectRoot: s.repoRoot,
	})
	if err != nil {
		return client.AutonomyCheckResult{}, fmt.Errorf("autonomy check: %w", err)
	}
	rows := make([]client.AutonomyCheckRow, 0, len(out.Results))
	var hard, soft, info int
	for _, r := range out.Results {
		if r.Status == autonomy.CheckFail {
			switch r.Tier {
			case autonomy.TierHard:
				hard++
			case autonomy.TierSoft:
				soft++
			case autonomy.TierInformational:
				info++
			}
		}
		rows = append(rows, client.AutonomyCheckRow{
			Name:     r.Name,
			Tier:     r.Tier.String(),
			Pass:     r.Status == autonomy.CheckPass,
			Detail:   r.Reason,
			Doctrine: s.cfg.Doctrine,
		})
	}
	return client.AutonomyCheckResult{
		OverallPass: out.Proceed,
		Rows:        rows,
		HardFailed:  hard,
		SoftFailed:  soft,
		InfoFailed:  info,
	}, nil
}

func (s *Plan5OrchestratorService) AutonomyMode(req client.AutonomyModeRequest) error {
	if req.Reset {
		s.autonomyMode.Store("")
		return nil
	}
	if req.Mode == "" {
		return errors.New("autonomy mode: mode required (use reset=true to clear)")
	}
	if _, err := autonomy.ParseMode(req.Mode); err != nil {
		return fmt.Errorf("autonomy mode: %w", err)
	}

	s.autonomyMode.Store(strings.ToLower(strings.TrimSpace(req.Mode)))
	return nil
}

func (s *Plan5OrchestratorService) flagMode() string {
	if v, ok := s.autonomyMode.Load().(string); ok {
		return v
	}
	return ""
}

func (s *Plan5OrchestratorService) effectiveAutonomyMode() string {
	res := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:  s.cfg.Doctrine,
		BuildFlag: parseAutonomyMode(s.flagMode()),
	})
	return res.Mode.String()
}

func parseAutonomyMode(s string) *autonomy.Mode {
	if s == "" {
		return nil
	}
	m, err := autonomy.ParseMode(s)
	if err != nil {
		return nil
	}
	return &m
}

func resolvedFromLabel(src autonomy.Source) string {
	switch src {
	case autonomy.SourceBuildFlag:
		return "flag"
	case autonomy.SourceProjectConfig:
		return "zenswarm_toml"
	case autonomy.SourceDoctrineDefault:
		return "doctrine"
	case autonomy.SourceCapaFirewallGuard:
		return "doctrine"
	}
	return "default"
}

func doctrineDefaultLabel(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "max-scope":
		return autonomy.ModeSemi.String()
	case "capa-firewall":
		return autonomy.ModeManual.String()
	}
	return autonomy.ModeManual.String()
}
