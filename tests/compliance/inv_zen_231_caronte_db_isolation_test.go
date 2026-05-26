//go:build cgo
// +build cgo

package compliance

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/caronteadapter"
)

func seedDaemonDB(t *testing.T, pathA, pathB string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "daemon.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open daemon db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE projects_alias (
		id_sha256 TEXT PRIMARY KEY, alias TEXT NOT NULL,
		canonical_path TEXT NOT NULL, archived_at DATETIME)`); err != nil {
		t.Fatalf("create projects_alias: %v", err)
	}
	for _, p := range []struct{ id, path string }{{"proj-a", pathA}, {"proj-b", pathB}} {
		if _, err := db.Exec(
			`INSERT INTO projects_alias (id_sha256, alias, canonical_path) VALUES (?, ?, ?)`,
			p.id, p.id, p.path,
		); err != nil {
			t.Fatalf("seed %s: %v", p.id, err)
		}
	}
	return db
}

func TestInvZen231DistinctProjectsDistinctDBs(t *testing.T) {
	pathA, pathB := t.TempDir(), t.TempDir()
	a := caronteadapter.NewAdapterFromDB(seedDaemonDB(t, pathA, pathB))
	t.Cleanup(func() { _ = a.Close() })
	ctx := context.Background()

	dbA, err := a.OpenProjectDB(ctx, "proj-a")
	if err != nil {
		t.Fatalf("OpenProjectDB A: %v", err)
	}
	dbB, err := a.OpenProjectDB(ctx, "proj-b")
	if err != nil {
		t.Fatalf("OpenProjectDB B: %v", err)
	}
	if dbA == dbB {
		t.Fatal("inv-zen-231 violated: two projects share one *sql.DB handle")
	}

	fileA := filepath.Join(pathA, ".zen", "caronte.db")
	fileB := filepath.Join(pathB, ".zen", "caronte.db")
	if _, err := os.Stat(fileA); err != nil {
		t.Errorf("project A caronte.db missing: %v", err)
	}
	if _, err := os.Stat(fileB); err != nil {
		t.Errorf("project B caronte.db missing: %v", err)
	}

	storeA, err := store.Open(ctx, dbA)
	if err != nil {
		t.Fatalf("store.Open A: %v", err)
	}
	storeB, err := store.Open(ctx, dbB)
	if err != nil {
		t.Fatalf("store.Open B: %v", err)
	}
	n := store.Node{
		NodeID: "pkg/secret.Token", Name: "Token", Kind: string(store.KindFunction),
		Language: "go", FilePath: "secret.go", ContentHash: "h",
	}
	if err := storeA.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode into A: %v", err)
	}
	if _, err := storeB.GetNode(ctx, "pkg/secret.Token"); err == nil {
		t.Error("inv-zen-231 violated: project A's node is visible in project B's store")
	}
}

func TestInvZen231StoreHasNoCrossProjectSurface(t *testing.T) {
	root := repoRoot(t)
	storeDir := filepath.Join(root, "internal", "caronte", "store")
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("inv-zen-231: ReadDir store: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(storeDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		body := string(src)

		if strings.Contains(body, "sql.Open(") {
			t.Errorf("inv-zen-231: %s calls sql.Open — the store must wrap an "+
				"injected *sql.DB; file open belongs to caronteadapter only", e.Name())
		}

		if strings.Contains(body, "projectA") || strings.Contains(body, "projectB") ||
			strings.Contains(body, "otherProjectID") {
			t.Errorf("inv-zen-231: %s appears to expose a cross-project surface", e.Name())
		}
	}
	if scanned == 0 {
		t.Fatal("inv-zen-231: sentinel failure — 0 production Go files scanned in store package")
	}
}
