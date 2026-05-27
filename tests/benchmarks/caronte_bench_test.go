// go:build benchmark && cgo
package benchmarks

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type benchDispatcher struct{}

func (benchDispatcher) Forward(_ context.Context, _ orchestrator.Call) (*providers.TierResponse, error) {
	return &providers.TierResponse{Status: 200, Body: []byte(`{}`)}, nil
}

type benchEmbedder struct{}

func (benchEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 1536)
	v[len(text)%1536] = 1.0
	return v, nil
}
func (benchEmbedder) Dimensions() int { return 1536 }

type corpusEntry struct {
	name      string
	pkgs      int
	fnsPerPkg int
}

var corpusScales = []corpusEntry{

	{"small_10k_loc", 50, 40},

	{"target_500k_loc", 1000, 100},
}

func generateCorpus(b *testing.B, dir string, pkgs, fnsPerPkg int) (loc int, diskBytes int64) {
	b.Helper()
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module corpus\n\ngo 1.25\n"),
		0o644,
	); err != nil {
		b.Fatalf("generateCorpus: write go.mod: %v", err)
	}
	for p := 0; p < pkgs; p++ {
		pkgDir := filepath.Join(dir, fmt.Sprintf("pkg%d", p))
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			b.Fatalf("generateCorpus: mkdir pkg%d: %v", p, err)
		}
		src := fmt.Sprintf("// Package pkg%d is a generated benchmark corpus package.\npackage pkg%d\n\n", p, p)
		for f := 0; f < fnsPerPkg; f++ {

			chainCall := ""
			if f+1 < fnsPerPkg {
				chainCall = fmt.Sprintf("\tF%d()\n", f+1)
			}
			crossPkg := ""
			if f == 0 && p+1 < pkgs {

				crossPkg = fmt.Sprintf("\t_ = %d // corpus cross-pkg coupling\n", p+1)
			}
			src += fmt.Sprintf(
				"// F%d is generated benchmark function %d in pkg%d.\nfunc F%d() {\n%s%s}\n\n",
				f, f, p, f, chainCall, crossPkg,
			)
		}
		path := filepath.Join(pkgDir, "gen.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			b.Fatalf("generateCorpus: write %s: %v", path, err)
		}
		loc += countLines(src)
		diskBytes += int64(len(src))
	}
	return loc, diskBytes
}

func countLines(s string) (n int) {
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

func openBenchDB(b *testing.B, dir, name string) *sql.DB {
	b.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(dir, name+".caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		b.Fatalf("openBenchDB: sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func openBenchStore(b *testing.B, dir, name string) *store.Store {
	b.Helper()
	db := openBenchDB(b, dir, name)
	ctx := context.Background()
	s, err := store.Open(ctx, db)
	if err != nil {
		b.Fatalf("openBenchStore: store.Open: %v", err)
	}
	return s
}

func newBenchParser(b *testing.B) *parser.Parser {
	b.Helper()
	p, err := parser.NewParser()
	if err != nil {
		b.Fatalf("newBenchParser: NewParser: %v", err)
	}
	return p
}

func indexCorpusDir(ctx context.Context, b *testing.B, idx *parser.Indexer, dir string) (total parser.IndexReport, files int) {
	b.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("read %s: %w", path, rerr)
		}
		rep, ierr := idx.IndexFile(ctx, path, src)
		if ierr != nil {
			return fmt.Errorf("IndexFile %s: %w", path, ierr)
		}
		total.Written += rep.Written
		total.Skipped += rep.Skipped
		files++
		return nil
	})
	if err != nil {
		b.Fatalf("indexCorpusDir: %v", err)
	}
	return total, files
}

func newBenchEngine(b *testing.B, dbDir string) (*caronte.Engine, func()) {
	b.Helper()
	sqlite_vec.Auto()
	var (
		mu  sync.Mutex
		dbs = map[string]*sql.DB{}
	)
	openDB := func(_ context.Context, projectID string) (*sql.DB, error) {
		mu.Lock()
		defer mu.Unlock()
		if db, ok := dbs[projectID]; ok {
			return db, nil
		}
		dbPath := filepath.Join(dbDir, projectID+".caronte.db")
		dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
		db, err := sql.Open(store.DefaultDriver, dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		dbs[projectID] = db
		return db, nil
	}
	eng, err := caronte.NewEngine(caronte.Deps{
		OpenProjectDB: openDB,
		Dispatcher:    benchDispatcher{},
		Embedder:      benchEmbedder{},
		Reranker:      nil,
		AuditEmit:     func(string, []byte) {},
		Params:        nil,
		IntentParams:  intent.DefaultIntentParams(intent.IntentParams{}),
		RepoRootFor:   nil,
	})
	if err != nil {
		b.Fatalf("newBenchEngine: NewEngine: %v", err)
	}
	return eng, func() {
		_ = eng.Close()
		mu.Lock()
		for _, db := range dbs {
			_ = db.Close()
		}
		mu.Unlock()
	}
}

func seedEngineWithCorpus(ctx context.Context, b *testing.B, eng *caronte.Engine, projectID, corpusDir string) {
	b.Helper()

	_, _ = eng.CodeGraph(ctx, "warmup", projectID)

	dbDir := b.TempDir()

	sqlite_vec.Auto()
	dbPath := filepath.Join(dbDir, projectID+".caronte.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		_, _ = eng.CodeGraph(ctx, "trigger-open", projectID)
	}
	_ = dbPath
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	c := make([]time.Duration, len(d))
	copy(c, d)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	idx := int(float64(len(c)-1) * p)
	return c[idx]
}

func BenchmarkCaronteFullParse(b *testing.B) {
	for _, sc := range corpusScales {
		sc := sc
		b.Run(sc.name, func(b *testing.B) {
			if testing.Short() && sc.name == "target_500k_loc" {
				b.Skip("skipping 500k-LOC corpus under -short (sandbox gate uses small_10k_loc)")
			}

			corpusDir := b.TempDir()
			loc, diskBytes := generateCorpus(b, corpusDir, sc.pkgs, sc.fnsPerPkg)
			b.Logf("corpus: %d LOC, %.1f MB on disk", loc, float64(diskBytes)/(1<<20))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {

				b.StopTimer()
				s := openBenchStore(b, b.TempDir(), "bench")
				p := newBenchParser(b)
				idx := parser.NewIndexer(p, s)
				b.StartTimer()

				ctx := context.Background()
				rep, files := indexCorpusDir(ctx, b, idx, corpusDir)
				_ = rep
				_ = files

				b.StopTimer()
				idx.Close()
				b.StartTimer()
			}
			b.ReportMetric(float64(loc), "loc")
		})
	}
}

func BenchmarkCaronteIncrementalReparse(b *testing.B) {
	const sampleCap = 200

	corpusDir := b.TempDir()
	loc, diskBytes := generateCorpus(b, corpusDir, 200, 80)
	b.Logf("incremental corpus: %d LOC, %.1f MB on disk", loc, float64(diskBytes)/(1<<20))

	storeDir := b.TempDir()
	s := openBenchStore(b, storeDir, "incremental")
	p := newBenchParser(b)
	idx := parser.NewIndexer(p, s)
	defer idx.Close()
	ctx := context.Background()
	_, _ = indexCorpusDir(ctx, b, idx, corpusDir)

	editTarget := filepath.Join(corpusDir, "pkg0", "gen.go")
	origContent, err := os.ReadFile(editTarget)
	if err != nil {
		b.Fatalf("read edit target: %v", err)
	}

	lat := make([]time.Duration, 0, sampleCap)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		edited := append(append([]byte(nil), origContent...),
			[]byte(fmt.Sprintf("\nfunc Edit%d() { /* incremental bench edit */ }\n", i))...)
		if werr := os.WriteFile(editTarget, edited, 0o644); werr != nil {
			b.Fatalf("write edit target: %v", werr)
		}

		start := time.Now()
		indexCorpusDir(ctx, b, idx, corpusDir)
		d := time.Since(start)
		if len(lat) < sampleCap {
			lat = append(lat, d)
		}
	}
	b.StopTimer()

	if p95 := percentile(lat, 0.95); p95 > 0 {
		b.ReportMetric(float64(p95.Milliseconds()), "p95-ms")
	}

	_ = os.WriteFile(editTarget, origContent, 0o644)
}

func BenchmarkCaronteQuery(b *testing.B) {
	const sampleCap = 500

	corpusDir := b.TempDir()
	loc, diskBytes := generateCorpus(b, corpusDir, 50, 40)
	b.Logf("query corpus: %d LOC, %.1f MB on disk", loc, float64(diskBytes)/(1<<20))

	dbDir := b.TempDir()
	eng, teardown := newBenchEngine(b, dbDir)
	defer teardown()
	ctx := context.Background()

	sqlite_vec.Auto()
	dbPath := filepath.Join(dbDir, "bench.caronte.db")
	seedDB, openErr := sql.Open(store.DefaultDriver,
		dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL")
	if openErr != nil {
		b.Fatalf("seed DB open: %v", openErr)
	}
	seedDB.SetMaxOpenConns(1)
	defer func() { _ = seedDB.Close() }()
	seedStore, openErr := store.Open(ctx, seedDB)
	if openErr != nil {
		b.Fatalf("seed store.Open: %v", openErr)
	}

	seedParser := newBenchParser(b)
	seedIdx := parser.NewIndexer(seedParser, seedStore)
	_, _ = indexCorpusDir(ctx, b, seedIdx, corpusDir)
	seedIdx.Close()

	emb := benchEmbedder{}
	rows, rowErr := seedDB.QueryContext(ctx, "SELECT node_id, doc FROM graph_nodes LIMIT 2000")
	if rowErr != nil {
		b.Fatalf("query nodes for vector seeding: %v", rowErr)
	}
	type nodeRow struct{ id, doc string }
	var nodes []nodeRow
	for rows.Next() {
		var nr nodeRow
		if serr := rows.Scan(&nr.id, &nr.doc); serr != nil {
			_ = rows.Close()
			b.Fatalf("scan node row: %v", serr)
		}
		nodes = append(nodes, nr)
	}
	_ = rows.Close()
	for _, nr := range nodes {
		vec, _ := emb.Embed(ctx, nr.doc)
		if verr := seedStore.UpsertNodeVector(ctx, nr.id, vec); verr != nil {
			b.Fatalf("UpsertNodeVector %s: %v", nr.id, verr)
		}
	}
	_ = seedDB.Close()

	if _, werr := eng.CodeGraph(ctx, "warmup", "bench"); werr != nil {

		b.Logf("warmup query: %v (non-fatal if vec0 still empty)", werr)
	}

	lat := make([]time.Duration, 0, sampleCap)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, qerr := eng.CodeGraph(ctx, "F0", "bench"); qerr != nil {

			b.Logf("CodeGraph iteration %d: %v", i, qerr)
		}
		d := time.Since(start)
		if len(lat) < sampleCap {
			lat = append(lat, d)
		}
	}
	b.StopTimer()

	if p95 := percentile(lat, 0.95); p95 > 0 {
		b.ReportMetric(float64(p95.Microseconds())/1000.0, "p95-ms")
	}
}

var (
	_ semantic.CaronteDispatcher = benchDispatcher{}
	_ intent.CodeEmbedder        = benchEmbedder{}
)
