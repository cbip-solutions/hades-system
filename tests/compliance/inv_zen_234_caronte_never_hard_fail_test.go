//go:build cgo
// +build cgo

package compliance

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func openCaronteDBForInv234(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	db, err := sql.Open(store.DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

type absentSCIPRunner struct{}

func (absentSCIPRunner) Available(semantic.IndexerKind) bool { return false }

func (absentSCIPRunner) Index(context.Context, semantic.IndexerKind, string) ([]byte, error) {
	return nil, nil
}

func TestInvZen234GoBuildBrokenFallsBackNeverHardFails(t *testing.T) {
	s := openCaronteDBForInv234(t)
	r := semantic.NewResolver(s, nil, semantic.ResolverOpts{})
	brokenDir := filepath.Join(repoRoot(t), "internal", "caronte", "semantic", "testdata", "broken")
	stats, err := r.ResolveProject(context.Background(), "proj-broken", brokenDir)
	if err != nil {
		t.Fatalf("inv-zen-234 VIOLATION: ResolveProject on a build-broken Go project hard-failed: %v (must classify to CHA, never error)", err)
	}
	if stats.Mode != semantic.ModeCHA && stats.Mode != semantic.ModeStaleSnapshot {
		t.Errorf("inv-zen-234: build-broken Mode = %q; want cha or stale_snapshot (degraded, never vta)", stats.Mode)
	}
}

func TestInvZen234MultiLangIndexerAbsentDegradesToHeuristic(t *testing.T) {
	s := openCaronteDBForInv234(t)
	ctx := context.Background()

	seed := []store.Node{
		{NodeID: "m.R", Name: "R", Kind: string(store.KindInterface), Language: "typescript", FilePath: "m.ts", ContentHash: "a"},
		{NodeID: "m.W", Name: "W", Kind: string(store.KindStruct), Language: "typescript", FilePath: "m.ts", ContentHash: "b"},
		{NodeID: "m.R.f", Name: "f", Kind: string(store.KindField), Language: "typescript", FilePath: "m.ts", ContentHash: "c"},
		{NodeID: "m.W.f", Name: "f", Kind: string(store.KindMethod), Language: "typescript", FilePath: "m.ts", ContentHash: "d"},
	}
	for _, n := range seed {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.NodeID, err)
		}
	}
	r := semantic.NewMultiLangResolver(s, absentSCIPRunner{}, nil, semantic.MultiLangOpts{})
	stats, err := r.ResolveLanguage(ctx, "proj-ts", "/repo", "typescript")
	if err != nil {
		t.Fatalf("inv-zen-234 VIOLATION: ResolveLanguage with absent indexer hard-failed: %v (must degrade to heuristic)", err)
	}
	if stats.Mode != semantic.ModeHeuristic {
		t.Errorf("inv-zen-234: indexer-absent Mode = %q; want heuristic", stats.Mode)
	}
	if stats.HeuristicEdges == 0 {
		t.Error("inv-zen-234: degraded resolution produced no edges; a degraded graph must still be served")
	}
}

func TestInvZen234NilRunnerNeverPanics(t *testing.T) {
	s := openCaronteDBForInv234(t)
	r := semantic.NewMultiLangResolver(s, nil, nil, semantic.MultiLangOpts{})
	_, err := r.ResolveLanguage(context.Background(), "p", "/repo", "rust")
	if err != nil {
		t.Fatalf("inv-zen-234 VIOLATION: nil runner hard-failed: %v", err)
	}
}
