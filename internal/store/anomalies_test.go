package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "anomalies.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRecordAnomalyInsertsThenIncrements(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordAnomaly("cache_metadata", "", 1700000000); err != nil {
		t.Fatalf("Record: %v", err)
	}
	rows, err := s.ListAnomalies(false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].FieldPath != "cache_metadata" {
		t.Errorf("FieldPath = %q", rows[0].FieldPath)
	}
	if rows[0].Count != 1 {
		t.Errorf("Count = %d, want 1", rows[0].Count)
	}
	if rows[0].FirstSeen != 1700000000 || rows[0].LastSeen != 1700000000 {
		t.Errorf("FirstSeen/LastSeen = %d/%d", rows[0].FirstSeen, rows[0].LastSeen)
	}

	if err := s.RecordAnomaly("cache_metadata", "", 1700000123); err != nil {
		t.Fatalf("Record 2: %v", err)
	}
	rows, _ = s.ListAnomalies(false)
	if rows[0].Count != 2 {
		t.Errorf("Count = %d, want 2", rows[0].Count)
	}
	if rows[0].FirstSeen != 1700000000 {
		t.Errorf("FirstSeen mutated: %d", rows[0].FirstSeen)
	}
	if rows[0].LastSeen != 1700000123 {
		t.Errorf("LastSeen = %d, want 1700000123", rows[0].LastSeen)
	}
}

func TestRecordAnomalyRejectsEmptyField(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordAnomaly("", "", 1700000000); err == nil {
		t.Error("expected error for empty fieldPath")
	}
}

func TestRecordAnomalyRejectsZeroTS(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordAnomaly("x", "", 0); err == nil {
		t.Error("expected error for ts=0")
	}
}

func TestQueryWindowReturnsCountAndTotal(t *testing.T) {
	s := newTestStore(t)

	now := int64(1700000000)

	for i := int64(0); i < 5; i++ {
		if err := s.RecordAnomaly("x", "", now-i*60); err != nil {
			t.Fatal(err)
		}
	}

	count, err := s.QueryAnomalyCount("x", now-24*3600, now)
	if err != nil {
		t.Fatalf("QueryAnomalyCount: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}

	count, _ = s.QueryAnomalyCount("x", now+1, now+10000)
	if count != 0 {
		t.Errorf("future window count = %d, want 0", count)
	}

	count, err = s.QueryAnomalyCount("unknown", now-24*3600, now)
	if err != nil {
		t.Fatalf("QueryAnomalyCount unknown: %v", err)
	}
	if count != 0 {
		t.Errorf("unknown field count = %d, want 0", count)
	}
}

func TestAcknowledgeAnomaly(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordAnomaly("signature", "", 1700000000); err != nil {
		t.Fatal(err)
	}
	if err := s.AcknowledgeAnomaly("signature"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	rows, _ := s.ListAnomalies(false)
	if len(rows) != 0 {
		t.Errorf("len = %d, want 0 (acknowledged hidden)", len(rows))
	}
	rows, _ = s.ListAnomalies(true)
	if len(rows) != 1 || !rows[0].Acknowledged {
		t.Errorf("rows = %#v", rows)
	}
}

func TestAcknowledgeAnomalyMissingRow(t *testing.T) {
	s := newTestStore(t)
	if err := s.AcknowledgeAnomaly("nope"); err == nil {
		t.Error("expected error for missing row")
	}
}

func TestIsAnomalyAcknowledged(t *testing.T) {
	s := newTestStore(t)

	ack, err := s.IsAnomalyAcknowledged("missing")
	if err != nil {
		t.Fatalf("IsAnomalyAcknowledged missing: %v", err)
	}
	if ack {
		t.Error("missing row must report unacknowledged")
	}

	if err := s.RecordAnomaly("x", "", 1700000000); err != nil {
		t.Fatal(err)
	}
	ack, err = s.IsAnomalyAcknowledged("x")
	if err != nil || ack {
		t.Errorf("unack: ack=%v err=%v", ack, err)
	}

	if err := s.AcknowledgeAnomaly("x"); err != nil {
		t.Fatal(err)
	}
	ack, err = s.IsAnomalyAcknowledged("x")
	if err != nil || !ack {
		t.Errorf("ack: ack=%v err=%v", ack, err)
	}
}

func TestUpdateAnomalyMetrics(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordAnomaly("cache_metadata", "", 1700000000); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAnomalyMetrics("cache_metadata", 100, 0.42); err != nil {
		t.Fatalf("UpdateAnomalyMetrics: %v", err)
	}
	rows, _ := s.ListAnomalies(false)
	if rows[0].TotalResponsesInWindow != 100 {
		t.Errorf("Total = %d", rows[0].TotalResponsesInWindow)
	}
	if rows[0].Percentage < 0.41 || rows[0].Percentage > 0.43 {
		t.Errorf("Percentage = %f", rows[0].Percentage)
	}
}

func TestListAnomaliesParentPath(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordAnomaly("usage.thinking_tokens", "usage", 1700000000); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListAnomalies(false)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].ParentPath != "usage" {
		t.Errorf("ParentPath = %q, want usage", rows[0].ParentPath)
	}
}

// TestRollingWindowDecaysCorrectly is the load-bearing regression test
// for I-1: the rolling-window numerator MUST be
// computed from per-event observations whose ts lies in the window,
// not from the row's lifetime count whenever last_seen is fresh. With
// the old implementation 1000 historic observations would have
// "leaked" into the window the moment the field was observed once
// inside it, producing a near-100% percentage and triggering a false
// notification storm. The fix counts observation rows directly.
func TestRollingWindowDecaysCorrectly(t *testing.T) {
	s := newTestStore(t)

	now := int64(1700000000)
	thirtyDaysAgo := now - 30*86400
	oneHourAgo := now - 3600

	for i := int64(0); i < 1000; i++ {
		if err := s.RecordAnomaly("cache_metadata", "", thirtyDaysAgo+i); err != nil {
			t.Fatalf("Record old: %v", err)
		}
	}

	if err := s.RecordAnomaly("cache_metadata", "", oneHourAgo); err != nil {
		t.Fatalf("Record fresh: %v", err)
	}

	count, err := s.QueryAnomalyCount("cache_metadata", now-86400, now)
	if err != nil {
		t.Fatalf("QueryAnomalyCount: %v", err)
	}
	if count != 1 {
		t.Errorf("rolling-24h count = %d, want 1 (1000 historic obs must NOT leak in)", count)
	}

	rows, err := s.ListAnomalies(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Count != 1001 {
		t.Errorf("lifetime count = %d, want 1001", rows[0].Count)
	}
}

func TestPurgeObservationsOlderThan24h(t *testing.T) {
	s := newTestStore(t)

	now := int64(1700000000)
	cutoff := now - 86400

	for _, ts := range []int64{cutoff - 3600, cutoff - 1800, cutoff - 60, cutoff - 1, cutoff - 7200} {
		if err := s.RecordAnomaly("foo", "", ts); err != nil {
			t.Fatalf("Record old: %v", err)
		}
	}
	for _, ts := range []int64{cutoff + 60, cutoff + 3600, now} {
		if err := s.RecordAnomaly("foo", "", ts); err != nil {
			t.Fatalf("Record fresh: %v", err)
		}
	}

	deleted, err := s.PurgeAnomalyObservationsOlderThan(cutoff)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if deleted != 5 {
		t.Errorf("deleted = %d, want 5", deleted)
	}

	count, err := s.QueryAnomalyCount("foo", cutoff, now)
	if err != nil {
		t.Fatalf("QueryAnomalyCount: %v", err)
	}
	if count != 3 {
		t.Errorf("post-purge count = %d, want 3", count)
	}

	rows, _ := s.ListAnomalies(true)
	if len(rows) != 1 || rows[0].Count != 8 {
		t.Errorf("aggregate count = %d, want 8 (purge must not touch aggregate)", rows[0].Count)
	}

	deleted, err = s.PurgeAnomalyObservationsOlderThan(cutoff)
	if err != nil {
		t.Fatalf("re-purge: %v", err)
	}
	if deleted != 0 {
		t.Errorf("re-purge deleted = %d, want 0", deleted)
	}
}

func TestParentPathPreservedOnFirstInsert(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordAnomaly("usage.thinking_tokens", "usage", 1700000000); err != nil {
		t.Fatalf("Record first: %v", err)
	}
	rows, _ := s.ListAnomalies(false)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ParentPath != "usage" {
		t.Errorf("first ParentPath = %q, want usage", rows[0].ParentPath)
	}

	if err := s.RecordAnomaly("usage.thinking_tokens", "", 1700000060); err != nil {
		t.Fatalf("Record second: %v", err)
	}
	rows, _ = s.ListAnomalies(false)
	if rows[0].ParentPath != "usage" {
		t.Errorf("after second insert ParentPath = %q, want usage (must not be clobbered)", rows[0].ParentPath)
	}
	if rows[0].Count != 2 {
		t.Errorf("Count = %d, want 2", rows[0].Count)
	}
}

func TestAnomalyMethodsAfterClose(t *testing.T) {
	// Close the DB then exercise every CRUD wrapper to cover the
	// error-path branches (DB.Exec/QueryRow returning sql.ErrConnDone
	// or similar). These are the lowest-value branches but we cover
	// them to lift the file above the 85% non-security-critical floor.
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "closed.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_ = s.Close()

	if err := s.RecordAnomaly("x", "", 1700000000); err == nil {
		t.Error("RecordAnomaly: expected error on closed DB")
	}
	if _, err := s.QueryAnomalyCount("x", 0, 1); err == nil {
		t.Error("QueryAnomalyCount: expected error on closed DB")
	}
	if _, err := s.ListAnomalies(false); err == nil {
		t.Error("ListAnomalies: expected error on closed DB")
	}
	if err := s.AcknowledgeAnomaly("x"); err == nil {
		t.Error("AcknowledgeAnomaly: expected error on closed DB")
	}
	if _, err := s.IsAnomalyAcknowledged("x"); err == nil {
		t.Error("IsAnomalyAcknowledged: expected error on closed DB")
	}
	if err := s.UpdateAnomalyMetrics("x", 1, 0.1); err == nil {
		t.Error("UpdateAnomalyMetrics: expected error on closed DB")
	}
	if _, err := s.PurgeAnomalyObservationsOlderThan(0); err == nil {
		t.Error("PurgeAnomalyObservationsOlderThan: expected error on closed DB")
	}
}
