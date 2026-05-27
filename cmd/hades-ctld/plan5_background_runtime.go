// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	daemonorch "github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	core "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

const (
	plan5DaemonSessionID = "daemon-plan5-background"
	plan5DaemonProjectID = "daemon"
)

type plan5BackgroundRuntimeConfig struct {
	Service    *daemon.Plan5OrchestratorService
	Gate       core.GateAPI
	Budget     core.BudgetSnapshotReader
	Heartbeats core.HeartbeatProbe
}

func startPlan5BackgroundSupervisor(ctx context.Context, cfg plan5BackgroundRuntimeConfig) (*daemon.Plan5BackgroundSupervisor, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: parent context is nil", daemon.ErrPlan5BackgroundSupervisorInvalidConfig)
	}
	if cfg.Service == nil {
		return nil, fmt.Errorf("%w: service is nil", daemon.ErrPlan5BackgroundSupervisorInvalidConfig)
	}
	runners, err := plan5BackgroundRunners(cfg)
	if err != nil {
		return nil, err
	}
	supervisor := daemon.NewPlan5BackgroundSupervisor()
	if err := supervisor.Start(ctx, runners...); err != nil {
		return nil, err
	}
	cfg.Service.SetBackgroundSupervisor(supervisor)
	return supervisor, nil
}

func plan5BackgroundRunners(cfg plan5BackgroundRuntimeConfig) ([]daemon.Plan5BackgroundRunner, error) {
	svc := cfg.Service
	clk := svc.Plan5Clock()
	if clk == nil {
		clk = clock.Real{}
	}
	doctrine := svc.DoctrineName()
	if cfg.Gate == nil {
		cfg.Gate = newPlan5MemoryGate()
	}
	if cfg.Budget == nil {
		return nil, fmt.Errorf("daemon/plan5: budget snapshot reader is nil")
	}
	if cfg.Heartbeats == nil {
		cfg.Heartbeats = &plan5EventLogHeartbeatProbe{
			log:       svc.EventLog(),
			sessionID: plan5DaemonSessionID,
		}
	}

	appender := svc.EventLog()
	stateMachine := core.NewStateMachine(appender, clk, plan5DaemonSessionID, plan5DaemonProjectID)
	mainLoopInbox := newPlan5BackgroundInbox(256)
	dispatcher := newPlan5EventDispatcher(svc.EventLog(), eventlog.Filter{
		Types:     core.MainLoopEventTypes(),
		ProjectID: plan5DaemonProjectID,
	}, 256)
	dispatcher.Handle(func(ctx context.Context, rec eventlog.Record) error {
		return mainLoopInbox.Publish(ctx, rec)
	})

	mainLoop, err := core.NewMainLoop(core.MainLoopConfig{
		Subscription: mainLoopInbox,
		SM:           stateMachine,
		Clock:        clk,
		SessionID:    plan5DaemonSessionID,
		ProjectID:    plan5DaemonProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: main loop: %w", err)
	}

	confirmationHandler := core.NewConfirmationHandler(
		plan5ConfirmationPolicy(),
		stateMachine,
		appender,
		cfg.Gate,
		plan5DaemonSessionID,
		plan5DaemonProjectID,
	)
	confirmationWatcher, err := core.NewConfirmationWatcher(core.ConfirmationWatcherConfig{
		Handler: confirmationHandler,
		Clock:   clk,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: confirmation watcher: %w", err)
	}

	profile, err := core.BuiltinCostProfile(doctrine)
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: cost profile: %w", err)
	}
	costEngine, err := core.NewCostGatingEngine(core.CostGatingEngineConfig{
		Clock:     clk,
		EventLog:  appender,
		Budget:    cfg.Budget,
		Workers:   plan5ReadyWorkerSet{},
		Actuator:  plan5StateActuator{sm: stateMachine, confirmation: confirmationHandler},
		Profile:   profile,
		SessionID: plan5DaemonSessionID,
		ProjectID: plan5DaemonProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: cost-gating watcher: %w", err)
	}

	recoveryEngine, err := core.NewRecoveryEngine(core.RecoveryEngineConfig{
		Doctrine:  plan5Doctrine{name: doctrine},
		EventLog:  svc.EventLog(),
		TierChain: core.AdaptTierChain([]string{"primary", "fallback", "emergency"}, 0),
		Clock:     clk,
		ProjectID: plan5DaemonProjectID,
		SessionID: plan5DaemonSessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: recovery engine: %w", err)
	}
	heartbeat, err := core.NewHeartbeatMonitor(core.HeartbeatConfig{
		Engine: recoveryEngine,
		Probe:  cfg.Heartbeats,
		Clock:  clk,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: recovery heartbeat: %w", err)
	}

	hraCoordinator, err := hra.New(hra.Config{
		Clock:    clk,
		EventLog: svc.EventLog(),
		Context: plan5HRAContext{
			sessionID: plan5DaemonSessionID,
			projectID: plan5DaemonProjectID,
			doctrine:  doctrine,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: hra coordinator: %w", err)
	}
	hraSlots, err := plan5HRACadenceSlots(doctrine)
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: hra cadence slots: %w", err)
	}

	driftWatcher, err := safetynet.NewDriftWatcher(safetynet.DriftWatcherConfig{
		Validator: svc.DriftValidator(),
		Clock:     clk,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: drift watcher: %w", err)
	}

	regressionUpdater, err := safetynet.NewRegressionUpdater(safetynet.RegressionUpdaterConfig{
		Regression: svc.RegressionRecorder(),
		EventLog:   svc.EventLog(),
		Clock:      clk,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: regression updater: %w", err)
	}

	amendmentDetector, err := newPlan5AmendmentPatternDetector(svc, clk)
	if err != nil {
		return nil, fmt.Errorf("daemon/plan5: amendment pattern detector: %w", err)
	}

	return []daemon.Plan5BackgroundRunner{
		{Name: "orchestrator-main-loop", Slots: 0, Run: mainLoop.Run},
		{Name: "eventlog-subscriber-dispatcher", Slots: 1, Run: dispatcher.Run},
		{Name: "cost-gating-evaluator", Slots: 1, Run: costEngine.Run},
		{Name: "confirmation-watcher", Slots: 0, Run: confirmationWatcher.Run},
		{Name: "recovery-heartbeat-monitor", Slots: 1, Run: heartbeat.Run},
		{Name: "hra-cadence", Slots: hraSlots, Run: func(ctx context.Context) { _ = hraCoordinator.Run(ctx) }},
		{Name: "amendment-pattern-detector", Slots: 1, Run: amendmentDetector.Run},
		{Name: "safetynet-drift-detector", Slots: 1, Run: driftWatcher.Run},
		{Name: "safetynet-regression-updater", Slots: 1, Run: regressionUpdater.Run},
	}, nil
}

func plan5ConfirmationPolicy() *core.ConfirmationPolicy {
	return core.NewConfirmationPolicy(map[core.DecisionClass]core.Threshold{
		core.DecisionBudgetBreach:                  core.ThresholdHigh,
		core.DecisionSpecAmendmentProposal:         core.ThresholdHigh,
		core.DecisionInvariantViolation:            core.ThresholdHigh,
		core.DecisionArchitecturalReviewEscalation: core.ThresholdHigh,
		core.DecisionHighBlastRadius:               core.ThresholdHigh,
	}, false)
}

type plan5RecordHandler func(context.Context, eventlog.Record) error

type plan5EventDispatcher struct {
	log      *eventlog.Log
	filter   eventlog.Filter
	buffer   int
	handlers []plan5RecordHandler
}

func newPlan5EventDispatcher(log *eventlog.Log, filter eventlog.Filter, buffer int) *plan5EventDispatcher {
	if buffer <= 0 {
		buffer = eventlog.DefaultBufferSize
	}
	return &plan5EventDispatcher{log: log, filter: filter, buffer: buffer}
}

func (d *plan5EventDispatcher) Handle(h plan5RecordHandler) {
	if h != nil {
		d.handlers = append(d.handlers, h)
	}
}

func (d *plan5EventDispatcher) Run(ctx context.Context) {
	sub := d.log.Subscribe(d.filter, d.buffer)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			return
		case rec := <-sub.Events():
			for _, h := range d.handlers {
				_ = h(context.WithoutCancel(ctx), rec)
			}
		}
	}
}

type plan5BackgroundInbox struct {
	ch     chan eventlog.Record
	closed chan struct{}
	once   sync.Once
}

func newPlan5BackgroundInbox(buffer int) *plan5BackgroundInbox {
	if buffer <= 0 {
		buffer = eventlog.DefaultBufferSize
	}
	return &plan5BackgroundInbox{
		ch:     make(chan eventlog.Record, buffer),
		closed: make(chan struct{}),
	}
}

func (i *plan5BackgroundInbox) Events() <-chan eventlog.Record { return i.ch }
func (i *plan5BackgroundInbox) Done() <-chan struct{}          { return i.closed }
func (i *plan5BackgroundInbox) Close()                         { i.once.Do(func() { close(i.closed) }) }

func (i *plan5BackgroundInbox) Publish(ctx context.Context, rec eventlog.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.closed:
		return nil
	default:
	}
	select {
	case i.ch <- rec:
		return nil
	case <-i.closed:
		return nil
	default:
	}
	select {
	case <-i.ch:
	default:
	}
	select {
	case i.ch <- rec:
	case <-i.closed:
	default:
	}
	return nil
}

type plan5BudgetSnapshotReader struct {
	counters   *daemonorch.CostCounters
	repoRoot   string
	projectID  string
	doctrine   string
	paygActive bool
}

func (r plan5BudgetSnapshotReader) Snapshot(context.Context) (core.BudgetSnapshot, error) {
	if r.counters == nil {
		return core.BudgetSnapshot{}, fmt.Errorf("daemon/plan5 budget reader: cost counters not wired")
	}
	projectID := r.projectID
	if projectID == "" {
		projectID = plan5DaemonProjectID
	}
	doctrineName := r.doctrine
	if doctrineName == "" {
		doctrineName = "max-scope"
	}
	capUSD, err := r.dailyCapUSD(doctrineName)
	if err != nil {
		return core.BudgetSnapshot{}, err
	}
	var cumulative float64
	for _, key := range r.counters.AllKeys() {
		if key.Project != projectID {
			continue
		}
		cumulative += r.counters.ProjectProfileTierTotal(key.Project, key.Profile, key.Tier, 24*time.Hour)
	}
	return core.BudgetSnapshot{
		CumulativeUSD:   cumulative,
		DailyCapUSD:     capUSD,
		ProjectedEODUSD: cumulative,
		PAYGActive:      r.paygActive,
		ProjectID:       projectID,
		DoctrineName:    doctrineName,
	}, nil
}

func (r plan5BudgetSnapshotReader) dailyCapUSD(doctrineName string) (float64, error) {
	projectPath := plan5DoctrineProjectPath(r.repoRoot)
	resolved, err := (doctrine.Resolver{
		ChosenDoctrine: doctrineName,
		ProjectPath:    projectPath,
	}).Resolve()
	if err != nil {
		return 0, fmt.Errorf("daemon/plan5 budget reader: resolve doctrine cap: %w", err)
	}
	amount, currency, err := resolved.Schema.Budget.Caps.Project.Parse()
	if err != nil {
		return 0, fmt.Errorf("daemon/plan5 budget reader: parse project cap: %w", err)
	}
	if currency != "USD" {
		return 0, fmt.Errorf("daemon/plan5 budget reader: expected USD project cap, got %s", currency)
	}
	return amount, nil
}

func plan5DoctrineProjectPath(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}
	for _, name := range []string{"hadessystem.toml", ".hades-system.toml"} {
		path := filepath.Join(repoRoot, name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return ""
}

type plan5ReadyWorkerSet struct{}

func (plan5ReadyWorkerSet) WaitAtomicBoundary(context.Context) <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

type plan5StateActuator struct {
	sm           core.StateMachineAPI
	confirmation *core.ConfirmationHandler
}

func (a plan5StateActuator) DropAtDepth(ctx context.Context, _ int) error {
	return a.transitionIfLegal(ctx, core.StateDegradedTier, "cost-gating:drop-depth")
}

func (a plan5StateActuator) SetTier(ctx context.Context, _ int) error {
	return a.transitionIfLegal(ctx, core.StateDegradedTier, "cost-gating:set-tier")
}

func (a plan5StateActuator) SetParallelism(ctx context.Context, _, _ int) error {
	return a.transitionIfLegal(ctx, core.StateDegradedTier, "cost-gating:parallelism")
}

func (a plan5StateActuator) HardPause(ctx context.Context, reason string) error {
	return a.transitionIfLegal(ctx, core.StateHardPaused, reason)
}

func (a plan5StateActuator) EmergencyOnlyTier(ctx context.Context) error {
	return a.transitionIfLegal(ctx, core.StateEmergencyTier, "cost-gating:emergency-only")
}

func (a plan5StateActuator) EscalateL4(ctx context.Context, payload map[string]any) error {
	if !core.IsLegal(a.sm.Current(), core.StateWaitingForConfirmation) {
		return nil
	}
	summary := "cost-gating L4 escalation"
	if raw, ok := payload["reason"].(string); ok && raw != "" {
		summary = raw
	}
	_, err := a.confirmation.RequestConfirmation(ctx, core.RequestConfirmationInput{
		Class:   core.DecisionBudgetBreach,
		Summary: summary,
	})
	return err
}

func (a plan5StateActuator) WaitForConfirmation(ctx context.Context, decisionID string) error {
	if !core.IsLegal(a.sm.Current(), core.StateWaitingForConfirmation) {
		return nil
	}
	_, err := a.confirmation.RequestConfirmation(ctx, core.RequestConfirmationInput{
		Class:   core.DecisionBudgetBreach,
		Summary: "cost-gating confirmation required: " + decisionID,
	})
	return err
}

func (a plan5StateActuator) Waiting(ctx context.Context, reason string) error {
	return a.transitionIfLegal(ctx, core.StateWaitingForConfirmation, reason)
}

func (a plan5StateActuator) RestoreDefaults(ctx context.Context) error {
	switch a.sm.Current() {
	case core.StateDegradedTier, core.StateHardPaused:
		return a.transitionIfLegal(ctx, core.StateRunning, "cost-gating:restore-defaults")
	default:
		return nil
	}
}

func (a plan5StateActuator) transitionIfLegal(ctx context.Context, to core.State, reason string) error {
	from := a.sm.Current()
	if from == to {
		return nil
	}
	if !core.IsLegal(from, to) {
		return nil
	}
	return a.sm.Transition(ctx, to, reason)
}

type plan5EventLogHeartbeatProbe struct {
	log       *eventlog.Log
	sessionID string
	mu        sync.Mutex
	lastSeen  int64
	beats     map[string]time.Time
}

func (p *plan5EventLogHeartbeatProbe) LastBeats(ctx context.Context) (map[string]time.Time, error) {
	if p == nil {
		return nil, fmt.Errorf("daemon/plan5 heartbeat probe: nil probe")
	}
	if p.log == nil {
		return nil, fmt.Errorf("daemon/plan5 heartbeat probe: event log not wired")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	sessionID := p.sessionID
	if sessionID == "" {
		sessionID = plan5DaemonSessionID
	}
	records, err := p.log.Query(ctx, sessionID, p.lastSeen)
	if err != nil {
		return nil, err
	}
	if p.beats == nil {
		p.beats = map[string]time.Time{}
	}
	for _, rec := range records {
		if rec.EventID > p.lastSeen {
			p.lastSeen = rec.EventID
		}
		switch rec.EventType {
		case eventlog.EvtWorkerDispatched, eventlog.EvtWorkerCheckpoint:
			payload := map[string]any{}
			if len(rec.Payload) > 0 {
				if err := json.Unmarshal(rec.Payload, &payload); err != nil {
					continue
				}
			}
			workerID, _ := payload["worker_id"].(string)
			if workerID == "" {
				continue
			}
			p.beats[workerID] = time.Unix(0, rec.Timestamp)
		}
	}
	beats := make(map[string]time.Time, len(p.beats))
	for workerID, beat := range p.beats {
		beats[workerID] = beat
	}
	return beats, nil
}

func plan5HRACadenceSlots(doctrineName string) (int, error) {
	cadence, err := hra.CadenceFor(doctrineName)
	if err != nil {
		return 0, err
	}
	slots := 0
	if cadence.Tactical > 0 {
		slots++
	}
	if cadence.Strategic > 0 {
		slots++
	}
	if cadence.Architectural > 0 {
		slots++
	}
	return slots, nil
}

type plan5AmendmentPatternDetector struct {
	log      *eventlog.Log
	proposer *amendment.AmendmentProposer
}

func newPlan5AmendmentPatternDetector(svc *daemon.Plan5OrchestratorService, clk clock.Clock) (*plan5AmendmentPatternDetector, error) {
	repoRoot := svc.RepoRoot()
	if repoRoot == "" {
		return nil, fmt.Errorf("repo root is empty")
	}
	emitter := plan5AmendmentEmitter{
		log:       svc.EventLog(),
		sessionID: plan5DaemonSessionID,
		projectID: plan5DaemonProjectID,
	}
	proposer := amendment.NewProposer(amendment.ProposerConfig{
		DecisionsDir: filepath.Join(repoRoot, "docs", "decisions"),
		Doctrine:     svc.DoctrineName(),
		Emitter:      emitter,
		Drafter:      plan5EvidenceDrafter{},
		Allocator:    &amendment.RangeAllocatorReal{Emitter: emitter},
		Cooldown:     amendment.NewCooldownRegistry(clk),
		Clock:        clk,
	})
	return &plan5AmendmentPatternDetector{log: svc.EventLog(), proposer: proposer}, nil
}

func (d *plan5AmendmentPatternDetector) Run(ctx context.Context) {
	sub := d.log.Subscribe(eventlog.Filter{
		Types: []eventlog.EventType{
			eventlog.EvtOperatorOverrideApplied,
			eventlog.EvtBudgetDegradationApplied,
			eventlog.EvtEscalationDecision,
		},
	}, eventlog.DefaultBufferSize)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			return
		case rec := <-sub.Events():
			ev, ok := plan5RecordToEvent(rec)
			if !ok {
				continue
			}
			plan5NormalizeAmendmentPayload(&ev)
			_ = d.proposer.OnEvent(context.WithoutCancel(ctx), ev)
		}
	}
}

type plan5AmendmentEmitter struct {
	log       *eventlog.Log
	sessionID string
	projectID string
}

func (e plan5AmendmentEmitter) Append(ctx context.Context, ev eventlog.Event) error {
	if ev.SessionID == "" {
		ev.SessionID = e.sessionID
	}
	if ev.ProjectID == "" {
		ev.ProjectID = e.projectID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	_, err := e.log.Append(ctx, ev)
	return err
}

type plan5EvidenceDrafter struct{}

func (plan5EvidenceDrafter) Draft(_ context.Context, ev amendment.Evidence) (amendment.ADRBody, error) {
	title := fmt.Sprintf("doctrine amendment for %s %s", ev.Doctrine, ev.TriggerClass)
	body := fmt.Sprintf(`# %s

## Trigger

- Doctrine: %s
- Pattern: %s
- Project: %s
- Window hours: %d
- Count: %d
- Threshold: %d

## Evidence

The daemon observed a repeated %s pattern above the doctrine threshold.
This proposal is generated from durable event-log evidence and is pending
operator review before any doctrine change is applied.

## Recommendation

Review the sampled events, decide whether the doctrine should encode this
repeated operational override/degradation/escalation, and either apply a
tighter rule or deny the proposal to arm cooldown.
`, title, ev.Doctrine, ev.Pattern, ev.ProjectID, ev.WindowHours, ev.Count, ev.Threshold, ev.TriggerClass)
	return amendment.ADRBody{Title: title, Markdown: body}, nil
}

func plan5RecordToEvent(rec eventlog.Record) (eventlog.Event, bool) {
	payload := map[string]any{}
	if len(rec.Payload) > 0 {
		if err := json.Unmarshal(rec.Payload, &payload); err != nil {
			return eventlog.Event{}, false
		}
	}
	return eventlog.Event{
		Type:        rec.EventType,
		SessionID:   rec.SessionID,
		ProjectID:   rec.ProjectID,
		Timestamp:   time.Unix(0, rec.Timestamp),
		Payload:     payload,
		CausalChain: rec.CausalChain,
	}, true
}

func plan5NormalizeAmendmentPayload(ev *eventlog.Event) {
	if ev.Payload == nil {
		ev.Payload = map[string]any{}
	}
	if ev.Payload["project_id"] == nil && ev.ProjectID != "" {
		ev.Payload["project_id"] = ev.ProjectID
	}
	switch ev.Type {
	case eventlog.EvtOperatorOverrideApplied:
		if ev.Payload["override_class"] == nil {
			if kind, _ := ev.Payload["override_kind"].(string); kind != "" {
				ev.Payload["override_class"] = kind
			} else {
				ev.Payload["override_class"] = "operator_override"
			}
		}
	case eventlog.EvtEscalationDecision:
		if ev.Payload["destination"] == nil {
			if to, _ := ev.Payload["to_layer"].(string); to != "" {
				ev.Payload["destination"] = to
			}
		}
	case eventlog.EvtBudgetDegradationApplied:
		if ev.Payload["severity"] == nil {
			action, _ := ev.Payload["action"].(string)
			if severity := plan5BudgetActionSeverity(action); severity != "" {
				ev.Payload["severity"] = severity
			}
		}
	}
}

func plan5BudgetActionSeverity(action string) string {
	switch action {
	case "drop_l3_strategic", "tier_degrade_l2", "tier_degrade_l1_l2", "reduce_parallelism", "escalate_l4":
		return "medium"
	case "emergency_only_tier", "waiting_for_confirmation", "waiting":
		return "hard"
	case "hard_pause":
		return "emergency"
	default:
		return ""
	}
}

type plan5Doctrine struct {
	name string
}

func (d plan5Doctrine) Name() string { return d.name }

func (d plan5Doctrine) TransientLLMRetries() int {
	switch d.name {
	case "max-scope":
		return 3
	case "capa-firewall":
		return 0
	default:
		return 1
	}
}

func (d plan5Doctrine) TransientInfraRetries() int {
	switch d.name {
	case "max-scope":
		return 2
	case "capa-firewall":
		return 0
	default:
		return 1
	}
}

func (d plan5Doctrine) PermanentAfterNRetries() int {
	if d.name == "capa-firewall" {
		return 1
	}
	return 3
}

func (d plan5Doctrine) OnExhaustAction(class core.FailureClass) core.RecoveryAction {
	switch d.name {
	case "max-scope":
		return core.RecoveryActionEscalateL4
	case "capa-firewall":
		return core.RecoveryActionWaitForConfirmation
	default:
		if class == core.FailurePermanentInfra {
			return core.RecoveryActionHardPause
		}
		return core.RecoveryActionSkipTask
	}
}

func (d plan5Doctrine) TierFallbackPolicy() core.TierFallbackPolicy {
	switch d.name {
	case "max-scope":
		return core.TierFallbackFullChain
	case "capa-firewall":
		return core.TierFallbackNone
	default:
		return core.TierFallbackPartial
	}
}

type plan5HRAContext struct {
	sessionID string
	projectID string
	doctrine  string
}

func (c plan5HRAContext) SessionID() string { return c.sessionID }
func (c plan5HRAContext) ProjectID() string { return c.projectID }
func (c plan5HRAContext) Doctrine() string  { return c.doctrine }

type plan5MemoryGate struct {
	mu    sync.Mutex
	state gate.State
}

func newPlan5MemoryGate() *plan5MemoryGate {
	return &plan5MemoryGate{state: gate.StateRunning}
}

func (g *plan5MemoryGate) Pause(_ context.Context, mode gate.PauseMode, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch mode {
	case gate.PauseDescriptive:
		g.state = gate.StatePausedDescriptive
	case gate.PauseQuiet:
		g.state = gate.StatePausedQuiet
	case gate.PauseAfterApply:
		g.state = gate.StatePausedAfterApply
	default:
		return fmt.Errorf("daemon/plan5 memory gate: unknown pause mode %d", mode)
	}
	return nil
}

func (g *plan5MemoryGate) Resume(context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state = gate.StateRunning
	return nil
}

func (g *plan5MemoryGate) State() gate.State {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}
