// go:build cgo
//go:build cgo
// +build cgo

package intent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func stalenessFixture(t *testing.T, adrTouched, nodeFileTouched int64) (*store.Store, *StalenessChecker, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: "internal/caronte/intent/getwhy.go",
		PackageID: "internal/caronte/intent", ContentHash: "current-hash",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID, PackageID: n.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	prober := fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md":    adrTouched,
		"internal/caronte/intent/getwhy.go": nodeFileTouched,
	}}
	checker := NewStalenessChecker(s, "/repo", prober)
	return s, checker, ctx
}

func TestStaleFlipsWhenCodeNewerThanADR(t *testing.T) {
	s, checker, ctx := stalenessFixture(t, 1000, 2000)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if len(links) != 1 || !links[0].Stale {
		t.Errorf("link not flagged stale though code is newer than ADR: %+v", links)
	}
}

func TestNotStaleWhenADRNewerThanCode(t *testing.T) {
	s, checker, ctx := stalenessFixture(t, 3000, 2000)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if len(links) != 1 || links[0].Stale {
		t.Errorf("link wrongly flagged stale though ADR is newer: %+v", links)
	}
}

func TestStaleClearsWhenADRReTouched(t *testing.T) {
	s, checker, ctx := stalenessFixture(t, 1000, 2000)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute 1: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if !links[0].Stale {
		t.Fatal("precondition: link should be stale after first recompute")
	}

	checker.git = fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md":    4000,
		"internal/caronte/intent/getwhy.go": 2000,
	}}
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute 2: %v", err)
	}
	links, _ = s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if links[0].Stale {
		t.Errorf("stale not cleared after ADR re-touch: %+v", links)
	}
}

func TestStalenessSkipsCoverageManifestLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: "", PackageID: "internal/caronte/intent",
		LinkKind: string(store.LinkCoverageManifest), Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatal(err)
	}
	checker := NewStalenessChecker(s, "/repo", fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md": 1000,
	}})
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}

	var stale int
	_ = s.DB().QueryRowContext(ctx,
		`SELECT stale FROM adr_links WHERE link_kind = ? AND package_id = ?`,
		string(store.LinkCoverageManifest), "internal/caronte/intent",
	).Scan(&stale)
	if stale != 0 {
		t.Errorf("coverage_manifest link was staled (stale=%d); want 0", stale)
	}
}

func TestStalenessGitUnreachableFallsBackToMtime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{NodeID: "internal/x.F", Name: "F", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/x/f.go", PackageID: "internal/x", ContentHash: "h"}
	_ = s.UpsertNode(ctx, n)
	_ = s.UpsertADRLink(ctx, store.ADRLink{ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID, PackageID: n.PackageID, LinkKind: string(store.LinkExplicitRef), Confidence: 1.0})

	checker := NewStalenessChecker(s, t.TempDir(), nil)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute with nil prober: %v", err)
	}
}

func TestStalenessOrphanNodeContinues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := store.Node{NodeID: "internal/orphan.F", Name: "F", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/orphan/f.go", PackageID: "internal/orphan", ContentHash: "h"}
	_ = s.UpsertNode(ctx, n)
	_ = s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID, PackageID: n.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0,
	})

	_, err := s.DB().ExecContext(ctx, `DELETE FROM graph_nodes WHERE node_id = ?`, n.NodeID)
	if err != nil {
		t.Fatalf("delete node: %v", err)
	}
	checker := NewStalenessChecker(s, "/repo", fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md": 1000,
	}})

	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute with orphan node: %v", err)
	}
}

func TestStalenessStrictlyAfter(t *testing.T) {

	s, checker, ctx := stalenessFixture(t, 1500, 1500)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if len(links) != 1 || links[0].Stale {
		t.Errorf("link wrongly flagged stale at equal timestamps (want strictly after): %+v", links)
	}
}

func TestStalenessNilProberMtimeSuccess(t *testing.T) {
	tempDir := t.TempDir()
	s := newTestStore(t)
	ctx := context.Background()

	adrRel := "docs/decisions/0200-test.md"
	adrFull := tempDir + "/" + adrRel
	if err := os.MkdirAll(tempDir+"/docs/decisions", 0o755); err != nil {
		t.Fatalf("mkdir adr dir: %v", err)
	}
	if err := os.WriteFile(adrFull, []byte("# test ADR\n"), 0o644); err != nil {
		t.Fatalf("write adr file: %v", err)
	}

	nodeFP := "internal/x/f.go"
	nodeFull := tempDir + "/" + nodeFP
	if err := os.MkdirAll(tempDir+"/internal/x", 0o755); err != nil {
		t.Fatalf("mkdir node dir: %v", err)
	}
	if err := os.WriteFile(nodeFull, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write node file: %v", err)
	}

	n := store.Node{NodeID: "internal/x.F2", Name: "F2", Kind: string(store.KindFunction), Language: "go", FilePath: nodeFP, PackageID: "internal/x", ContentHash: "h2"}
	_ = s.UpsertNode(ctx, n)
	_ = s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: adrRel, NodeID: n.NodeID, PackageID: n.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0,
	})

	checker := NewStalenessChecker(s, tempDir, nil)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute with nil prober + real files: %v", err)
	}

}

func TestStalenessSemanticLinkIsStaled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: "internal/caronte/intent/getwhy.go",
		PackageID: "internal/caronte/intent", ContentHash: "h",
	}
	_ = s.UpsertNode(ctx, n)
	_ = s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID, PackageID: n.PackageID,
		LinkKind: string(store.LinkSemantic), Confidence: 0.8, Stale: false,
	})
	prober := fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md":    1000,
		"internal/caronte/intent/getwhy.go": 2000,
	}}
	checker := NewStalenessChecker(s, "/repo", prober)
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if len(links) != 1 || !links[0].Stale {
		t.Errorf("semantic link not flagged stale though code is newer than ADR: %+v", links)
	}
}

func TestStalenessRecomputeClosedDB(t *testing.T) {
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_foreign_keys=1"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	ctx := context.Background()
	s, err := store.Open(ctx, db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("store.Open: %v", err)
	}

	_ = db.Close()
	checker := NewStalenessChecker(s, "/repo", fakeGitProber{})
	if err := checker.Recompute(ctx); err == nil {
		t.Error("Recompute(closed db) returned nil; want error")
	}
}

func TestStalenessSetStaleError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy",
		Kind: string(store.KindFunction), Language: "go",
		FilePath:  "internal/caronte/intent/getwhy.go",
		PackageID: "internal/caronte/intent", ContentHash: "h",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID,
		PackageID: n.PackageID, LinkKind: string(store.LinkExplicitRef),
		Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	if _, err := s.DB().ExecContext(ctx, `DROP TABLE adr_links`); err != nil {
		t.Fatalf("drop adr_links: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		CREATE VIEW adr_links AS
		SELECT
			'docs/decisions/0100-caronte.md' AS adr_id,
			'internal/caronte/intent.GetWhy'  AS node_id,
			'internal/caronte/intent'          AS package_id,
			'explicit_ref'                     AS link_kind,
			1.0                                AS confidence,
			0                                  AS stale`); err != nil {
		t.Fatalf("create view adr_links: %v", err)
	}

	checker := NewStalenessChecker(s, "/repo", fakeGitProber{touched: map[string]int64{
		"docs/decisions/0100-caronte.md":    1000,
		"internal/caronte/intent/getwhy.go": 2000,
	}})
	err := checker.Recompute(ctx)
	if err == nil {
		t.Error("Recompute(view-trap adr_links) returned nil; want error from SetADRLinkStale")
	}
}
