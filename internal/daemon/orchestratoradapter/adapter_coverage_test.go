package orchestratoradapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

func TestBoolToInt_BothBranches(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Errorf("boolToInt(true) = %d, want 1", boolToInt(true))
	}
	if boolToInt(false) != 0 {
		t.Errorf("boolToInt(false) = %d, want 0", boolToInt(false))
	}
}

func TestParseBoolish_AllEncodings(t *testing.T) {
	truthyForms := []string{"1", "true", "TRUE", "True", "t", "T"}
	for _, s := range truthyForms {
		if !parseBoolish(sql.RawBytes(s)) {
			t.Errorf("parseBoolish(%q) = false, want true", s)
		}
	}
	falsyForms := []string{"0", "false", "FALSE", "False", "f", "F", "garbage", ""}
	for _, s := range falsyForms {
		if parseBoolish(sql.RawBytes(s)) {
			t.Errorf("parseBoolish(%q) = true, want false (unknown encodings default to false; empty is also false)", s)
		}
	}
}

func TestQueryEventsByType_HappyPath_ReturnsMatchingRows(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	now := time.Now().UnixNano()
	for i, et := range []eventlog.EventType{
		eventlog.EvtOrchestratorStarted,
		eventlog.EvtSubstrateDriftDetected,
		eventlog.EvtSubstrateDriftDetected,
		eventlog.EvtOrchestratorStopped,
	} {
		_, err := a.EmitRaw(context.Background(), "p1", "s1",
			int(et), []byte(`{}`), now+int64(i))
		if err != nil {
			t.Fatalf("EmitRaw[%d]: %v", i, err)
		}
	}
	rows, err := a.QueryEventsByType(context.Background(),
		eventlog.EvtSubstrateDriftDetected.String(), now/int64(time.Second)-3600)
	if err != nil {
		t.Fatalf("QueryEventsByType: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2 SubstrateDriftDetected matches", len(rows))
	}
}

func TestQueryEventsByType_CtxCancelled_ReturnsCtxErr(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.QueryEventsByType(ctx, "AnyType", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestQueryEventsByType_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.QueryEventsByType(context.Background(), "AnyType", 0)
	if err == nil {
		t.Fatal("expected error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter: query audit_events_raw by type") {
		t.Errorf("err = %v, want prefix 'orchestratoradapter: query audit_events_raw by type'", err)
	}
}

func TestCountEventsByType_HappyPath_AndNegativeSinceClamps(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	now := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		_, err := a.EmitRaw(context.Background(), "p1", "s1",
			int(eventlog.EvtConfigDivergenceDetected), []byte(`{}`), now+int64(i))
		if err != nil {
			t.Fatalf("EmitRaw: %v", err)
		}
	}
	n, err := a.CountEventsByType(context.Background(),
		eventlog.EvtConfigDivergenceDetected.String(), -1)
	if err != nil {
		t.Fatalf("CountEventsByType: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3 (negative since must clamp to 0)", n)
	}
}

func TestCountEventsByType_CtxCancelled_ReturnsCtxErr(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.CountEventsByType(ctx, "AnyType", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCountEventsByType_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.CountEventsByType(context.Background(), "AnyType", 0)
	if err == nil {
		t.Fatal("expected error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter: count events by type") {
		t.Errorf("err = %v, want prefix 'orchestratoradapter: count events by type'", err)
	}
}

func TestLastEventByTypeUnix_NoRows_ReturnsZeroNilNoError(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ts, err := a.LastEventByTypeUnix(context.Background(), "NonExistentType")
	if err != nil {
		t.Fatalf("err = %v, want nil for no-rows", err)
	}
	if ts != 0 {
		t.Fatalf("ts = %d, want 0 for no-rows", ts)
	}
}

func TestLastEventByTypeUnix_HappyPath_ReturnsMostRecent(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	base := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		_, err := a.EmitRaw(context.Background(), "p1", "s1",
			int(eventlog.EvtSafetynetPrevMissing), []byte(`{}`), base+int64(i)*int64(time.Second))
		if err != nil {
			t.Fatalf("EmitRaw: %v", err)
		}
	}
	ts, err := a.LastEventByTypeUnix(context.Background(),
		eventlog.EvtSafetynetPrevMissing.String())
	if err != nil {
		t.Fatalf("LastEventByTypeUnix: %v", err)
	}
	expected := (base + 2*int64(time.Second)) / int64(time.Second)
	if ts != expected {
		t.Fatalf("ts = %d, want %d (most-recent)", ts, expected)
	}
}

func TestLastEventByTypeUnix_CtxCancelled_ReturnsCtxErr(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.LastEventByTypeUnix(ctx, "AnyType")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLastEventByTypeUnix_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.LastEventByTypeUnix(context.Background(), "AnyType")
	if err == nil {
		t.Fatal("expected error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter: last event by type") {
		t.Errorf("err = %v, want prefix 'orchestratoradapter: last event by type'", err)
	}
}

func TestEnsureSubstrateHealthUnique_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()

	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	err := a.Insert(context.Background(), safetynet.HealthRecord{
		CommitSHA:    "abc1234",
		AuthoredBy:   "substrate",
		TestPassRate: 1.0,
		TestTotal:    10,
		TestPassed:   10,
		RecordedAt:   time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected Insert error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "ensure substrate_health unique index") &&
		!strings.Contains(err.Error(), "insert substrate_health") {
		t.Errorf("err = %v, want a substrate_health-prefixed error (either ensure-unique or insert)", err)
	}
}

func TestRecent_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.Recent(context.Background(), "substrate", time.Now().Add(-24*time.Hour))
	if err == nil {
		t.Fatal("expected Recent error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter") {
		t.Errorf("err = %v, want orchestratoradapter-prefixed error", err)
	}
}

func TestNewRowID_RandReadFailure(t *testing.T) {
	saved := randReader
	defer func() { randReader = saved }()
	randReader = errReader{}

	_, err := newRowID()
	if err == nil {
		t.Fatal("expected error from newRowID when rand.Read fails, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want propagated 'boom' error from errReader", err)
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

func TestParseEventType_AllBranches(t *testing.T) {

	et, err := parseEventType(eventlog.EvtOrchestratorStarted.String())
	if err != nil {
		t.Fatalf("stringer name: %v", err)
	}
	if et != eventlog.EvtOrchestratorStarted {
		t.Errorf("stringer name: got %v want %v", et, eventlog.EvtOrchestratorStarted)
	}

	et, err = parseEventType("1")
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	if et != eventlog.EventType(1) {
		t.Errorf("decimal: got %v want EventType(1)", et)
	}

	et, err = parseEventType("garbage-not-a-type")
	if err == nil {
		t.Fatal("garbage: expected error, got nil")
	}
	if et != eventlog.EvtUnknown {
		t.Errorf("garbage: got %v want EvtUnknown", et)
	}
	if !strings.Contains(err.Error(), "unknown event type") {
		t.Errorf("err = %v, want 'unknown event type' prefix", err)
	}
}

func TestEmitRaw_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.EmitRaw(context.Background(), "p", "s",
		int(eventlog.EvtOrchestratorStarted), []byte(`{}`), time.Now().UnixNano())
	if err == nil {
		t.Fatal("expected EmitRaw error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter") {
		t.Errorf("err = %v, want orchestratoradapter-prefixed", err)
	}
}

func TestQueryRaw_AfterStoreClose_ReturnsWrappedErr(t *testing.T) {
	a, s, cleanup := newTestAdapter(t)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	_, err := a.QueryRaw(context.Background(), "any", 0)
	if err == nil {
		t.Fatal("expected QueryRaw error after store close, got nil")
	}
	if !strings.Contains(err.Error(), "orchestratoradapter") {
		t.Errorf("err = %v, want orchestratoradapter-prefixed", err)
	}
}

func TestInsert_DuplicateRowIsIdempotent(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()
	rec := safetynet.HealthRecord{
		CommitSHA:    "deadbeef",
		AuthoredBy:   "substrate",
		TestPassRate: 1.0,
		TestTotal:    10,
		TestPassed:   10,
		RecordedAt:   1_700_000_000,
	}
	if err := a.Insert(context.Background(), rec); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if err := a.Insert(context.Background(), rec); err != nil {
		t.Fatalf("second Insert (idempotent): %v", err)
	}
	rows, err := a.Recent(context.Background(), "substrate", time.Unix(1_699_999_999, 0))
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Recent rows = %d, want 1 (INSERT OR IGNORE collapses duplicate)", len(rows))
	}
}
