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

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestInvZen263_PerRepoConfidenceTierSchemaWitness(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "internal", "caronte", "store", "schema.go")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read schema.go: %v", err)
	}
	body := string(raw)
	const checkClause = "CHECK (confidence IN ('exact_proto_import','spec_artifact','static_path','fuzzy_path'))"
	if !strings.Contains(body, checkClause) {
		t.Errorf("inv-zen-263 (per-repo): schema.go does NOT contain the api_calls.confidence CHECK clause:\n  want: %s", checkClause)
	}
	for _, tier := range []string{"exact_proto_import", "spec_artifact", "static_path", "fuzzy_path"} {
		if !strings.Contains(body, tier) {
			t.Errorf("inv-zen-263 (per-repo): C-5 tier %q absent from schema.go", tier)
		}
	}
}

func TestInvZen263_PerRepoConfidenceTierCheckConstraint(t *testing.T) {
	db := openCaronteTestDB(t)
	defer db.Close()
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	_, err = s.DB().Exec(`
		INSERT INTO api_calls
			(call_id, repo, caller_node_id, confidence, extracted_at, extractor_id)
		VALUES ('c1', 'r1', 'n1', 'forged-tier', 1, 'x')`)
	if err == nil {
		t.Fatal("inv-zen-263 (per-repo): CHECK refused did NOT fire on forged confidence; want error")
	}
	// Defence: even on unexpected error shape, the row MUST NOT persist.
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM api_calls`).Scan(&n); err != nil {
		t.Fatalf("count api_calls: %v", err)
	}
	if n != 0 {
		t.Errorf("api_calls rows after refused INSERT = %d; want 0", n)
	}
}

func TestInvZen263_APIEndpointKindCheckConstraint(t *testing.T) {
	db := openCaronteTestDB(t)
	defer db.Close()
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	_, err = s.DB().Exec(`
		INSERT INTO api_endpoints
			(endpoint_id, repo, kind, handler_node_id, extracted_at, extractor_id)
		VALUES ('e1', 'r1', 'forged-kind', 'n1', 1, 'x')`)
	if err == nil {
		t.Fatal("inv-zen-263 (per-repo): CHECK on api_endpoints.kind did NOT fire on forged kind; want error")
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM api_endpoints`).Scan(&n); err != nil {
		t.Fatalf("count api_endpoints: %v", err)
	}
	if n != 0 {
		t.Errorf("api_endpoints rows after refused INSERT = %d; want 0", n)
	}
}

func openCaronteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v", err)
	}
	return db
}
