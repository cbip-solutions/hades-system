//go:build cgo
// +build cgo

package caronteadapter

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newDaemonDB(t *testing.T, canonicalPath string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "daemon.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open daemon db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE projects_alias (
			id_sha256 TEXT PRIMARY KEY,
			alias TEXT NOT NULL,
			canonical_path TEXT NOT NULL,
			archived_at DATETIME
		)`)
	if err != nil {
		t.Fatalf("create projects_alias: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO projects_alias (id_sha256, alias, canonical_path) VALUES (?, ?, ?)`,
		"proj-1", "demo", canonicalPath,
	)
	if err != nil {
		t.Fatalf("seed projects_alias: %v", err)
	}
	return db
}

func TestOpenProjectDBCreatesCaronteDB(t *testing.T) {
	canonical := t.TempDir()
	a := NewAdapterFromDB(newDaemonDB(t, canonical))
	t.Cleanup(func() { _ = a.Close() })

	db, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OpenProjectDB: %v", err)
	}
	wantPath := filepath.Join(canonical, ".zen", "caronte.db")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("caronte.db not created at %s: %v", wantPath, err)
	}

	if _, err := db.Exec(`CREATE VIRTUAL TABLE probe_vec USING vec0(embedding float[4])`); err != nil {
		t.Errorf("vec0 not available on opened handle (sqlite-vec not registered): %v", err)
	}
}

func TestOpenProjectDBCachesByProjectID(t *testing.T) {
	a := NewAdapterFromDB(newDaemonDB(t, t.TempDir()))
	t.Cleanup(func() { _ = a.Close() })
	db1, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OpenProjectDB 1: %v", err)
	}
	db2, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OpenProjectDB 2: %v", err)
	}
	if db1 != db2 {
		t.Error("OpenProjectDB returned a different handle on second call; cache broken")
	}
}

func TestOpenProjectDBUnknownProject(t *testing.T) {
	a := NewAdapterFromDB(newDaemonDB(t, t.TempDir()))
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.OpenProjectDB(context.Background(), "nope"); err == nil {
		t.Error("OpenProjectDB(unknown) returned nil error")
	}
}

func TestOpenProjectDBWALPosture(t *testing.T) {
	a := NewAdapterFromDB(newDaemonDB(t, t.TempDir()))
	t.Cleanup(func() { _ = a.Close() })
	db, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OpenProjectDB: %v", err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q; want wal", mode)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1 (single-writer WAL)", got)
	}
}

func TestCloseDrainsCache(t *testing.T) {
	a := NewAdapterFromDB(newDaemonDB(t, t.TempDir()))
	if _, err := a.OpenProjectDB(context.Background(), "proj-1"); err != nil {
		t.Fatalf("OpenProjectDB: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := a.OpenProjectDB(context.Background(), "proj-1"); err != nil {
		t.Errorf("OpenProjectDB after Close: %v", err)
	}
	_ = a.Close()
}

func TestOpenProjectDBMkdirFail(t *testing.T) {

	canonical := t.TempDir()

	zenFile := filepath.Join(canonical, ".zen")
	if err := os.WriteFile(zenFile, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("setup: write blocker file: %v", err)
	}
	a := NewAdapterFromDB(newDaemonDB(t, canonical))
	t.Cleanup(func() { _ = a.Close() })

	_, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("OpenProjectDB on blocked .zen/ returned nil error")
	}
}

func TestCloseDrainsCacheWithAlreadyClosedHandle(t *testing.T) {
	a := NewAdapterFromDB(newDaemonDB(t, t.TempDir()))

	db, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OpenProjectDB: %v", err)
	}

	_ = db.Close()

	err = a.Close()

	_ = err
}

func TestOpenProjectDBConcurrentCachePath(t *testing.T) {
	canonical := t.TempDir()
	daemonDB := newDaemonDB(t, canonical)
	a := NewAdapterFromDB(daemonDB)
	t.Cleanup(func() { _ = a.Close() })

	const goroutines = 8
	results := make([]*sql.DB, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	barrier := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-barrier
			results[i], errs[i] = a.OpenProjectDB(context.Background(), "proj-1")
		}()
	}
	close(barrier)
	wg.Wait()

	var first *sql.DB
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: OpenProjectDB error: %v", i, err)
			continue
		}
		if first == nil {
			first = results[i]
		}
		if results[i] != first {
			t.Errorf("goroutine %d: got different handle (cache miss on concurrent path)", i)
		}
	}
}

func TestResolveProjectPathQueryError(t *testing.T) {
	daemonDB := newDaemonDB(t, t.TempDir())
	a := NewAdapterFromDB(daemonDB)

	_ = daemonDB.Close()
	_, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("expected error when daemonDB is closed, got nil")
	}
}

func TestOpenProjectDBPingFail(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod restriction bypass skipped")
	}
	canonical := t.TempDir()
	zenDir := filepath.Join(canonical, ".zen")
	if err := os.MkdirAll(zenDir, 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	if err := os.Chmod(zenDir, 0o500); err != nil {
		t.Fatalf("setup chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(zenDir, 0o700) })

	a := NewAdapterFromDB(newDaemonDB(t, canonical))
	t.Cleanup(func() { _ = a.Close() })
	_, err := a.OpenProjectDB(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("expected error with read-only .zen dir, got nil")
	}
}
