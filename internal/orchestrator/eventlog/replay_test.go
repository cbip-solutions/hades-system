package eventlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

func TestReplayHappyPath(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStarted{
		SessionID: "s1", ProjectID: "p1", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("append OrchestratorStarted: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStateTransition{
		From: "IDLE", To: "INITIALIZING", Reason: "boot",
	}); err != nil {
		t.Fatalf("append transition: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{
		WorkerID: "w-1", TaskID: "t-1", Tier: "t1_bypass",
	}); err != nil {
		t.Fatalf("append w-1 dispatched: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{
		WorkerID: "w-2", TaskID: "t-2", Tier: "t1_bypass",
	}); err != nil {
		t.Fatalf("append w-2 dispatched: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerCheckpoint{
		WorkerID: "w-1", CheckpointSHA: "abc", Summary: "ok",
	}); err != nil {
		t.Fatalf("append w-1 checkpoint: %v", err)
	}

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if st.SessionID != "s1" {
		t.Errorf("SessionID = %q want s1", st.SessionID)
	}
	if st.EventsReplayed != 5 {
		t.Errorf("EventsReplayed = %d want 5", st.EventsReplayed)
	}
	if st.EventsCorrupted != 0 {
		t.Errorf("EventsCorrupted = %d want 0", st.EventsCorrupted)
	}
	if st.LastTransition != "INITIALIZING" {
		t.Errorf("LastTransition = %q want INITIALIZING", st.LastTransition)
	}
	if got, want := len(st.InFlightWorkers), 1; got != want {
		t.Errorf("InFlightWorkers len = %d want %d (w-2 in-flight; w-1 checkpointed)", got, want)
	}
	if _, ok := st.InFlightWorkers["w-2"]; !ok {
		t.Errorf("InFlightWorkers missing w-2")
	}
	if _, ok := st.InFlightWorkers["w-1"]; ok {
		t.Errorf("InFlightWorkers still contains w-1 (should have been deleted by checkpoint)")
	}
}

func TestReplayWorkerDeathRemovesInFlight(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: "w-x", TaskID: "t-x", Tier: "t1_bypass"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDeath{WorkerID: "w-x", Cause: "panic", RetryCount: 1}); err != nil {
		t.Fatalf("death: %v", err)
	}

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if _, ok := st.InFlightWorkers["w-x"]; ok {
		t.Errorf("InFlightWorkers still contains w-x after Death")
	}
}

func TestReplayWorkerRedispatchedNoIndexing(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: "w-old", TaskID: "t-1", Tier: "t1_bypass"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerRedispatched{
		TaskID: "t-1", WorkerID: "w-old", Class: "TRANSIENT_LLM",
		Action: "redispatch_same_tier", NewTierIndex: 0, RetryCount: 1,
		Reason: "within_budget",
	}); err != nil {
		t.Fatalf("redispatch: %v", err)
	}

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if _, ok := st.InFlightWorkers["w-old"]; !ok {
		t.Errorf("InFlightWorkers should still contain w-old (Replay does not index Redispatched)")
	}
}

func TestReplayPendingWavesAndConfirmations(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", ReviewerWaveStarted{Layer: "L1", Reviewers: []string{"r1", "r2"}}); err != nil {
		t.Fatalf("wave L1 start: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", ReviewerWaveStarted{Layer: "L2", Reviewers: []string{"r3"}}); err != nil {
		t.Fatalf("wave L2 start: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", ReviewerWaveComplete{Layer: "L1", Verdict: "approve"}); err != nil {
		t.Fatalf("wave L1 complete: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", ConfirmationRequested{EventID: "ev-1", DecisionClass: "destructive"}); err != nil {
		t.Fatalf("confirm req 1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", ConfirmationRequested{EventID: "ev-2", DecisionClass: "amendment"}); err != nil {
		t.Fatalf("confirm req 2: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", OperatorConfirmation{EventID: "ev-1", Decision: "approve", Rationale: "ok"}); err != nil {
		t.Fatalf("operator confirm: %v", err)
	}

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if _, ok := st.PendingWaves["L1"]; ok {
		t.Errorf("PendingWaves still contains L1 after Complete")
	}
	if _, ok := st.PendingWaves["L2"]; !ok {
		t.Errorf("PendingWaves missing L2")
	}

	if _, ok := st.OpenConfirmations["ev-1"]; ok {
		t.Errorf("OpenConfirmations still contains ev-1 after OperatorConfirmation")
	}
	if _, ok := st.OpenConfirmations["ev-2"]; !ok {
		t.Errorf("OpenConfirmations missing ev-2")
	}
}

func TestReplayCorruptionToleratedUpTo5(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStarted{
		SessionID: "s1", ProjectID: "p1", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("append started: %v", err)
	}

	for i := 0; i < 5; i++ {
		em.injectCorrupted(t, "p1", "s1", EvtWorkerDispatched, []byte("not-json"))
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStopped{Outcome: "success"}); err != nil {
		t.Fatalf("append stopped: %v", err)
	}

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay tolerated 5 corruptions: %v", err)
	}
	if st.EventsCorrupted != 5 {
		t.Errorf("EventsCorrupted = %d want 5", st.EventsCorrupted)
	}
	if st.EventsReplayed != 7 {
		t.Errorf("EventsReplayed = %d want 7", st.EventsReplayed)
	}
	// Each corruption MUST have been audited via a ReplayCorruptionDetected
	// event re-appended to the log (inv-zen-095 contract).
	rows, err := log.Query(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("Query post-replay: %v", err)
	}
	corruptionCount := 0
	for _, r := range rows {
		if r.EventType == EvtReplayCorruptionDetected {
			corruptionCount++
			// IMP-3 privacy: the audit event MUST NOT include raw payload
			// bytes ("not-json" was the corrupt payload). Decode + check.
			decoded, derr := Decode(r.EventType, r.Payload)
			if derr != nil {
				t.Fatalf("decode emitted ReplayCorruptionDetected: %v", derr)
			}
			ev, ok := decoded.(ReplayCorruptionDetected)
			if !ok {
				t.Fatalf("decoded ReplayCorruptionDetected is not the right type: %T", decoded)
			}
			if strings.Contains(ev.Reason, "not-json") {
				t.Errorf("ReplayCorruptionDetected.Reason leaked raw payload bytes (IMP-3 violation): %q", ev.Reason)
			}
			if ev.EventOffset == 0 {
				t.Errorf("ReplayCorruptionDetected.EventOffset = 0 (should reference the corrupt row's event_id)")
			}
		}
	}
	if corruptionCount != 5 {
		t.Errorf("ReplayCorruptionDetected emitted %d times; want 5 (one per skipped corruption)", corruptionCount)
	}
}

func TestReplayCorruptionBudgetExceededOn6th(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStarted{
		SessionID: "s1", ProjectID: "p1", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("append started: %v", err)
	}
	for i := 0; i < 6; i++ {
		em.injectCorrupted(t, "p1", "s1", EvtWorkerDispatched, []byte("not-json"))
	}
	st, err := log.Replay(ctx, "s1")
	if err == nil {
		t.Fatalf("Replay did not return ErrCorruptionBudgetExceeded on 6th corruption")
	}
	if !errors.Is(err, ErrCorruptionBudgetExceeded) {
		t.Errorf("err = %v want ErrCorruptionBudgetExceeded", err)
	}

	if st == nil {
		t.Errorf("Replay returned nil state on budget exceeded; want partial state")
	} else if st.EventsCorrupted <= ReplayCorruptionBudget {
		t.Errorf("EventsCorrupted = %d; want > ReplayCorruptionBudget=%d", st.EventsCorrupted, ReplayCorruptionBudget)
	}

	// IMP-2: the budget-breaching (6th) corruption MUST itself be audited
	// via a ReplayCorruptionDetected event before Replay returns the
	// error. Post-mortem audit logs would otherwise show only 5/N
	// corruption rows for the session that hit N+1, hiding the breaching
	// row from inv-zen-095 forensics.
	rows, qerr := log.Query(context.Background(), "s1", 0)
	if qerr != nil {
		t.Fatalf("Query post-replay: %v", qerr)
	}
	emittedCorruptions := 0
	for _, r := range rows {
		if r.EventType == EvtReplayCorruptionDetected {
			emittedCorruptions++
		}
	}
	if emittedCorruptions != 6 {
		t.Errorf("ReplayCorruptionDetected emitted %d times; want 6 (each skip including the breaching one is audited)", emittedCorruptions)
	}
}

func TestReplayUnknownEventTypeTreatedAsCorruption(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	em.injectCorrupted(t, "p1", "s1", EventType(99), []byte(`{}`))

	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if st.EventsCorrupted != 1 {
		t.Errorf("EventsCorrupted = %d want 1", st.EventsCorrupted)
	}
}

func TestReplayNoEmitter(t *testing.T) {
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(nil, fc)
	_, err := log.Replay(context.Background(), "s1")
	if !errors.Is(err, ErrNoEmitter) {
		t.Errorf("Replay no-emitter: got %v want ErrNoEmitter", err)
	}
}

func TestReplayPropagatesQueryError(t *testing.T) {
	em := &errorEmitter{err: errors.New("synthetic-query-failure")}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	st, err := log.Replay(context.Background(), "s1")
	if err == nil {
		t.Fatalf("Replay did not propagate QueryRaw error")
	}
	if !strings.Contains(err.Error(), "synthetic-query-failure") {
		t.Errorf("error not wrapped: %v", err)
	}
	if st != nil {
		t.Errorf("Replay returned non-nil state on Query error: %+v", st)
	}
}

func TestReplayRejectsCancelledContext(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	if _, err := log.appendTyped(context.Background(), "s1", "p1", OrchestratorStarted{
		SessionID: "s1", ProjectID: "p1", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("seed append: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	st, err := log.Replay(ctx, "s1")
	if err == nil {
		t.Fatalf("Replay did not reject pre-cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got %v", err)
	}
	if st != nil {
		t.Errorf("Replay returned non-nil state on cancelled ctx: %+v", st)
	}
}

func TestReplayLastEventIDWatermark(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	stEmpty, err := log.Replay(ctx, "empty")
	if err != nil {
		t.Fatalf("Replay empty: %v", err)
	}
	if stEmpty.LastEventID != 0 {
		t.Errorf("empty-session LastEventID = %d want 0", stEmpty.LastEventID)
	}

	var maxID int64
	for i := 0; i < 4; i++ {
		id, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{
			WorkerID: fmt.Sprintf("w-%d", i),
			TaskID:   fmt.Sprintf("t-%d", i),
			Tier:     "t1_bypass",
		})
		if err != nil {
			t.Fatalf("seed append %d: %v", i, err)
		}
		if id > maxID {
			maxID = id
		}
	}
	st, err := log.Replay(ctx, "s1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if st.LastEventID != maxID {
		t.Errorf("LastEventID = %d want %d (max appended event_id)", st.LastEventID, maxID)
	}

	em2 := &inMemoryEmitter{}
	log2 := New(em2, fc)
	if _, err := log2.appendTyped(ctx, "s2", "p2", OrchestratorStarted{
		SessionID: "s2", ProjectID: "p2", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	em2.injectCorrupted(t, "p2", "s2", EvtWorkerDispatched, []byte("not-json"))
	st2, err := log2.Replay(ctx, "s2")
	if err != nil {
		t.Fatalf("Replay s2: %v", err)
	}

	if st2.LastEventID < 2 {
		t.Errorf("LastEventID after corrupt row = %d; expected >= 2 (cursor must advance through corruption)", st2.LastEventID)
	}
	if st2.EventsCorrupted != 1 {
		t.Errorf("EventsCorrupted = %d want 1", st2.EventsCorrupted)
	}
}

// TestReplayHonorsCancelMidLoop per IMP-1, the Replay row-fold loop
// MUST re-check ctx.Err() periodically so daemon shutdown / deadline
// expiry preempts a long replay (10k-event sessions approach the <5s
// recovery spec; Phase E crash-recovery hot path relies on this).
//
// Strategy use a wrappedEmitter whose QueryRaw returns rows AND cancels
// the ctx the caller passes through, so by the time Replay enters the
// row-fold loop ctx is already cancelled. This forces the periodic
// check (every 256 rows) — not the top-of-Replay check — to fire.
func TestReplayHonorsCancelMidLoop(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	bg := context.Background()

	const n = 300
	for i := 0; i < n; i++ {
		if _, err := log.appendTyped(bg, "s1", "p1", WorkerDispatched{
			WorkerID: fmt.Sprintf("w-%d", i),
			TaskID:   fmt.Sprintf("t-%d", i),
			Tier:     "t1_bypass",
		}); err != nil {
			t.Fatalf("seed append %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithCancel(bg)
	wem := &cancelOnQueryEmitter{inner: em, cancel: cancel}
	cancelLog := New(wem, fc)

	st, err := cancelLog.Replay(ctx, "s1")
	if err == nil {
		t.Fatalf("Replay did not return error on mid-loop ctx cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if st == nil {
		t.Fatal("expected partial state on mid-loop cancel; got nil")
	}

	if st.EventsReplayed >= n {
		t.Errorf("EventsReplayed = %d; expected < %d (partial state on cancel)", st.EventsReplayed, n)
	}
	if st.EventsReplayed == 0 {
		t.Errorf("EventsReplayed = 0; expected partial progress before cancel fired")
	}
}

func TestReplayPerformance1000Events(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{
			WorkerID: fmt.Sprintf("w-%d", i),
			TaskID:   fmt.Sprintf("t-%d", i),
			Tier:     "t1_bypass",
		}); err != nil {
			t.Fatalf("seed append %d: %v", i, err)
		}
	}
	start := time.Now()
	st, err := log.Replay(ctx, "s1")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Replay 1000-event: %v", err)
	}
	if st.EventsReplayed != 1000 {
		t.Errorf("EventsReplayed = %d want 1000", st.EventsReplayed)
	}

	if elapsed > 2*time.Second {
		t.Errorf("Replay 1000-event took %v, want <2s (spec target <500ms)", elapsed)
	}
}

type cancelOnQueryEmitter struct {
	inner  RawEmitter
	cancel context.CancelFunc
}

func (e *cancelOnQueryEmitter) EmitRaw(ctx context.Context, projectID, sessionID string, et int, payload []byte, ts int64) (int64, error) {
	return e.inner.EmitRaw(ctx, projectID, sessionID, et, payload, ts)
}

func (e *cancelOnQueryEmitter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]Record, error) {
	rows, err := e.inner.QueryRaw(ctx, sessionID, since)

	e.cancel()
	return rows, err
}

func (m *inMemoryEmitter) injectCorrupted(t *testing.T, projectID, sessionID string, et EventType, payload []byte) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.rows = append(m.rows, Record{
		EventID:   m.nextID,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: et,
		Payload:   append([]byte(nil), payload...),
		Timestamp: 0,
	})
}
