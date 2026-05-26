package orchestratoradapter

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// newTestAdapter builds a temp-dir-backed *Adapter with migrations
// applied. Caller MUST defer cleanup() to close + remove resources.
func newTestAdapter(t *testing.T) (*Adapter, *store.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		t.Fatalf("Migrate: %v", err)
	}
	a, err := New(s)
	if err != nil {
		_ = s.Close()
		t.Fatalf("New: %v", err)
	}
	return a, s, func() {
		_ = a.Close()
		_ = s.Close()
	}
}

func TestAdapter_SatisfiesEventlogRawEmitter(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	var _ eventlog.RawEmitter = a

	ts := time.Now().UnixNano()
	id, err := a.EmitRaw(context.Background(), "proj-1", "sess-1",
		int(eventlog.EvtOrchestratorStarted), []byte(`{"mode":"semi"}`), ts)
	if err != nil {
		t.Fatalf("EmitRaw: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive event_id, got %d", id)
	}

	rows, err := a.QueryRaw(context.Background(), "sess-1", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("QueryRaw len: got %d want 1", len(rows))
	}
	if rows[0].SessionID != "sess-1" {
		t.Errorf("SessionID: got %q want %q", rows[0].SessionID, "sess-1")
	}
	if rows[0].EventType != eventlog.EvtOrchestratorStarted {
		t.Errorf("EventType: got %v want %v", rows[0].EventType, eventlog.EvtOrchestratorStarted)
	}
	if rows[0].EventID != id {
		t.Errorf("EventID: got %d want %d", rows[0].EventID, id)
	}
}

func TestAdapter_QueryRawHonoursSinceFilter(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ts := time.Now().UnixNano()
	for i := 0; i < 5; i++ {
		_, err := a.EmitRaw(context.Background(), "proj-1", "sess-A",
			int(eventlog.EvtOrchestratorStarted), []byte(`{}`), ts+int64(i))
		if err != nil {
			t.Fatalf("EmitRaw %d: %v", i, err)
		}
	}
	rows, err := a.QueryRaw(context.Background(), "sess-A", 2)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (event_id > 2), got %d", len(rows))
	}
	for _, r := range rows {
		if r.EventID <= 2 {
			t.Errorf("event_id %d should have been excluded by since=2", r.EventID)
		}
	}
}

func TestAdapter_QueryRawIsolatesSessions(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	ts := time.Now().UnixNano()
	_, _ = a.EmitRaw(context.Background(), "p", "sess-A", int(eventlog.EvtOrchestratorStarted), []byte(`{}`), ts)
	_, _ = a.EmitRaw(context.Background(), "p", "sess-B", int(eventlog.EvtOrchestratorStarted), []byte(`{}`), ts)
	rows, err := a.QueryRaw(context.Background(), "sess-A", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "sess-A" {
		t.Errorf("expected sole row for sess-A, got %+v", rows)
	}
}

func TestAdapter_EmitRawValidations(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := a.EmitRaw(ctx, "p", "", int(eventlog.EvtOrchestratorStarted), nil, time.Now().UnixNano()); err == nil {
		t.Error("expected error on empty session_id")
	}
	if _, err := a.EmitRaw(ctx, "p", "s", int(eventlog.EvtOrchestratorStarted), nil, 0); err == nil {
		t.Error("expected error on ts<=0")
	}
	if _, err := a.EmitRaw(ctx, "p", "s", int(eventlog.EvtOrchestratorStarted), nil, -1); err == nil {
		t.Error("expected error on ts<0")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.EmitRaw(cctx, "p", "s", int(eventlog.EvtOrchestratorStarted), nil, time.Now().UnixNano()); err == nil {
		t.Error("expected ctx cancelled error")
	}
}

func TestAdapter_QueryRawCtxCancelled(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.QueryRaw(ctx, "x", 0); err == nil {
		t.Error("expected ctx cancelled error")
	}
}

func TestAdapter_EmitRawAfterClose(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := a.EmitRaw(context.Background(), "p", "s",
		int(eventlog.EvtOrchestratorStarted), []byte(`{}`), time.Now().UnixNano()); err == nil {
		t.Error("expected error after Close")
	}
	rec := safetynet.HealthRecord{
		CommitSHA: "x", AuthoredBy: "substrate", TestPassRate: 0.9,
		TestTotal: 10, TestPassed: 9, RecordedAt: time.Now().Unix(),
	}
	if err := a.Insert(context.Background(), rec); err == nil {
		t.Error("expected error after Close (Insert)")
	}
}

func TestAdapter_EmitRawNonJSONPayloadRoundTrip(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ts := time.Now().UnixNano()
	id, err := a.EmitRaw(context.Background(), "p", "sess-X",
		int(eventlog.EvtOrchestratorStarted), []byte("not-json"), ts)
	if err != nil {
		t.Fatalf("EmitRaw: %v", err)
	}
	if id != 1 {
		t.Errorf("first event id: got %d want 1", id)
	}
	rows, err := a.QueryRaw(context.Background(), "sess-X", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestAdapter_SatisfiesEventlogAppenderViaLog(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	log := eventlog.New(a, clock.Real{})
	ev := eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "sess-1",
		ProjectID: "proj-1",
		Timestamp: time.Now(),
		Payload:   map[string]any{"mode": "semi"},
	}
	id, err := log.Append(context.Background(), ev)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	recs, err := log.Query(context.Background(), "sess-1", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

func TestAdapter_AmendmentEventEmitter(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	emitter := a.AmendmentEventEmitter()
	var _ amendment.EventEmitter = emitter
	ev := eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentProposed,
		SessionID: "sess-amend",
		ProjectID: "proj-amend",
		Timestamp: time.Now(),
		Payload:   map[string]any{"adr": "0020"},
	}
	if err := emitter.Append(context.Background(), ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rows, err := a.QueryRaw(context.Background(), "sess-amend", 0)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row from amendment emitter, got %d", len(rows))
	}
}

func TestAdapter_SatisfiesSafetynetHealthWriter(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	var _ safetynet.HealthWriter = a
	rec := safetynet.HealthRecord{
		CommitSHA:                "abcd1234567890abcd1234567890abcd12345678",
		AuthoredBy:               "substrate",
		TestPassRate:             1.0,
		TestTotal:                100,
		TestPassed:               100,
		DoctrineLintPass:         true,
		DoctrineLintFindingsJSON: "[]",
		RecordedAt:               time.Now().Unix(),
	}
	if err := a.Insert(context.Background(), rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rows, err := a.Recent(context.Background(), "substrate", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Recent len: got %d want 1", len(rows))
	}
	if rows[0].CommitSHA != rec.CommitSHA {
		t.Errorf("commit_sha: got %q want %q", rows[0].CommitSHA, rec.CommitSHA)
	}
	if !rows[0].DoctrineLintPass {
		t.Error("DoctrineLintPass round-trip lost")
	}
}

func TestAdapter_SafetynetInsertIdempotent(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	rec := safetynet.HealthRecord{
		CommitSHA: "deadbeef", AuthoredBy: "substrate",
		TestPassRate: 0.99, TestTotal: 100, TestPassed: 99,
		DoctrineLintPass: true, RecordedAt: 1714531080,
	}
	for i := 0; i < 3; i++ {
		if err := a.Insert(context.Background(), rec); err != nil {
			t.Fatalf("Insert iter %d: %v", i, err)
		}
	}
	rows, _ := a.Recent(context.Background(), "substrate", time.Unix(0, 0))
	if len(rows) != 1 {
		t.Errorf("idempotency violated: got %d rows, want 1 (INSERT OR IGNORE)", len(rows))
	}
}

func TestAdapter_SafetynetRecentFiltersByAuthor(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	for _, author := range []string{"substrate", "operator", "manual"} {
		rec := safetynet.HealthRecord{
			CommitSHA: "sha-" + author, AuthoredBy: author,
			TestPassRate: 1.0, TestTotal: 1, TestPassed: 1,
			DoctrineLintPass: true, RecordedAt: time.Now().Unix(),
		}
		if err := a.Insert(context.Background(), rec); err != nil {
			t.Fatalf("Insert %s: %v", author, err)
		}
	}
	rows, err := a.Recent(context.Background(), "operator", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 operator row, got %d", len(rows))
	}
	if rows[0].AuthoredBy != "operator" {
		t.Errorf("AuthoredBy: got %q want operator", rows[0].AuthoredBy)
	}
}

func TestAdapter_SafetynetRecentCtxCancelled(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Recent(ctx, "substrate", time.Now()); err == nil {
		t.Error("expected ctx cancelled error")
	}
}

func TestAdapter_CloseIdempotent(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestAdapter_RejectsNilStore(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected error on nil store")
	}
}

func TestAdapter_AmendmentReloadSignal(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	rs := a.AmendmentReloadSignal("http://127.0.0.1:1")
	var _ amendment.ReloadSignal = rs

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := rs.Reload(ctx); err == nil {
		t.Error("expected reload error against unreachable URL")
	}
}

func TestAdapter_ConcurrentEmitRaw(t *testing.T) {

	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	const G = 8
	const N = 25
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess := "concurrent-" + time.Now().Format("150405.000000") +
				"-g" + string(rune('A'+idx))
			for i := 0; i < N; i++ {
				_, err := a.EmitRaw(context.Background(), "p", sess,
					int(eventlog.EvtOrchestratorStarted), []byte(`{}`),
					time.Now().UnixNano())
				if err != nil {
					t.Errorf("EmitRaw: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
