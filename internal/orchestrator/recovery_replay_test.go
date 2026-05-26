package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type corruptableEmitter struct {
	mu     sync.Mutex
	rows   []eventlog.Record
	nextID int64
}

func (c *corruptableEmitter) EmitRaw(_ context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	c.rows = append(c.rows, eventlog.Record{
		EventID:   id,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: eventlog.EventType(eventType),
		Payload:   append([]byte(nil), payload...),
		Timestamp: ts,
	})
	return id, nil
}

func (c *corruptableEmitter) QueryRaw(_ context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]eventlog.Record, 0, len(c.rows))
	for _, r := range c.rows {
		if r.SessionID != sessionID {
			continue
		}
		if r.EventID <= since {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (c *corruptableEmitter) InjectCorrupt(sessionID, projectID string, et eventlog.EventType, ts int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.rows = append(c.rows, eventlog.Record{
		EventID:   c.nextID,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: et,
		Payload:   []byte("not-json-payload"),
		Timestamp: ts,
	})
}

type replayFixture struct {
	t         *testing.T
	ctx       context.Context
	eng       *RecoveryEngine
	evlog     *eventlog.Log
	emitter   *corruptableEmitter
	sessionID string
	projectID string
	clk       *clock.Fake
}

func newReplayFixture(t *testing.T) *replayFixture {
	t.Helper()
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	emitter := &corruptableEmitter{}
	evlog := eventlog.New(emitter, fc)
	doc := loadFakeDoctrine(t, "max-scope")
	tiers, stopBefore := canonicalTierSlice("max-scope")
	tier := AdaptTierChain(tiers, stopBefore)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine:  doc,
		EventLog:  evlog,
		TierChain: tier,
		Clock:     fc,
		ProjectID: "p-replay",
		SessionID: "s-replay",
	})
	if err != nil {
		t.Fatalf("NewRecoveryEngine: %v", err)
	}
	return &replayFixture{
		t:         t,
		ctx:       context.Background(),
		eng:       eng,
		evlog:     evlog,
		emitter:   emitter,
		sessionID: "s-replay",
		projectID: "p-replay",
		clk:       fc,
	}
}

func (f *replayFixture) records() []eventlog.Record {
	f.t.Helper()
	out, err := f.evlog.Query(f.ctx, f.sessionID, 0)
	if err != nil {
		f.t.Fatalf("Query: %v", err)
	}
	return out
}

func (f *replayFixture) appendDispatched(workerID, taskID, tier string, offsetSec int) time.Time {
	f.t.Helper()
	ts := f.clk.Now().Add(time.Duration(offsetSec) * time.Second)
	if _, err := f.evlog.Append(f.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: ts,
		Payload: map[string]any{
			"worker_id": workerID,
			"task_id":   taskID,
			"tier":      tier,
		},
	}); err != nil {
		f.t.Fatalf("Append dispatched: %v", err)
	}
	return ts
}

func (f *replayFixture) appendCheckpoint(workerID, taskID string, offsetSec int) time.Time {
	f.t.Helper()
	ts := f.clk.Now().Add(time.Duration(offsetSec) * time.Second)
	payload := map[string]any{
		"worker_id":      workerID,
		"checkpoint_sha": "sha-" + workerID,
		"summary":        "step done",
	}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	if _, err := f.evlog.Append(f.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: ts,
		Payload:   payload,
	}); err != nil {
		f.t.Fatalf("Append checkpoint: %v", err)
	}
	return ts
}

func (f *replayFixture) appendDeath(workerID, taskID string, offsetSec int) time.Time {
	f.t.Helper()
	ts := f.clk.Now().Add(time.Duration(offsetSec) * time.Second)
	payload := map[string]any{
		"worker_id":   workerID,
		"class":       "TRANSIENT_INFRA",
		"reason":      "heartbeat_timeout",
		"retry_count": 0,
	}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	if _, err := f.evlog.Append(f.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDeath,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: ts,
		Payload:   payload,
	}); err != nil {
		f.t.Fatalf("Append death: %v", err)
	}
	return ts
}

func (f *replayFixture) appendRedispatched(workerID, taskID string, offsetSec int) time.Time {
	f.t.Helper()
	ts := f.clk.Now().Add(time.Duration(offsetSec) * time.Second)
	if _, err := f.evlog.Append(f.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerRedispatched,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: ts,
		Payload: map[string]any{
			"task_id":        taskID,
			"worker_id":      workerID,
			"class":          "TRANSIENT_INFRA",
			"action":         "redispatch_same_tier",
			"new_tier_index": 0,
			"retry_count":    1,
			"reason":         "within_budget",
		},
	}); err != nil {
		f.t.Fatalf("Append redispatched: %v", err)
	}
	return ts
}

func recordsByType(records []eventlog.Record, et eventlog.EventType) int {
	n := 0
	for _, r := range records {
		if r.EventType == et {
			n++
		}
	}
	return n
}

func TestReconstructInFlight_RedispatchesUnmatchedDispatches(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "t1", 1)

	fx.appendDispatched("w2", "t2", "t1_bypass", 2)

	fx.appendDispatched("w3", "t3", "t2_anthropic_paygo", 3)
	fx.appendDeath("w3", "t3", 4)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if plan.HardPause {
		t.Fatalf("HardPause=true unexpected: %+v", plan)
	}
	if got := len(plan.Redispatches); got != 2 {
		t.Fatalf("Redispatches len=%d want 2; plan=%+v", got, plan)
	}
	wantTasks := map[string]string{
		"t2": "unmatched_dispatch",
		"t3": "worker_death_unrecovered",
	}
	wantTier := map[string]string{
		"t2": "t1_bypass",
		"t3": "t2_anthropic_paygo",
	}
	for _, rd := range plan.Redispatches {
		cause, ok := wantTasks[rd.TaskID]
		if !ok {
			t.Errorf("unexpected task in redispatches: %+v", rd)
			continue
		}
		if rd.Cause != cause {
			t.Errorf("task %s cause=%q want %q", rd.TaskID, rd.Cause, cause)
		}
		if rd.Tier != wantTier[rd.TaskID] {
			t.Errorf("task %s tier=%q want %q", rd.TaskID, rd.Tier, wantTier[rd.TaskID])
		}
	}

	recs := fx.records()
	if got := recordsByType(recs, eventlog.EvtOrchestratorRestoreFromReplay); got != 1 {
		t.Fatalf("OrchestratorRestoreFromReplay count=%d want 1", got)
	}

	for _, r := range recs {
		if r.EventType != eventlog.EvtOrchestratorRestoreFromReplay {
			continue
		}
		dec, err := eventlog.Decode(r.EventType, r.Payload)
		if err != nil {
			t.Fatalf("decode restore-from-replay: %v", err)
		}
		evt, ok := dec.(eventlog.OrchestratorRestoreFromReplay)
		if !ok {
			t.Fatalf("decode wrong type %T", dec)
		}
		if evt.RecoveredTaskCount != 2 {
			t.Errorf("RecoveredTaskCount=%d want 2", evt.RecoveredTaskCount)
		}
		if evt.HardPause {
			t.Errorf("HardPause=true unexpected on success path")
		}
		if evt.SessionID != fx.sessionID {
			t.Errorf("SessionID=%q want %q", evt.SessionID, fx.sessionID)
		}
	}
}

func TestReconstructInFlight_NoInFlight_NoOp(t *testing.T) {
	fx := newReplayFixture(t)
	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if plan.HardPause {
		t.Fatalf("HardPause=true unexpected on empty session")
	}
	if len(plan.Redispatches) != 0 {
		t.Fatalf("Redispatches=%d want 0 (empty session)", len(plan.Redispatches))
	}
	if plan.Reason != "no in-flight tasks" {
		t.Errorf("Reason=%q want %q", plan.Reason, "no in-flight tasks")
	}
	recs := fx.records()
	if got := recordsByType(recs, eventlog.EvtOrchestratorRestoreFromReplay); got != 1 {
		t.Fatalf("OrchestratorRestoreFromReplay count=%d want 1 (empty session still emits)", got)
	}
}

func TestReconstructInFlight_CompletedSession_AllMatched(t *testing.T) {
	fx := newReplayFixture(t)

	for i := 0; i < 5; i++ {
		taskID := "task-" + itoaSimple(i)
		workerID := "worker-" + itoaSimple(i)
		fx.appendDispatched(workerID, taskID, "t1_bypass", i*2)
		fx.appendCheckpoint(workerID, taskID, i*2+1)
	}
	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (all paired); plan=%+v", got, plan)
	}
}

func TestReconstructInFlight_DeathWithRecoveryRedispatched(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendDeath("w1", "t1", 1)
	fx.appendRedispatched("w1", "t1", 2)
	fx.appendCheckpoint("w1", "t1", 3)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (death recovered pre-crash); plan=%+v", got, plan)
	}
}

func TestReconstructInFlight_BoundedByCorruption_Inv095(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w1", "t-good", "t1_bypass", 0)

	baseTs := fx.clk.Now().Add(10 * time.Second).UnixNano()
	for i := 0; i < 5; i++ {
		fx.emitter.InjectCorrupt(fx.sessionID, fx.projectID, eventlog.EvtWorkerCheckpoint, baseTs+int64(i))
	}
	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight (5 corrupt): %v", err)
	}
	if plan.HardPause {
		t.Fatalf("≤5 corruption: HardPause=true unexpected (boundary should be inclusive)")
	}

	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1 (single unmatched dispatch survives 5 corruption); plan=%+v", got, plan)
	}

	fx.emitter.InjectCorrupt(fx.sessionID, fx.projectID, eventlog.EvtWorkerCheckpoint, baseTs+10)
	plan2, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight (6 corrupt): %v", err)
	}
	if !plan2.HardPause {
		t.Fatalf("6 corruption: HardPause=false; expected HardPause=true per inv-zen-095")
	}
	if got := len(plan2.Redispatches); got != 0 {
		t.Errorf("Redispatches=%d want 0 on HardPause path; plan=%+v", got, plan2)
	}
	if plan2.Reason == "" {
		t.Errorf("Reason empty on HardPause path; want explanatory string")
	}

	recs := fx.records()
	var sawHardPauseAudit bool
	for _, r := range recs {
		if r.EventType != eventlog.EvtOrchestratorRestoreFromReplay {
			continue
		}
		dec, err := eventlog.Decode(r.EventType, r.Payload)
		if err != nil {
			t.Fatalf("decode restore-from-replay: %v", err)
		}
		evt := dec.(eventlog.OrchestratorRestoreFromReplay)
		if evt.HardPause {
			sawHardPauseAudit = true
			if evt.EventsCorrupted < 6 {
				t.Errorf("EventsCorrupted=%d want >=6 on HardPause audit", evt.EventsCorrupted)
			}
		}
	}
	if !sawHardPauseAudit {
		t.Errorf("no OrchestratorRestoreFromReplay audit row with HardPause=true found")
	}
}

func TestReconstructInFlight_Idempotent(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "t1", 1)
	fx.appendDispatched("w2", "t2", "t1_bypass", 2)
	fx.appendDispatched("w3", "t3", "t2_anthropic_paygo", 3)
	fx.appendDeath("w3", "t3", 4)

	plan1, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("first ReconstructInFlight: %v", err)
	}
	plan2, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("second ReconstructInFlight: %v", err)
	}

	if len(plan1.Redispatches) != len(plan2.Redispatches) {
		t.Fatalf("idempotency violated: len plan1=%d plan2=%d", len(plan1.Redispatches), len(plan2.Redispatches))
	}

	for i := range plan1.Redispatches {
		if plan1.Redispatches[i] != plan2.Redispatches[i] {
			t.Errorf("idempotency violated at i=%d: plan1=%+v plan2=%+v", i, plan1.Redispatches[i], plan2.Redispatches[i])
		}
	}
	if plan1.HardPause != plan2.HardPause {
		t.Errorf("HardPause drift: plan1=%v plan2=%v", plan1.HardPause, plan2.HardPause)
	}

	recs := fx.records()
	if got := recordsByType(recs, eventlog.EvtOrchestratorRestoreFromReplay); got != 2 {
		t.Fatalf("OrchestratorRestoreFromReplay count=%d want 2 (one per ReconstructInFlight call)", got)
	}
}

func TestReconstructInFlight_AuditSurvivesCancelledCtx(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)

	ctx, cancel := context.WithCancel(fx.ctx)

	cancel()

	plan, err := fx.eng.ReconstructInFlight(ctx, fx.sessionID)

	if err == nil {
		t.Fatalf("ReconstructInFlight with cancelled ctx: err=nil; expected ctx error from Query short-circuit")
	}
	if plan.SessionID != fx.sessionID {
		t.Errorf("plan.SessionID=%q want %q (zero-value plan should still carry SessionID)", plan.SessionID, fx.sessionID)
	}
}

func TestReconstructInFlight_AuditEmittedThroughWithoutCancel(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)

	ctx, cancel := context.WithCancel(fx.ctx)
	defer cancel()

	plan, err := fx.eng.ReconstructInFlight(ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1", got)
	}

	cancel()
	recs := fx.records()
	if got := recordsByType(recs, eventlog.EvtOrchestratorRestoreFromReplay); got != 1 {
		t.Fatalf("OrchestratorRestoreFromReplay count=%d want 1 (audit must have landed before cancel)", got)
	}
}

func TestReconstructInFlight_OtherSessionIgnored(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w-mine", "task-mine", "t1_bypass", 0)

	otherSess := "OTHER"
	otherProj := "p-other"
	otherTs := fx.clk.Now().Add(time.Second)
	if _, err := fx.evlog.Append(fx.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: otherSess,
		ProjectID: otherProj,
		Timestamp: otherTs,
		Payload: map[string]any{
			"worker_id": "w-other",
			"task_id":   "task-other",
			"tier":      "t1_bypass",
		},
	}); err != nil {
		t.Fatalf("seed other session: %v", err)
	}

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1 (only fixture session task)", got)
	}
	if plan.Redispatches[0].TaskID != "task-mine" {
		t.Errorf("redispatched task=%q want %q", plan.Redispatches[0].TaskID, "task-mine")
	}
}

func TestReconstructInFlight_PreE6CheckpointFallsBackToWorkerLookup(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "", 1)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (worker→last-dispatch fallback should pair the checkpoint); plan=%+v", got, plan)
	}
}

func TestReconstructInFlight_DeathWithoutTaskIDFallsBackToWorkerLookup(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)

	deathTs := fx.clk.Now().Add(2 * time.Second)
	if _, err := fx.evlog.Append(fx.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDeath,
		SessionID: fx.sessionID,
		ProjectID: fx.projectID,
		Timestamp: deathTs,
		Payload: map[string]any{
			"worker_id":   "w1",
			"cause":       "panic",
			"retry_count": 0,
		},
	}); err != nil {
		t.Fatalf("Append legacy death: %v", err)
	}

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1 (death without task_id should fall back to worker→last-dispatch)", got)
	}
	if plan.Redispatches[0].TaskID != "t1" {
		t.Errorf("TaskID=%q want %q", plan.Redispatches[0].TaskID, "t1")
	}
	if plan.Redispatches[0].Cause != "worker_death_unrecovered" {
		t.Errorf("Cause=%q want %q", plan.Redispatches[0].Cause, "worker_death_unrecovered")
	}
}

func TestReconstructInFlight_DeathWithoutResolvableTaskIsSkipped(t *testing.T) {
	fx := newReplayFixture(t)

	deathTs := fx.clk.Now()
	if _, err := fx.evlog.Append(fx.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDeath,
		SessionID: fx.sessionID,
		ProjectID: fx.projectID,
		Timestamp: deathTs,
		Payload: map[string]any{
			"worker_id":   "w-orphan",
			"class":       "TRANSIENT_INFRA",
			"reason":      "heartbeat_timeout",
			"retry_count": 0,
		},
	}); err != nil {
		t.Fatalf("Append death: %v", err)
	}
	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (orphan death must not redispatch)", got)
	}
}

func TestReconstructInFlight_DeduplicatesAcrossPasses(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendDeath("w1", "t1", 1)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1 (dedup across passes); plan=%+v", got, plan)
	}

	if plan.Redispatches[0].Cause != "worker_death_unrecovered" {
		t.Errorf("Cause=%q want %q (death-first dedup)", plan.Redispatches[0].Cause, "worker_death_unrecovered")
	}
}

func TestReconstructInFlight_OrphanCheckpointSkipped(t *testing.T) {
	fx := newReplayFixture(t)

	if _, err := fx.evlog.Append(fx.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: fx.sessionID,
		ProjectID: fx.projectID,
		Timestamp: fx.clk.Now(),
		Payload: map[string]any{
			"worker_id":      "w-orphan",
			"checkpoint_sha": "abc",
			"summary":        "step",
		},
	}); err != nil {
		t.Fatalf("Append orphan checkpoint: %v", err)
	}

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (orphan checkpoint must not surface a task)", got)
	}
}

func TestReconstructInFlight_PostDeathCheckpointResolvesTask(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendDeath("w1", "t1", 1)
	fx.appendCheckpoint("w1", "t1", 2)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 0 {
		t.Fatalf("Redispatches=%d want 0 (post-death checkpoint resolves task); plan=%+v", got, plan)
	}
}

func TestReconstructInFlight_DoubleDeathSameTaskDeduped(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "t1", "t1_bypass", 0)
	fx.appendDeath("w1", "t1", 1)
	fx.appendDeath("w1", "t1", 2)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if got := len(plan.Redispatches); got != 1 {
		t.Fatalf("Redispatches=%d want 1 (double death must dedup)", got)
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_True_WhenCheckpointExists(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "tdone", 1)

	if !fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned false; expected true (checkpoint present for tdone)")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_False_WhenNoCheckpoint(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)

	if fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned true; expected false (no checkpoint present)")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_RedispatchSkipsAlreadyDoneTask(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "tdone", 1)

	plan, err := fx.eng.ReconstructInFlight(fx.ctx, fx.sessionID)
	if err != nil {
		t.Fatalf("ReconstructInFlight: %v", err)
	}
	if len(plan.Redispatches) != 0 {
		t.Fatalf("Redispatches=%d want 0 (task already checkpointed); plan=%+v", len(plan.Redispatches), plan)
	}
	if !fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned false; expected true (checkpoint present)")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_BackCompatWorkerLookup(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)

	fx.appendCheckpoint("w1", "", 1)

	if !fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned false; expected true via worker→dispatch fallback")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_EmptyTaskID(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "tdone", 1)

	if fx.eng.IsTaskAlreadyComplete(fx.ctx, "") {
		t.Fatal("IsTaskAlreadyComplete returned true for empty taskID; expected false (defensive guard)")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_OtherSessionIgnored(t *testing.T) {
	fx := newReplayFixture(t)

	otherTs := fx.clk.Now().Add(time.Second)
	if _, err := fx.evlog.Append(fx.ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: "OTHER",
		ProjectID: "p-other",
		Timestamp: otherTs,
		Payload: map[string]any{
			"worker_id":      "w-other",
			"task_id":        "tdone",
			"checkpoint_sha": "sha-other",
			"summary":        "done",
		},
	}); err != nil {
		t.Fatalf("seed other-session checkpoint: %v", err)
	}

	if fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned true for cross-session checkpoint; must be false")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_QueryError(t *testing.T) {
	fx := newReplayFixture(t)

	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)
	fx.appendCheckpoint("w1", "tdone", 1)

	ctx, cancel := context.WithCancel(fx.ctx)
	cancel()

	if fx.eng.IsTaskAlreadyComplete(ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned true despite Query error (cancelled ctx); expected false (fail-safe)")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_CorruptDispatchRow(t *testing.T) {
	fx := newReplayFixture(t)

	baseTs := fx.clk.Now().UnixNano()
	fx.emitter.InjectCorrupt(fx.sessionID, fx.projectID, eventlog.EvtWorkerDispatched, baseTs)

	fx.appendDispatched("w1", "tgood", "t1_bypass", 1)
	fx.appendCheckpoint("w1", "tgood", 2)

	if !fx.eng.IsTaskAlreadyComplete(fx.ctx, "tgood") {
		t.Fatal("IsTaskAlreadyComplete returned false; corrupt dispatch row should not affect valid task lookup")
	}

	if fx.eng.IsTaskAlreadyComplete(fx.ctx, "tbad") {
		t.Fatal("IsTaskAlreadyComplete returned true for tbad; corrupt row must not invent a task")
	}
}

func TestRecoveryEngine_IsTaskAlreadyComplete_CorruptCheckpointRow(t *testing.T) {
	fx := newReplayFixture(t)
	fx.appendDispatched("w1", "tdone", "t1_bypass", 0)

	baseTs := fx.clk.Now().Add(time.Second).UnixNano()
	fx.emitter.InjectCorrupt(fx.sessionID, fx.projectID, eventlog.EvtWorkerCheckpoint, baseTs)

	if fx.eng.IsTaskAlreadyComplete(fx.ctx, "tdone") {
		t.Fatal("IsTaskAlreadyComplete returned true; corrupt checkpoint row must not count as completion")
	}
}

func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
