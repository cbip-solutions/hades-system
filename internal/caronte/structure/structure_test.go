//go:build cgo
// +build cgo

package structure

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func newStore(t *testing.T) *store.Store {
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
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

func seedTrianglePlusCycle(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	mk := func(id, file string) store.Node {
		return store.Node{NodeID: id, Name: id, Kind: string(store.KindFunction), Language: "go", FilePath: file, ContentHash: "h"}
	}
	for _, n := range []store.Node{
		mk("pkg/x.A", "pkg/x/a.go"), mk("pkg/x.B", "pkg/x/b.go"), mk("pkg/x.C", "pkg/x/c.go"),
		mk("pkg/x.D", "pkg/x/d.go"), mk("pkg/y.E", "pkg/y/e.go"), mk("pkg/y.F", "pkg/y/f.go"),
		mk("pkg/y.Lonely", "pkg/y/lonely.go"),
	} {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.NodeID, err)
		}
	}
	mke := func(src, tgt string, line int) store.Edge {
		return store.Edge{SourceID: src, TargetID: tgt, Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteFile: "x.go", SiteLine: line}
	}
	for i, e := range []store.Edge{
		mke("pkg/x.A", "pkg/x.B", 1), mke("pkg/x.B", "pkg/x.C", 2), mke("pkg/x.C", "pkg/x.A", 3),
		mke("pkg/x.A", "pkg/x.D", 4),
		mke("pkg/y.E", "pkg/y.F", 5), mke("pkg/y.F", "pkg/y.E", 6),
	} {
		_ = i
		if err := s.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge: %v", err)
		}
	}
}

func TestRecomputeWritesBackStructure(t *testing.T) {
	s := newStore(t)
	seedTrianglePlusCycle(t, s)
	ctx := context.Background()
	dec, wrote, err := Recompute(ctx, s, "")
	if err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	if !wrote {
		t.Error("cold-start Recompute (lastHashKey=\"\") must write back")
	}

	for _, id := range []string{"pkg/x.A", "pkg/x.B", "pkg/x.C"} {
		n, err := s.GetNode(ctx, id)
		if err != nil {
			t.Fatalf("GetNode %s: %v", id, err)
		}
		if n.Coreness != 2 {
			t.Errorf("persisted coreness[%s] = %d; want 2", id, n.Coreness)
		}
	}

	d, _ := s.GetNode(ctx, "pkg/x.D")
	if d.Coreness != 1 {
		t.Errorf("persisted coreness[D] = %d; want 1", d.Coreness)
	}
	if d.PackageID != "pkg/x" {
		t.Errorf("persisted package_id[D] = %q; want pkg/x", d.PackageID)
	}

	if dec.SCCOf("pkg/y.E") != dec.SCCOf("pkg/y.F") {
		t.Error("E,F must share an scc_id (cycle)")
	}
	if !dec.IsCyclic("pkg/y.E") {
		t.Error("E must be flagged cyclic (SCC size 2)")
	}

	l, _ := s.GetNode(ctx, "pkg/y.Lonely")
	if l.Coreness != 0 || l.PackageID != "pkg/y" {
		t.Errorf("Lonely persisted = {coreness %d, pkg %q}; want {0, pkg/y}", l.Coreness, l.PackageID)
	}
	if dec.IsCyclic("pkg/y.Lonely") {
		t.Error("Lonely (singleton SCC) must not be cyclic")
	}
}

func TestRecomputeSkipsOnUnchangedTopology(t *testing.T) {
	s := newStore(t)
	seedTrianglePlusCycle(t, s)
	ctx := context.Background()
	dec1, wrote1, err := Recompute(ctx, s, "")
	if err != nil || !wrote1 {
		t.Fatalf("first Recompute: wrote=%v err=%v", wrote1, err)
	}
	dec2, wrote2, err := Recompute(ctx, s, dec1.HashKey)
	if err != nil {
		t.Fatalf("second Recompute: %v", err)
	}
	if wrote2 {
		t.Error("Recompute with unchanged hash-key must NOT write back")
	}
	if dec2.HashKey != dec1.HashKey {
		t.Errorf("hash-key changed across no-op recompute: %q vs %q", dec1.HashKey, dec2.HashKey)
	}
}

func TestRecomputeWritesOnTopologyChange(t *testing.T) {
	s := newStore(t)
	seedTrianglePlusCycle(t, s)
	ctx := context.Background()
	dec1, _, _ := Recompute(ctx, s, "")

	if err := s.UpsertEdge(ctx, store.Edge{SourceID: "pkg/x.D", TargetID: "pkg/y.E", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteFile: "x.go", SiteLine: 7}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	_, wrote, err := Recompute(ctx, s, dec1.HashKey)
	if err != nil {
		t.Fatalf("Recompute after change: %v", err)
	}
	if !wrote {
		t.Error("topology changed (new edge) but Recompute did not write back")
	}
}

func TestKindTypeNodeAppearsInDecomposition(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	typeNode := store.Node{
		NodeID:      "internal/widget.Alias",
		Name:        "Alias",
		Kind:        string(store.KindType),
		Language:    "go",
		FilePath:    "internal/widget/alias.go",
		ContentHash: "h",
	}
	if err := s.UpsertNode(ctx, typeNode); err != nil {
		t.Fatalf("UpsertNode KindType: %v", err)
	}

	dec, wrote, err := Recompute(ctx, s, "")
	if err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	if !wrote {
		t.Error("cold-start Recompute (lastHashKey=\"\") must write back")
	}

	if _, ok := dec.Coreness["internal/widget.Alias"]; !ok {
		t.Error("KindType node \"internal/widget.Alias\" absent from Coreness map; readAllNodes must include store.KindType")
	}
	if _, ok := dec.SCCID["internal/widget.Alias"]; !ok {
		t.Error("KindType node \"internal/widget.Alias\" absent from SCCID map; readAllNodes must include store.KindType")
	}

	if c := dec.CorenessOf("internal/widget.Alias"); c != 0 {
		t.Errorf("isolated KindType node coreness = %d; want 0", c)
	}
	if dec.IsCyclic("internal/widget.Alias") {
		t.Error("isolated KindType node must not be cyclic (singleton SCC)")
	}
}

func TestComputeAccessors(t *testing.T) {
	nodes := []store.Node{
		{NodeID: "pkg/x.A", FilePath: "pkg/x/a.go", Language: "go", ContentHash: "h"},
		{NodeID: "pkg/x.B", FilePath: "pkg/x/b.go", Language: "go", ContentHash: "h"},
	}
	edges := []store.Edge{
		{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteLine: 1},
		{SourceID: "pkg/x.B", TargetID: "pkg/x.A", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteLine: 2},
	}
	dec := Compute(nodes, edges)
	if dec.PackageOf("pkg/x.A") != "pkg/x" {
		t.Errorf("PackageOf(A) = %q; want pkg/x", dec.PackageOf("pkg/x.A"))
	}
	if !dec.IsCyclic("pkg/x.A") {
		t.Error("A↔B cycle must be cyclic")
	}

	if dec.CorenessOf("nope") != 0 || dec.SCCOf("nope") != 0 || dec.PackageOf("nope") != "" || dec.IsCyclic("nope") {
		t.Error("absent node accessors must return zero values")
	}
	if dec.HashKey == "" {
		t.Error("Compute must set HashKey")
	}
}
