package inbox

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const inboxProberDaemonSchema = `
CREATE TABLE inbox_aggregator_cache (
    cache_id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id        TEXT NOT NULL,
    project_alias     TEXT NOT NULL,
    notification_id   INTEGER NOT NULL,
    severity          TEXT NOT NULL CHECK (severity IN (
                          'urgent','action-needed','info-immediate','info-digest'
                      )),
    event_type        TEXT NOT NULL,
    content_hash      TEXT NOT NULL,
    created_at        INTEGER NOT NULL,
    acked_at          INTEGER,
    UNIQUE (project_id, notification_id)
);
`

const inboxProberPerProjectSchema = `
CREATE TABLE inbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id        TEXT NOT NULL,
    severity          TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    content_hash      TEXT NOT NULL,
    payload           TEXT NOT NULL DEFAULT '{}',
    created_at        INTEGER NOT NULL,
    created_at_bucket INTEGER NOT NULL,
    acked_at          INTEGER,
    snoozed_until     INTEGER
);
`

func newInboxProberDB(t *testing.T) (*sql.DB, PerProjectDBOpenerFn) {
	t.Helper()
	dir := t.TempDir()
	daemonPath := filepath.Join(dir, "daemon.db")
	daemon, err := sql.Open("sqlite3_ncruces", "file:"+daemonPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open daemon: %v", err)
	}
	t.Cleanup(func() { daemon.Close() })
	if _, err := daemon.Exec(inboxProberDaemonSchema); err != nil {
		t.Fatalf("daemon schema: %v", err)
	}
	perProjectDBs := map[string]*sql.DB{}
	t.Cleanup(func() {
		for _, db := range perProjectDBs {
			db.Close()
		}
	})
	opener := func(ctx context.Context, alias string) (*sql.DB, error) {
		if db, ok := perProjectDBs[alias]; ok {
			return db, nil
		}
		path := filepath.Join(dir, "project-"+alias+".db")
		db, err := sql.Open("sqlite3_ncruces", "file:"+path)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(inboxProberPerProjectSchema); err != nil {
			db.Close()
			return nil, err
		}
		perProjectDBs[alias] = db
		return db, nil
	}
	return daemon, opener
}

func staticOutboxPending(n int) OutboxPendingFn {
	return func(ctx context.Context) (int, error) { return n, nil }
}

func errOutboxPending(err error) OutboxPendingFn {
	return func(ctx context.Context) (int, error) { return 0, err }
}

func insertCacheRow(t *testing.T, db *sql.DB, alias string, projectID, fingerprint string, sev string, createdAt int64, notifID int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO inbox_aggregator_cache
		(project_id, project_alias, notification_id, severity, event_type, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		projectID, alias, notifID, sev, "test.event", fingerprint, createdAt)
	if err != nil {
		t.Fatalf("insert cache row: %v", err)
	}
}

func insertPerProjectRow(t *testing.T, db *sql.DB, projectID, fingerprint string, sev string, createdAt int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO inbox
		(project_id, severity, event_type, content_hash, created_at, created_at_bucket)
		VALUES (?, ?, 'test.event', ?, ?, ?)`,
		projectID, sev, fingerprint, createdAt, createdAt/300)
	if err != nil {
		t.Fatalf("insert per-project row: %v", err)
	}
}

func TestProberAggregatorCacheConsistent(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f2", "action-needed", now, 2)
	pdb, _ := opener(context.Background(), "internal-platform-x")
	insertPerProjectRow(t, pdb, "pid-a", "f1", "info-immediate", now)
	insertPerProjectRow(t, pdb, "pid-a", "f2", "action-needed", now)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	consistent, drift, _, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}
	if !consistent || drift != 0 {
		t.Errorf("got consistent=%v drift=%d, want true / 0", consistent, drift)
	}
}

func TestProberAggregatorCacheDrift(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f2", "action-needed", now, 2)
	pdb, _ := opener(context.Background(), "internal-platform-x")
	insertPerProjectRow(t, pdb, "pid-a", "f1", "info-immediate", now)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	consistent, drift, detail, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}
	if consistent {
		t.Error("expected inconsistent")
	}
	if drift != 1 {
		t.Errorf("drift=%d, want 1", drift)
	}
	if detail == "" {
		t.Error("expected detail to include mismatch")
	}
}

func TestProberAggregatorCachePerProjectUnreachable(t *testing.T) {
	daemon, _ := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f2", "info-immediate", now, 2)
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f3", "info-immediate", now, 3)

	failingOpener := func(ctx context.Context, alias string) (*sql.DB, error) {
		return nil, errors.New("project DB locked")
	}

	p := NewProber(daemon, failingOpener, staticOutboxPending(0))
	consistent, drift, detail, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}
	if consistent {
		t.Error("expected inconsistent")
	}

	if drift != 3 {
		t.Errorf("drift=%d, want 3 (full cache count)", drift)
	}
	if detail == "" || !contains(detail, "unreachable") {
		t.Errorf("detail should mention unreachable: %q", detail)
	}
}

func TestProberAggregatorCacheNoProjects(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, staticOutboxPending(0))
	consistent, drift, _, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}
	if !consistent || drift != 0 {
		t.Errorf("got consistent=%v drift=%d, want true / 0 (empty cache)", consistent, drift)
	}
}

func TestProberOutboxQueueDepth(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, staticOutboxPending(7))
	n, err := p.OutboxQueueDepth(context.Background())
	if err != nil {
		t.Fatalf("OutboxQueueDepth: %v", err)
	}
	if n != 7 {
		t.Errorf("depth=%d, want 7", n)
	}
}

func TestProberOutboxQueueDepthError(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, errOutboxPending(errors.New("bridge offline")))
	_, err := p.OutboxQueueDepth(context.Background())
	if err == nil {
		t.Error("expected error")
	}
}

func TestProberDedupConstraintNoViolations(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "a", "p", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "a", "p", "f2", "info-immediate", now, 2)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	n, err := p.DedupConstraintViolations(context.Background())
	if err != nil {
		t.Fatalf("DedupConstraintViolations: %v", err)
	}
	if n != 0 {
		t.Errorf("violations=%d, want 0", n)
	}
}

func TestProberDedupConstraintWithViolations(t *testing.T) {

	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	bucket := now / 300
	bucketTime := bucket * 300
	insertCacheRow(t, daemon, "a", "pa", "samehash", "info-immediate", bucketTime, 1)
	insertCacheRow(t, daemon, "a", "pa", "samehash", "info-immediate", bucketTime+1, 2)
	insertCacheRow(t, daemon, "a", "pa", "samehash", "info-immediate", bucketTime+2, 3)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	n, err := p.DedupConstraintViolations(context.Background())
	if err != nil {
		t.Fatalf("DedupConstraintViolations: %v", err)
	}

	if n != 1 {
		t.Errorf("violations=%d, want 1 group with multiplicity>1", n)
	}
}

func TestProberSeverityDistribution24h(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "a", "pa", "f1", "urgent", now, 1)
	insertCacheRow(t, daemon, "a", "pa", "f2", "urgent", now, 2)
	insertCacheRow(t, daemon, "a", "pa", "f3", "info-immediate", now, 3)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	dist, urgent, err := p.SeverityDistribution24h(context.Background())
	if err != nil {
		t.Fatalf("SeverityDistribution24h: %v", err)
	}
	if urgent != 2 {
		t.Errorf("urgent=%d, want 2", urgent)
	}
	if dist["info-immediate"] != 1 {
		t.Errorf("info-immediate=%d, want 1", dist["info-immediate"])
	}
}

func TestProberSeverityDistribution24hExcludesOlder(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()
	older := time.Now().Add(-25 * time.Hour).Unix()
	insertCacheRow(t, daemon, "a", "pa", "f1", "urgent", now, 1)
	insertCacheRow(t, daemon, "a", "pa", "f2", "urgent", older, 2)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	_, urgent, err := p.SeverityDistribution24h(context.Background())
	if err != nil {
		t.Fatalf("SeverityDistribution24h: %v", err)
	}
	if urgent != 1 {
		t.Errorf("urgent=%d, want 1 (older row excluded)", urgent)
	}
}

func TestProberNewPanicsOnNilDaemonDB(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(nil, *, *) should panic")
		}
	}()
	_ = NewProber(nil,
		func(context.Context, string) (*sql.DB, error) { return nil, nil },
		staticOutboxPending(0),
	)
}

func TestProberNewPanicsOnNilOpener(t *testing.T) {
	daemon, _ := newInboxProberDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, nil, *) should panic")
		}
	}()
	_ = NewProber(daemon, nil, staticOutboxPending(0))
}

func TestProberNewPanicsOnNilOutboxPending(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, *, nil) should panic")
		}
	}()
	_ = NewProber(daemon, opener, nil)
}

func TestProberAggregatorCacheConsistentDaemonClosed(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, staticOutboxPending(0))
	_ = daemon.Close()
	_, _, _, err := p.AggregatorCacheConsistent(context.Background())
	if err == nil {
		t.Error("expected error on closed daemon DB")
	}
}

func TestProberAggregatorCacheConsistentSortsByDrift(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()

	insertCacheRow(t, daemon, "a", "pa", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "a", "pa", "f2", "info-immediate", now, 2)
	insertCacheRow(t, daemon, "a", "pa", "f3", "info-immediate", now, 3)
	insertCacheRow(t, daemon, "b", "pb", "f4", "info-immediate", now, 4)
	insertCacheRow(t, daemon, "b", "pb", "f5", "info-immediate", now, 5)

	pdb, _ := opener(context.Background(), "b")
	insertPerProjectRow(t, pdb, "pb", "f4", "info-immediate", now)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	_, _, detail, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}

	if !contains(detail, "a:") {
		t.Errorf("detail missing project a: %q", detail)
	}
	aIdx := indexOf(detail, "a:")
	bIdx := indexOf(detail, "b:")
	if aIdx > bIdx {
		t.Errorf("expected a (drift=3) sorted before b (drift=1); got %q", detail)
	}
}

func TestProberAggregatorCacheConsistentSortAlphaTiebreak(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	now := time.Now().Unix()

	insertCacheRow(t, daemon, "zee", "pz", "f1", "info-immediate", now, 1)
	insertCacheRow(t, daemon, "alfa", "pa", "f2", "info-immediate", now, 2)

	p := NewProber(daemon, opener, staticOutboxPending(0))
	_, _, detail, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}

	alfaIdx := indexOf(detail, "alfa:")
	zeeIdx := indexOf(detail, "zee:")
	if alfaIdx == -1 || zeeIdx == -1 {
		t.Errorf("detail missing both projects: %q", detail)
		return
	}
	if alfaIdx > zeeIdx {
		t.Errorf("expected alfa before zee on alpha tiebreak; got %q", detail)
	}
}

func TestProberAggregatorCacheConsistentNilDBFromOpener(t *testing.T) {
	daemon, _ := newInboxProberDB(t)
	now := time.Now().Unix()
	insertCacheRow(t, daemon, "internal-platform-x", "pid-a", "f1", "info-immediate", now, 1)

	nilOpener := func(ctx context.Context, alias string) (*sql.DB, error) {
		return nil, nil
	}
	p := NewProber(daemon, nilOpener, staticOutboxPending(0))
	consistent, drift, _, err := p.AggregatorCacheConsistent(context.Background())
	if err != nil {
		t.Fatalf("AggregatorCacheConsistent: %v", err)
	}
	if consistent {
		t.Error("expected inconsistent")
	}

	if drift != 1 {
		t.Errorf("drift=%d, want 1", drift)
	}
}

func TestProberDedupConstraintViolationsClosedDB(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, staticOutboxPending(0))
	_ = daemon.Close()
	_, err := p.DedupConstraintViolations(context.Background())
	if err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestProberSeverityDistribution24hClosedDB(t *testing.T) {
	daemon, opener := newInboxProberDB(t)
	p := NewProber(daemon, opener, staticOutboxPending(0))
	_ = daemon.Close()
	_, _, err := p.SeverityDistribution24h(context.Background())
	if err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestJoinLinesEmpty(t *testing.T) {
	if got := joinLines(nil); got != "" {
		t.Errorf("joinLines(nil) = %q, want empty", got)
	}
	if got := joinLines([]string{}); got != "" {
		t.Errorf("joinLines([]) = %q, want empty", got)
	}
}

func TestJoinLinesSingle(t *testing.T) {
	if got := joinLines([]string{"abc"}); got != "abc" {
		t.Errorf("joinLines = %q, want %q", got, "abc")
	}
}

func TestAbsNegative(t *testing.T) {
	if got := abs(-5); got != 5 {
		t.Errorf("abs(-5) = %d, want 5", got)
	}
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
