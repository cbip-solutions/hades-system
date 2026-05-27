// go:build cgo
//go:build cgo
// +build cgo

package semantic

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/providers"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func openTestStore(t *testing.T) *store.Store {
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

// seedBuildableNodes inserts the graph_nodes rows matching the buildable
// fixture's symbols so the resolver's edges (and the read-back queries)
// reference real nodes. The node_ids MUST be the SAME repo-relative form the
// resolver's canonicalNodeID produces (the parse↔resolve JOIN KEY): the
// fixture is module example.com/buildable with files at the module root, so
// canonicalNodeID strips the module prefix to the BARE form emits for
// a repo-root file (Circle, Circle.Area, TotalArea — no import-path prefix).
// Seeding the full import path here would make every resolver edge dangle —
// the exact cross-phase-drift bug this fix closes.
func seedBuildableNodes(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	nodes := []store.Node{
		{NodeID: "Shape", Name: "Shape", Kind: string(store.KindInterface), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "Circle", Name: "Circle", Kind: string(store.KindStruct), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "Square", Name: "Square", Kind: string(store.KindStruct), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "Circle.Area", Name: "Area", Kind: string(store.KindMethod), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "Square.Area", Name: "Area", Kind: string(store.KindMethod), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "TotalArea", Name: "TotalArea", Kind: string(store.KindFunction), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
		{NodeID: "helper", Name: "helper", Kind: string(store.KindFunction), Language: "go", FilePath: "shapes.go", ContentHash: "h"},
	}
	for _, n := range nodes {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seed UpsertNode %s: %v", n.NodeID, err)
		}
	}
}

func TestResolveProjectBuildableWritesVTAEdges(t *testing.T) {
	s := openTestStore(t)
	seedBuildableNodes(t, s)
	r := NewResolver(s, nil, ResolverOpts{})
	stats, err := r.ResolveProject(context.Background(), "proj-1", "testdata/buildable")
	if err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if stats.Mode != ModeVTA {
		t.Errorf("Mode = %q; want vta", stats.Mode)
	}
	if stats.CallEdges == 0 {
		t.Error("no call edges written")
	}
	if stats.ImplementsEdges == 0 {
		t.Error("no implements edges written")
	}

	impls, err := r.GetImplementations(context.Background(), "Shape")
	if err != nil {
		t.Fatalf("GetImplementations: %v", err)
	}
	gotCircle, gotSquare := false, false
	for _, im := range impls {
		if im.ImplID == "Circle" {
			gotCircle = true
		}
		if im.ImplID == "Square" {
			gotSquare = true
		}
		if im.Confidence != string(store.ConfExactVTA) {
			t.Errorf("impl %s confidence = %q; want exact_vta", im.ImplID, im.Confidence)
		}
	}
	if !gotCircle || !gotSquare {
		t.Errorf("GetImplementations missed an impl: circle=%v square=%v", gotCircle, gotSquare)
	}
}

func TestResolveProjectBrokenFallsBackToCHA(t *testing.T) {
	s := openTestStore(t)

	ctx := context.Background()
	for _, n := range []store.Node{
		{NodeID: "Caller", Name: "Caller", Kind: string(store.KindFunction), Language: "go", FilePath: "broken.go", ContentHash: "h"},
		{NodeID: "Callee", Name: "Callee", Kind: string(store.KindFunction), Language: "go", FilePath: "broken.go", ContentHash: "h"},
	} {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	r := NewResolver(s, nil, ResolverOpts{})
	stats, err := r.ResolveProject(ctx, "proj-broken", "testdata/broken")
	if err != nil {
		t.Fatalf("ResolveProject(broken) hard-failed; §15 forbids: %v", err)
	}
	if stats.Mode != ModeCHA {
		t.Errorf("Mode = %q; want cha (build broken)", stats.Mode)
	}
	edges, err := r.store.ListEdgesBySource(ctx, "Caller", store.EdgeCalls)
	if err != nil {
		t.Fatalf("ListEdgesBySource: %v", err)
	}
	foundCHA := false
	for _, e := range edges {
		if e.TargetID == "Callee" {
			foundCHA = true
			if e.Confidence != store.ConfExactCHA {
				t.Errorf("fallback edge confidence = %q; want exact_cha", e.Confidence)
			}
			if e.Reachable != nil {
				t.Error("CHA edge Reachable must be NULL")
			}
		}
	}
	if !foundCHA {
		t.Error("CHA fallback wrote no Caller→Callee edge")
	}
}

func TestTraceCallPathFromTotalArea(t *testing.T) {
	s := openTestStore(t)
	seedBuildableNodes(t, s)
	r := NewResolver(s, nil, ResolverOpts{})
	if _, err := r.ResolveProject(context.Background(), "p", "testdata/buildable"); err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	hops, err := r.TraceCallPath(context.Background(), "TotalArea", 3)
	if err != nil {
		t.Fatalf("TraceCallPath: %v", err)
	}
	reached := map[string]bool{}
	for _, h := range hops {
		reached[h.ToID] = true
		if h.Depth < 1 {
			t.Errorf("hop depth %d < 1", h.Depth)
		}
	}
	if !reached["helper"] {
		t.Error("trace missed TotalArea→helper")
	}
	if !reached["Circle.Area"] && !reached["Square.Area"] {
		t.Error("trace missed the interface-dispatch targets")
	}
}

func TestLLMTailRoutesThroughSeam(t *testing.T) {
	s := openTestStore(t)
	seedBuildableNodes(t, s)
	fd := &fakeDispatcher{resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)}}
	r := NewResolver(s, fd, ResolverOpts{})

	cand := []unresolvedSite{{
		CallerID: "TotalArea",
		SiteFile: "shapes.go", SiteLine: 17,
		Hint: "dynamic dispatch via Shape",
	}}
	n, err := r.resolveTail(context.Background(), cand)
	if err != nil {
		t.Fatalf("resolveTail: %v", err)
	}
	if fd.calls != 1 {
		t.Errorf("dispatcher invoked %d times; want 1", fd.calls)
	}
	if fd.lastCall.Profile != DefaultLLMProfile {
		t.Errorf("LLM-tail Profile = %q; want %q", fd.lastCall.Profile, DefaultLLMProfile)
	}
	if fd.lastCall.Path != "/v1/messages" || fd.lastCall.Method != "POST" {
		t.Errorf("LLM-tail call shape = %s %s; want POST /v1/messages", fd.lastCall.Method, fd.lastCall.Path)
	}
	_ = n
}

func TestResolveTailNoDispatcherReturnsErrNoDispatcher(t *testing.T) {
	s := openTestStore(t)
	r := NewResolver(s, nil, ResolverOpts{})
	_, err := r.resolveTail(context.Background(), []unresolvedSite{{CallerID: "x"}})
	if !errors.Is(err, ErrNoDispatcher) {
		t.Errorf("resolveTail(no dispatcher) err = %v; want ErrNoDispatcher", err)
	}
}

func TestResolveTailEmptySitesReturnsZero(t *testing.T) {
	s := openTestStore(t)
	fd := &fakeDispatcher{resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)}}
	r := NewResolver(s, fd, ResolverOpts{})
	n, err := r.resolveTail(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveTail(empty): unexpected err: %v", err)
	}
	if n != 0 {
		t.Errorf("resolveTail(empty) n = %d; want 0", n)
	}
	if fd.calls != 0 {
		t.Errorf("dispatcher called %d times for empty sites; want 0", fd.calls)
	}
}

func TestResolveTailParsesResolutionEdges(t *testing.T) {
	s := openTestStore(t)
	seedBuildableNodes(t, s)
	respBody := []byte(`{"content":[{"type":"text","text":"{\"resolutions\":[{\"from_id\":\"TotalArea\",\"to_id\":\"Circle.Area\",\"site_file\":\"shapes.go\",\"site_line\":17}]}"}]}`)
	fd := &fakeDispatcher{resp: &providers.TierResponse{Status: 200, Body: respBody}}
	r := NewResolver(s, fd, ResolverOpts{})
	cand := []unresolvedSite{{CallerID: "TotalArea", SiteFile: "shapes.go", SiteLine: 17, Hint: "iface"}}
	n, err := r.resolveTail(context.Background(), cand)
	if err != nil {
		t.Fatalf("resolveTail: %v", err)
	}
	if n != 1 {
		t.Errorf("resolveTail wrote %d edges; want 1", n)
	}

	ctx := context.Background()
	edges, err := r.store.ListEdgesBySource(ctx, "TotalArea", store.EdgeInvoke)
	if err != nil {
		t.Fatalf("ListEdgesBySource: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.TargetID == "Circle.Area" && e.Confidence == store.ConfLLMHint {
			found = true
		}
	}
	if !found {
		t.Error("resolveTail did not write ConfLLMHint edge TotalArea→Circle.Area")
	}
}

func TestResolveTailDispatcherError(t *testing.T) {
	s := openTestStore(t)
	fd := &fakeDispatcher{err: errors.New("ollama down")}
	r := NewResolver(s, fd, ResolverOpts{})
	_, err := r.resolveTail(context.Background(), []unresolvedSite{{CallerID: "x"}})
	if err == nil {
		t.Fatal("resolveTail(dispatcher error): expected error, got nil")
	}
}

// TestParseTailResolutionsMalformed asserts parseTailResolutions returns nil
// for unparseable JSON (precision > recall: do not fabricate edges).
func TestParseTailResolutionsMalformed(t *testing.T) {
	out := parseTailResolutions([]byte("not json"))
	if len(out) != 0 {
		t.Errorf("parseTailResolutions(malformed) = %v; want nil", out)
	}
}

func TestParseTailResolutionsEmpty(t *testing.T) {
	out := parseTailResolutions(nil)
	if len(out) != 0 {
		t.Errorf("parseTailResolutions(nil) = %v; want nil", out)
	}
}

func TestParseTailResolutionsNonTextContent(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_result","text":""}]}`)
	out := parseTailResolutions(body)
	if len(out) != 0 {
		t.Errorf("parseTailResolutions(non-text) = %v; want nil", out)
	}
}

func TestResolveProjectNilStore(t *testing.T) {
	r := &Resolver{store: nil, opts: ResolverOpts{MaxTailSites: DefaultMaxTailSites}}
	_, err := r.ResolveProject(context.Background(), "p", "testdata/buildable")
	if err == nil {
		t.Fatal("ResolveProject(nil store): expected error, got nil")
	}
}

func TestGetImplementationsNilStore(t *testing.T) {
	r := &Resolver{store: nil, opts: ResolverOpts{MaxTailSites: DefaultMaxTailSites}}
	_, err := r.GetImplementations(context.Background(), "Shape")
	if err == nil {
		t.Fatal("GetImplementations(nil store): expected error, got nil")
	}
}

func TestTraceCallPathNilStore(t *testing.T) {
	r := &Resolver{store: nil, opts: ResolverOpts{MaxTailSites: DefaultMaxTailSites}}
	_, err := r.TraceCallPath(context.Background(), "TotalArea", 3)
	if err == nil {
		t.Fatal("TraceCallPath(nil store): expected error, got nil")
	}
}

func TestTraceCallPathMaxDepthZeroClampedToOne(t *testing.T) {
	s := openTestStore(t)
	seedBuildableNodes(t, s)
	r := NewResolver(s, nil, ResolverOpts{})
	if _, err := r.ResolveProject(context.Background(), "p", "testdata/buildable"); err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	hops, err := r.TraceCallPath(context.Background(), "TotalArea", 0)
	if err != nil {
		t.Fatalf("TraceCallPath(depth=0): %v", err)
	}

	for _, h := range hops {
		if h.Depth > 1 {
			t.Errorf("hop depth %d > 1 with maxDepth=0 (clamped to 1)", h.Depth)
		}
	}
}

func TestCollectUnresolvedMaxTailSitesCap(t *testing.T) {
	s := openTestStore(t)
	r := NewResolver(s, nil, ResolverOpts{MaxTailSites: 1})
	reachSet := map[string]bool{"A": true}
	edges := []store.Edge{
		{SourceID: "Caller", TargetID: "B", Kind: string(store.EdgeInvoke), Confidence: store.ConfExactVTA, SiteLine: 10},
		{SourceID: "Caller", TargetID: "C", Kind: string(store.EdgeInvoke), Confidence: store.ConfExactVTA, SiteLine: 20},
	}
	cand := r.collectUnresolved(edges, reachSet)
	if len(cand) != 1 {
		t.Errorf("collectUnresolved with cap=1: got %d candidates; want 1", len(cand))
	}

	if cand[0].SiteLine != 10 {
		t.Errorf("first candidate SiteLine = %d; want 10 (sorted, capped)", cand[0].SiteLine)
	}
}

func TestCollectUnresolvedNilReachSetSkipped(t *testing.T) {
	s := openTestStore(t)
	r := NewResolver(s, nil, ResolverOpts{MaxTailSites: DefaultMaxTailSites})
	edges := []store.Edge{
		{SourceID: "Caller", TargetID: "Callee", Kind: string(store.EdgeInvoke), Confidence: store.ConfExactCHA},
	}
	cand := r.collectUnresolved(edges, nil)
	if len(cand) != 0 {
		t.Errorf("collectUnresolved(nil reachSet) = %d candidates; want 0", len(cand))
	}
}

func TestCollectUnresolvedIgnoresCallsEdges(t *testing.T) {
	s := openTestStore(t)
	r := NewResolver(s, nil, ResolverOpts{MaxTailSites: DefaultMaxTailSites})
	reachSet := map[string]bool{}
	edges := []store.Edge{
		{SourceID: "A", TargetID: "B", Kind: string(store.EdgeCalls), Confidence: store.ConfExactVTA},
	}
	cand := r.collectUnresolved(edges, reachSet)
	if len(cand) != 0 {
		t.Errorf("collectUnresolved(calls edge) produced %d candidates; want 0", len(cand))
	}
}

func TestWriteEdgesErrorPropagates(t *testing.T) {
	s := openTestStore(t)
	r := NewResolver(s, nil, ResolverOpts{MaxTailSites: DefaultMaxTailSites})
	badEdges := []store.Edge{
		{SourceID: "A", TargetID: "B", Kind: string(store.EdgeCalls), Confidence: ""},
	}
	_, err := r.writeEdges(context.Background(), badEdges)
	if err == nil {
		t.Fatal("writeEdges(invalid confidence): expected error, got nil")
	}
}
