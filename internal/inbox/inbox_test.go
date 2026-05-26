package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustHashFor(t *testing.T, s string) string {
	t.Helper()
	h := ComputeContentHash(map[string]any{"key": s})
	if len(h) != 64 {
		t.Fatalf("ComputeContentHash returned len=%d, want 64", len(h))
	}
	return h
}

func TestStoreInsertRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityUrgent,
		EventType:   "hra.l4_alert",
		ContentHash: mustHashFor(t, "h-1"),
		Payload:     json.RawMessage(`{"finding":"invariant-violation"}`),
		CreatedAt:   now,
	}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if n.ID == 0 {
		t.Error("ID was not populated after Insert")
	}

	got, err := s.List(ctx, ListFilter{ProjectID: n.ProjectID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List len = %d, want 1", len(got))
	}
	if got[0].EventType != "hra.l4_alert" {
		t.Errorf("EventType = %q, want hra.l4_alert", got[0].EventType)
	}
	if got[0].Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent", got[0].Severity)
	}
}

func TestStoreInsertRejectsInvalidSeverity(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    "made-up",
		EventType:   "x.y",
		ContentHash: mustHashFor(t, "z"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	err := s.Insert(ctx, n)
	if err == nil {
		t.Fatal("Insert with invalid severity must fail")
	}
	if !errors.Is(err, ErrInvalidSeverity) {
		t.Errorf("expected ErrInvalidSeverity, got: %v", err)
	}
}

func TestStoreInsertRejectsZeroProjectID(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "",
		Severity:    SeverityInfoImmediate,
		EventType:   "x.y",
		ContentHash: mustHashFor(t, "z"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	err := s.Insert(ctx, n)
	if err == nil {
		t.Fatal("Insert with empty ProjectID must fail")
	}
	if !errors.Is(err, ErrInvalidProjectID) {
		t.Errorf("expected ErrInvalidProjectID, got: %v", err)
	}
}

func TestStoreInsertRejectsNilNotification(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	err := s.Insert(ctx, nil)
	if err == nil {
		t.Fatal("Insert(nil) must fail")
	}
}

func TestStoreInsertRejectsEmptyEventType(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityInfoImmediate,
		EventType:   "",
		ContentHash: mustHashFor(t, "z"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	err := s.Insert(ctx, n)
	if err == nil {
		t.Fatal("Insert with empty EventType must fail")
	}
}

func TestStoreInsertRejectsBadContentHashLength(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityInfoImmediate,
		EventType:   "x.y",
		ContentHash: "tooshort",
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	err := s.Insert(ctx, n)
	if err == nil {
		t.Fatal("Insert with bad ContentHash length must fail")
	}
}

func TestStoreInsertRejectsZeroCreatedAt(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityInfoImmediate,
		EventType:   "x.y",
		ContentHash: mustHashFor(t, "z"),
		Payload:     json.RawMessage(`{}`),
	}
	err := s.Insert(ctx, n)
	if err == nil {
		t.Fatal("Insert with zero CreatedAt must fail")
	}
}

func TestStoreInsertRejectsDuplicateInBucket(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	hash := mustHashFor(t, "dup")
	pid := "a" + strings.Repeat("0", 63)
	t1 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Minute)

	n1 := &Notification{
		ProjectID:   pid,
		Severity:    SeverityActionNeeded,
		EventType:   "x.y",
		ContentHash: hash,
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   t1,
	}
	if err := s.Insert(ctx, n1); err != nil {
		t.Fatalf("Insert n1: %v", err)
	}

	n2 := &Notification{
		ProjectID:   pid,
		Severity:    SeverityActionNeeded,
		EventType:   "x.y",
		ContentHash: hash,
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   t2,
	}
	err := s.Insert(ctx, n2)
	if err == nil {
		t.Fatal("Insert(duplicate within bucket) must fail")
	}
	if !errors.Is(err, ErrDedupViolation) {
		t.Errorf("expected ErrDedupViolation, got: %v", err)
	}
}

func TestStoreAckSetsTimestamp(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityActionNeeded,
		EventType:   "gate.failed",
		ContentHash: mustHashFor(t, "g"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := s.Ack(ctx, n.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	got, err := s.List(ctx, ListFilter{ProjectID: n.ProjectID, IncludeAcked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len got = %d, want 1", len(got))
	}
	if got[0].AckedAt == nil {
		t.Error("AckedAt was not populated")
	}
}

func TestStoreAckErrorOnUnknownID(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	err := s.Ack(ctx, 9999)
	if err == nil {
		t.Fatal("Ack(unknown) must fail")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestStoreAckExcludesByDefault(t *testing.T) {

	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityActionNeeded,
		EventType:   "x.y",
		ContentHash: mustHashFor(t, "ack"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := s.Ack(ctx, n.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	got, err := s.List(ctx, ListFilter{ProjectID: n.ProjectID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("default List should exclude acked rows; got %d", len(got))
	}
}

func TestStoreSnoozeSetsUntil(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	n := &Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    SeverityActionNeeded,
		EventType:   "x.y",
		ContentHash: mustHashFor(t, "z"),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	until := time.Now().Add(8 * time.Hour).UTC()
	if err := s.Snooze(ctx, n.ID, until); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	got, err := s.List(ctx, ListFilter{ProjectID: n.ProjectID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].SnoozedUntil == nil || !got[0].SnoozedUntil.Equal(until) {
		t.Errorf("SnoozedUntil = %v, want %v", got[0].SnoozedUntil, until)
	}
}

func TestStoreSnoozeErrorOnUnknownID(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	err := s.Snooze(ctx, 9999, time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("Snooze(unknown) must fail")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestStoreListFiltersBySeverity(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i, sv := range AllSeverities() {
		n := &Notification{
			ProjectID:   "a" + strings.Repeat("0", 63),
			Severity:    sv,
			EventType:   "x.y",
			ContentHash: mustHashFor(t, string(rune('a'+i))),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}
	urgent := SeverityUrgent
	got, err := s.List(ctx, ListFilter{Severity: &urgent})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent", got[0].Severity)
	}
}

func TestStoreListFiltersBySince(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		n := &Notification{
			ProjectID:   "a" + strings.Repeat("0", 63),
			Severity:    SeverityInfoImmediate,
			EventType:   "x.y",
			ContentHash: mustHashFor(t, string(rune('a'+i))),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   base.Add(time.Duration(i) * time.Hour),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	since := base.Add(2 * time.Hour)
	got, err := s.List(ctx, ListFilter{Since: &since})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestStoreListLimit(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		n := &Notification{
			ProjectID:   "a" + strings.Repeat("0", 63),
			Severity:    SeverityInfoDigest,
			EventType:   "x.y",
			ContentHash: mustHashFor(t, string(rune('a'+i))),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}
	got, err := s.List(ctx, ListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestStoreListSortsByCreatedAtDesc(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		n := &Notification{
			ProjectID:   "a" + strings.Repeat("0", 63),
			Severity:    SeverityInfoImmediate,
			EventType:   "x.y",
			ContentHash: mustHashFor(t, string(rune('a'+i))),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   base.Add(time.Duration(i) * time.Hour),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}
	got, err := s.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		if !got[i-1].CreatedAt.After(got[i].CreatedAt) {
			t.Errorf("expected DESC order: got[%d]=%v, got[%d]=%v",
				i-1, got[i-1].CreatedAt, i, got[i].CreatedAt)
		}
	}
}

func TestStoreDeleteCascadesProject(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	insert := func(pid string) {
		n := &Notification{
			ProjectID:   pid,
			Severity:    SeverityInfoImmediate,
			EventType:   "x.y",
			ContentHash: mustHashFor(t, pid),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC(),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert(%s): %v", pid, err)
		}
	}
	insert(pidA)
	insert(pidB)

	if err := s.Delete(ctx, pidA); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	gotA, _ := s.List(ctx, ListFilter{ProjectID: pidA})
	if len(gotA) != 0 {
		t.Errorf("after Delete: A len = %d, want 0", len(gotA))
	}
	gotB, _ := s.List(ctx, ListFilter{ProjectID: pidB})
	if len(gotB) != 1 {
		t.Errorf("after Delete: B len = %d, want 1 (cascade must NOT cross project)", len(gotB))
	}
}

func TestComputeContentHashStable(t *testing.T) {
	a := ComputeContentHash(map[string]any{"key": "value", "n": 42})
	b := ComputeContentHash(map[string]any{"n": 42, "key": "value"})
	if a != b {
		t.Errorf("ComputeContentHash not stable: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hash len = %d, want 64", len(a))
	}
}

func TestComputeContentHashDifferentInputs(t *testing.T) {
	a := ComputeContentHash(map[string]any{"key": "value"})
	b := ComputeContentHash(map[string]any{"key": "different"})
	if a == b {
		t.Error("hashes for different inputs should differ")
	}
}

func TestComputeContentHashEmpty(t *testing.T) {

	got := ComputeContentHash(map[string]any{})
	if len(got) != 64 {
		t.Errorf("hash len = %d, want 64", len(got))
	}
}

func TestComputeContentHashFallbackOnMarshalFailure(t *testing.T) {

	a := ComputeContentHash(map[string]any{"ch": make(chan int)})
	if len(a) != 64 {
		t.Errorf("fallback hash len = %d, want 64", len(a))
	}
}

var _ Store = (*memStore)(nil)
