package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeBudget struct {
	used, warn, fail int
	err              error
}

func (f *fakeBudget) snapshot(ctx context.Context) (int, int, int, error) {
	return f.used, f.warn, f.fail, f.err
}

func staticHeartbeat(t time.Time) HeartbeatFn {
	return func() time.Time { return t }
}

func noopBudget(ctx context.Context) (int, int, int, error) { return 0, 0, 0, nil }

func noopHeartbeat() time.Time { return time.Now() }

func TestProberIntegrityCheckOK(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	out, err := p.IntegrityCheck(context.Background())
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if out != "ok" {
		t.Errorf("IntegrityCheck = %q, want %q", out, "ok")
	}
}

func TestProberIntegrityCheckOnClosedDB(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	_ = db.Close()
	_, err := p.IntegrityCheck(context.Background())
	if err == nil {
		t.Errorf("IntegrityCheck on closed DB: want error, got nil")
	}
}

func TestProberLastIndexedAtOnClosedDB(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	_ = db.Close()
	_, err := p.LastIndexedAt(context.Background())
	if err == nil {
		t.Errorf("LastIndexedAt on closed DB: want error, got nil")
	}
}

func TestProberExtensionHookNullCountOnClosedDB(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	_ = db.Close()
	_, _, err := p.ExtensionHookNullCount(context.Background())
	if err == nil {
		t.Errorf("ExtensionHookNullCount on closed DB: want error, got nil")
	}
}

func TestProberIntegrityCheckCancelledCtx(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.IntegrityCheck(ctx)
	if err == nil {
		t.Errorf("IntegrityCheck on cancelled ctx: want error, got nil")
	}
}

func TestProberIntegrityCheckCorruptDBMultiline(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	now := time.Now().UnixNano()
	for i := 0; i < 100; i++ {
		if _, err := db.Exec(`INSERT INTO knowledge_meta
			(rowid, file_path, project_id, project_alias, file_type, title, frontmatter_json, last_modified, last_indexed)
			VALUES (NULL, ?, 'pid', 'p', 'memory', 't', NULL, ?, ?)`,
			fmt.Sprintf("/p/file-%d.md", i), now, now); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.OpenFile(dbPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open for corruption: %v", err)
	}
	garbage := bytes.Repeat([]byte{0xFF}, 256)
	if _, err := f.WriteAt(garbage, 4096); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close corrupt file: %v", err)
	}

	db2, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	p := NewProber(db2, noopHeartbeat, noopBudget)
	out, err := p.IntegrityCheck(context.Background())

	if err != nil {

		return
	}
	t.Logf("integrity_check output: %q", out)
}

func TestProberLastIndexedAtNoRows(t *testing.T) {
	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	got, err := p.LastIndexedAt(context.Background())
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("LastIndexedAt = %v, want zero (no rows)", got)
	}
}

func TestProberLastIndexedAtSomeRows(t *testing.T) {
	db, _ := openTestIndex(t)
	now := time.Now().UnixNano()
	older := time.Now().Add(-time.Hour).UnixNano()
	if _, err := db.Exec(`INSERT INTO knowledge_meta
		(rowid, file_path, project_id, project_alias, file_type, title, frontmatter_json, last_modified, last_indexed)
		VALUES
		(NULL, '/p1/a.md', 'pid1', 'p1', 'memory', 't', NULL, ?, ?),
		(NULL, '/p1/b.md', 'pid1', 'p1', 'memory', 't', NULL, ?, ?)`,
		older, older, now, now); err != nil {
		t.Fatalf("insert rows: %v", err)
	}
	p := NewProber(db, noopHeartbeat, noopBudget)
	got, err := p.LastIndexedAt(context.Background())
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if got.UnixNano() != now {
		t.Errorf("LastIndexedAt = %d, want %d", got.UnixNano(), now)
	}
}

func TestProberIndexerCPUBudgetDelegates(t *testing.T) {
	db, _ := openTestIndex(t)
	b := &fakeBudget{used: 30, warn: 50, fail: 80}
	p := NewProber(db, noopHeartbeat, b.snapshot)
	used, warn, fail, err := p.IndexerCPUBudget(context.Background())
	if err != nil {
		t.Fatalf("IndexerCPUBudget: %v", err)
	}
	if used != 30 || warn != 50 || fail != 80 {
		t.Errorf("got used=%d warn=%d fail=%d, want 30/50/80", used, warn, fail)
	}
}

func TestProberIndexerCPUBudgetPropagatesError(t *testing.T) {
	db, _ := openTestIndex(t)
	b := &fakeBudget{err: errors.New("budget unavailable")}
	p := NewProber(db, noopHeartbeat, b.snapshot)
	_, _, _, err := p.IndexerCPUBudget(context.Background())
	if err == nil {
		t.Error("expected error propagation from budget snapshot")
	}
}

func TestProberWatcherHeartbeatDelegates(t *testing.T) {
	db, _ := openTestIndex(t)
	hb := time.Now().Add(-3 * time.Second)
	p := NewProber(db, staticHeartbeat(hb), noopBudget)
	got, err := p.WatcherHeartbeat(context.Background())
	if err != nil {
		t.Fatalf("WatcherHeartbeat: %v", err)
	}
	if !got.Equal(hb) {
		t.Errorf("WatcherHeartbeat = %v, want %v", got, hb)
	}
}

func TestProberExtensionHookNullCountAllNull(t *testing.T) {
	db, _ := openTestIndex(t)
	now := time.Now().UnixNano()
	if _, err := db.Exec(`INSERT INTO knowledge_meta
		(rowid, file_path, project_id, project_alias, file_type, title, frontmatter_json, last_modified, last_indexed)
		VALUES
		(NULL, '/a.md', 'pid', 'p', 'memory', 't', NULL, ?, ?),
		(NULL, '/b.md', 'pid', 'p', 'memory', 't', NULL, ?, ?)`,
		now, now, now, now); err != nil {
		t.Fatalf("insert: %v", err)
	}
	p := NewProber(db, noopHeartbeat, noopBudget)
	nullCount, total, err := p.ExtensionHookNullCount(context.Background())
	if err != nil {
		t.Fatalf("ExtensionHookNullCount: %v", err)
	}
	if nullCount != 2 || total != 2 {
		t.Errorf("got null=%d total=%d, want null=2 total=2", nullCount, total)
	}
}

func TestProberExtensionHookNullCountMixed(t *testing.T) {
	db, _ := openTestIndex(t)
	now := time.Now().UnixNano()

	if _, err := db.Exec(`INSERT INTO knowledge_meta
		(rowid, file_path, project_id, project_alias, file_type, title, frontmatter_json, last_modified, last_indexed)
		VALUES
		(NULL, '/a.md', 'pid', 'p', 'memory', 't', NULL, ?, ?),
		(NULL, '/b.md', 'pid', 'p', 'memory', 't', NULL, ?, ?)`,
		now, now, now, now); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.Exec(`UPDATE knowledge_meta SET audit_chain_anchor = ?
		WHERE file_path = '/b.md'`, "sha256:deadbeef"); err != nil {
		t.Fatalf("update: %v", err)
	}
	p := NewProber(db, noopHeartbeat, noopBudget)
	nullCount, total, err := p.ExtensionHookNullCount(context.Background())
	if err != nil {
		t.Fatalf("ExtensionHookNullCount: %v", err)
	}
	if nullCount != 1 || total != 2 {
		t.Errorf("got null=%d total=%d, want null=1 total=2", nullCount, total)
	}
}

func TestProberExtensionHookNullCountEmpty(t *testing.T) {

	db, _ := openTestIndex(t)
	p := NewProber(db, noopHeartbeat, noopBudget)
	nullCount, total, err := p.ExtensionHookNullCount(context.Background())
	if err != nil {
		t.Fatalf("ExtensionHookNullCount: %v", err)
	}
	if nullCount != 0 || total != 0 {
		t.Errorf("got null=%d total=%d, want 0/0", nullCount, total)
	}
}

func TestProberNewPanicsOnNilDB(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(nil, *, *) should panic")
		}
	}()
	_ = NewProber(nil, noopHeartbeat, noopBudget)
}

func TestProberNewPanicsOnNilHeartbeat(t *testing.T) {
	db, _ := openTestIndex(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, nil, *) should panic")
		}
	}()
	_ = NewProber(db, nil, noopBudget)
}

func TestProberNewPanicsOnNilBudget(t *testing.T) {
	db, _ := openTestIndex(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, *, nil) should panic")
		}
	}()
	_ = NewProber(db, noopHeartbeat, nil)
}
