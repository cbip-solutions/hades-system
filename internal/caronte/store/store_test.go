//go:build cgo
// +build cgo

package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func boolPtr(b bool) *bool { return &b }

func openRawTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), openRawTestDB(t))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

func TestOpenRejectsNilDB(t *testing.T) {
	_, err := Open(context.Background(), nil)
	if err == nil {
		t.Fatal("Open(nil) returned nil error; want ErrEmptyDB")
	}
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("Open(nil) err = %v; want ErrEmptyDB", err)
	}
}

func TestInitMaterializesAllTables(t *testing.T) {
	s := newTestStore(t)
	tables := []string{
		"graph_nodes", "graph_edges", "co_change_matrix",
		"churn_metrics", "adr_links", "lore_trailers",
		"code_node_vec", "graph_nodes_fts",
	}
	for _, name := range tables {
		var got string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'table'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %s not materialized: %v", name, err)
		}
	}
	indexes := []string{"idx_edges_target", "idx_edges_source", "idx_nodes_iface"}
	for _, name := range indexes {
		var got string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'index'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("index %s not materialized: %v", name, err)
		}
	}
}

func TestVecTableIs1536(t *testing.T) {
	s := newTestStore(t)
	var ddl string
	if err := s.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE name = 'code_node_vec'`,
	).Scan(&ddl); err != nil {
		t.Fatalf("code_node_vec not created: %v", err)
	}
	if !strings.Contains(ddl, "vec0") {
		t.Errorf("code_node_vec not vec0: %s", ddl)
	}
	if !strings.Contains(ddl, "float[1536]") {
		t.Errorf("code_node_vec not 1536-dim: %s", ddl)
	}
}

func TestFTSIsExternalContent(t *testing.T) {
	s := newTestStore(t)
	var ddl string
	if err := s.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE name = 'graph_nodes_fts'`,
	).Scan(&ddl); err != nil {
		t.Fatalf("graph_nodes_fts not created: %v", err)
	}
	if !strings.Contains(ddl, "fts5") {
		t.Errorf("graph_nodes_fts not fts5: %s", ddl)
	}
	if !strings.Contains(ddl, "content='graph_nodes'") {
		t.Errorf("graph_nodes_fts not external-content over graph_nodes: %s", ddl)
	}
}

func TestInitIdempotent(t *testing.T) {
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	for i := 0; i < 3; i++ {
		db, err := sql.Open(DefaultDriver, dsn)
		if err != nil {
			t.Fatalf("Open[%d]: %v", i, err)
		}
		db.SetMaxOpenConns(1)
		if _, err := Open(context.Background(), db); err != nil {
			_ = db.Close()
			t.Fatalf("store.Open[%d]: %v (re-init must be idempotent)", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close[%d]: %v", i, err)
		}
	}
}

func TestSingleWriterWAL(t *testing.T) {
	s := newTestStore(t)
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q; want wal", mode)
	}
}

func TestBoundarySentinelReachable(t *testing.T) {
	if err := caronteBoundarySentinel(); err != nil {
		t.Errorf("caronteBoundarySentinel() = %v; want nil", err)
	}
}

func sampleNode() Node {
	return Node{
		NodeID: "pkg/x.T.M", Name: "M", Kind: string(KindMethod),
		Language: "go", FilePath: "pkg/x/x.go",
		StartLine: 10, EndLine: 20,
		Signature: "func (T) M() error", Doc: "M does things.",
		Coreness: 0, SCCID: 0, PackageID: "pkg/x",
		ContentHash: "hash-v1",
	}
}

func TestUpsertNodeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	got, err := s.GetNode(ctx, in.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestUpsertNodeIsUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode 1: %v", err)
	}
	in.Signature = "func (T) M() (int, error)"
	in.ContentHash = "hash-v2"
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode 2: %v", err)
	}
	got, err := s.GetNode(ctx, in.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Signature != "func (T) M() (int, error)" || got.ContentHash != "hash-v2" {
		t.Errorf("upsert did not update mutable fields: %+v", got)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("graph_nodes count = %d; want 1 (upsert, not duplicate)", count)
	}
}

func TestUpsertNodeSyncsFTS(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	in.Doc = "alpha unique_token_one"
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode 1: %v", err)
	}
	var rowid int64
	if err := s.db.QueryRow(
		`SELECT rowid FROM graph_nodes_fts WHERE graph_nodes_fts MATCH ?`, "unique_token_one",
	).Scan(&rowid); err != nil {
		t.Fatalf("FTS should match unique_token_one after first upsert: %v", err)
	}

	in.Doc = "beta unique_token_two"
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode 2: %v", err)
	}
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM graph_nodes_fts WHERE graph_nodes_fts MATCH ?`, "unique_token_one",
	).Scan(&n); err != nil {
		t.Fatalf("FTS count old token: %v", err)
	}
	if n != 0 {
		t.Errorf("stale FTS token: unique_token_one still matches after re-upsert (n=%d)", n)
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM graph_nodes_fts WHERE graph_nodes_fts MATCH ?`, "unique_token_two",
	).Scan(&n); err != nil {
		t.Fatalf("FTS count new token: %v", err)
	}
	if n != 1 {
		t.Errorf("new FTS token unique_token_two count = %d; want 1", n)
	}
}

func TestUpsertNodeVectorRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb := make([]float32, 1536)
	for i := range emb {
		emb[i] = 0.0
	}
	emb[0] = 1.0
	if err := s.UpsertNodeVector(ctx, in.NodeID, emb); err != nil {
		t.Fatalf("UpsertNodeVector: %v", err)
	}

	var gotNodeID string
	err := s.db.QueryRow(`
		SELECT gn.node_id
		FROM code_node_vec cv
		JOIN graph_nodes gn ON gn.rowid = cv.rowid
		WHERE cv.embedding MATCH ? AND k = 1
		ORDER BY distance`,
		float32SliceBytes(emb),
	).Scan(&gotNodeID)
	if err != nil {
		t.Fatalf("KNN query: %v", err)
	}
	if gotNodeID != in.NodeID {
		t.Errorf("KNN nearest = %q; want %q", gotNodeID, in.NodeID)
	}
}

func TestUpsertNodeVectorWrongDim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	_ = s.UpsertNode(ctx, in)
	if err := s.UpsertNodeVector(ctx, in.NodeID, make([]float32, 384)); err == nil {
		t.Error("UpsertNodeVector(384-d) returned nil; want dimension error")
	}
}

func TestUpdateNodeStructure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if err := s.UpdateNodeStructure(ctx, in.NodeID, 5, 9, "pkg/x"); err != nil {
		t.Fatalf("UpdateNodeStructure: %v", err)
	}
	got, err := s.GetNode(ctx, in.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Coreness != 5 || got.SCCID != 9 || got.PackageID != "pkg/x" {
		t.Errorf("structure not updated: coreness=%d scc=%d pkg=%q", got.Coreness, got.SCCID, got.PackageID)
	}
	if got.Signature != in.Signature {
		t.Error("UpdateNodeStructure clobbered an unrelated column")
	}
}

func TestGetNodeNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetNode(context.Background(), "does/not.Exist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNode(absent) err = %v; want ErrNotFound", err)
	}
}

func TestListNodesByKind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	iface := sampleNode()
	iface.NodeID, iface.Kind = "pkg/x.Reader", string(KindInterface)
	fn := sampleNode()
	fn.NodeID, fn.Kind = "pkg/x.Run", string(KindFunction)
	if err := s.UpsertNode(ctx, iface); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(ctx, fn); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListNodesByKind(ctx, KindInterface)
	if err != nil {
		t.Fatalf("ListNodesByKind: %v", err)
	}
	if len(got) != 1 || got[0].NodeID != "pkg/x.Reader" {
		t.Errorf("ListNodesByKind(interface) = %+v; want [pkg/x.Reader]", got)
	}
}

func TestContentHashFor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatal(err)
	}
	h, err := s.ContentHashFor(ctx, in.NodeID)
	if err != nil {
		t.Fatalf("ContentHashFor: %v", err)
	}
	if h != "hash-v1" {
		t.Errorf("ContentHashFor = %q; want hash-v1", h)
	}
	if _, err := s.ContentHashFor(ctx, "absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ContentHashFor(absent) err = %v; want ErrNotFound", err)
	}
}

func TestUpsertNodeVectorNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	emb := make([]float32, 1536)
	err := s.UpsertNodeVector(ctx, "does/not.Exist", emb)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpsertNodeVector(absent node) err = %v; want ErrNotFound", err)
	}
}

func TestUpsertNodeVectorReplace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleNode()
	if err := s.UpsertNode(ctx, in); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb1 := make([]float32, 1536)
	emb1[0] = 1.0
	emb2 := make([]float32, 1536)
	emb2[1] = 1.0
	if err := s.UpsertNodeVector(ctx, in.NodeID, emb1); err != nil {
		t.Fatalf("UpsertNodeVector 1: %v", err)
	}
	if err := s.UpsertNodeVector(ctx, in.NodeID, emb2); err != nil {
		t.Fatalf("UpsertNodeVector 2 (replace): %v", err)
	}

	var gotNodeID string
	err := s.db.QueryRow(`
		SELECT gn.node_id
		FROM code_node_vec cv
		JOIN graph_nodes gn ON gn.rowid = cv.rowid
		WHERE cv.embedding MATCH ? AND k = 1
		ORDER BY distance`,
		float32SliceBytes(emb2),
	).Scan(&gotNodeID)
	if err != nil {
		t.Fatalf("KNN query after replace: %v", err)
	}
	if gotNodeID != in.NodeID {
		t.Errorf("KNN nearest after replace = %q; want %q", gotNodeID, in.NodeID)
	}
}

func TestListNodesByKindEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertNode(ctx, sampleNode()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListNodesByKind(ctx, KindInterface)
	if err != nil {
		t.Fatalf("ListNodesByKind(interface, empty): %v", err)
	}
	if got == nil {
		t.Error("ListNodesByKind returned nil; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ListNodesByKind(interface) = %d nodes; want 0", len(got))
	}
}

func TestUpsertNodePreservesStructureColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := sampleNode()
	n.Coreness, n.SCCID, n.PackageID = 0, 0, ""
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode (initial): %v", err)
	}

	if err := s.UpdateNodeStructure(ctx, n.NodeID, 5, 9, "pkg/x"); err != nil {
		t.Fatalf("UpdateNodeStructure: %v", err)
	}

	n.ContentHash = "hash-v2"
	n.Coreness, n.SCCID, n.PackageID = 0, 0, ""
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode (re-parse): %v", err)
	}

	got, err := s.GetNode(ctx, n.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Coreness != 5 || got.SCCID != 9 || got.PackageID != "pkg/x" {
		t.Errorf("L3 structure columns wiped by re-parse: coreness=%d scc_id=%d package_id=%q; want 5, 9, \"pkg/x\"",
			got.Coreness, got.SCCID, got.PackageID)
	}
}

func TestUpdateNodeStructureAbsent(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateNodeStructure(context.Background(), "absent/node.ID", 3, 7, "pkg/absent"); err != nil {
		t.Errorf("UpdateNodeStructure(absent) = %v; want nil (no-op)", err)
	}
}

func TestDBGetter(t *testing.T) {
	s := newTestStore(t)
	if s.DB() == nil {
		t.Error("DB() returned nil; want the injected *sql.DB")
	}
}

func TestFloat32SliceBytesEmpty(t *testing.T) {
	if got := float32SliceBytes(nil); got != nil {
		t.Errorf("float32SliceBytes(nil) = %v; want nil", got)
	}
	if got := float32SliceBytes([]float32{}); got != nil {
		t.Errorf("float32SliceBytes([]) = %v; want nil", got)
	}
}

func TestFloat32SliceBytesEncoding(t *testing.T) {

	got := float32SliceBytes([]float32{1.0, 0.0})
	want := []byte{0x00, 0x00, 0x80, 0x3F, 0x00, 0x00, 0x00, 0x00}
	if len(got) != len(want) {
		t.Fatalf("float32SliceBytes len = %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte[%d] = 0x%02X; want 0x%02X (little-endian IEEE-754)", i, got[i], want[i])
		}
	}
}

func newClosedStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), openRawTestDB(t))
	if err != nil {
		t.Fatalf("store.Open for closed-store: %v", err)
	}

	if err := s.db.Close(); err != nil {
		t.Fatalf("close underlying db: %v", err)
	}
	return s
}

func TestUpsertNodeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	err := s.UpsertNode(context.Background(), sampleNode())
	if err == nil {
		t.Error("UpsertNode(closed db) returned nil; want error")
	}
}

func TestUpsertNodeVectorClosedDB(t *testing.T) {
	s := newClosedStore(t)
	emb := make([]float32, 1536)
	err := s.UpsertNodeVector(context.Background(), "any/node.ID", emb)
	if err == nil {
		t.Error("UpsertNodeVector(closed db) returned nil; want error")
	}
}

func TestGetNodeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.GetNode(context.Background(), "any/node.ID")
	if err == nil {
		t.Error("GetNode(closed db) returned nil; want error")
	}
}

func TestListNodesByKindClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.ListNodesByKind(context.Background(), KindFunction)
	if err == nil {
		t.Error("ListNodesByKind(closed db) returned nil; want error")
	}
}

func TestUpdateNodeStructureClosedDB(t *testing.T) {
	s := newClosedStore(t)
	err := s.UpdateNodeStructure(context.Background(), "any/node.ID", 1, 2, "pkg")
	if err == nil {
		t.Error("UpdateNodeStructure(closed db) returned nil; want error")
	}
}

func TestContentHashForClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.ContentHashFor(context.Background(), "any/node.ID")
	if err == nil {
		t.Error("ContentHashFor(closed db) returned nil; want error")
	}
}

func TestUpsertNodeCancelledCtx(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.UpsertNode(ctx, sampleNode())
	if err == nil {
		t.Error("UpsertNode(cancelled ctx) returned nil; want error")
	}
}

func TestUpsertNodeVectorCancelledCtx(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.UpsertNodeVector(ctx, "any/node.ID", make([]float32, 1536))
	if err == nil {
		t.Error("UpsertNodeVector(cancelled ctx) returned nil; want error")
	}
}

func mustUpsertNode(t *testing.T, s *Store, n Node) {
	t.Helper()
	if err := s.UpsertNode(context.Background(), n); err != nil {
		t.Fatalf("mustUpsertNode %s: %v", n.NodeID, err)
	}
}

func mustUpsertVector(t *testing.T, s *Store, nodeID string) {
	t.Helper()
	emb := make([]float32, 1536)
	emb[0] = 1.0
	if err := s.UpsertNodeVector(context.Background(), nodeID, emb); err != nil {
		t.Fatalf("mustUpsertVector %s: %v", nodeID, err)
	}
}

func mustUpsertEdge(t *testing.T, s *Store, e Edge) {
	t.Helper()
	if err := s.UpsertEdge(context.Background(), e); err != nil {
		t.Fatalf("mustUpsertEdge %s→%s: %v", e.SourceID, e.TargetID, err)
	}
}

func mustInsertAPIEndpoint(t *testing.T, s *Store, e APIEndpoint) {
	t.Helper()
	if err := s.InsertAPIEndpoint(context.Background(), e); err != nil {
		t.Fatalf("InsertAPIEndpoint(%q): %v", e.EndpointID, err)
	}
}

func mustInsertAPICall(t *testing.T, s *Store, c APICall) {
	t.Helper()
	if err := s.InsertAPICall(context.Background(), c); err != nil {
		t.Fatalf("InsertAPICall(%q): %v", c.CallID, err)
	}
}

func ftsMatchCount(t *testing.T, s *Store, token string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM graph_nodes_fts WHERE graph_nodes_fts MATCH ?`, token,
	).Scan(&n); err != nil {
		t.Fatalf("ftsMatchCount(%q): %v", token, err)
	}
	return n
}

func vecRowCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM code_node_vec`).Scan(&n); err != nil {
		t.Fatalf("vecRowCount: %v", err)
	}
	return n
}

func edgeCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM graph_edges`).Scan(&n); err != nil {
		t.Fatalf("edgeCount: %v", err)
	}
	return n
}

func TestDeleteNodesByFileRemovesNodesEdgesFTSVec(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	mustUpsertNode(t, s, Node{NodeID: "a1", Name: "Alpha", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h1"})
	mustUpsertNode(t, s, Node{NodeID: "a2", Name: "AlphaTwo", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h2"})
	mustUpsertNode(t, s, Node{NodeID: "b1", Name: "Beta", Kind: "func", Language: "go", FilePath: "pkg/b.go", ContentHash: "h3"})

	mustUpsertVector(t, s, "a1")
	mustUpsertVector(t, s, "b1")

	mustUpsertEdge(t, s, Edge{SourceID: "a1", TargetID: "a2", Kind: string(EdgeCalls), Confidence: ConfExactStatic})
	mustUpsertEdge(t, s, Edge{SourceID: "a1", TargetID: "b1", Kind: string(EdgeCalls), Confidence: ConfExactStatic})
	mustUpsertEdge(t, s, Edge{SourceID: "b1", TargetID: "a2", Kind: string(EdgeCalls), Confidence: ConfExactStatic})

	n, err := s.DeleteNodesByFile(ctx, "pkg/a.go")
	if err != nil {
		t.Fatalf("DeleteNodesByFile: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows deleted = %d; want 2 (a1, a2)", n)
	}

	if _, err := s.GetNode(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNode(a1) err = %v; want ErrNotFound", err)
	}
	if _, err := s.GetNode(ctx, "a2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNode(a2) err = %v; want ErrNotFound", err)
	}
	if _, err := s.GetNode(ctx, "b1"); err != nil {
		t.Errorf("GetNode(b1) err = %v; want nil (b1 survives)", err)
	}

	if cnt := ftsMatchCount(t, s, "Alpha"); cnt != 0 {
		t.Errorf("fts match 'Alpha' = %d; want 0 (a1/a2 deleted)", cnt)
	}
	if cnt := ftsMatchCount(t, s, "Beta"); cnt != 1 {
		t.Errorf("fts match 'Beta' = %d; want 1 (b1 survives)", cnt)
	}

	if cnt := vecRowCount(t, s); cnt != 1 {
		t.Errorf("code_node_vec rows = %d; want 1 (only b1)", cnt)
	}

	if cnt := edgeCount(t, s); cnt != 0 {
		t.Errorf("graph_edges rows = %d; want 0 (all 3 edges referenced a1 or a2)", cnt)
	}
}

func TestDeleteNodesByFileNoMatchReturnsZero(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mustUpsertNode(t, s, Node{NodeID: "x1", Name: "X", Kind: "func", Language: "go", FilePath: "pkg/x.go", ContentHash: "h"})
	n, err := s.DeleteNodesByFile(ctx, "pkg/nonexistent.go")
	if err != nil {
		t.Fatalf("DeleteNodesByFile no-match: %v", err)
	}
	if n != 0 {
		t.Errorf("rows deleted = %d; want 0", n)
	}

	if _, err := s.GetNode(ctx, "x1"); err != nil {
		t.Errorf("GetNode(x1) err = %v; want nil", err)
	}
}

func TestListNodesByKindCancelledCtx(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ListNodesByKind(ctx, KindFunction)
	if err == nil {
		t.Error("ListNodesByKind(cancelled ctx) returned nil; want error")
	}
}

func TestInitNilDB(t *testing.T) {
	s := &Store{db: nil}
	if err := s.Init(context.Background()); !errors.Is(err, ErrEmptyDB) {
		t.Errorf("Init(nil db) = %v; want ErrEmptyDB", err)
	}
}

func TestInitClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.Init(context.Background()); err == nil {
		t.Error("Init(closed db) returned nil; want error")
	}
}

func TestOpenSqliteVecAutoCalledTwice(t *testing.T) {

	db := openRawTestDB(t)
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatalf("Open with double Auto(): %v", err)
	}
	if s.DB() == nil {
		t.Error("DB() nil after double Auto()")
	}
}

func TestOpenInitFails(t *testing.T) {
	sqlite_vec.Auto()
	db := openRawTestDB(t)

	if err := db.Close(); err != nil {
		t.Fatalf("close db for TestOpenInitFails: %v", err)
	}
	_, err := Open(context.Background(), db)
	if err == nil {
		t.Error("Open(closed db) returned nil error; want error from Init")
	}
}

func TestUpsertNodeMissingTableError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.DB().ExecContext(ctx, `DROP TABLE graph_nodes_fts`); err != nil {
		t.Fatalf("drop graph_nodes_fts: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE graph_nodes`); err != nil {
		t.Fatalf("drop graph_nodes: %v", err)
	}
	err := s.UpsertNode(ctx, sampleNode())
	if err == nil {
		t.Error("UpsertNode(missing table) returned nil; want probe error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("UpsertNode(missing table) returned ErrNotFound; want a real DB error, got: %v", err)
	}
}

func TestUpsertNodeVectorMissingTableError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.DB().ExecContext(ctx, `DROP TABLE graph_nodes_fts`); err != nil {
		t.Fatalf("drop graph_nodes_fts: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE graph_nodes`); err != nil {
		t.Fatalf("drop graph_nodes: %v", err)
	}
	emb := make([]float32, 1536)
	err := s.UpsertNodeVector(ctx, "any/node.ID", emb)
	if err == nil {
		t.Error("UpsertNodeVector(missing table) returned nil; want resolve-rowid error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("UpsertNodeVector(missing table) returned ErrNotFound; want a real DB error, got: %v", err)
	}
}

func TestListCoChangeForFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rows := []CoChange{
		{FileA: "a.go", FileB: "b.go", SharedRevs: 4, RevsA: 8, RevsB: 12, WindowDays: 90, UpdatedAt: 1},
		{FileA: "c.go", FileB: "a.go", SharedRevs: 3, RevsA: 9, RevsB: 6, WindowDays: 90, UpdatedAt: 1},
		{FileA: "d.go", FileB: "e.go", SharedRevs: 5, RevsA: 5, RevsB: 5, WindowDays: 90, UpdatedAt: 1},
	}
	for _, r := range rows {
		if err := s.UpsertCoChange(ctx, r); err != nil {
			t.Fatalf("seed UpsertCoChange: %v", err)
		}
	}
	got, err := s.ListCoChangeForFile(ctx, "a.go", 90)
	if err != nil {
		t.Fatalf("ListCoChangeForFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (a.go couples b.go + c.go, both slots)", len(got))
	}

	none, err := s.ListCoChangeForFile(ctx, "zzz.go", 90)
	if err != nil {
		t.Fatalf("ListCoChangeForFile(absent): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("absent file len = %d; want 0", len(none))
	}

	other, err := s.ListCoChangeForFile(ctx, "a.go", 180)
	if err != nil {
		t.Fatalf("ListCoChangeForFile(window=180): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("window=180 len = %d; want 0 (rows are window=90)", len(other))
	}
}

func TestInitMaterializesAPIEndpointsAndCalls(t *testing.T) {
	s := newTestStore(t)
	tables := []string{"api_endpoints", "api_calls"}
	for _, name := range tables {
		var got string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'table'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %s not materialized: %v", name, err)
		}
	}
	indexes := []string{
		"idx_endpoints_http", "idx_endpoints_proto", "idx_endpoints_topic",
		"idx_calls_target_http", "idx_calls_target_proto", "idx_calls_target_topic", "idx_calls_base_url_ref",
	}
	for _, name := range indexes {
		var got string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'index'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("partial index %s not materialized: %v", name, err)
		}
	}
}

func TestAPIEndpointsKindCheckConstraintRefuses(t *testing.T) {
	s := newTestStore(t)

	_, err := s.db.Exec(`
		INSERT INTO api_endpoints
			(endpoint_id, repo, kind, handler_node_id, extracted_at, extractor_id)
		VALUES ('e1', 'r1', 'forged-kind', 'n1', 1, 'x')`)
	if err == nil {
		t.Fatal("CHECK constraint did NOT refuse forged kind; want failure")
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_endpoints`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("api_endpoints rows after refused INSERT = %d; want 0", n)
	}
}

func TestAPICallsConfidenceCheckConstraintRefuses(t *testing.T) {
	s := newTestStore(t)
	_, err := s.db.Exec(`
		INSERT INTO api_calls
			(call_id, repo, caller_node_id, confidence, extracted_at, extractor_id)
		VALUES ('c1', 'r1', 'n1', 'forged-tier', 1, 'x')`)
	if err == nil {
		t.Fatal("CHECK constraint did NOT refuse forged confidence; want failure")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_calls`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("api_calls rows after refused INSERT = %d; want 0", n)
	}
}

func TestGetNodeByPosition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertNode(ctx, Node{
		NodeID: "internal/widget.Server.Serve", Name: "Serve", Kind: string(KindMethod),
		Language: "go", FilePath: "internal/widget/server.go", StartLine: 42, EndLine: 50,
		ContentHash: "hash-serve",
	}); err != nil {
		t.Fatal(err)
	}

	id, ok, err := s.GetNodeByPosition(ctx, "internal/widget/server.go", 42)
	if err != nil || !ok || id != "internal/widget.Server.Serve" {
		t.Fatalf("GetNodeByPosition(server.go,42) = (%q,%v,%v); want (internal/widget.Server.Serve,true,nil)", id, ok, err)
	}

	if _, ok, err := s.GetNodeByPosition(ctx, "internal/widget/server.go", 999); err != nil || ok {
		t.Errorf("GetNodeByPosition(unbacked position) = (ok=%v,err=%v); want (false,nil)", ok, err)
	}
}
