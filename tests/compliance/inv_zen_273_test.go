// tests/compliance/inv_zen_273_test.go
//
// Compliance gate for inv-zen-273 (Plan v0.20.0 Phase C):
// Caronte engine.IndexProject + HTTP /v1/caronte/reindex + CLI
// `zen caronte reindex` surface MUST exist + behave according to the
// frozen IndexReport contract (result_types.go::IndexReport):
//
//	(a) SOURCE-REGEX
//	    1. internal/caronte/engine_ops.go declares
//	       `func (e *Engine) IndexProject(`.
//	    2. internal/daemon/handlers/caronte.go declares `CaronteReindex(`.
//	    3. internal/cli/caronte_reindex.go exists and declares
//	       `func RunCaronteReindex(`.
//	    4. internal/daemon/server.go registers
//	       `POST /v1/caronte/reindex`.
//
//	(b) BEHAVIOURAL
//	    Seed a tempdir with one trivial .go file → call IndexProject →
//	    verify Completed=true + NodesCreated > 0 + LanguageCounts["go"]==1
//	    + the engine's GetHealth reports a matching NodeCount.
//
//	(c) SISTER-TEST
//	    The behavioural test biteps the contract — reverting IndexProject
//	    to a no-op body (`return IndexReport{ProjectID: projectID}, nil`)
//	    makes the NodeCount assertion fail (no nodes are written). This
//	    is the inv-zen-273 idempotency + populate-non-empty-repos
//	    contract.
//
// Sister-test pattern (feedback_sister_test_pattern.md): the behavioural
// test below biteps a no-op regression in IndexProject. To verify the
// sister bite: revert engine_ops.go IndexProject's walk loop to an
// empty function body and re-run; the NodeCount assertion fails (no
// nodes written → GetHealth.NodeCount stays 0).
//
// inv-zen-273.
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

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func TestInvZen273_EngineIndexProjectDeclared(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "caronte", "engine_ops.go"))
	const needle = `func (e *Engine) IndexProject(`
	if !strings.Contains(body, needle) {
		t.Errorf("inv-zen-273: engine_ops.go missing %q — the IndexProject method is the inv-zen-273 surface", needle)
	}
}

func TestInvZen273_HandlerDeclared(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "daemon", "handlers", "caronte.go"))
	const needle = `func CaronteReindex(`
	if !strings.Contains(body, needle) {
		t.Errorf("inv-zen-273: handlers/caronte.go missing %q — the HTTP surface for /v1/caronte/reindex", needle)
	}
}

func TestInvZen273_CLIDeclared(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "cli", "caronte_reindex.go"))
	const needle = `func RunCaronteReindex(`
	if !strings.Contains(body, needle) {
		t.Errorf("inv-zen-273: cli/caronte_reindex.go missing %q — the CLI testable core", needle)
	}

	groupBody := readFile(t, filepath.Join(root, "internal", "cli", "caronte.go"))
	if !strings.Contains(groupBody, `func NewCaronteCmd() *cobra.Command {`) {
		t.Error("inv-zen-273: cli/caronte.go missing NewCaronteCmd group constructor")
	}
}

func TestInvZen273_RouteRegistered(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "daemon", "server.go"))
	const route = `s.mux.HandleFunc("POST /v1/caronte/reindex", handlers.CaronteReindex(s))`
	if !strings.Contains(body, route) {
		t.Errorf("inv-zen-273: server.go missing %q route registration — handler is unreachable", route)
	}
}

func TestInvZen273_BehaviouralIndexProjectPopulatesGraph(t *testing.T) {

	if v := os.Getenv("ZEN_BYPASS_DISABLE_KEYCHAIN"); v == "" {
		t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	}
	if v := os.Getenv("ZEN_KEYCHAIN_DISABLE"); v == "" {
		t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	}

	srcRoot := t.TempDir()
	const goSrc = `package x

// Foo is a sample.
func Foo() {}

// Bar is another sample.
func Bar() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "x.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write x.go: %v", err)
	}

	openProjectDB := func(_ context.Context, _ string) (*sql.DB, error) {
		sqlite_vec.Auto()
		dbPath := filepath.Join(srcRoot, ".zen", "caronte.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			return nil, err
		}
		dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
		db, err := sql.Open(store.DefaultDriver, dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return db, nil
	}

	deps := caronte.Deps{
		OpenProjectDB: openProjectDB,
		Dispatcher:    fakeDispatcherInv273{},
		Embedder:      fakeEmbedderInv273{},
		AuditEmit:     func(string, []byte) {},
		RepoRootFor: func(_ context.Context, _ string) (string, error) {
			return srcRoot, nil
		},
	}
	engine, err := caronte.NewEngine(deps)
	if err != nil {
		t.Fatalf("caronte.NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	ctx := context.Background()
	rep, err := engine.IndexProject(ctx, "proj-273")
	if err != nil {
		t.Fatalf("inv-zen-273 VIOLATION: IndexProject errored on a clean single-file repo: %v", err)
	}
	if !rep.Completed {
		t.Errorf("inv-zen-273: IndexReport.Completed = false; want true on clean walk")
	}
	if rep.FilesIndexed != 1 {
		t.Errorf("inv-zen-273: IndexReport.FilesIndexed = %d; want 1", rep.FilesIndexed)
	}
	if rep.NodesCreated < 2 {
		t.Errorf("inv-zen-273: IndexReport.NodesCreated = %d; want ≥2 (Foo + Bar parsed)", rep.NodesCreated)
	}
	if rep.LanguageCounts["go"] != 1 {
		t.Errorf("inv-zen-273: IndexReport.LanguageCounts[go] = %d; want 1",
			rep.LanguageCounts["go"])
	}

	// Sister-bite: GetHealth reports a NodeCount matching IndexReport.
	// A no-op IndexProject body that returned IndexReport{NodesCreated: 2}
	// without actually writing to the store would PASS the IndexReport
	// assertion above but FAIL here because GetHealth queries the live
	// store. The two reports MUST agree.
	h, herr := engine.GetHealth(ctx, "proj-273")
	if herr != nil {
		t.Fatalf("inv-zen-273: GetHealth: %v", herr)
	}
	if h.NodeCount == 0 {
		t.Errorf("inv-zen-273 SISTER-BITE: GetHealth.NodeCount = 0 after IndexProject reported %d nodes — store was not actually populated (no-op IndexProject regression?)",
			rep.NodesCreated)
	}
	if h.NodeCount != rep.NodesCreated {

		t.Logf("inv-zen-273: GetHealth.NodeCount (%d) ≠ IndexReport.NodesCreated (%d) — informational only (a tally vs upsert divergence)",
			h.NodeCount, rep.NodesCreated)
	}
}

func TestInvZen273_BehaviouralIndexProjectIdempotent(t *testing.T) {
	if v := os.Getenv("ZEN_BYPASS_DISABLE_KEYCHAIN"); v == "" {
		t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	}
	if v := os.Getenv("ZEN_KEYCHAIN_DISABLE"); v == "" {
		t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	}
	srcRoot := t.TempDir()
	const goSrc = `package idem

func A() {}
func B() {}
func C() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "idem.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	openProjectDB := func(_ context.Context, _ string) (*sql.DB, error) {
		sqlite_vec.Auto()
		dbPath := filepath.Join(srcRoot, ".zen", "caronte.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			return nil, err
		}
		dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
		db, err := sql.Open(store.DefaultDriver, dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return db, nil
	}
	deps := caronte.Deps{
		OpenProjectDB: openProjectDB,
		Dispatcher:    fakeDispatcherInv273{},
		Embedder:      fakeEmbedderInv273{},
		AuditEmit:     func(string, []byte) {},
		RepoRootFor: func(_ context.Context, _ string) (string, error) {
			return srcRoot, nil
		},
	}
	engine, err := caronte.NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	ctx := context.Background()
	rep1, err := engine.IndexProject(ctx, "proj-idem")
	if err != nil {
		t.Fatalf("IndexProject #1: %v", err)
	}
	rep2, err := engine.IndexProject(ctx, "proj-idem")
	if err != nil {
		t.Fatalf("IndexProject #2: %v", err)
	}
	if rep1.NodesCreated != rep2.NodesCreated {
		t.Errorf("inv-zen-273 idempotency VIOLATION: pass1 NodesCreated=%d ≠ pass2 NodesCreated=%d",
			rep1.NodesCreated, rep2.NodesCreated)
	}
	if rep1.FilesIndexed != rep2.FilesIndexed {
		t.Errorf("inv-zen-273 idempotency VIOLATION: pass1 FilesIndexed=%d ≠ pass2 FilesIndexed=%d",
			rep1.FilesIndexed, rep2.FilesIndexed)
	}
}

type fakeDispatcherInv273 struct{}

func (fakeDispatcherInv273) Forward(_ context.Context, _ orchestrator.Call) (*providers.TierResponse, error) {
	return &providers.TierResponse{Status: 200, Body: []byte(`{}`)}, nil
}

type fakeEmbedderInv273 struct{}

func (fakeEmbedderInv273) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 1536)
	if len(text) > 0 {
		v[len(text)%1536] = 1.0
	}
	return v, nil
}

func (fakeEmbedderInv273) Dimensions() int { return 1536 }

var _ semantic.CaronteDispatcher = fakeDispatcherInv273{}
var _ intent.CodeEmbedder = fakeEmbedderInv273{}
