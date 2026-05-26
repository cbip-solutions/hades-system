package dispatcheradapter_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

// fakeStore captures rows passed to InsertCostLedger without doing any
// real persistence. Mutex-protected so concurrent-Insert tests can
// assert on accumulated rows under -race.
//
// errOn lets tests inject a per-row error (e.g., "fail iff Project ==
// X") without needing a separate fake type. Returning a non-nil error
// from the function aborts that Insert; the adapter MUST propagate the
// error verbatim.
type fakeStore struct {
	mu       sync.Mutex
	inserted []dispatcheradapter.CostLedgerRow
	errOn    func(row dispatcheradapter.CostLedgerRow) error
}

func (f *fakeStore) InsertCostLedger(_ context.Context, row dispatcheradapter.CostLedgerRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errOn != nil {
		if err := f.errOn(row); err != nil {
			return err
		}
	}
	f.inserted = append(f.inserted, row)
	return nil
}

func (f *fakeStore) snapshot() []dispatcheradapter.CostLedgerRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dispatcheradapter.CostLedgerRow, len(f.inserted))
	copy(out, f.inserted)
	return out
}

func openIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "adapter.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNewPanicsOnNilSink(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New(nil, s) to panic, got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be string, got %T: %v", r, r)
		}
		if msg == "" {
			t.Fatal("expected non-empty panic message")
		}
		if !strings.Contains(msg, "dispatcheradapter.New") {
			t.Errorf("panic message should identify caller; got %q", msg)
		}
		if !strings.Contains(msg, "sink") {
			t.Errorf("panic message should identify which arg is nil; got %q", msg)
		}
	}()
	_ = dispatcheradapter.New(nil, s)
}

func TestNewPanicsOnNilStore(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New(sink, nil) to panic, got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be string, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "dispatcheradapter.New") {
			t.Errorf("panic message should identify caller; got %q", msg)
		}
		if !strings.Contains(msg, "store") {
			t.Errorf("panic message should identify which arg is nil; got %q", msg)
		}
	}()
	_ = dispatcheradapter.New(&fakeStore{}, nil)
}

func TestNewReturnsAdapter(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)
	if a == nil {
		t.Fatal("expected non-nil Adapter from New(non-nil sink, non-nil store)")
	}
}

func TestInsertTranslatesAllFields(t *testing.T) {
	t.Parallel()
	sink := &fakeStore{}
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(sink, s)

	ts := time.Date(2026, 4, 30, 12, 34, 56, 0, time.UTC)
	evt := dispatcher.CostEvent{
		Timestamp:    ts,
		Project:      "internal-platform-x",
		SessionID:    "sess-7f3",
		Profile:      "research-deep",
		Tier:         providers.TierAnthropicPAYG,
		Model:        "claude-opus-4-7",
		InputTokens:  1234,
		OutputTokens: 5678,
		Status:       200,
		LatencyMS:    910,
		Err:          "",
	}

	if err := a.Insert(context.Background(), evt); err != nil {
		t.Fatalf("Insert: unexpected error: %v", err)
	}

	rows := sink.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected exactly one row inserted, got %d", len(rows))
	}
	got := rows[0]

	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, ts)
	}
	if got.Project != "internal-platform-x" {
		t.Errorf("Project: got %q, want %q", got.Project, "internal-platform-x")
	}
	if got.SessionID != "sess-7f3" {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, "sess-7f3")
	}
	if got.Profile != "research-deep" {
		t.Errorf("Profile: got %q, want %q", got.Profile, "research-deep")
	}
	if got.Tier != providers.TierAnthropicPAYG {
		t.Errorf("Tier: got %v, want %v", got.Tier, providers.TierAnthropicPAYG)
	}
	if got.Model != "claude-opus-4-7" {
		t.Errorf("Model: got %q, want %q", got.Model, "claude-opus-4-7")
	}
	if got.InputTokens != 1234 {
		t.Errorf("InputTokens: got %d, want %d", got.InputTokens, 1234)
	}
	if got.OutputTokens != 5678 {
		t.Errorf("OutputTokens: got %d, want %d", got.OutputTokens, 5678)
	}
	if got.Status != 200 {
		t.Errorf("Status: got %d, want %d", got.Status, 200)
	}
	if got.LatencyMS != 910 {
		t.Errorf("LatencyMS: got %d, want %d", got.LatencyMS, 910)
	}
	if got.Err != "" {
		t.Errorf("Err: got %q, want empty", got.Err)
	}
}

func TestInsertTranslatesFailureEvent(t *testing.T) {
	t.Parallel()
	sink := &fakeStore{}
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(sink, s)

	ts := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	evt := dispatcher.CostEvent{
		Timestamp: ts,
		Project:   "p1",
		SessionID: "s1",
		Profile:   "default",
		Tier:      providers.TierInHouse,
		Model:     "claude-opus-4-7",
		LatencyMS: 42,
		Err:       "context deadline exceeded",
	}

	if err := a.Insert(context.Background(), evt); err != nil {
		t.Fatalf("Insert: unexpected error: %v", err)
	}

	rows := sink.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	got := rows[0]
	if got.Err != "context deadline exceeded" {
		t.Errorf("Err: got %q, want %q", got.Err, "context deadline exceeded")
	}
	if got.InputTokens != 0 || got.OutputTokens != 0 || got.Status != 0 {
		t.Errorf("expected zero token/status fields on failure event, got input=%d output=%d status=%d",
			got.InputTokens, got.OutputTokens, got.Status)
	}
	if got.Tier != providers.TierInHouse {
		t.Errorf("Tier: got %v, want %v", got.Tier, providers.TierInHouse)
	}
}

// TestInsertPropagatesStoreError a sink-side error MUST bubble up
// unchanged so AsyncEmitter.deliver can log it via slog.
func TestInsertPropagatesStoreError(t *testing.T) {
	t.Parallel()
	want := errors.New("simulated SQLite write failure")
	sink := &fakeStore{
		errOn: func(_ dispatcheradapter.CostLedgerRow) error { return want },
	}
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(sink, s)

	got := a.Insert(context.Background(), dispatcher.CostEvent{
		Project: "p", SessionID: "s", Tier: providers.TierInHouse,
	})
	if !errors.Is(got, want) {
		t.Fatalf("Insert: got err=%v, want %v (errors.Is should match)", got, want)
	}

	if rows := sink.snapshot(); len(rows) != 0 {
		t.Fatalf("expected zero stored rows on error path, got %d", len(rows))
	}
}

func TestInsertForwardsContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	want := "marker-value"

	var seen any
	sink := &mockStoreWithCtxCapture{onInsert: func(ctx context.Context, _ dispatcheradapter.CostLedgerRow) error {
		seen = ctx.Value(ctxKey{})
		return nil
	}}
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(sink, s)

	ctx := context.WithValue(context.Background(), ctxKey{}, want)
	if err := a.Insert(ctx, dispatcher.CostEvent{Tier: providers.TierInHouse}); err != nil {
		t.Fatalf("Insert: unexpected error: %v", err)
	}
	if seen != want {
		t.Errorf("ctx not forwarded: sink saw %v, want %q", seen, want)
	}
}

type mockStoreWithCtxCapture struct {
	onInsert func(ctx context.Context, row dispatcheradapter.CostLedgerRow) error
}

func (m *mockStoreWithCtxCapture) InsertCostLedger(ctx context.Context, row dispatcheradapter.CostLedgerRow) error {
	return m.onInsert(ctx, row)
}

func TestInsertConcurrent(t *testing.T) {
	t.Parallel()
	sink := &fakeStore{}
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(sink, s)

	const goroutines = 32
	const perGoroutine = 16

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = a.Insert(context.Background(), dispatcher.CostEvent{
					Project:   "p",
					SessionID: "s",
					Tier:      providers.TierInHouse,
					Model:     "claude-opus-4-7",
					Status:    200,
				})
			}
		}(g)
	}
	wg.Wait()

	if got, want := len(sink.snapshot()), goroutines*perGoroutine; got != want {
		t.Fatalf("concurrent inserts: got %d rows, want %d", got, want)
	}
}

func TestAdapterImplementsCostSink(t *testing.T) {
	t.Parallel()
	var _ dispatcher.CostSink = (*dispatcheradapter.Adapter)(nil)

	sink := &fakeStore{}
	s := openIntegrationStore(t)
	var iface dispatcher.CostSink = dispatcheradapter.New(sink, s)
	if err := iface.Insert(context.Background(), dispatcher.CostEvent{Tier: providers.TierInHouse}); err != nil {
		t.Fatalf("Insert via interface: unexpected error: %v", err)
	}
	if len(sink.snapshot()) != 1 {
		t.Fatalf("expected 1 row via interface dispatch, got %d", len(sink.snapshot()))
	}
}

// TestAdapterInsert_CarriesProvider pins the Plan 16 Phase B C8 contract:
// dispatcher.CostEvent.Provider (the registry Name of the backend that
// handled the attempt) MUST round-trip into the dispatcher-flavoured
// CostLedgerRow.Provider so per-provider attribution survives the
// CostSink path. Twin of B-6's eager Provider: name writes inside
// dispatcher.attempt() — this is the cross-package consumer.
func TestAdapterInsert_CarriesProvider(t *testing.T) {
	t.Parallel()
	f := &fakeStore{}
	a := dispatcheradapter.New(f, openIntegrationStore(t))
	err := a.Insert(context.Background(), dispatcher.CostEvent{
		Project:  "internal-platform-x",
		Profile:  "worker-code",
		Provider: "deepseek-direct",
		Tier:     providers.TierGenericOpenAICompat,
		Model:    "deepseek-chat",
		Status:   200,
	})
	if err != nil {
		t.Fatalf("Insert: unexpected error: %v", err)
	}
	got := f.snapshot()
	if len(got) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(got))
	}
	if got[0].Provider != "deepseek-direct" {
		t.Errorf("CostLedgerRow.Provider = %q, want deepseek-direct", got[0].Provider)
	}
}

func TestCostEventAndCostLedgerRowFieldParity(t *testing.T) {
	t.Parallel()
	evt := reflect.TypeOf(dispatcher.CostEvent{})
	row := reflect.TypeOf(dispatcheradapter.CostLedgerRow{})
	if evt.NumField() != row.NumField() {
		t.Fatalf("CostEvent has %d fields, CostLedgerRow has %d — adapter.Insert MUST be updated when either type grows; see dispatcheradapter.go (Insert)",
			evt.NumField(), row.NumField())
	}
	for i := 0; i < evt.NumField(); i++ {
		ef := evt.Field(i)
		rf := row.Field(i)
		if ef.Name != rf.Name {
			t.Errorf("field %d name: CostEvent=%q, CostLedgerRow=%q", i, ef.Name, rf.Name)
		}
		if ef.Type != rf.Type {
			t.Errorf("field %d (%s) type: CostEvent=%s, CostLedgerRow=%s",
				i, ef.Name, ef.Type, rf.Type)
		}
	}
}

func TestAdapterImplementsCostStore(t *testing.T) {
	t.Parallel()
	var _ orchestrator.CostStore = (*dispatcheradapter.Adapter)(nil)
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)
	var cs orchestrator.CostStore = a

	if _, err := cs.QueryAllRecentCosts(time.Now().Add(-1 * time.Hour)); err != nil {
		t.Errorf("QueryAllRecentCosts via interface: %v", err)
	}
}

func TestAdapterInsertCostLedgerRoundtrip(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)

	now := time.Now().Truncate(time.Millisecond)
	row := orchestrator.CostLedgerRow{
		IdempotencyKey: "round-1",
		TS:             now,
		Project:        "internal-platform-x",
		Profile:        "orchestrator",
		Tier:           "tier2-paygo",
		Model:          "claude-opus-4-6",
		InputTokens:    100,
		OutputTokens:   50,
		CostUSD:        0.0125,
		SessionID:      "sess-A",
	}
	id, err := a.InsertCostLedger(row)
	if err != nil {
		t.Fatalf("InsertCostLedger: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero LastInsertId, got 0")
	}

	rows, err := a.QueryAllRecentCosts(now.Add(-1 * time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.IdempotencyKey != "round-1" {
		t.Errorf("IdempotencyKey lost in roundtrip: %q", got.IdempotencyKey)
	}
	if got.CostUSD != 0.0125 {
		t.Errorf("CostUSD lost: got %f, want %f", got.CostUSD, 0.0125)
	}
	if got.ID == 0 {
		t.Errorf("ID not populated in roundtrip; got 0")
	}
	if got.ID != id {
		t.Errorf("ID mismatch: insert returned %d, query returned %d", id, got.ID)
	}
}

// TestAdapterDuplicateIdempotencyWrapsOrchestratorSentinel the second
// insert of a row with the same IdempotencyKey MUST surface
// orchestrator.ErrDuplicateIdempotency (via errors.Is). The store-level
// sentinel store.ErrDuplicateIdempotency MUST NOT be exposed at the
// orchestrator boundary — that would force orchestrator callers to take
// a transitive dependency on store types, defeating inv-zen-031.
func TestAdapterDuplicateIdempotencyWrapsOrchestratorSentinel(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)
	row := orchestrator.CostLedgerRow{
		IdempotencyKey: "dup-1",
		TS:             time.Now(),
		Project:        "internal-platform-x",
		Profile:        "orchestrator",
		Tier:           "tier2-paygo",
		Model:          "claude-opus-4-6",
		CostUSD:        1.0,
	}
	if _, err := a.InsertCostLedger(row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := a.InsertCostLedger(row)
	if err == nil {
		t.Fatal("second insert: expected error, got nil")
	}
	if !errors.Is(err, orchestrator.ErrDuplicateIdempotency) {
		t.Fatalf("want errors.Is(err, orchestrator.ErrDuplicateIdempotency)=true, got err=%v", err)
	}

	if errors.Is(err, store.ErrDuplicateIdempotency) {
		t.Errorf("store.ErrDuplicateIdempotency must NOT be exposed at orchestrator boundary; got %v (this would force orchestrator callers into a transitive store dependency, breaking inv-zen-031)", err)
	}
}

// TestAdapterInsertCostLedgerPropagatesNonDuplicateError a non-duplicate
// store error (e.g., validation failure on empty IdempotencyKey) MUST
// surface unchanged — no wrapping in orchestrator.ErrDuplicateIdempotency.
func TestAdapterInsertCostLedgerPropagatesNonDuplicateError(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)

	_, err := a.InsertCostLedger(orchestrator.CostLedgerRow{
		TS:      time.Now(),
		Project: "p", Profile: "pr", Tier: "t", Model: "m",
		CostUSD: 1.0,
	})
	if err == nil {
		t.Fatal("expected error for empty IdempotencyKey, got nil")
	}
	if errors.Is(err, orchestrator.ErrDuplicateIdempotency) {
		t.Errorf("non-duplicate error should NOT match orchestrator.ErrDuplicateIdempotency; got %v", err)
	}
	if !strings.Contains(err.Error(), "idempotency_key") {
		t.Errorf("expected error to mention idempotency_key validation; got %v", err)
	}
}

func TestAdapterQueryAllRecentCostsEmpty(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)
	rows, err := a.QueryAllRecentCosts(time.Now().Add(-30 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts on empty store: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows on empty store, got %d", len(rows))
	}
}

func TestAdapterQueryAllRecentCostsFiltersBySince(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)

	now := time.Now().Truncate(time.Millisecond)
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	for i, ts := range []time.Time{old, recent} {
		_, err := a.InsertCostLedger(orchestrator.CostLedgerRow{
			IdempotencyKey: "filter-" + string(rune('a'+i)),
			TS:             ts,
			Project:        "p", Profile: "pr", Tier: "t", Model: "m",
			CostUSD: 0.1,
		})
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	rows, err := a.QueryAllRecentCosts(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row in 24h window, got %d", len(rows))
	}
	if rows[0].IdempotencyKey != "filter-b" {
		t.Errorf("wrong row returned; got IdempotencyKey=%q", rows[0].IdempotencyKey)
	}
}

func TestAdapterAllFieldsPreservedInRoundtrip(t *testing.T) {
	t.Parallel()
	s := openIntegrationStore(t)
	a := dispatcheradapter.New(&fakeStore{}, s)
	row := orchestrator.CostLedgerRow{
		IdempotencyKey:      "all-fields-1",
		TS:                  time.Now().Truncate(time.Millisecond),
		Project:             "internal-platform-x",
		Profile:             "orchestrator",
		Tier:                "tier2-paygo",
		Model:               "claude-opus-4-6",
		InputTokens:         123,
		OutputTokens:        456,
		CacheReadTokens:     789,
		CacheCreationTokens: 12,
		CostUSD:             0.34,
		ConversationID:      "conv-7",
		SessionID:           "sess-8",
		RequestHash:         []byte{0xAB, 0xCD, 0xEF},
	}
	id, err := a.InsertCostLedger(row)
	if err != nil {
		t.Fatalf("InsertCostLedger: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	rows, err := a.QueryAllRecentCosts(time.Now().Add(-1 * time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0]

	if got.IdempotencyKey != row.IdempotencyKey {
		t.Errorf("IdempotencyKey lost: got %q, want %q", got.IdempotencyKey, row.IdempotencyKey)
	}
	if !got.TS.Equal(row.TS) {
		t.Errorf("TS: got %v, want %v", got.TS, row.TS)
	}
	if got.Project != row.Project {
		t.Errorf("Project lost: got %q, want %q", got.Project, row.Project)
	}
	if got.Profile != row.Profile {
		t.Errorf("Profile lost: got %q, want %q", got.Profile, row.Profile)
	}
	if got.Tier != row.Tier {
		t.Errorf("Tier lost: got %q, want %q", got.Tier, row.Tier)
	}
	if got.Model != row.Model {
		t.Errorf("Model lost: got %q, want %q", got.Model, row.Model)
	}
	if got.InputTokens != row.InputTokens {
		t.Errorf("InputTokens lost: got %d, want %d", got.InputTokens, row.InputTokens)
	}
	if got.OutputTokens != row.OutputTokens {
		t.Errorf("OutputTokens lost: got %d, want %d", got.OutputTokens, row.OutputTokens)
	}
	if got.CacheReadTokens != row.CacheReadTokens {
		t.Errorf("CacheReadTokens lost: got %d, want %d", got.CacheReadTokens, row.CacheReadTokens)
	}
	if got.CacheCreationTokens != row.CacheCreationTokens {
		t.Errorf("CacheCreationTokens lost: got %d, want %d", got.CacheCreationTokens, row.CacheCreationTokens)
	}
	if got.CostUSD != row.CostUSD {
		t.Errorf("CostUSD lost: got %f, want %f", got.CostUSD, row.CostUSD)
	}
	if got.ConversationID != row.ConversationID {
		t.Errorf("ConversationID lost: got %q, want %q", got.ConversationID, row.ConversationID)
	}
	if got.SessionID != row.SessionID {
		t.Errorf("SessionID lost: got %q, want %q", got.SessionID, row.SessionID)
	}
	if !bytes.Equal(got.RequestHash, row.RequestHash) {
		t.Errorf("RequestHash lost: got %x, want %x", got.RequestHash, row.RequestHash)
	}
}

// TestAdapterQueryAllRecentCostsPropagatesStoreError a failure from
// store.QueryAllRecentCosts (e.g., DB closed mid-call) MUST surface
// unchanged from the adapter so callers (F-7 RebuildFromLedger) can log
// the cause and choose to abort startup vs. proceed with empty counters.
//
// Forcing the error: close the underlying *store.Store before the call.
// This is the simplest reliable trigger — any subsequent db.Query returns
// "sql: database is closed". Avoids a slow-test mock and keeps the test
// integration-shaped like the rest of the F-6 surface.
func TestAdapterQueryAllRecentCostsPropagatesStoreError(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	defer func() { _ = s.Close() }()

	a := dispatcheradapter.New(&fakeStore{}, s)

	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}

	rows, err := a.QueryAllRecentCosts(time.Now().Add(-1 * time.Hour))
	if err == nil {
		t.Fatal("expected error from QueryAllRecentCosts on closed store, got nil")
	}
	if rows != nil {
		t.Errorf("expected nil rows on error, got %v", rows)
	}
}

func TestCostLedgerRowParityOrchestratorVsStore(t *testing.T) {
	t.Parallel()
	orchType := reflect.TypeOf(orchestrator.CostLedgerRow{})
	storeType := reflect.TypeOf(store.CostLedgerRow{})
	if orchType.NumField() != storeType.NumField() {
		t.Fatalf("orchestrator.CostLedgerRow has %d fields, store.CostLedgerRow has %d — translation layer in dispatcheradapter MUST be updated when either type grows",
			orchType.NumField(), storeType.NumField())
	}
	for i := 0; i < orchType.NumField(); i++ {
		of := orchType.Field(i)
		sf := storeType.Field(i)
		if of.Name != sf.Name {
			t.Errorf("field %d name: orchestrator=%q, store=%q", i, of.Name, sf.Name)
		}
		if of.Type != sf.Type {
			t.Errorf("field %d (%s) type: orchestrator=%s, store=%s", i, of.Name, of.Type, sf.Type)
		}
	}
}

func openPinStoreAdapter(t *testing.T) *dispatcheradapter.PinStoreAdapter {
	t.Helper()
	s := openIntegrationStore(t)
	return dispatcheradapter.NewPinStoreAdapter(s)
}

func TestNewPinStoreAdapterPanicsOnNilStore(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected NewPinStoreAdapter(nil) to panic, got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be string, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "dispatcheradapter.NewPinStoreAdapter") {
			t.Errorf("panic message should identify caller; got %q", msg)
		}
		if !strings.Contains(msg, "store") {
			t.Errorf("panic message should identify which arg is nil; got %q", msg)
		}
	}()
	_ = dispatcheradapter.NewPinStoreAdapter(nil)
}

func TestAdapterImplementsPinStore(t *testing.T) {
	t.Parallel()

	var _ orchestrator.PinStore = (*dispatcheradapter.PinStoreAdapter)(nil)

	pa := openPinStoreAdapter(t)
	var ps orchestrator.PinStore = pa
	rows, err := ps.ListAll()
	if err != nil {
		t.Fatalf("ListAll via interface: %v", err)
	}
	if rows != nil {
		t.Fatalf("expected nil slice on empty store, got %v", rows)
	}
}

func TestAdapterPinInsertAndQueryRoundtrip(t *testing.T) {
	t.Parallel()
	pa := openPinStoreAdapter(t)

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(1 * time.Hour)
	in := orchestrator.PinRow{
		Scope:     "project",
		ScopeID:   "proj-alpha",
		Tier:      "tier2-paygo",
		Provider:  "anthropic",
		SetAt:     now,
		ExpiresAt: &exp,
		Reason:    "perf testing pin",
	}

	if err := pa.Insert(in); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := pa.Query("project", "proj-alpha")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got == nil {
		t.Fatal("Query: expected non-nil result, got nil")
	}
	if got.Scope != in.Scope {
		t.Errorf("Scope: got %q, want %q", got.Scope, in.Scope)
	}
	if got.ScopeID != in.ScopeID {
		t.Errorf("ScopeID: got %q, want %q", got.ScopeID, in.ScopeID)
	}
	if got.Tier != in.Tier {
		t.Errorf("Tier: got %q, want %q", got.Tier, in.Tier)
	}
	if got.Provider != in.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, in.Provider)
	}
	if !got.SetAt.Equal(in.SetAt) {
		t.Errorf("SetAt: got %v, want %v", got.SetAt, in.SetAt)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt: got nil, want non-nil")
	}
	if !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, exp)
	}
	if got.Reason != in.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, in.Reason)
	}
}

func TestAdapterPinDeleteRoundtrip(t *testing.T) {
	t.Parallel()
	pa := openPinStoreAdapter(t)

	pin := orchestrator.PinRow{
		Scope:   "session",
		ScopeID: "sess-001",
		Tier:    "tier1-inhouse",
		SetAt:   time.Now().UTC().Truncate(time.Second),
		Reason:  "delete-roundtrip test",
	}
	if err := pa.Insert(pin); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := pa.Query("session", "sess-001")
	if err != nil {
		t.Fatalf("Query before delete: %v", err)
	}
	if got == nil {
		t.Fatal("Query before delete: expected row, got nil")
	}

	if err := pa.Delete("session", "sess-001"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = pa.Query("session", "sess-001")
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("Query after delete: expected nil, got %v", got)
	}

	if err := pa.Delete("session", "sess-001"); err != nil {
		t.Errorf("second Delete (idempotent): %v", err)
	}
}

func TestAdapterPinListAll(t *testing.T) {
	t.Parallel()
	pa := openPinStoreAdapter(t)

	base := time.Now().UTC().Truncate(time.Second)
	pins := []orchestrator.PinRow{
		{Scope: "project", ScopeID: "proj-1", Tier: "tier1", SetAt: base.Add(-2 * time.Hour), Reason: "oldest"},
		{Scope: "project", ScopeID: "proj-2", Tier: "tier2", SetAt: base.Add(-1 * time.Hour), Reason: "middle"},
		{Scope: "global", ScopeID: "", Tier: "tier3", SetAt: base, Reason: "newest"},
	}
	for i, p := range pins {
		if err := pa.Insert(p); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	rows, err := pa.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	if rows[0].Reason != "newest" {
		t.Errorf("rows[0].Reason: got %q, want %q", rows[0].Reason, "newest")
	}
	if rows[1].Reason != "middle" {
		t.Errorf("rows[1].Reason: got %q, want %q", rows[1].Reason, "middle")
	}
	if rows[2].Reason != "oldest" {
		t.Errorf("rows[2].Reason: got %q, want %q", rows[2].Reason, "oldest")
	}
}

func TestAdapterPinPurgeExpired(t *testing.T) {
	t.Parallel()
	pa := openPinStoreAdapter(t)

	now := time.Now().UTC().Truncate(time.Second)
	pastExp := now.Add(-1 * time.Hour)

	permanent := orchestrator.PinRow{
		Scope:   "global",
		ScopeID: "",
		Tier:    "tier1",
		SetAt:   now,
		Reason:  "permanent pin",
	}
	expired := orchestrator.PinRow{
		Scope:     "session",
		ScopeID:   "sess-expired",
		Tier:      "tier2",
		SetAt:     now.Add(-2 * time.Hour),
		ExpiresAt: &pastExp,
		Reason:    "expired pin",
	}
	if err := pa.Insert(permanent); err != nil {
		t.Fatalf("Insert permanent: %v", err)
	}
	if err := pa.Insert(expired); err != nil {
		t.Fatalf("Insert expired: %v", err)
	}

	purged, err := pa.PurgeExpired(now)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if purged != 1 {
		t.Errorf("PurgeExpired: got %d rows purged, want 1", purged)
	}

	got, err := pa.Query("global", "")
	if err != nil {
		t.Fatalf("Query permanent after purge: %v", err)
	}
	if got == nil {
		t.Fatal("permanent pin was incorrectly purged")
	}

	got, err = pa.Query("session", "sess-expired")
	if err != nil {
		t.Fatalf("Query expired after purge: %v", err)
	}
	if got != nil {
		t.Fatal("expired pin was NOT purged")
	}
}

func TestAdapterAllPinFieldsPreservedInRoundtrip(t *testing.T) {
	t.Parallel()
	pa := openPinStoreAdapter(t)

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(6 * time.Hour)
	in := orchestrator.PinRow{

		Scope:     "session",
		ScopeID:   "sess-all-fields",
		Tier:      "tier2-paygo",
		Provider:  "anthropic-paygo",
		SetAt:     now,
		ExpiresAt: &exp,
		Reason:    "all fields roundtrip test",
	}

	if err := pa.Insert(in); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := pa.Query("session", "sess-all-fields")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got == nil {
		t.Fatal("Query: nil result")
	}

	if got.ID == 0 {
		t.Error("ID: expected non-zero DB-assigned ID, got 0")
	}
	if got.Scope != in.Scope {
		t.Errorf("Scope: got %q, want %q", got.Scope, in.Scope)
	}
	if got.ScopeID != in.ScopeID {
		t.Errorf("ScopeID: got %q, want %q", got.ScopeID, in.ScopeID)
	}
	if got.Tier != in.Tier {
		t.Errorf("Tier: got %q, want %q", got.Tier, in.Tier)
	}
	if got.Provider != in.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, in.Provider)
	}
	if !got.SetAt.Equal(in.SetAt) {
		t.Errorf("SetAt: got %v, want %v", got.SetAt, in.SetAt)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt: got nil, want non-nil")
	}
	if !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, exp)
	}
	if got.Reason != in.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, in.Reason)
	}
}

// TestAdapterPinListAllPropagatesStoreError a failure from store.ListAllPins
// (e.g., DB closed mid-call) MUST surface unchanged from the adapter.
// Forcing the error by closing the underlying *store.Store before the call —
// same technique as TestAdapterQueryAllRecentCostsPropagatesStoreError.
func TestAdapterPinListAllPropagatesStoreError(t *testing.T) {
	t.Parallel()
	s, err := store.Open(filepath.Join(t.TempDir(), "closed-pin.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	defer func() { _ = s.Close() }()

	pa := dispatcheradapter.NewPinStoreAdapter(s)

	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}

	rows, err := pa.ListAll()
	if err == nil {
		t.Fatal("expected error from ListAll on closed store, got nil")
	}
	if rows != nil {
		t.Errorf("expected nil rows on error, got %v", rows)
	}
}

func TestPinRowParityOrchestratorVsStore(t *testing.T) {
	t.Parallel()
	orchType := reflect.TypeOf(orchestrator.PinRow{})
	storeType := reflect.TypeOf(store.PinRow{})
	if orchType.NumField() != storeType.NumField() {
		t.Fatalf("orchestrator.PinRow has %d fields, store.PinRow has %d — translation layer in dispatcheradapter MUST be updated when either type grows",
			orchType.NumField(), storeType.NumField())
	}
	for i := 0; i < orchType.NumField(); i++ {
		of := orchType.Field(i)
		sf := storeType.Field(i)
		if of.Name != sf.Name {
			t.Errorf("field %d name: orchestrator=%q, store=%q", i, of.Name, sf.Name)
		}
		if of.Type != sf.Type {
			t.Errorf("field %d (%s) type: orchestrator=%s, store=%s", i, of.Name, of.Type, sf.Type)
		}
	}
}
