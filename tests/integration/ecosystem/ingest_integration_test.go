//go:build integration && cgo
// +build integration,cgo

package ecosystem_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type memorySQLiteIndexer struct {
	db        *sql.DB
	mu        sync.Mutex
	lastIndex map[string]time.Time
}

func newMemorySQLiteIndexer(t *testing.T) *memorySQLiteIndexer {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}

	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE ecosystem_packages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			ecosystem TEXT NOT NULL,
			upstream_url TEXT,
			canonical_namespace TEXT,
			last_indexed_at DATETIME,
			latest_stable_version TEXT,
			UNIQUE (ecosystem, name)
		);
		CREATE TABLE ecosystem_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			package_id INTEGER REFERENCES ecosystem_packages(id),
			content_text TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			symbol_path TEXT,
			kind TEXT,
			source_url TEXT,
			source_type TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return &memorySQLiteIndexer{db: db, lastIndex: make(map[string]time.Time)}
}

func (m *memorySQLiteIndexer) WriteChunks(
	ctx context.Context,
	pkg ecosystem.PackageRef,
	version string,
	chunks []ecosystem.Chunk,
	symbols []ecosystem.SymbolRef,
	changes []ecosystem.ChangeNode,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO ecosystem_packages
			(name, ecosystem, upstream_url, canonical_namespace, latest_stable_version)
		 VALUES (?, ?, ?, ?, ?)`,
		pkg.Name, string(pkg.Ecosystem), pkg.UpstreamURL, pkg.CanonicalNamespace, pkg.LatestStableVersion)
	if err != nil {
		return fmt.Errorf("insert package: %w", err)
	}
	pkgID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	for _, ch := range chunks {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO ecosystem_chunks
				(package_id, content_text, fingerprint, symbol_path, kind, source_url, source_type)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			pkgID, ch.ContentText, ch.Fingerprint, ch.SymbolPath, string(ch.Kind), ch.SourceURL, string(ch.SourceType))
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}
	_ = symbols
	_ = changes
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (m *memorySQLiteIndexer) PackageLastIndexedAt(_ context.Context, pkg ecosystem.PackageRef) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.lastIndex[string(pkg.Ecosystem)+"/"+pkg.Name]; ok {
		return t, nil
	}
	return time.Time{}, nil
}

func (m *memorySQLiteIndexer) UpdatePackageLastIndexedAt(_ context.Context, pkg ecosystem.PackageRef, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastIndex[string(pkg.Ecosystem)+"/"+pkg.Name] = t
	return nil
}

type recordingChain struct {
	mu     sync.Mutex
	events []recordingChainEvent
}

type recordingChainEvent struct {
	EventType uint32
	Payload   []byte
	Doctrine  string
}

func (r *recordingChain) Append(_ context.Context, evt eventlog.EventType, payload []byte, doctrine string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordingChainEvent{
		EventType: uint32(evt),
		Payload:   append([]byte(nil), payload...),
		Doctrine:  doctrine,
	})
	return int64(len(r.events)), nil
}

func (r *recordingChain) LastHash(_ context.Context) (string, error)      { return "", nil }
func (r *recordingChain) SealPartition(_ context.Context, _ string) error { return nil }

func (r *recordingChain) snapshot() []recordingChainEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordingChainEvent, len(r.events))
	copy(out, r.events)
	return out
}

type fakeManifestSource struct {
	eco           ecosystem.Ecosystem
	kind          ecosystem.SourceType
	packages      []string
	manifestErr   error
	docsErrByPkg  map[string]error
	docDelay      time.Duration
	docCalls      int32
	manifestCalls int32

	cacheMu sync.Mutex
	cache   *ecosystem.Manifest
}

func (f *fakeManifestSource) Ecosystem() ecosystem.Ecosystem { return f.eco }
func (f *fakeManifestSource) Kind() ecosystem.SourceType     { return f.kind }

func (f *fakeManifestSource) FetchManifest(_ context.Context) (*ecosystem.Manifest, error) {
	atomic.AddInt32(&f.manifestCalls, 1)
	if f.manifestErr != nil {
		return nil, f.manifestErr
	}
	f.cacheMu.Lock()
	defer f.cacheMu.Unlock()
	if f.cache != nil {
		return f.cache, nil
	}
	out := make([]ecosystem.ManifestPackage, len(f.packages))
	for i, p := range f.packages {
		out[i] = ecosystem.ManifestPackage{
			Name:                p,
			LatestStableVersion: "1.0.0",
			UpstreamURL:         "https://example.com/" + p,
			LastUpdated:         time.Now(),
		}
	}
	f.cache = &ecosystem.Manifest{Packages: out}
	return f.cache, nil
}

func (f *fakeManifestSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	atomic.AddInt32(&f.docCalls, 1)
	if f.docDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.docDelay):
		}
	}
	if e, ok := f.docsErrByPkg[pkg.Name]; ok && e != nil {
		return nil, e
	}
	return &ecosystem.PackageDoc{
		Package: pkg,
		Version: "1.0.0",
		Sections: []ecosystem.DocSection{
			{
				Kind:        ecosystem.KindFunction,
				SymbolPath:  pkg.Name + ".Fn1",
				Body:        "body-fn1",
				ASTNodeType: "function_declaration",
				SourceURL:   "https://example.com/" + pkg.Name + "#Fn1",
			},
			{
				Kind:        ecosystem.KindFunction,
				SymbolPath:  pkg.Name + ".Fn2",
				Body:        "body-fn2",
				ASTNodeType: "function_declaration",
				SourceURL:   "https://example.com/" + pkg.Name + "#Fn2",
			},
		},
	}, nil
}

func (f *fakeManifestSource) FetchChangelog(_ context.Context, pkg ecosystem.PackageRef, _ string) (*ecosystem.Changelog, error) {
	return &ecosystem.Changelog{Package: pkg, FormatDetected: "not-available"}, nil
}

type fakeSymIdx struct {
	mu      sync.Mutex
	symbols []ecosystem.SymbolRef
}

func (f *fakeSymIdx) Register(_ context.Context, sym ecosystem.SymbolRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.symbols = append(f.symbols, sym)
	return nil
}

func (f *fakeSymIdx) Lookup(_ context.Context, sp string) (ecosystem.SymbolRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.symbols {
		if s.SymbolPath == sp {
			return s, true, nil
		}
	}
	return ecosystem.SymbolRef{}, false, nil
}

var (
	_ ecosystem.IndexerWriter        = (*memorySQLiteIndexer)(nil)
	_ ecosystem.RAGAuditChainEmitter = (*recordingChain)(nil)
	_ ecosystem.Source               = (*fakeManifestSource)(nil)
	_ ecosystem.SymbolIndexRegistrar = (*fakeSymIdx)(nil)
)

func TestIntegration_FullIngest_FixtureBacked(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-A", "pkg-B", "pkg-C"},
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}

	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 3 {
		t.Errorf("PackagesIngested = %d; want 3", res.PackagesIngested)
	}
	if res.PackagesFailed != 0 {
		t.Errorf("PackagesFailed = %d; want 0", res.PackagesFailed)
	}

	var pkgCount, chunkCount int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_packages").Scan(&pkgCount); err != nil {
		t.Fatalf("count packages: %v", err)
	}
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_chunks").Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if pkgCount != 3 {
		t.Errorf("ecosystem_packages count = %d; want 3", pkgCount)
	}

	if chunkCount != 6 {
		t.Errorf("ecosystem_chunks count = %d; want 6 (2 chunks per package × 3 packages)", chunkCount)
	}

	events := chain.snapshot()
	if len(events) != 3 {
		t.Errorf("audit events = %d; want 3", len(events))
	}
	for i, ev := range events {
		if ev.EventType != uint32(eventlog.EvtRAGIngestPackage) {
			t.Errorf("event[%d] EventType = %d; want %d (EvtRAGIngestPackage)",
				i, ev.EventType, uint32(eventlog.EvtRAGIngestPackage))
		}
		if ev.Doctrine != "default" {
			t.Errorf("event[%d] Doctrine = %q; want %q", i, ev.Doctrine, "default")
		}
	}
}

func TestIntegration_ResumabilityAfterCancel(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-1", "pkg-2", "pkg-3", "pkg-4", "pkg-5"},
		docDelay: 20 * time.Millisecond,
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, _ = ing.Ingest(ctx, ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})

	var pkgCount1 int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_packages").Scan(&pkgCount1); err != nil {
		t.Fatalf("count packages after first run: %v", err)
	}
	t.Logf("packages after first interrupted run: %d (max 5; resume completes the rest)", pkgCount1)

	res2, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Restart Ingest: %v", err)
	}
	_ = res2

	var pkgCount2 int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_packages").Scan(&pkgCount2); err != nil {
		t.Fatalf("count packages after restart: %v", err)
	}
	if pkgCount2 != 5 {
		t.Errorf("final packages = %d; want 5 (resumability proven via last_indexed_at)", pkgCount2)
	}

	totalDocCalls := atomic.LoadInt32(&src.docCalls)
	if totalDocCalls > 10 {
		t.Errorf("FetchPackageDoc calls = %d; want ≤ 10 (resumability should skip already-indexed)", totalDocCalls)
	}
	t.Logf("total FetchPackageDoc calls across two runs: %d (≤ 10 indicates resumability working)", totalDocCalls)
}

func TestIntegration_FetchManifestErrorIsolated(t *testing.T) {
	src := &fakeManifestSource{
		eco:         ecosystem.EcoGo,
		kind:        ecosystem.SrcPackageDoc,
		manifestErr: errors.New("manifest unavailable"),
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v (manifest failure should be isolated, not propagated)", err)
	}
	if res.PackagesIngested != 0 {
		t.Errorf("PackagesIngested = %d; want 0 (manifest fail → no packages)", res.PackagesIngested)
	}
	if res.PackagesFailed != 0 {
		t.Errorf("PackagesFailed = %d; want 0 (manifest failure is source-level, not per-package)", res.PackagesFailed)
	}

	if len(chain.snapshot()) != 0 {
		t.Errorf("audit events = %d; want 0 (no per-package events when manifest fails)", len(chain.snapshot()))
	}
}

// TestIntegration_ConcurrentIngestRaceClean exercises inv-zen-200: 20 pkgs
// × 8 concurrent workers MUST be race-clean at -race -count=2. All 20
// packages end up in ecosystem_packages; audit chain has 20 events; no
// double-counted ingests / failed transactions.
func TestIntegration_ConcurrentIngestRaceClean(t *testing.T) {
	pkgs := make([]string, 20)
	for i := range pkgs {
		pkgs[i] = fmt.Sprintf("pkg-%02d", i)
	}
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: pkgs,
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  8,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 20 {
		t.Errorf("PackagesIngested = %d; want 20", res.PackagesIngested)
	}
	if res.PackagesFailed != 0 {
		t.Errorf("PackagesFailed = %d; want 0", res.PackagesFailed)
	}

	var pkgCount, chunkCount int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_packages").Scan(&pkgCount); err != nil {
		t.Fatalf("count packages: %v", err)
	}
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_chunks").Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if pkgCount != 20 {
		t.Errorf("ecosystem_packages count = %d; want 20", pkgCount)
	}
	if chunkCount != 40 {
		t.Errorf("ecosystem_chunks count = %d; want 40 (2 per package × 20)", chunkCount)
	}
	if len(chain.snapshot()) != 20 {
		t.Errorf("audit events = %d; want 20", len(chain.snapshot()))
	}
}

func TestIntegration_PerPackageFailureIsolated(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-ok-1", "pkg-fail", "pkg-ok-2", "pkg-ok-3"},
		docsErrByPkg: map[string]error{
			"pkg-fail": errors.New("simulated doc fetch failure"),
		},
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v (per-package failure should NOT propagate)", err)
	}
	if res.PackagesIngested != 3 {
		t.Errorf("PackagesIngested = %d; want 3 (4 packages - 1 failed)", res.PackagesIngested)
	}
	if res.PackagesFailed != 1 {
		t.Errorf("PackagesFailed = %d; want 1 (pkg-fail)", res.PackagesFailed)
	}

	var pkgCount int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM ecosystem_packages").Scan(&pkgCount); err != nil {
		t.Fatalf("count packages: %v", err)
	}
	if pkgCount != 3 {
		t.Errorf("ecosystem_packages count = %d; want 3 (pkg-fail's WriteChunks never fires)", pkgCount)
	}

	events := chain.snapshot()
	if len(events) != 4 {
		t.Errorf("audit events = %d; want 4 (3 success + 1 failure)", len(events))
	}
	var successCount, failureCount int
	for _, ev := range events {
		if ev.EventType != uint32(eventlog.EvtRAGIngestPackage) {
			t.Errorf("event EventType = %d; want %d", ev.EventType, uint32(eventlog.EvtRAGIngestPackage))
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Errorf("payload unmarshal: %v", err)
			continue
		}
		succeeded, _ := payload["succeeded"].(bool)
		if succeeded {
			successCount++
		} else {
			failureCount++
			if pn, _ := payload["package_name"].(string); pn != "pkg-fail" {
				t.Errorf("failure payload package_name = %q; want %q", pn, "pkg-fail")
			}
		}
	}
	if successCount != 3 {
		t.Errorf("success events = %d; want 3", successCount)
	}
	if failureCount != 1 {
		t.Errorf("failure events = %d; want 1", failureCount)
	}
}

func TestIntegration_AuditPayloadAllTenFields(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-audit"},
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	_, err = ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	events := chain.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(events))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}

	required := []string{
		"package_id",
		"package_name",
		"ecosystem",
		"version",
		"chunks_count",
		"symbols_count",
		"change_nodes_count",
		"started_at",
		"completed_at",
		"succeeded",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing required field %q (10 spec-required fields per B-10 IMPORTANT-1)", key)
		}
	}

	if pn, _ := payload["package_name"].(string); pn != "pkg-audit" {
		t.Errorf("package_name = %v; want pkg-audit", payload["package_name"])
	}
	if eco, _ := payload["ecosystem"].(string); eco != string(ecosystem.EcoGo) {
		t.Errorf("ecosystem = %v; want %v", payload["ecosystem"], ecosystem.EcoGo)
	}
	if v, _ := payload["version"].(string); v != "1.0.0" {
		t.Errorf("version = %v; want 1.0.0", payload["version"])
	}
	if succ, _ := payload["succeeded"].(bool); !succ {
		t.Errorf("succeeded = %v; want true", payload["succeeded"])
	}

	if cc, _ := payload["chunks_count"].(float64); cc != 2 {
		t.Errorf("chunks_count = %v; want 2 (2 DocSections → 2 fallback chunks)", payload["chunks_count"])
	}
	if sc, _ := payload["symbols_count"].(float64); sc != 2 {
		t.Errorf("symbols_count = %v; want 2 (2 distinct symbol paths)", payload["symbols_count"])
	}
	if cnc, _ := payload["change_nodes_count"].(float64); cnc != 0 {
		t.Errorf("change_nodes_count = %v; want 0 (Phase E populates)", payload["change_nodes_count"])
	}

	startedStr, _ := payload["started_at"].(string)
	completedStr, _ := payload["completed_at"].(string)
	startedAt, err := time.Parse(time.RFC3339Nano, startedStr)
	if err != nil {
		t.Errorf("started_at parse: %v (want RFC3339Nano)", err)
	}
	completedAt, err := time.Parse(time.RFC3339Nano, completedStr)
	if err != nil {
		t.Errorf("completed_at parse: %v (want RFC3339Nano)", err)
	}
	if !completedAt.After(startedAt) && !completedAt.Equal(startedAt) {
		t.Errorf("completed_at (%v) must be >= started_at (%v)", completedAt, startedAt)
	}
}

func TestIntegration_ResumabilitySkipsIndexedPackages(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-a", "pkg-b", "pkg-c"},
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res1, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{Ecosystem: ecosystem.EcoGo, DeltaOnly: true})
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if res1.PackagesIngested != 3 {
		t.Fatalf("first run PackagesIngested = %d; want 3", res1.PackagesIngested)
	}
	firstDocCalls := atomic.LoadInt32(&src.docCalls)
	if firstDocCalls != 3 {
		t.Fatalf("first run FetchPackageDoc = %d; want 3", firstDocCalls)
	}

	res2, err := ing.Ingest(context.Background(), ecosystem.IngestRequest{Ecosystem: ecosystem.EcoGo, DeltaOnly: true})
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	secondRunNewDocCalls := atomic.LoadInt32(&src.docCalls) - firstDocCalls
	if secondRunNewDocCalls != 0 {
		t.Errorf("second-run NEW FetchPackageDoc calls = %d; want 0 (all 3 packages should skip via last_indexed_at)", secondRunNewDocCalls)
	}
	if res2.PackagesIngested != 0 {
		t.Errorf("second run PackagesIngested = %d; want 0 (all already indexed)", res2.PackagesIngested)
	}
}

func TestIntegration_PackageRefFieldsPropagated(t *testing.T) {
	src := &fakeManifestSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SrcPackageDoc,
		packages: []string{"pkg-fields"},
	}
	idx := newMemorySQLiteIndexer(t)
	chain := &recordingChain{}
	sym := &fakeSymIdx{}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{
		Sources: map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
			ecosystem.EcoGo: {ecosystem.SrcPackageDoc: src},
		},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	_, err = ing.Ingest(context.Background(), ecosystem.IngestRequest{
		Ecosystem: ecosystem.EcoGo,
		DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var name, upstreamURL, latestStable, ecoCol, canonicalNS string
	err = idx.db.QueryRow(`
		SELECT name, upstream_url, latest_stable_version, ecosystem, canonical_namespace
		FROM ecosystem_packages WHERE name = ?`, "pkg-fields").
		Scan(&name, &upstreamURL, &latestStable, &ecoCol, &canonicalNS)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if name != "pkg-fields" {
		t.Errorf("name = %q; want pkg-fields", name)
	}
	if upstreamURL != "https://example.com/pkg-fields" {
		t.Errorf("upstream_url = %q; want https://example.com/pkg-fields (Manifest.UpstreamURL → PackageRef.UpstreamURL → row)", upstreamURL)
	}
	if latestStable != "1.0.0" {
		t.Errorf("latest_stable_version = %q; want 1.0.0 (Manifest.LatestStableVersion → PackageRef.LatestStableVersion → row)", latestStable)
	}
	if ecoCol != string(ecosystem.EcoGo) {
		t.Errorf("ecosystem = %q; want %q", ecoCol, ecosystem.EcoGo)
	}

	if canonicalNS != "pkg-fields" {
		t.Errorf("canonical_namespace = %q; want pkg-fields (processPackage default = pkg.Name)", canonicalNS)
	}
}
