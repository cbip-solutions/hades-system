//go:build cgo
// +build cgo

package caronte

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeDispatcher struct{}

func (fakeDispatcher) Forward(_ context.Context, _ orchestrator.Call) (*providers.TierResponse, error) {
	return &providers.TierResponse{Status: 200, Body: []byte(`{}`)}, nil
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 1536)
	v[len(text)%1536] = 1.0
	return v, nil
}
func (fakeEmbedder) Dimensions() int { return 1536 }

func openCaronteDBAt(t *testing.T, dir string) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(dir, ".zen", "caronte.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testDeps(t *testing.T) (Deps, map[string]string) {
	t.Helper()
	dirs := map[string]string{}
	deps := Deps{
		OpenProjectDB: func(_ context.Context, projectID string) (*sql.DB, error) {
			dir, ok := dirs[projectID]
			if !ok {
				return nil, errors.New("unknown project (test)")
			}
			return openCaronteDBAt(t, dir), nil
		},
		Dispatcher: fakeDispatcher{},
		Embedder:   fakeEmbedder{},
		Reranker:   nil,
		AuditEmit:  func(string, []byte) {},
	}
	return deps, dirs
}

func TestDropInSatisfiesGitnexusClient(t *testing.T) {
	deps, _ := testDeps(t)
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	var _ research.GitnexusClient = e
}

func TestNewEngineRejectsNilOpenProjectDB(t *testing.T) {
	deps, _ := testDeps(t)
	deps.OpenProjectDB = nil
	if _, err := NewEngine(deps); err == nil {
		t.Fatal("NewEngine(nil OpenProjectDB) returned nil error")
	}
}

func TestNewEngineRejectsNilDispatcher(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Dispatcher = nil
	if _, err := NewEngine(deps); err == nil {
		t.Fatal("NewEngine(nil Dispatcher) returned nil error")
	}
}

func TestNewEngineNilEmbedderConsultsSelector(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Embedder = nil
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine(nil Embedder) returned error; want selector-fallback OK: %v", err)
	}
	defer e.Close()
	if e.deps.Embedder == nil {
		t.Error("NewEngine did not wire an embedder via the default selector")
	}
}

func TestNewEngineNilAuditEmitNormalised(t *testing.T) {
	deps, _ := testDeps(t)
	deps.AuditEmit = nil
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine(nil AuditEmit) returned error; want nil: %v", err)
	}
	defer e.Close()
}

func TestEngineRegistryCachesPerProject(t *testing.T) {
	deps, dirs := testDeps(t)
	dirs["proj-1"] = t.TempDir()
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	ctx := context.Background()
	pe1, err := e.projectEngineFor(ctx, "proj-1")
	if err != nil {
		t.Fatalf("projectEngineFor 1: %v", err)
	}
	pe2, err := e.projectEngineFor(ctx, "proj-1")
	if err != nil {
		t.Fatalf("projectEngineFor 2: %v", err)
	}
	if pe1 != pe2 {
		t.Error("registry returned a different projectEngine on second call; cache broken")
	}
}

func TestCloseDrainsRegistry(t *testing.T) {
	deps, dirs := testDeps(t)
	dirs["proj-1"] = t.TempDir()
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if _, err := e.projectEngineFor(context.Background(), "proj-1"); err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := e.CodeGraph(context.Background(), "x", "proj-1"); !errors.Is(err, ErrEngineClosed) {
		t.Errorf("CodeGraph after Close err = %v; want ErrEngineClosed", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	deps, _ := testDeps(t)
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close #2 (idempotent): %v", err)
	}
}

func TestProjectEngineForAfterCloseReturnsEngineClosed(t *testing.T) {
	deps, dirs := testDeps(t)
	dirs["proj-1"] = t.TempDir()
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = e.projectEngineFor(context.Background(), "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("projectEngineFor after Close = %v; want ErrEngineClosed", err)
	}
}

func TestCodeGraphQueryReturnsHits(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-1"] = dir
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-1")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}

	n := store.Node{
		NodeID: "pkg/x.Widget", Name: "Widget", Kind: string(store.KindStruct),
		Language: "go", FilePath: "pkg/x/widget.go", Doc: "Widget renders things.",
		ContentHash: "h1",
	}
	if err := pe.store.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb, _ := fakeEmbedder{}.Embed(ctx, "Widget renders things.")
	if err := pe.store.UpsertNodeVector(ctx, n.NodeID, emb); err != nil {
		t.Fatalf("UpsertNodeVector: %v", err)
	}
	res, err := e.CodeGraph(ctx, "Widget", "proj-1")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if res.ProjectID != "proj-1" {
		t.Errorf("CodeGraph ProjectID = %q; want proj-1", res.ProjectID)
	}
	var found bool
	for _, h := range res.Hits {
		if h.Node == "pkg/x.Widget" {
			found = true
		}
	}
	if !found {
		t.Errorf("CodeGraph(\"Widget\") missed pkg/x.Widget; got %+v", res.Hits)
	}
}

func TestCodeGraphUnknownProjectDegrades(t *testing.T) {
	deps, _ := testDeps(t)
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	res, err := e.CodeGraph(context.Background(), "x", "ghost-project")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("CodeGraph(unknown project) err = %v; want ErrProjectUnavailable", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("CodeGraph(unknown project) hits = %+v; want empty", res.Hits)
	}
}

func TestCodeGraphProjectIDPropagated(t *testing.T) {
	deps, _ := testDeps(t)
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()

	res, err := e.CodeGraph(context.Background(), "q", "my-unknown-project")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("err = %v; want ErrProjectUnavailable", err)
	}
	if res.ProjectID != "my-unknown-project" {
		t.Errorf("degrade: ProjectID = %q; want my-unknown-project", res.ProjectID)
	}
}

func TestCodeGraphHitURLShape(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-url"] = dir
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-url")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	n := store.Node{
		NodeID: "pkg/a.Foo", Name: "Foo", Kind: string(store.KindFunction),
		Language: "go", FilePath: "pkg/a/foo.go", ContentHash: "h2",
	}
	if err := pe.store.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb, _ := fakeEmbedder{}.Embed(ctx, "Foo")
	if err := pe.store.UpsertNodeVector(ctx, n.NodeID, emb); err != nil {
		t.Fatalf("UpsertNodeVector: %v", err)
	}
	res, err := e.CodeGraph(ctx, "Foo", "proj-url")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	for _, h := range res.Hits {
		if h.Node == "pkg/a.Foo" {
			want := "caronte://proj-url/pkg/a.Foo"
			if h.URL != want {
				t.Errorf("hit URL = %q; want %q", h.URL, want)
			}
		}
	}
}

func TestCodeGraphScorePositive(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-s"] = dir
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-s")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	n := store.Node{
		NodeID: "pkg/s.Bar", Name: "Bar", Kind: string(store.KindFunction),
		Language: "go", FilePath: "pkg/s/bar.go", ContentHash: "h3",
	}
	if err := pe.store.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb, _ := fakeEmbedder{}.Embed(ctx, "Bar")
	if err := pe.store.UpsertNodeVector(ctx, n.NodeID, emb); err != nil {
		t.Fatalf("UpsertNodeVector: %v", err)
	}
	res, err := e.CodeGraph(ctx, "Bar", "proj-s")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	for _, h := range res.Hits {
		if h.Score <= 0 || h.Score > 1.0 {
			t.Errorf("hit %q score = %f; want in (0,1]", h.Node, h.Score)
		}
	}
}

func TestStaticDefaultParamsCoChangeParams(t *testing.T) {
	sdp := staticDefaultParams{}
	p := sdp.CoChangeParams("any-project")
	def := evolution.DefaultParams()
	if p.MinRevisions != def.MinRevisions {
		t.Errorf("CoChangeParams MinRevisions = %d; want %d", p.MinRevisions, def.MinRevisions)
	}
	if p.WindowDays != def.WindowDays {
		t.Errorf("CoChangeParams WindowDays = %d; want %d", p.WindowDays, def.WindowDays)
	}
}

func TestProjectEngineForRepoRootFor(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-root"] = dir
	deps.RepoRootFor = func(_ context.Context, projectID string) (string, error) {
		if projectID == "proj-root" {
			return "/src/proj-root", nil
		}
		return "", errors.New("not found")
	}
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	pe, err := e.projectEngineFor(context.Background(), "proj-root")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	if pe.repoRoot != "/src/proj-root" {
		t.Errorf("repoRoot = %q; want /src/proj-root", pe.repoRoot)
	}
}

func TestProjectEngineForRepoRootForError(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-noroot"] = dir
	deps.RepoRootFor = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("repo root unavailable")
	}
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	pe, err := e.projectEngineFor(context.Background(), "proj-noroot")
	if err != nil {
		t.Fatalf("projectEngineFor with failing RepoRootFor: %v (want success with empty repoRoot)", err)
	}
	if pe.repoRoot != "" {
		t.Errorf("repoRoot = %q; want empty on RepoRootFor failure", pe.repoRoot)
	}
}

type errorEmbedder struct{}

func (errorEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embed: subprocess unavailable")
}
func (errorEmbedder) Dimensions() int { return 1536 }

func TestCodeGraphEmbedError(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-emb-err"] = dir
	deps.Embedder = errorEmbedder{}
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()

	ctx := context.Background()
	if _, err := e.projectEngineFor(ctx, "proj-emb-err"); err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	_, err = e.CodeGraph(ctx, "anything", "proj-emb-err")
	if err == nil {
		t.Fatal("CodeGraph with failing embedder returned nil error; want embed error")
	}
	if errors.Is(err, ErrEngineClosed) || errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("CodeGraph embed error = %v; should be an embed error, not engine/project sentinel", err)
	}
}

func TestCodeGraphHitsOrdered(t *testing.T) {
	deps, dirs := testDeps(t)
	dir := t.TempDir()
	dirs["proj-ord"] = dir
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-ord")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}

	nodes := []store.Node{
		{NodeID: "pkg/z.Zebra", Name: "Zebra", Kind: string(store.KindFunction), Language: "go", FilePath: "pkg/z/z.go", ContentHash: "hz"},
		{NodeID: "pkg/a.Alpha", Name: "Alpha", Kind: string(store.KindFunction), Language: "go", FilePath: "pkg/a/a.go", ContentHash: "ha"},
	}
	for _, n := range nodes {
		if err := pe.store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.NodeID, err)
		}

		emb, _ := fakeEmbedder{}.Embed(ctx, "same")
		if err := pe.store.UpsertNodeVector(ctx, n.NodeID, emb); err != nil {
			t.Fatalf("UpsertNodeVector %s: %v", n.NodeID, err)
		}
	}
	res, err := e.CodeGraph(ctx, "same", "proj-ord")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("expected ≥2 hits; got %d", len(res.Hits))
	}

	if res.Hits[0].Node > res.Hits[1].Node {
		t.Errorf("hits not sorted by node_id on tie: [0]=%q [1]=%q; want alpha < zebra order",
			res.Hits[0].Node, res.Hits[1].Node)
	}
}

var _ semantic.CaronteDispatcher = fakeDispatcher{}

var _ intent.CodeEmbedder = fakeEmbedder{}

var _ intent.CodeEmbedder = errorEmbedder{}

func seedGraph(t *testing.T, e *Engine) *projectEngine {
	t.Helper()
	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-1")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	nodes := []store.Node{
		{NodeID: "pkg/x.A", Name: "A", Kind: string(store.KindFunction), Language: "go", FilePath: "pkg/x/a.go", PackageID: "pkg/x", Coreness: 2, SCCID: 1, ContentHash: "ha"},
		{NodeID: "pkg/x.B", Name: "B", Kind: string(store.KindFunction), Language: "go", FilePath: "pkg/x/b.go", PackageID: "pkg/x", Coreness: 2, SCCID: 1, ContentHash: "hb"},
		{NodeID: "pkg/y.C", Name: "C", Kind: string(store.KindFunction), Language: "go", FilePath: "pkg/y/c.go", PackageID: "pkg/y", Coreness: 1, SCCID: 2, ContentHash: "hc"},
	}
	for _, n := range nodes {
		if err := pe.store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.NodeID, err)
		}
	}
	edges := []store.Edge{
		{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(store.EdgeCalls), Confidence: store.ConfExactVTA, SiteFile: "pkg/x/a.go", SiteLine: 10},
		{SourceID: "pkg/x.B", TargetID: "pkg/y.C", Kind: string(store.EdgeCalls), Confidence: store.ConfExactVTA, SiteFile: "pkg/x/b.go", SiteLine: 20},
	}
	for _, ed := range edges {
		if err := pe.store.UpsertEdge(ctx, ed); err != nil {
			t.Fatalf("UpsertEdge: %v", err)
		}
	}
	return pe
}

func newSeededEngine(t *testing.T) *Engine {
	t.Helper()
	deps, dirs := testDeps(t)
	dirs["proj-1"] = t.TempDir()
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestContextIsDistinctFromCodeGraph(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	ctx := context.Background()
	res, err := e.Context(ctx, "pkg/x.B", "proj-1")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if res.Symbol != "pkg/x.B" {
		t.Errorf("Context.Symbol = %q; want pkg/x.B", res.Symbol)
	}

	if !contains(res.Callers, "pkg/x.A") {
		t.Errorf("Context.Callers = %v; want pkg/x.A", res.Callers)
	}
	if !contains(res.Callees, "pkg/y.C") {
		t.Errorf("Context.Callees = %v; want pkg/y.C", res.Callees)
	}
	if res.Community != "pkg/x" {
		t.Errorf("Context.Community = %q; want pkg/x", res.Community)
	}
}

func TestGetImplementationsDelegates(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	impls, err := e.GetImplementations(context.Background(), "pkg/x.Shape", "proj-1")
	if err != nil {
		t.Fatalf("GetImplementations: %v", err)
	}
	if impls == nil {
		t.Error("GetImplementations returned nil slice; want non-nil (empty ok)")
	}
}

func TestTraceCallPathDelegates(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	hops, err := e.TraceCallPath(context.Background(), "pkg/x.A", 3, "proj-1")
	if err != nil {
		t.Fatalf("TraceCallPath: %v", err)
	}

	if len(hops) == 0 {
		t.Error("TraceCallPath returned no hops for a seeded A→B→C path")
	}
}

func TestGetHealthCountsGraph(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	h, err := e.GetHealth(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if h.ProjectID != "proj-1" {
		t.Errorf("GetHealth.ProjectID = %q; want proj-1", h.ProjectID)
	}
	if h.NodeCount != 3 {
		t.Errorf("GetHealth.NodeCount = %d; want 3", h.NodeCount)
	}
	if h.EdgeCount != 2 {
		t.Errorf("GetHealth.EdgeCount = %d; want 2", h.EdgeCount)
	}
}

func TestGetArchitectureSurfacesPackages(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	a, err := e.GetArchitecture(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetArchitecture: %v", err)
	}
	var sawX bool
	for _, p := range a.Packages {
		if p.PackageID == "pkg/x" {
			sawX = true
		}
	}
	if !sawX {
		t.Errorf("GetArchitecture.Packages missing pkg/x; got %+v", a.Packages)
	}
}

func TestGetCoChangeReturnsPeers(t *testing.T) {
	e := newSeededEngine(t)
	pe := seedGraph(t, e)
	ctx := context.Background()
	if err := pe.store.UpsertCoChange(ctx, store.CoChange{
		FileA: "pkg/x/a.go", FileB: "pkg/x/b.go",
		SharedRevs: 4, RevsA: 8, RevsB: 12, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertCoChange: %v", err)
	}
	peers, err := e.GetCoChange(ctx, "pkg/x/a.go", "proj-1")
	if err != nil {
		t.Fatalf("GetCoChange: %v", err)
	}
	var sawB bool
	for _, p := range peers {
		if p.Path == "pkg/x/b.go" {
			sawB = true
			if p.CouplingPercent <= 0 {
				t.Errorf("GetCoChange peer coupling = %v; want >0", p.CouplingPercent)
			}
		}
	}
	if !sawB {
		t.Errorf("GetCoChange(a.go) missed b.go peer; got %+v", peers)
	}
}

func TestGetWhyDelegates(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	ans, err := e.GetWhy(context.Background(), "proj-1", "pkg/x.A")
	if err != nil {
		t.Fatalf("GetWhy: %v", err)
	}
	if ans.Subject != "pkg/x.A" {
		t.Errorf("GetWhy.Subject = %q; want pkg/x.A", ans.Subject)
	}
}

func TestWikiGeneratesModuleMarkdown(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	w, err := e.Wiki(context.Background(), "pkg/x", "proj-1")
	if err != nil {
		t.Fatalf("Wiki: %v", err)
	}
	if w.Module != "pkg/x" || w.Markdown == "" {
		t.Errorf("Wiki = %+v; want module=pkg/x + non-empty markdown", w)
	}
}

func TestBlastRadiusDelegates(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	rs, err := e.BlastRadius(context.Background(), "proj-1", nil, nil)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if rs.Score != 0 {
		t.Errorf("BlastRadius(empty change).Score = %v; want 0", rs.Score)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestContextClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.Context(context.Background(), "pkg/x.A", "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("Context after Close = %v; want ErrEngineClosed", err)
	}
}

func TestContextUnknownProjectDegrades(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	res, err := e.Context(context.Background(), "pkg/x.A", "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("Context(unknown project) err = %v; want ErrProjectUnavailable", err)
	}
	if res.Symbol != "pkg/x.A" {
		t.Errorf("Context(unknown project).Symbol = %q; want pkg/x.A", res.Symbol)
	}
}

func TestBlastRadiusClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.BlastRadius(context.Background(), "proj-1", nil, nil)
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("BlastRadius after Close = %v; want ErrEngineClosed", err)
	}
}

func TestBlastRadiusUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	_, err := e.BlastRadius(context.Background(), "ghost", nil, nil)
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("BlastRadius(unknown) err = %v; want ErrProjectUnavailable", err)
	}
}

func TestGetWhyClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	ans, err := e.GetWhy(context.Background(), "proj-1", "pkg/x.A")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("GetWhy after Close = %v; want ErrEngineClosed", err)
	}
	if ans.Subject != "pkg/x.A" {
		t.Errorf("GetWhy after Close ans.Subject = %q; want pkg/x.A", ans.Subject)
	}
	if !ans.Degraded {
		t.Error("GetWhy after Close ans.Degraded = false; want true")
	}
}

func TestGetWhyUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	ans, err := e.GetWhy(context.Background(), "ghost", "pkg/x.A")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("GetWhy(unknown project) err = %v; want ErrProjectUnavailable", err)
	}
	if ans.Subject != "pkg/x.A" {
		t.Errorf("GetWhy(unknown project).Subject = %q; want pkg/x.A", ans.Subject)
	}
}

func TestGetImplementationsClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.GetImplementations(context.Background(), "pkg/x.Shape", "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("GetImplementations after Close = %v; want ErrEngineClosed", err)
	}
}

func TestTraceCallPathClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.TraceCallPath(context.Background(), "pkg/x.A", 3, "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("TraceCallPath after Close = %v; want ErrEngineClosed", err)
	}
}

func TestGetCoChangeClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.GetCoChange(context.Background(), "pkg/x/a.go", "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("GetCoChange after Close = %v; want ErrEngineClosed", err)
	}
}

func TestGetCoChangeUncoupledFileReturnsEmpty(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	peers, err := e.GetCoChange(context.Background(), "pkg/x/a.go", "proj-1")
	if err != nil {
		t.Fatalf("GetCoChange(uncoupled) err = %v; want nil", err)
	}
	if len(peers) != 0 {
		t.Errorf("GetCoChange(uncoupled) peers = %v; want empty", peers)
	}
}

func TestGetHealthClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	h, err := e.GetHealth(context.Background(), "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("GetHealth after Close = %v; want ErrEngineClosed", err)
	}
	if !h.Degraded {
		t.Error("GetHealth after Close .Degraded = false; want true")
	}
}

func TestGetArchitectureClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	_, err := e.GetArchitecture(context.Background(), "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("GetArchitecture after Close = %v; want ErrEngineClosed", err)
	}
}

func TestWikiClosedReturnsEngineClosed(t *testing.T) {
	e := newSeededEngine(t)
	_ = e.Close()
	w, err := e.Wiki(context.Background(), "pkg/x", "proj-1")
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("Wiki after Close = %v; want ErrEngineClosed", err)
	}
	if w.Module != "pkg/x" {
		t.Errorf("Wiki after Close .Module = %q; want pkg/x", w.Module)
	}
}

func TestDecompositionCacheHit(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	ctx := context.Background()
	pe, _ := e.projectEngineFor(ctx, "proj-1")
	d1, err := pe.decomposition(ctx)
	if err != nil {
		t.Fatalf("decomposition #1: %v", err)
	}
	d2, err := pe.decomposition(ctx)
	if err != nil {
		t.Fatalf("decomposition #2: %v", err)
	}
	if d1.HashKey != d2.HashKey {
		t.Errorf("decomposition cache: HashKey changed between calls (%q != %q)", d1.HashKey, d2.HashKey)
	}
}

func TestContextNeighborsDeterministic(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	ctx := context.Background()
	res1, _ := e.Context(ctx, "pkg/x.A", "proj-1")
	res2, _ := e.Context(ctx, "pkg/x.A", "proj-1")
	if len(res1.Neighbors) != len(res2.Neighbors) {
		t.Fatalf("Neighbors length changed between calls (%d vs %d)", len(res1.Neighbors), len(res2.Neighbors))
	}
	for i := range res1.Neighbors {
		if res1.Neighbors[i] != res2.Neighbors[i] {
			t.Errorf("Neighbors[%d]: %q != %q (not deterministic)", i, res1.Neighbors[i], res2.Neighbors[i])
		}
	}
}

func TestGetArchitectureDeterministic(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	ctx := context.Background()
	a1, _ := e.GetArchitecture(ctx, "proj-1")
	a2, _ := e.GetArchitecture(ctx, "proj-1")
	if len(a1.Packages) != len(a2.Packages) {
		t.Fatalf("Packages length changed (%d vs %d)", len(a1.Packages), len(a2.Packages))
	}
	for i := range a1.Packages {
		if a1.Packages[i].PackageID != a2.Packages[i].PackageID {
			t.Errorf("Packages[%d] not deterministic: %q vs %q", i, a1.Packages[i].PackageID, a2.Packages[i].PackageID)
		}
	}

	for i := 1; i < len(a1.Packages); i++ {
		if a1.Packages[i].PackageID < a1.Packages[i-1].PackageID {
			t.Errorf("Packages not sorted asc at [%d]: %q < %q", i, a1.Packages[i].PackageID, a1.Packages[i-1].PackageID)
		}
	}
}

func TestGetArchitectureUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	_, err := e.GetArchitecture(context.Background(), "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("GetArchitecture(unknown) err = %v; want ErrProjectUnavailable", err)
	}
}

func TestWikiUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	w, err := e.Wiki(context.Background(), "pkg/x", "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("Wiki(unknown) err = %v; want ErrProjectUnavailable", err)
	}
	if w.Module != "pkg/x" {
		t.Errorf("Wiki(unknown).Module = %q; want pkg/x", w.Module)
	}
}

func TestGetCoChangePeerFileABranch(t *testing.T) {
	e := newSeededEngine(t)
	pe := seedGraph(t, e)
	ctx := context.Background()

	if err := pe.store.UpsertCoChange(ctx, store.CoChange{
		FileA: "pkg/x/a.go", FileB: "pkg/x/b.go",
		SharedRevs: 3, RevsA: 6, RevsB: 9, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertCoChange: %v", err)
	}
	peers, err := e.GetCoChange(ctx, "pkg/x/b.go", "proj-1")
	if err != nil {
		t.Fatalf("GetCoChange: %v", err)
	}
	var sawA bool
	for _, p := range peers {
		if p.Path == "pkg/x/a.go" {
			sawA = true
		}
	}
	if !sawA {
		t.Errorf("GetCoChange(b.go) missed a.go peer (FileA branch); got %+v", peers)
	}
}

func TestGetImplementationsUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	_, err := e.GetImplementations(context.Background(), "I", "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("GetImplementations(unknown) err = %v; want ErrProjectUnavailable", err)
	}
}

func TestBlastRadiusWithSeededSymbols(t *testing.T) {
	e := newSeededEngine(t)
	seedGraph(t, e)
	rs, err := e.BlastRadius(context.Background(), "proj-1", []string{"pkg/x.A"}, nil)
	if err != nil {
		t.Fatalf("BlastRadius(seeded symbol): %v", err)
	}
	if rs.Score < 0 || rs.Score > 1 {
		t.Errorf("BlastRadius.Score = %v; want in [0,1]", rs.Score)
	}
}

func TestGetCoChangeUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	_, err := e.GetCoChange(context.Background(), "pkg/x/a.go", "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("GetCoChange(unknown) err = %v; want ErrProjectUnavailable", err)
	}
}

func TestGetHealthUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, _ := NewEngine(deps)
	defer e.Close()
	h, err := e.GetHealth(context.Background(), "ghost")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("GetHealth(unknown) err = %v; want ErrProjectUnavailable", err)
	}
	if !h.Degraded {
		t.Error("GetHealth(unknown).Degraded = false; want true")
	}
}

func seedCyclicGraph(t *testing.T, e *Engine) *projectEngine {
	t.Helper()
	ctx := context.Background()
	deps, _ := testDeps(t)
	_ = deps
	pe, err := e.projectEngineFor(ctx, "proj-1")
	if err != nil {
		t.Fatalf("seedCyclicGraph projectEngineFor: %v", err)
	}
	nodes := []store.Node{
		{NodeID: "cyc/x.Alpha", Name: "Alpha", Kind: string(store.KindFunction), Language: "go", FilePath: "cyc/x/alpha.go", PackageID: "cyc/x", ContentHash: "ca"},
		{NodeID: "cyc/x.Beta", Name: "Beta", Kind: string(store.KindFunction), Language: "go", FilePath: "cyc/x/beta.go", PackageID: "cyc/x", ContentHash: "cb"},
	}
	for _, n := range nodes {
		if err := pe.store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seedCyclicGraph UpsertNode %s: %v", n.NodeID, err)
		}
	}
	edges := []store.Edge{
		{SourceID: "cyc/x.Alpha", TargetID: "cyc/x.Beta", Kind: string(store.EdgeCalls), Confidence: store.ConfExactVTA, SiteFile: "cyc/x/alpha.go", SiteLine: 5},
		{SourceID: "cyc/x.Beta", TargetID: "cyc/x.Alpha", Kind: string(store.EdgeCalls), Confidence: store.ConfExactVTA, SiteFile: "cyc/x/beta.go", SiteLine: 5},
	}
	for _, ed := range edges {
		if err := pe.store.UpsertEdge(ctx, ed); err != nil {
			t.Fatalf("seedCyclicGraph UpsertEdge: %v", err)
		}
	}
	return pe
}

func newCyclicEngine(t *testing.T) *Engine {
	t.Helper()
	deps, dirs := testDeps(t)
	dirs["proj-1"] = t.TempDir()
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("newCyclicEngine NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestGetArchitectureExposesCycles(t *testing.T) {
	e := newCyclicEngine(t)
	seedCyclicGraph(t, e)
	a, err := e.GetArchitecture(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetArchitecture: %v", err)
	}
	if len(a.Cycles) == 0 {
		t.Errorf("GetArchitecture.Cycles = empty; want ≥1 cycle (Alpha↔Beta mutual call)")
	}
}

func TestGetHealthCyclicSCCCount(t *testing.T) {
	e := newCyclicEngine(t)
	seedCyclicGraph(t, e)
	h, err := e.GetHealth(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetHealth (cyclic): %v", err)
	}
	if h.CyclicSCCs == 0 {
		t.Errorf("GetHealth.CyclicSCCs = 0 with mutual-call cycle; want > 0")
	}
}

func TestGetCoChangeDifferentCouplingOrders(t *testing.T) {
	e := newSeededEngine(t)
	pe := seedGraph(t, e)
	ctx := context.Background()

	if err := pe.store.UpsertCoChange(ctx, store.CoChange{
		FileA: "pkg/x/a.go", FileB: "pkg/x/b.go",
		SharedRevs: 8, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertCoChange a-b: %v", err)
	}
	if err := pe.store.UpsertCoChange(ctx, store.CoChange{
		FileA: "pkg/x/a.go", FileB: "pkg/y/c.go",
		SharedRevs: 2, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertCoChange a-c: %v", err)
	}
	peers, err := e.GetCoChange(ctx, "pkg/x/a.go", "proj-1")
	if err != nil {
		t.Fatalf("GetCoChange: %v", err)
	}
	if len(peers) < 2 {
		t.Fatalf("expected ≥2 peers; got %d", len(peers))
	}

	if peers[0].CouplingPercent < peers[1].CouplingPercent {
		t.Errorf("peers not sorted by coupling desc: [0]=%v < [1]=%v", peers[0].CouplingPercent, peers[1].CouplingPercent)
	}
}
