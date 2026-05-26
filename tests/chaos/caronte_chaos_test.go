//go:build chaos && cgo

package chaos

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// chaosDispatcher satisfies semantic.CaronteDispatcher (single-egress LLM
// seam). When dispatchErr is nil it returns a canned empty TierResponse so
// the residual-tail path is exercised without Ollama. When dispatchErr is
// set it simulates the LLM tail being down (spec §15: omit llm_hint edges,
// do not block the engine).
type chaosDispatcher struct {
	calls       atomic.Int64
	dispatchErr error
}

func (d *chaosDispatcher) Forward(_ context.Context, _ orchestrator.Call) (*providers.TierResponse, error) {
	d.calls.Add(1)
	if d.dispatchErr != nil {
		return nil, d.dispatchErr
	}

	return &providers.TierResponse{Status: 200, Body: []byte(`{}`)}, nil
}

var _ semantic.CaronteDispatcher = (*chaosDispatcher)(nil)

type chaosEmbedder struct{}

func (chaosEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 1536)
	if len(text) > 0 {
		v[len(text)%1536] = 1.0
	} else {
		v[0] = 1.0
	}
	return v, nil
}
func (chaosEmbedder) Dimensions() int { return 1536 }

func chaosRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not resolve this file path")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (no go.mod found in ancestors of %s)", dir)
		}
		dir = parent
	}
	t.Fatalf("repo root not found within 8 levels of %s", filepath.Dir(thisFile))
	return ""
}

func openChaosDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("openChaosDB mkdir: %v", err)
	}
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("openChaosDB sql.Open %s: %v", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

func seedStoreNodes(t *testing.T, ctx context.Context, st *store.Store, projectID string, n int) {
	t.Helper()
	emb := chaosEmbedder{}
	for i := 0; i < n; i++ {
		nid := fmt.Sprintf("%s.F%d", projectID, i)
		doc := fmt.Sprintf("F%d is fixture function %d.", i, i)
		nd := store.Node{
			NodeID: nid, Name: fmt.Sprintf("F%d", i),
			Kind: string(store.KindFunction), Language: "go",
			FilePath: fmt.Sprintf("fix/fix.go"), Doc: doc, ContentHash: fmt.Sprintf("h%d", i),
		}
		if err := st.UpsertNode(ctx, nd); err != nil {
			t.Fatalf("seedStoreNodes UpsertNode %s: %v", nid, err)
		}
		vec, _ := emb.Embed(ctx, doc)
		if err := st.UpsertNodeVector(ctx, nid, vec); err != nil {
			t.Fatalf("seedStoreNodes UpsertNodeVector %s: %v", nid, err)
		}
	}
}

func newChaosEngine(t *testing.T, projectID string, db *sql.DB, disp *chaosDispatcher) (*caronte.Engine, func()) {
	t.Helper()
	openDB := func(_ context.Context, pid string) (*sql.DB, error) {
		if pid == projectID {
			return db, nil
		}
		return nil, fmt.Errorf("chaos: unknown project %q", pid)
	}
	eng, err := caronte.NewEngine(caronte.Deps{
		OpenProjectDB: openDB,
		Dispatcher:    disp,
		Embedder:      chaosEmbedder{},

		Reranker:     nil,
		AuditEmit:    func(string, []byte) {},
		Params:       chaosParamsAccessor{p: evolution.DefaultParams()},
		IntentParams: intent.DefaultIntentParams(intent.IntentParams{}),

		RepoRootFor: nil,
	})
	if err != nil {
		t.Fatalf("newChaosEngine: %v", err)
	}
	teardown := func() { _ = eng.Close() }
	return eng, teardown
}

type chaosParamsAccessor struct{ p evolution.Params }

func (s chaosParamsAccessor) CoChangeParams(string) evolution.Params { return s.p }

var _ evolution.ParamsAccessor = chaosParamsAccessor{}

func TestCaronteConcurrentReindexAndQuery(t *testing.T) {
	root := chaosRepoRoot(t)
	srcDir := filepath.Join(root, "internal", "caronte", "semantic", "testdata", "buildable")
	dbPath := filepath.Join(t.TempDir(), "caronte_concurrent.db")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	db := openChaosDB(t, dbPath)
	defer db.Close()

	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	const projectID = "chaos-concurrent"
	seedStoreNodes(t, ctx, st, projectID, 12)

	disp := &chaosDispatcher{}
	eng, teardown := newChaosEngine(t, projectID, db, disp)
	defer teardown()

	if _, err := eng.CodeGraph(ctx, "F0", projectID); err != nil {
		t.Fatalf("initial CodeGraph: %v", err)
	}

	st2, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("store.Open for resolver: %v", err)
	}
	resolver := semantic.NewResolver(st2, disp, semantic.ResolverOpts{})

	const queriers = 24
	var wg sync.WaitGroup
	wg.Add(queriers + 1)

	go func() {
		defer wg.Done()
		for i := 0; i < 4; i++ {
			if _, err := resolver.ResolveProject(ctx, projectID, srcDir); err != nil {

				t.Errorf("concurrent ResolveProject %d: %v", i, err)
				return
			}
		}
	}()

	var queryErrs atomic.Int64
	for i := 0; i < queriers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := eng.CodeGraph(ctx, "F0", projectID); err != nil {
					queryErrs.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Queries MUST NOT hard-fail during a concurrent re-index (WAL readers
	// see a consistent snapshot; the writer serializes behind busy_timeout).
	if n := queryErrs.Load(); n != 0 {
		t.Errorf("%d query goroutines hard-failed during concurrent re-index; expected 0 (WAL read concurrency)", n)
	}
}

func TestCaronteRestartMidIndex(t *testing.T) {
	root := chaosRepoRoot(t)
	srcDir := filepath.Join(root, "internal", "caronte", "semantic", "testdata", "buildable")
	dbPath := filepath.Join(t.TempDir(), "caronte_restart.db")

	const projectID = "chaos-restart"

	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	db1 := openChaosDB(t, dbPath)

	st1, err := store.Open(ctx1, db1)
	if err != nil {
		cancel1()
		db1.Close()
		t.Fatalf("store.Open (eng1): %v", err)
	}
	seedStoreNodes(t, ctx1, st1, projectID, 20)

	disp1 := &chaosDispatcher{}
	eng1, teardown1 := newChaosEngine(t, projectID, db1, disp1)

	if _, err := eng1.CodeGraph(ctx1, "F0", projectID); err != nil {
		teardown1()
		cancel1()
		db1.Close()
		t.Fatalf("initial CodeGraph (eng1): %v", err)
	}

	resolver1 := semantic.NewResolver(st1, disp1, semantic.ResolverOpts{})

	idxDone := make(chan error, 1)
	go func() {
		idxDone <- func() error { _, err := resolver1.ResolveProject(ctx1, projectID, srcDir); return err }()
	}()

	time.Sleep(30 * time.Millisecond)
	cancel1()
	teardown1()
	db1.Close()
	<-idxDone

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	db2 := openChaosDB(t, dbPath)
	defer db2.Close()

	st2, err := store.Open(ctx2, db2)
	if err != nil {
		t.Fatalf("store.Open (eng2): %v (orphaned WAL lock or corrupt state?)", err)
	}

	disp2 := &chaosDispatcher{}
	eng2, teardown2 := newChaosEngine(t, projectID, db2, disp2)
	defer teardown2()

	resolver2 := semantic.NewResolver(st2, disp2, semantic.ResolverOpts{})
	if _, err := resolver2.ResolveProject(ctx2, projectID, srcDir); err != nil {
		t.Fatalf("fresh engine ResolveProject after restart: %v (orphaned WAL lock or corrupt state?)", err)
	}

	res, err := eng2.CodeGraph(ctx2, "F0", projectID)
	if err != nil {
		t.Fatalf("fresh engine CodeGraph after restart: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("fresh engine returned 0 hits after restart; expected ≥1 (state recovered from WAL)")
	}
}

func TestCaronteBuildBrokenFallsBackToCHAUnderLoad(t *testing.T) {
	root := chaosRepoRoot(t)
	buildableDir := filepath.Join(root, "internal", "caronte", "semantic", "testdata", "buildable")
	brokenDir := filepath.Join(root, "internal", "caronte", "semantic", "testdata", "broken")
	dbPath := filepath.Join(t.TempDir(), "caronte_broken.db")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	db := openChaosDB(t, dbPath)
	defer db.Close()

	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	const projectID = "chaos-broken-build"
	seedStoreNodes(t, ctx, st, projectID, 16)

	disp := &chaosDispatcher{}
	eng, teardown := newChaosEngine(t, projectID, db, disp)
	defer teardown()

	if _, err := eng.CodeGraph(ctx, "F0", projectID); err != nil {
		t.Fatalf("initial CodeGraph: %v", err)
	}
	resolver := semantic.NewResolver(st, disp, semantic.ResolverOpts{})
	if _, initStats := resolver.ResolveProject(ctx, projectID, buildableDir); initStats != nil {

		t.Logf("initial ResolveProject (buildable): %v", initStats)
	}

	const queriers = 16
	var wg sync.WaitGroup
	wg.Add(queriers + 1)

	var reindexHardFailed atomic.Bool
	go func() {
		defer wg.Done()
		// Re-index over the BROKEN source. Per inv-zen-234 this MUST NOT
		// hard-fail — it classifies to CHA and serves a sound over-approximation.
		_, err := resolver.ResolveProject(ctx, projectID, brokenDir)
		if err != nil {
			reindexHardFailed.Store(true)
			t.Errorf("re-index over broken build hard-failed: %v; expected CHA fallback / graceful degrade (inv-zen-234)", err)
		}
	}()

	var blankQueries atomic.Int64
	for i := 0; i < queriers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				res, err := eng.CodeGraph(ctx, "F0", projectID)
				if err != nil {

					t.Errorf("query during broken-build window hard-failed: %v", err)
					return
				}
				if len(res.Hits) == 0 {
					blankQueries.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if reindexHardFailed.Load() {
		t.Errorf("re-index over broken build hard-failed; expected CHA fallback / stale snapshot (inv-zen-234)")
	}

	if blankQueries.Load() > int64(queriers) {

		t.Errorf("%d / %d query goroutines returned blank during broken-build window; "+
			"expected the seeded nodes to keep serving (spec §15)",
			blankQueries.Load(), queriers*15)
	}
}

func TestCaronteDegradedLayerServesWhatItCan(t *testing.T) {
	root := chaosRepoRoot(t)
	srcDir := filepath.Join(root, "internal", "caronte", "semantic", "testdata", "buildable")
	dbPath := filepath.Join(t.TempDir(), "caronte_degraded.db")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := openChaosDB(t, dbPath)
	defer db.Close()

	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	disp := &chaosDispatcher{dispatchErr: fmt.Errorf("ollama: connection refused (simulated)")}

	const projectID = "chaos-degraded-layer"
	seedStoreNodes(t, ctx, st, projectID, 10)

	eng, teardown := newChaosEngine(t, projectID, db, disp)
	defer teardown()

	resolver := semantic.NewResolver(st, disp, semantic.ResolverOpts{})
	stats, err := resolver.ResolveProject(ctx, projectID, srcDir)
	if err != nil {
		t.Fatalf("ResolveProject with LLM tail down hard-failed: %v; expected graceful degrade (spec §15)", err)
	}

	if stats.LLMHintEdges != 0 {
		t.Errorf("LLMHintEdges = %d with dispatcher down; expected 0 (tail silently omitted)", stats.LLMHintEdges)
	}

	if stats.Mode != semantic.ModeVTA && stats.Mode != semantic.ModeCHA {
		t.Errorf("ResolveProject mode = %q with LLM down; expected vta or cha (static layers served)", stats.Mode)
	}

	// CodeGraph MUST NOT hard-fail just because the LLM tail is down.
	res, err := eng.CodeGraph(ctx, "F0", projectID)
	if err != nil {
		t.Fatalf("CodeGraph with LLM tail down hard-failed: %v; expected static-layer results", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("CodeGraph with LLM tail down returned 0 hits; expected the seeded static nodes to serve")
	}

	// GetHealth must report the project as NOT degraded — the static layers
	// (parse + resolve) worked; only the llm_hint edges are absent. That is
	// the engine's correct self-report (spec §15: "LLM tail unavailable → omit
	// llm_hint edges, mark unresolved; do not block").
	health, err := eng.GetHealth(ctx, projectID)
	if err != nil {
		t.Fatalf("GetHealth with LLM tail down: %v", err)
	}
	if health.Degraded {
		t.Errorf("GetHealth.Degraded = true with LLM tail down; expected false (static layers served; LLM tail absent is not engine-degraded)")
	}
	if health.NodeCount == 0 {
		t.Errorf("GetHealth.NodeCount = 0; expected ≥1 (seeded nodes visible)")
	}
}
