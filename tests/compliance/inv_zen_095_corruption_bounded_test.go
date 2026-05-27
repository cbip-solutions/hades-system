// tests/compliance/inv_zen_095_corruption_bounded_test.go
//
// Compliance gate for invariant: Replay tolerates at most
// ReplayCorruptionBudget (5) corrupted events per session. The
// (budget+1)th corruption MUST return ErrCorruptionBudgetExceeded so
// the orchestrator caller can transition to HARD_PAUSED instead of
// silently advancing past unrecoverable state drift.
//
// This compliance test sits alongside the package-internal unit tests
// in eventlog/replay_test.go (Task A-4) so make verify-invariants-style
// indexing — which globs tests/compliance/inv_zen_*_test.go — picks up
// the runtime contract end-to-end. The unit test exercises internal
// paths via package-private helpers; this compliance test exercises the
// PUBLIC surface (eventlog.New, Log.Append, Log.Replay, exported
// constants and sentinel errors) so a future API refactor that
// accidentally changed the public budget contract would be caught here.
//
// Three sub-tests:
// 1. budget tolerated — N corrupted rows replay with no error +
// EventsCorrupted == N
// 2. budget exceeded — N+1 corrupted rows return
// ErrCorruptionBudgetExceeded (errors.Is-wrapped)
// 3. budget constant pinned — ReplayCorruptionBudget literally equals
// 5, so a future code change that loosened the budget without a
// spec amendment is caught at the compliance layer
package compliance

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// TestInvZen095ReplayCorruptionBounded — the canonical end-to-end gate
// for invariant (replay corruption budget = 5). All three sub-tests
// MUST pass on every commit; race-clean under -race.
func TestInvZen095ReplayCorruptionBounded(t *testing.T) {
	t.Run("budget_constant_matches_spec", func(t *testing.T) {
		// invariant fixes N=5. A tightening (N<5) is non-breaking; a
		// loosening (N>5) is a spec amendment that requires a parallel
		// update here. Either direction MUST surface in CI.
		if eventlog.ReplayCorruptionBudget != 5 {
			t.Errorf("inv-zen-095 VIOLATED: eventlog.ReplayCorruptionBudget = %d; spec fixes N=5",
				eventlog.ReplayCorruptionBudget)
		}
	})

	t.Run("at_budget_tolerated", func(t *testing.T) {
		em := newComplianceEmitter()
		log := eventlog.New(em, clock.NewFake(time.Unix(1700000000, 0)))
		ctx := context.Background()

		appendStart(t, log, ctx, "s1", "p1")
		for i := 0; i < eventlog.ReplayCorruptionBudget; i++ {
			em.injectCorrupted("p1", "s1", eventlog.EvtWorkerDispatched, []byte("not-json"))
		}
		appendStop(t, log, ctx, "s1", "p1")

		st, err := log.Replay(ctx, "s1")
		if err != nil {
			t.Fatalf("inv-zen-095 VIOLATED: Replay errored at budget (N=%d): %v",
				eventlog.ReplayCorruptionBudget, err)
		}
		if st == nil {
			t.Fatal("Replay returned nil state at budget — invariant requires partial state on success")
		}
		if st.EventsCorrupted != eventlog.ReplayCorruptionBudget {
			t.Errorf("EventsCorrupted = %d; want %d (one per skipped row)",
				st.EventsCorrupted, eventlog.ReplayCorruptionBudget)
		}
		// Replay re-emits ReplayCorruptionDetected for each skip. The
		// post-replay log MUST contain exactly N such audit rows so
		// forensics can reconstruct which rows were skipped.
		rows, qerr := log.Query(ctx, "s1", 0)
		if qerr != nil {
			t.Fatalf("Query post-replay: %v", qerr)
		}
		audits := 0
		for _, r := range rows {
			if r.EventType == eventlog.EvtReplayCorruptionDetected {
				audits++
			}
		}
		if audits != eventlog.ReplayCorruptionBudget {
			t.Errorf("ReplayCorruptionDetected audit rows = %d; want %d", audits,
				eventlog.ReplayCorruptionBudget)
		}
	})

	t.Run("over_budget_exceeds", func(t *testing.T) {
		em := newComplianceEmitter()
		log := eventlog.New(em, clock.NewFake(time.Unix(1700000000, 0)))
		ctx := context.Background()

		appendStart(t, log, ctx, "s2", "p2")
		// budget+1 corrupted rows — the (N+1)th MUST trip the gate.
		for i := 0; i < eventlog.ReplayCorruptionBudget+1; i++ {
			em.injectCorrupted("p2", "s2", eventlog.EvtWorkerDispatched, []byte("not-json"))
		}
		_, err := log.Replay(ctx, "s2")
		if err == nil {
			t.Fatalf("inv-zen-095 VIOLATED: Replay returned nil error after %d corruptions (budget=%d)",
				eventlog.ReplayCorruptionBudget+1, eventlog.ReplayCorruptionBudget)
		}
		if !errors.Is(err, eventlog.ErrCorruptionBudgetExceeded) {
			t.Fatalf("inv-zen-095 VIOLATED: err = %v; want errors.Is(ErrCorruptionBudgetExceeded)", err)
		}
		// IMP-2 (I-1): the breaching N+1th corruption MUST be audited
		// BEFORE Replay returns ErrCorruptionBudgetExceeded — otherwise
		// forensics lose the row that tripped the gate. internal
		// replay_test.go covers this; pin it on the public surface here
		// so a future refactor reordering audit-emit vs budget-check
		// (e.g. early-return before audit) is caught at the compliance
		// layer instead of silently regressing the contract documented
		// at internal/orchestrator/eventlog/replay.go:148-153.
		rows, qerr := log.Query(ctx, "s2", 0)
		if qerr != nil {
			t.Fatalf("Query post-replay: %v", qerr)
		}
		audits := 0
		for _, r := range rows {
			if r.EventType == eventlog.EvtReplayCorruptionDetected {
				audits++
			}
		}
		want := eventlog.ReplayCorruptionBudget + 1
		if audits != want {
			t.Errorf("inv-zen-095/IMP-2 VIOLATED: ReplayCorruptionDetected audit rows = %d; want %d (breaching N+1th MUST also be audited)",
				audits, want)
		}
	})
}

func appendStart(t *testing.T, log *eventlog.Log, ctx context.Context, sess, proj string) {
	t.Helper()
	if _, err := log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: sess,
		ProjectID: proj,
		Payload: map[string]any{
			"session_id":    sess,
			"project_id":    proj,
			"autonomy_mode": "semi",
		},
	}); err != nil {
		t.Fatalf("Append(OrchestratorStarted): %v", err)
	}
}

func appendStop(t *testing.T, log *eventlog.Log, ctx context.Context, sess, proj string) {
	t.Helper()
	if _, err := log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtOrchestratorStopped,
		SessionID: sess,
		ProjectID: proj,
		Payload: map[string]any{
			"outcome": "success",
		},
	}); err != nil {
		t.Fatalf("Append(OrchestratorStopped): %v", err)
	}
}

type complianceEmitter struct {
	mu     sync.Mutex
	rows   []eventlog.Record
	nextID int64
}

func newComplianceEmitter() *complianceEmitter { return &complianceEmitter{} }

func (m *complianceEmitter) EmitRaw(_ context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.rows = append(m.rows, eventlog.Record{
		EventID:   m.nextID,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: eventlog.EventType(eventType),
		Payload:   append([]byte(nil), payload...),
		Timestamp: ts,
	})
	return m.nextID, nil
}

func (m *complianceEmitter) QueryRaw(_ context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]eventlog.Record, 0, len(m.rows))
	for _, r := range m.rows {
		if r.SessionID == sessionID && r.EventID > since {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventID < out[j].EventID })
	return out, nil
}

func (m *complianceEmitter) injectCorrupted(projectID, sessionID string, et eventlog.EventType, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.rows = append(m.rows, eventlog.Record{
		EventID:   m.nextID,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: et,
		Payload:   append([]byte(nil), payload...),
	})
}

var _ eventlog.RawEmitter = (*complianceEmitter)(nil)
