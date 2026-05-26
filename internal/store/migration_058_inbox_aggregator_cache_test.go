package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func openMigratedInboxStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigration058InboxTableExists(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	var count int
	err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='inbox'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query inbox table: %v", err)
	}
	if count != 1 {
		t.Errorf("inbox table count = %d, want 1", count)
	}
}

func TestMigration058InboxAggregatorCacheTableExists(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	var count int
	err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='inbox_aggregator_cache'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query inbox_aggregator_cache table: %v", err)
	}
	if count != 1 {
		t.Errorf("inbox_aggregator_cache table count = %d, want 1", count)
	}
}

func TestMigration058InboxSeverityCheckRejectsInvalid(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	_, err := s.DB().Exec(
		`INSERT INTO inbox (project_id, severity, event_type, content_hash, payload, created_at, created_at_bucket)
		 VALUES (?,?,?,?,?,?,?)`,
		"a"+strings.Repeat("0", 63),
		"made-up-severity",
		"job.failed",
		strings.Repeat("a", 64),
		`{"x":1}`,
		1714560000,
		1714560000/300,
	)
	if err == nil {
		t.Fatal("INSERT with invalid severity must fail CHECK constraint")
	}
	if !strings.Contains(err.Error(), "CHECK") && !strings.Contains(err.Error(), "constraint") {
		t.Errorf("expected CHECK/constraint error, got: %v", err)
	}
}

func TestMigration058InboxSeverityAcceptsAllFour(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	tiers := []string{"urgent", "action-needed", "info-immediate", "info-digest"}
	for i, tier := range tiers {

		contentHash := strings.Repeat(byte4HexLetter(i), 64)
		createdAt := int64(1714560000 + i*301)
		_, err := s.DB().Exec(
			`INSERT INTO inbox (project_id, severity, event_type, content_hash, payload, created_at, created_at_bucket)
			 VALUES (?,?,?,?,?,?,?)`,
			"a"+strings.Repeat("0", 63),
			tier,
			"job.completed",
			contentHash,
			`{"x":1}`,
			createdAt,
			createdAt/300,
		)
		if err != nil {
			t.Errorf("tier %q: INSERT must succeed, got: %v", tier, err)
		}
	}
}

func byte4HexLetter(i int) string {
	return []string{"a", "b", "c", "d"}[i%4]
}

func TestMigration058InboxDedupUniqueRejectsDuplicate(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	insert := func(createdAt int64) error {
		_, err := s.DB().Exec(
			`INSERT INTO inbox (project_id, severity, event_type, content_hash, payload, created_at, created_at_bucket)
			 VALUES (?,?,?,?,?,?,?)`,
			"a"+strings.Repeat("0", 63),
			"info-immediate",
			"job.failed",
			strings.Repeat("a", 64),
			`{"x":1}`,
			createdAt,
			createdAt/300,
		)
		return err
	}

	if err := insert(1714560000); err != nil {
		t.Fatalf("first INSERT: %v", err)
	}

	if err := insert(1714560100); err == nil {
		t.Fatal("second INSERT in same bucket must fail UNIQUE")
	}

	if err := insert(1714560400); err != nil {
		t.Fatalf("INSERT in different bucket: %v", err)
	}
}

func TestMigration058AggregatorCacheProjectIDIndexed(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	var idxName string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master
		   WHERE type='index'
		     AND tbl_name='inbox_aggregator_cache'
		     AND sql LIKE '%project_id%'`,
	).Scan(&idxName)
	if err != nil {
		t.Fatalf("query index: %v", err)
	}
	if idxName == "" {
		t.Error("expected index on inbox_aggregator_cache.project_id")
	}
}

// TestMigration058AggregatorCacheUniqueRejectsDuplicateFanout verifies
// the (project_id, notification_id) UNIQUE constraint on
// inbox_aggregator_cache prevents duplicate fanout — same per-project
// inbox.id MUST NOT mirror twice into the cache. Outbox replays
// (Plan 7 Phase E-8) under at-least-once semantics depend on this for
// idempotency at the SQL layer (INSERT OR IGNORE pattern).
func TestMigration058AggregatorCacheUniqueRejectsDuplicateFanout(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	insert := func() error {
		_, err := s.DB().Exec(
			`INSERT INTO inbox_aggregator_cache
				(project_id, project_alias, notification_id, severity, event_type, content_hash, created_at)
			 VALUES (?,?,?,?,?,?,?)`,
			"a"+strings.Repeat("0", 63),
			"internal-platform-x",
			42,
			"info-immediate",
			"job.failed",
			strings.Repeat("a", 64),
			1714560000,
		)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("first INSERT: %v", err)
	}
	if err := insert(); err == nil {
		t.Fatal("duplicate (project_id, notification_id) INSERT must fail UNIQUE")
	}
}

// TestMigration058AggregatorCacheSeverityCheckRejectsInvalid verifies
// the cache's CHECK constraint mirrors the per-project inbox enum
// (inv-zen-124 mirror — denormalized fanout MUST agree on the typing
// surface).
func TestMigration058AggregatorCacheSeverityCheckRejectsInvalid(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	_, err := s.DB().Exec(
		`INSERT INTO inbox_aggregator_cache
			(project_id, project_alias, notification_id, severity, event_type, content_hash, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		"a"+strings.Repeat("0", 63),
		"internal-platform-x",
		1,
		"made-up-severity",
		"job.failed",
		strings.Repeat("a", 64),
		1714560000,
	)
	if err == nil {
		t.Fatal("aggregator_cache INSERT with invalid severity must fail CHECK")
	}
	if !strings.Contains(err.Error(), "CHECK") && !strings.Contains(err.Error(), "constraint") {
		t.Errorf("expected CHECK/constraint error, got: %v", err)
	}
}

func TestSchemaVersionIsAtLeast28(t *testing.T) {
	t.Parallel()
	if schemaVersion < 28 {
		t.Errorf("schemaVersion = %d, want >= 28 (Plan 7 Phase E-1 floor)", schemaVersion)
	}
}

func TestMigration058AppliedRecordsVersion28(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	var v int
	err := s.DB().QueryRow(
		`SELECT MAX(version) FROM schema_version`,
	).Scan(&v)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if v != schemaVersion {
		t.Errorf("schema_version MAX = %d, want %d (post-Migrate)", v, schemaVersion)
	}
	if v < 28 {
		t.Errorf("schema_version MAX = %d, want >= 28 (Plan 7 Phase E-1 must have applied 058)", v)
	}
}

func TestMigration058InboxUnackedPartialIndex(t *testing.T) {
	t.Parallel()
	s := openMigratedInboxStore(t)

	var idxSQL string
	err := s.DB().QueryRow(
		`SELECT sql FROM sqlite_master
		   WHERE type='index'
		     AND tbl_name='inbox'
		     AND name='idx_inbox_unacked'`,
	).Scan(&idxSQL)
	if err != nil {
		t.Fatalf("query idx_inbox_unacked: %v", err)
	}
	if !strings.Contains(idxSQL, "WHERE") || !strings.Contains(idxSQL, "acked_at IS NULL") {
		t.Errorf("idx_inbox_unacked must be partial WHERE acked_at IS NULL; got: %q", idxSQL)
	}
}
