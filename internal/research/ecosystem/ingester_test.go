// internal/research/ecosystem/ingester_test.go
//
// Tests for Ingester.
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical concurrent orchestration files require
// ≥90% per-function coverage. Tests cover happy path + failure isolation
// + concurrent safety (-race -count=10) + cancel + audit payload format
// + all NIL-component boundary paths + resumability + sources filter.
//
// Recording helpers (recordingIndexer / recordingSymbolIndex / fakeSource /
// recordingAuditChain) are TEST-ONLY (this file is _test.go); they satisfy
// the production interfaces (IndexerWriter / SymbolIndexRegistrar / Source /
// RAGAuditChainEmitter) declared in ingester.go + audit_emitter.go.

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var (
	_ IndexerWriter        = (*recordingIndexer)(nil)
	_ SymbolIndexRegistrar = (*recordingSymbolIndex)(nil)
	_ Source               = (*fakeSource)(nil)
	_ RAGAuditChainEmitter = (*recordingAuditChain)(nil)
)

type recordingIndexer struct {
	mu       sync.Mutex
	writes   []recordingWrite
	writeErr error

	lastIndexed    map[string]time.Time
	lastIndexedErr error
	updates        []recordingLastIndexedUpdate
}

type recordingWrite struct {
	PackageID   int64
	PackageName string
	Version     string
	ChunkCount  int
	SymbolCount int
	ChangeCount int
}

type recordingLastIndexedUpdate struct {
	PackageName string
	UpdatedAt   time.Time
}

func (r *recordingIndexer) WriteChunks(_ context.Context, pkg PackageRef, version string, chunks []Chunk, symbols []SymbolRef, changes []ChangeNode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	r.writes = append(r.writes, recordingWrite{
		PackageID:   pkg.ID,
		PackageName: pkg.Name,
		Version:     version,
		ChunkCount:  len(chunks),
		SymbolCount: len(symbols),
		ChangeCount: len(changes),
	})
	return nil
}

func (r *recordingIndexer) PackageLastIndexedAt(_ context.Context, pkg PackageRef) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastIndexedErr != nil {
		return time.Time{}, r.lastIndexedErr
	}
	if r.lastIndexed == nil {
		return time.Time{}, nil
	}
	return r.lastIndexed[pkg.Name], nil
}

func (r *recordingIndexer) UpdatePackageLastIndexedAt(_ context.Context, pkg PackageRef, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, recordingLastIndexedUpdate{PackageName: pkg.Name, UpdatedAt: t})
	return nil
}

type recordingSymbolIndex struct {
	mu      sync.Mutex
	symbols []SymbolRef
}

func (r *recordingSymbolIndex) Register(_ context.Context, sym SymbolRef) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.symbols = append(r.symbols, sym)
	return nil
}

func (r *recordingSymbolIndex) Lookup(_ context.Context, symPath string) (SymbolRef, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.symbols {
		if s.SymbolPath == symPath {
			return s, true, nil
		}
	}
	return SymbolRef{}, false, nil
}

type fakeSource struct {
	manifest       *Manifest
	manifestErr    error
	docsByPkg      map[string]*PackageDoc
	docsErrByPkg   map[string]error
	changelogs     map[string]*Changelog
	eco            Ecosystem
	kind           SourceType
	manifestCalls  int32
	docCalls       int32
	changelogCalls int32
	docDelay       time.Duration
}

func (f *fakeSource) Ecosystem() Ecosystem { return f.eco }
func (f *fakeSource) Kind() SourceType     { return f.kind }
func (f *fakeSource) FetchManifest(_ context.Context) (*Manifest, error) {
	atomic.AddInt32(&f.manifestCalls, 1)
	if f.manifestErr != nil {
		return nil, f.manifestErr
	}
	return f.manifest, nil
}
func (f *fakeSource) FetchPackageDoc(ctx context.Context, pkg PackageRef) (*PackageDoc, error) {
	atomic.AddInt32(&f.docCalls, 1)
	if f.docDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.docDelay):
		}
	}
	if e, ok := f.docsErrByPkg[pkg.Name]; ok {
		return nil, e
	}
	if d, ok := f.docsByPkg[pkg.Name]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("unknown package %s", pkg.Name)
}
func (f *fakeSource) FetchChangelog(_ context.Context, pkg PackageRef, _ string) (*Changelog, error) {
	atomic.AddInt32(&f.changelogCalls, 1)
	if c, ok := f.changelogs[pkg.Name]; ok {
		return c, nil
	}
	return &Changelog{Package: pkg, FormatDetected: "not-available"}, nil
}

type recordingAuditChain struct {
	mu     sync.Mutex
	events []recordingAuditEvent
}

type recordingAuditEvent struct {
	EventType uint32
	Payload   []byte
	Doctrine  string
}

func (r *recordingAuditChain) Append(_ context.Context, evt eventlog.EventType, payload []byte, doctrine string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordingAuditEvent{
		EventType: uint32(evt),
		Payload:   append([]byte(nil), payload...),
		Doctrine:  doctrine,
	})
	return int64(len(r.events)), nil
}

func (r *recordingAuditChain) LastHash(_ context.Context) (string, error) { return "", nil }

func (r *recordingAuditChain) SealPartition(_ context.Context, _ string) error { return nil }

func (r *recordingAuditChain) snapshot() []recordingAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordingAuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

func makeFakeManifest(n int) *Manifest {
	mp := make([]ManifestPackage, n)
	for i := 0; i < n; i++ {
		mp[i] = ManifestPackage{
			Name:                fmt.Sprintf("pkg-%d", i),
			LatestStableVersion: "1.0.0",
			UpstreamURL:         fmt.Sprintf("https://example.com/pkg-%d", i),
			LastUpdated:         time.Now(),
		}
	}
	return &Manifest{Packages: mp}
}

func makeFakeDoc(name string, sections int) *PackageDoc {
	secs := make([]DocSection, sections)
	for i := 0; i < sections; i++ {
		secs[i] = DocSection{
			Kind:        KindFunction,
			SymbolPath:  name + ".Fn" + fmt.Sprint(i),
			Body:        "package main\nfunc Fn" + fmt.Sprint(i) + "() {}\n",
			SourceURL:   "https://example.com",
			ASTNodeType: "function_declaration",
		}
	}
	return &PackageDoc{
		Package:   PackageRef{Ecosystem: EcoGo, Name: name, CanonicalNamespace: name},
		Version:   "1.0.0",
		RawBody:   "package main\n",
		SourceURL: "https://example.com",
		Sections:  secs,
	}
}

func TestIngester_Ingest_HappyPath(t *testing.T) {
	src := &fakeSource{
		manifest: makeFakeManifest(3),
		docsByPkg: map[string]*PackageDoc{
			"pkg-0": makeFakeDoc("pkg-0", 2),
			"pkg-1": makeFakeDoc("pkg-1", 2),
			"pkg-2": makeFakeDoc("pkg-2", 2),
		},
		eco:  EcoGo,
		kind: SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {SrcPackageDoc: src},
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
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 3 {
		t.Errorf("PackagesIngested = %d; want 3", res.PackagesIngested)
	}
	if res.PackagesFailed != 0 {
		t.Errorf("PackagesFailed = %d; want 0", res.PackagesFailed)
	}
	if len(idx.writes) != 3 {
		t.Errorf("indexer.WriteChunks calls = %d; want 3", len(idx.writes))
	}
	if len(chain.events) != 3 {
		t.Errorf("audit emits = %d; want 3", len(chain.events))
	}
	if res.StartedAt.IsZero() || res.CompletedAt.IsZero() {
		t.Errorf("StartedAt / CompletedAt: must be set (got %v / %v)", res.StartedAt, res.CompletedAt)
	}
	if res.CompletedAt.Before(res.StartedAt) {
		t.Errorf("CompletedAt < StartedAt: %v < %v", res.CompletedAt, res.StartedAt)
	}
}

func TestIngester_Ingest_PerPackageFailureIsolated(t *testing.T) {
	src := &fakeSource{
		manifest: makeFakeManifest(3),
		docsByPkg: map[string]*PackageDoc{
			"pkg-0": makeFakeDoc("pkg-0", 2),
			"pkg-2": makeFakeDoc("pkg-2", 2),
		},
		docsErrByPkg: map[string]error{
			"pkg-1": errors.New("boom"),
		},
		eco:  EcoGo,
		kind: SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest should not surface per-package errors: %v", err)
	}
	if res.PackagesIngested != 2 {
		t.Errorf("PackagesIngested = %d; want 2 (pkg-1 isolated)", res.PackagesIngested)
	}
	if res.PackagesFailed != 1 {
		t.Errorf("PackagesFailed = %d; want 1", res.PackagesFailed)
	}

	if got := len(chain.events); got != 3 {
		t.Errorf("audit events = %d; want 3 (2 success + 1 failure)", got)
	}
}

func TestIngester_Ingest_ConcurrentSafety(t *testing.T) {
	const N = 20
	src := &fakeSource{
		manifest:  makeFakeManifest(N),
		docsByPkg: make(map[string]*PackageDoc),
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	for i := 0; i < N; i++ {
		src.docsByPkg[fmt.Sprintf("pkg-%d", i)] = makeFakeDoc(fmt.Sprintf("pkg-%d", i), 2)
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  8,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != N {
		t.Errorf("PackagesIngested = %d; want %d", res.PackagesIngested, N)
	}
	if len(idx.writes) != N {
		t.Errorf("WriteChunks calls = %d; want %d", len(idx.writes), N)
	}
}

func TestIngester_Ingest_ContextCancelMidIngest(t *testing.T) {
	const N = 50
	src := &fakeSource{
		manifest:  makeFakeManifest(N),
		docsByPkg: make(map[string]*PackageDoc),
		eco:       EcoGo,
		kind:      SrcPackageDoc,
		docDelay:  2 * time.Millisecond,
	}
	for i := 0; i < N; i++ {
		src.docsByPkg[fmt.Sprintf("pkg-%d", i)] = makeFakeDoc(fmt.Sprintf("pkg-%d", i), 2)
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	res, err := ing.Ingest(ctx, IngestRequest{Ecosystem: EcoGo})

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected nil or context.Canceled; got %v", err)
	}

	if res != nil && res.PackagesIngested > N {
		t.Errorf("PackagesIngested = %d; should be ≤ %d", res.PackagesIngested, N)
	}
}

func TestIngester_Ingest_AuditPayloadFormat(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 3)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(chain.events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(chain.events))
	}
	ev := chain.events[0]

	if ev.EventType != uint32(eventlog.EvtRAGIngestPackage) {
		t.Errorf("EventType = %d; want %d (eventlog.EvtRAGIngestPackage)", ev.EventType, uint32(eventlog.EvtRAGIngestPackage))
	}
	if ev.Doctrine != "default" {
		t.Errorf("Doctrine = %s; want default", ev.Doctrine)
	}

	if len(ev.Payload) == 0 {
		t.Fatal("empty audit payload")
	}
	var got map[string]interface{}
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("Payload is not valid JSON: %v (raw=%s)", err, string(ev.Payload))
	}
	// Spec §4.6 lists 10 required payload fields on success-audit emit. All
	// 10 MUST be present (B-10 fix-cycle 2026-05-18: package_id +
	// change_nodes_count assertions added per IMPORTANT-1 finding).
	for _, key := range []string{"package_id", "package_name", "ecosystem", "version", "chunks_count", "symbols_count", "change_nodes_count", "succeeded", "started_at", "completed_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("audit payload missing field %q (got keys: %v)", key, mapKeys(got))
		}
	}
	if got["succeeded"] != true {
		t.Errorf("succeeded = %v; want true", got["succeeded"])
	}
	if got["package_name"] != "pkg-0" {
		t.Errorf("package_name = %v; want pkg-0", got["package_name"])
	}
	if got["ecosystem"] != string(EcoGo) {
		t.Errorf("ecosystem = %v; want %s", got["ecosystem"], string(EcoGo))
	}
}

func TestIngester_Ingest_NilSources_Error(t *testing.T) {
	ing, err := NewIngester(IngesterOptions{Sources: nil})
	if err == nil {
		t.Fatalf("NewIngester(nil Sources) = nil error; want non-nil")
	}
	if ing != nil {
		t.Errorf("NewIngester(nil Sources) = %v; want nil ingester", ing)
	}
}

func TestIngester_Ingest_UnknownEcosystem(t *testing.T) {
	src := &fakeSource{manifest: makeFakeManifest(1), eco: EcoGo, kind: SrcPackageDoc}
	ing, err := NewIngester(IngesterOptions{
		Sources: map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoPython})
	if err == nil {
		t.Errorf("Ingest(unregistered ecosystem) = nil error; want non-nil")
	}
}

func TestIngester_Ingest_ManifestFetchError_Continues(t *testing.T) {
	failSrc := &fakeSource{
		manifestErr: errors.New("manifest down"),
		eco:         EcoGo,
		kind:        SrcPackageDoc,
	}
	okSrc := &fakeSource{
		manifest:  makeFakeManifest(2),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1), "pkg-1": makeFakeDoc("pkg-1", 1)},
		eco:       EcoGo,
		kind:      SrcGitHub,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {SrcPackageDoc: failSrc, SrcGitHub: okSrc},
		},
		Indexer:      idx,
		AuditChain:   chain,
		WorkerCount:  2,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 2 {
		t.Errorf("PackagesIngested = %d; want 2 (only okSrc contributes)", res.PackagesIngested)
	}
	if atomic.LoadInt32(&failSrc.manifestCalls) != 1 {
		t.Errorf("failSrc.manifestCalls = %d; want 1", failSrc.manifestCalls)
	}
	if atomic.LoadInt32(&okSrc.manifestCalls) != 1 {
		t.Errorf("okSrc.manifestCalls = %d; want 1", okSrc.manifestCalls)
	}
}

func TestIngester_Ingest_Resumability_Skip(t *testing.T) {
	yesterday := time.Now().Add(-24 * time.Hour)
	manifest := &Manifest{Packages: []ManifestPackage{
		{Name: "pkg-0", LatestStableVersion: "1.0.0", LastUpdated: yesterday},
	}}
	src := &fakeSource{
		manifest:  manifest,
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 2)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{lastIndexed: map[string]time.Time{
		"pkg-0": time.Now(),
	}}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo, DeltaOnly: true})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 0 {
		t.Errorf("PackagesIngested = %d; want 0 (skipped via resumability)", res.PackagesIngested)
	}
	if got := len(idx.writes); got != 0 {
		t.Errorf("WriteChunks calls = %d; want 0 (skipped)", got)
	}
	if got := atomic.LoadInt32(&src.docCalls); got != 0 {
		t.Errorf("FetchPackageDoc calls = %d; want 0 (skipped before fetch)", got)
	}
}

func TestIngester_Ingest_Resumability_FetchOnNewerManifest(t *testing.T) {
	manifest := &Manifest{Packages: []ManifestPackage{
		{Name: "pkg-0", LatestStableVersion: "1.0.0", LastUpdated: time.Now()},
	}}
	src := &fakeSource{
		manifest:  manifest,
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 2)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{lastIndexed: map[string]time.Time{
		"pkg-0": time.Now().Add(-24 * time.Hour),
	}}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo, DeltaOnly: true})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (manifest newer → write)", res.PackagesIngested)
	}
	if got := len(idx.writes); got != 1 {
		t.Errorf("WriteChunks calls = %d; want 1", got)
	}
}

func TestIngester_Ingest_FailureAuditPayload(t *testing.T) {
	src := &fakeSource{
		manifest: makeFakeManifest(1),
		docsErrByPkg: map[string]error{
			"pkg-0": errors.New("upstream-down"),
		},
		eco:  EcoGo,
		kind: SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesFailed != 1 {
		t.Errorf("PackagesFailed = %d; want 1", res.PackagesFailed)
	}
	events := chain.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(events))
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("Payload unmarshal: %v (raw=%s)", err, string(events[0].Payload))
	}
	if payload["succeeded"] != false {
		t.Errorf("succeeded = %v; want false", payload["succeeded"])
	}
	if payload["error"] == nil || payload["error"] == "" {
		t.Errorf("error field empty; got %v", payload["error"])
	}
	errStr, _ := payload["error"].(string)
	if errStr == "" {
		t.Errorf("error field empty; want non-empty error string")
	}
}

func TestIngester_Ingest_DefaultWorkerCount(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(2),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1), "pkg-1": makeFakeDoc("pkg-1", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      &recordingIndexer{},
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	if got := ing.opts.WorkerCount; got != runtime.NumCPU() {
		t.Errorf("opts.WorkerCount = %d; want %d (runtime.NumCPU)", got, runtime.NumCPU())
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 2 {
		t.Errorf("PackagesIngested = %d; want 2", res.PackagesIngested)
	}
}

func TestIngester_Ingest_FallbackChunks_NoChunker(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 4)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1", res.PackagesIngested)
	}
	if len(idx.writes) != 1 {
		t.Fatalf("WriteChunks calls = %d; want 1", len(idx.writes))
	}

	if got := idx.writes[0].ChunkCount; got != 4 {
		t.Errorf("ChunkCount = %d; want 4 (one per DocSection)", got)
	}
}

func TestIngester_Ingest_MultipleSourceTypes(t *testing.T) {
	srcA := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "from-A", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"from-A": makeFakeDoc("from-A", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	srcB := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "from-B", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"from-B": makeFakeDoc("from-B", 1)},
		eco:       EcoGo,
		kind:      SrcGitHub,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {SrcPackageDoc: srcA, SrcGitHub: srcB},
		},
		Indexer:      idx,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 2 {
		t.Errorf("PackagesIngested = %d; want 2 (one per source type)", res.PackagesIngested)
	}
	if atomic.LoadInt32(&srcA.manifestCalls) != 1 {
		t.Errorf("srcA.manifestCalls = %d; want 1", srcA.manifestCalls)
	}
	if atomic.LoadInt32(&srcB.manifestCalls) != 1 {
		t.Errorf("srcB.manifestCalls = %d; want 1", srcB.manifestCalls)
	}
}

func TestIngester_Ingest_ChangelogStored(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(2),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1), "pkg-1": makeFakeDoc("pkg-1", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got := atomic.LoadInt32(&src.changelogCalls); got != 2 {
		t.Errorf("FetchChangelog calls = %d; want 2 (one per pkg)", got)
	}
}

func TestIngester_Ingest_NilIndexer_NoWrites(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 2)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      nil,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest with nil Indexer: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (still counts success)", res.PackagesIngested)
	}
	if got := len(chain.events); got != 1 {
		t.Errorf("audit events = %d; want 1 (still emitted)", got)
	}
}

func TestIngester_Ingest_NilSymbolIndex_NoRegister(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 2)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  nil,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest with nil SymbolIndex: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1", res.PackagesIngested)
	}

	if got := len(idx.writes); got != 1 {
		t.Fatalf("WriteChunks calls = %d; want 1", got)
	}
	if got := idx.writes[0].SymbolCount; got != 0 {
		t.Errorf("SymbolCount = %d; want 0 (no SymbolIndex configured)", got)
	}
	if got := idx.writes[0].ChunkCount; got == 0 {
		t.Errorf("ChunkCount = 0; want > 0 (chunks still processed)")
	}
}

func TestIngester_Ingest_NilAuditChain_Silent(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   nil,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest with nil AuditChain: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (still counts success even with no audit)", res.PackagesIngested)
	}
}

func TestIngester_Ingest_SourcesFilterApplied(t *testing.T) {
	srcA := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "from-A", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"from-A": makeFakeDoc("from-A", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	srcB := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "from-B", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"from-B": makeFakeDoc("from-B", 1)},
		eco:       EcoGo,
		kind:      SrcGitHub,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {SrcPackageDoc: srcA, SrcGitHub: srcB},
		},
		Indexer:      idx,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{
		Ecosystem: EcoGo,
		Sources:   []SourceType{SrcPackageDoc},
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (filter excluded SrcGitHub)", res.PackagesIngested)
	}
	if atomic.LoadInt32(&srcA.manifestCalls) != 1 {
		t.Errorf("srcA.manifestCalls = %d; want 1", srcA.manifestCalls)
	}
	if atomic.LoadInt32(&srcB.manifestCalls) != 0 {
		t.Errorf("srcB.manifestCalls = %d; want 0 (filtered out)", srcB.manifestCalls)
	}
}

func TestIngester_Ingest_IndexerWriteError_Isolated(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(2),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1), "pkg-1": makeFakeDoc("pkg-1", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{writeErr: errors.New("sqlite locked")}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 0 {
		t.Errorf("PackagesIngested = %d; want 0 (write errors isolated)", res.PackagesIngested)
	}
	if res.PackagesFailed != 2 {
		t.Errorf("PackagesFailed = %d; want 2", res.PackagesFailed)
	}
	events := chain.snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d; want 2", len(events))
	}
	for i, ev := range events {
		var p map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("event %d payload unmarshal: %v", i, err)
		}
		if p["succeeded"] != false {
			t.Errorf("event %d: succeeded = %v; want false", i, p["succeeded"])
		}
	}
}

func TestIngester_Ingest_PackageLastIndexedAtError_ProcessesAnyway(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{lastIndexedErr: errors.New("sqlite read err")}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo, DeltaOnly: true})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (lastIndexedErr → proceed)", res.PackagesIngested)
	}
}

func TestIngester_CountSymbols(t *testing.T) {
	cases := []struct {
		name   string
		chunks []Chunk
		want   int
	}{
		{"empty", []Chunk{}, 0},
		{"one", []Chunk{{SymbolPath: "a.B"}}, 1},
		{"duplicates", []Chunk{{SymbolPath: "a.B"}, {SymbolPath: "a.B"}}, 1},
		{"two distinct", []Chunk{{SymbolPath: "a.B"}, {SymbolPath: "a.C"}}, 2},
		{"skips empty", []Chunk{{SymbolPath: ""}, {SymbolPath: "a.B"}}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countSymbols(c.chunks); got != c.want {
				t.Errorf("countSymbols(%v) = %d; want %d", c.chunks, got, c.want)
			}
		})
	}
}

func TestIngester_SynthesizeFallbackChunks(t *testing.T) {
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x"},
		Version: "1.2.3",
		Sections: []DocSection{
			{Kind: KindFunction, SymbolPath: "x.A", Body: "func A() {}", SourceURL: "u1"},
			{Kind: KindType, SymbolPath: "x.T", Body: "type T struct{}", SourceURL: "u2"},
			{Kind: KindGuide, SymbolPath: "", Body: "# Heading", SourceURL: "u3"},
		},
	}
	chunks := synthesizeFallbackChunks(doc)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d; want 3", len(chunks))
	}
	if chunks[0].SymbolPath != "x.A" || chunks[0].Kind != KindFunction || chunks[0].ContentText != "func A() {}" {
		t.Errorf("chunks[0] = %+v; want SymbolPath=x.A Kind=function Content='func A() {}'", chunks[0])
	}
	if chunks[1].SymbolPath != "x.T" || chunks[1].Kind != KindType {
		t.Errorf("chunks[1] = %+v; want SymbolPath=x.T Kind=type", chunks[1])
	}
	if chunks[2].Kind != KindGuide {
		t.Errorf("chunks[2].Kind = %s; want %s", chunks[2].Kind, KindGuide)
	}
	for i, c := range chunks {
		if c.VersionIntroduced != "1.2.3" {
			t.Errorf("chunks[%d].VersionIntroduced = %s; want 1.2.3", i, c.VersionIntroduced)
		}
		if c.SourceType != SrcPackageDoc {
			t.Errorf("chunks[%d].SourceType = %s; want %s", i, c.SourceType, SrcPackageDoc)
		}
	}
}

func TestIngester_Ingest_SingleWorker_DeterministicAuditCount(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(5),
		docsByPkg: make(map[string]*PackageDoc),
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	for i := 0; i < 5; i++ {
		src.docsByPkg[fmt.Sprintf("pkg-%d", i)] = makeFakeDoc(fmt.Sprintf("pkg-%d", i), 1)
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 5 {
		t.Errorf("PackagesIngested = %d; want 5", res.PackagesIngested)
	}
	if got := len(chain.events); got != 5 {
		t.Errorf("audit events = %d; want 5", got)
	}
}

func TestIngester_Ingest_WithRealChunker(t *testing.T) {
	chunker, err := NewChunker(ChunkerOptions{
		MinTokens:       1,
		MaxLeafTokens:   2048,
		MaxParentTokens: 4096,
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer chunker.Close()

	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		Chunker:      chunker,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1", res.PackagesIngested)
	}
}

func TestIngester_Ingest_ChunkerErrorIsolated(t *testing.T) {
	chunker, err := NewChunker(ChunkerOptions{
		MinTokens:       1,
		MaxLeafTokens:   2048,
		MaxParentTokens: 4096,
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer chunker.Close()

	unknownEco := Ecosystem("fortran-not-registered")
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: unknownEco, Name: "pkg-0"},
		Version: "1.0.0",
		Sections: []DocSection{
			{Kind: KindFunction, SymbolPath: "pkg-0.Fn", Body: "func Fn() {}", SourceURL: "u"},
		},
	}
	src := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "pkg-0", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"pkg-0": doc},
		eco:       unknownEco,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{unknownEco: {SrcPackageDoc: src}},
		Indexer:      idx,
		Chunker:      chunker,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: unknownEco})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesFailed != 1 {
		t.Errorf("PackagesFailed = %d; want 1 (chunker error isolated)", res.PackagesFailed)
	}
	if res.PackagesIngested != 0 {
		t.Errorf("PackagesIngested = %d; want 0", res.PackagesIngested)
	}
}

type nilDocSource struct {
	fakeSource
}

func (n *nilDocSource) FetchPackageDoc(ctx context.Context, _ PackageRef) (*PackageDoc, error) {
	atomic.AddInt32(&n.fakeSource.docCalls, 1)
	return nil, nil
}

func TestIngester_Ingest_NilDocReturnedByFetch_Isolated(t *testing.T) {
	src := &nilDocSource{fakeSource: fakeSource{
		manifest: makeFakeManifest(1),
		eco:      EcoGo,
		kind:     SrcPackageDoc,
	}}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      &recordingIndexer{},
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesFailed != 1 {
		t.Errorf("PackagesFailed = %d; want 1 (nil doc isolated as failure)", res.PackagesFailed)
	}
	if res.PackagesIngested != 0 {
		t.Errorf("PackagesIngested = %d; want 0", res.PackagesIngested)
	}
}

func TestIngester_ProcessPackage_PreCancelledCtx(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	ing, err := NewIngester(IngesterOptions{
		Sources:     map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		WorkerCount: 1,
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = ing.processPackage(ctx, EcoGo, src, ManifestPackage{Name: "pkg-0", LatestStableVersion: "1.0.0"}, false)
	if err == nil {
		t.Errorf("processPackage(cancelled-ctx) = nil; want context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("processPackage(cancelled-ctx) = %v; want context.Canceled", err)
	}
}

func TestIngester_EmitFailureAudit_NilAuditChain_Silent(t *testing.T) {
	ing, err := NewIngester(IngesterOptions{
		Sources:     map[Ecosystem]map[SourceType]Source{EcoGo: {}},
		WorkerCount: 1,
		AuditChain:  nil,
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	ing.emitFailureAudit(context.Background(), EcoGo, ManifestPackage{Name: "x"}, errors.New("e"), time.Now())
}

func TestIngester_EmitSuccessAudit_NilAuditChain_Silent(t *testing.T) {
	ing, err := NewIngester(IngesterOptions{
		Sources:     map[Ecosystem]map[SourceType]Source{EcoGo: {}},
		WorkerCount: 1,
		AuditChain:  nil,
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	ing.emitSuccessAudit(context.Background(), EcoGo,
		PackageRef{Name: "x"},
		&PackageDoc{Version: "1.0.0"},
		[]Chunk{},
		time.Now())
}

func TestIngester_Ingest_EmptySymbolPath_SkipsRegister(t *testing.T) {
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "pkg-0", CanonicalNamespace: "pkg-0"},
		Version: "1.0.0",
		Sections: []DocSection{

			{Kind: KindGuide, SymbolPath: "", Body: "guide", SourceURL: "u1"},
			{Kind: KindFunction, SymbolPath: "pkg-0.Fn", Body: "func Fn() {}", SourceURL: "u2"},
		},
	}
	src := &fakeSource{
		manifest:  &Manifest{Packages: []ManifestPackage{{Name: "pkg-0", LatestStableVersion: "1.0.0", LastUpdated: time.Now()}}},
		docsByPkg: map[string]*PackageDoc{"pkg-0": doc},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   &recordingAuditChain{},
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1", res.PackagesIngested)
	}
	if got := len(sym.symbols); got != 1 {
		t.Errorf("registered symbols = %d; want 1 (empty SymbolPath skipped)", got)
	}
	if len(sym.symbols) == 1 && sym.symbols[0].SymbolPath != "pkg-0.Fn" {
		t.Errorf("registered symbol = %v; want SymbolPath=pkg-0.Fn", sym.symbols[0])
	}
}

// CRITICAL-1 fix-cycle test: DeltaOnly=false (force full re-ingest) MUST
// re-ingest an "up-to-date" package (manifest LastUpdated ≤ last_indexed_at).
// Pre-fix behavior: skipped unconditionally → PackagesIngested=0. Post-fix:
// proceeds with WriteChunks regardless of resumability staleness.
//
// Spec §4.1: skip iff (manifest.LastUpdated ≤ last_indexed_at AND req.DeltaOnly).
// Force-refresh path (DeltaOnly=false) MUST NOT be silently masked by the
// resumability optimization.
func TestIngester_Ingest_Resumability_ForceRefresh(t *testing.T) {
	// "Up to date" fixture: LastUpdated=1h ago, last_indexed_at=now (≥ LastUpdated).
	// With DeltaOnly=true: would skip. With DeltaOnly=false: MUST re-ingest.
	manifest := &Manifest{Packages: []ManifestPackage{
		{Name: "pkg-0", LatestStableVersion: "1.0.0", LastUpdated: time.Now().Add(-1 * time.Hour)},
	}}
	src := &fakeSource{
		manifest:  manifest,
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 2)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{lastIndexed: map[string]time.Time{
		"pkg-0": time.Now(),
	}}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	res, err := ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo, DeltaOnly: false})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.PackagesIngested != 1 {
		t.Errorf("PackagesIngested = %d; want 1 (DeltaOnly=false MUST force re-ingest)", res.PackagesIngested)
	}
	if got := len(idx.writes); got != 1 {
		t.Errorf("WriteChunks calls = %d; want 1 (force-refresh MUST call WriteChunks)", got)
	}
	if got := atomic.LoadInt32(&src.docCalls); got != 1 {
		t.Errorf("FetchPackageDoc calls = %d; want 1 (force-refresh MUST fetch)", got)
	}
}

type cancelTriggerSource struct {
	fakeSource
	triggerCancel context.CancelFunc
	fired         atomic.Bool
}

func (c *cancelTriggerSource) FetchPackageDoc(ctx context.Context, pkg PackageRef) (*PackageDoc, error) {
	atomic.AddInt32(&c.fakeSource.docCalls, 1)
	if c.fired.CompareAndSwap(false, true) {
		c.triggerCancel()
	}
	// Block until ctx is done so the worker MUST see ctx.Err() return path.
	<-ctx.Done()
	return nil, ctx.Err()
}

// CRITICAL-2 fix-cycle test: cancel mid-ingest MUST NOT inflate PackagesFailed.
// Pre-fix behavior: in-flight goroutines whose processPackage returns ctx.Err()
// were counted as PackagesFailed + emitted failure audit events. Spec requires
// "audit events for in-flight packages NOT emitted on cancel".
//
// Strategy cancelTriggerSource deterministically:
// 1. Fires triggerCancel on first FetchPackageDoc call.
// 2. Blocks on ctx.Done until cancelled (workers wait deterministically).
// 3. Returns ctx.Err() — worker's processPackage returns ctx.Err.
//
// This forces the worker loop's err != nil branch to execute with
// errors.Is(err, context.Canceled) == true, which the pre-fix code
// incorrectly counted as PackagesFailed.
func TestIngester_Ingest_ContextCancel_NoFailureCount(t *testing.T) {
	const N = 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &cancelTriggerSource{
		fakeSource: fakeSource{
			manifest:  makeFakeManifest(N),
			docsByPkg: make(map[string]*PackageDoc),
			eco:       EcoGo,
			kind:      SrcPackageDoc,
		},
		triggerCancel: cancel,
	}
	for i := 0; i < N; i++ {
		src.docsByPkg[fmt.Sprintf("pkg-%d", i)] = makeFakeDoc(fmt.Sprintf("pkg-%d", i), 1)
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	res, err := ing.Ingest(ctx, IngestRequest{Ecosystem: EcoGo})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Ingest: unexpected non-cancel error: %v", err)
	}
	if res == nil {
		t.Fatalf("Ingest returned nil result")
	}

	if res.PackagesFailed != 0 {
		t.Errorf("PackagesFailed = %d; want 0 (ctx-cancelled MUST NOT count as failures)", res.PackagesFailed)
	}
}

// CRITICAL-2 fix-cycle test: cancel mid-ingest MUST NOT emit failure audit
// events for in-flight packages. Verifies the other half of the invariant:
// even if a worker dequeues a job after cancel, no audit event with
// succeeded=false from ctx.Canceled is emitted.
//
// Strategy mirrors NoFailureCount: cancelTriggerSource fires cancel on first
// FetchPackageDoc, then blocks until ctx.Done so processPackage returns
// ctx.Err() deterministically. Audit chain MUST contain ZERO failure events
// (or, defense-in-depth, ZERO failure events whose error string is a
// context-cancellation sentinel).
func TestIngester_Ingest_ContextCancel_NoFailureAudit(t *testing.T) {
	const N = 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &cancelTriggerSource{
		fakeSource: fakeSource{
			manifest:  makeFakeManifest(N),
			docsByPkg: make(map[string]*PackageDoc),
			eco:       EcoGo,
			kind:      SrcPackageDoc,
		},
		triggerCancel: cancel,
	}
	for i := 0; i < N; i++ {
		src.docsByPkg[fmt.Sprintf("pkg-%d", i)] = makeFakeDoc(fmt.Sprintf("pkg-%d", i), 1)
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  2,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(ctx, IngestRequest{Ecosystem: EcoGo})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Ingest: unexpected non-cancel error: %v", err)
	}
	// Audit chain MUST NOT contain any failure event whose error string
	// is a context-cancellation sentinel. (Real failures still emit; cancel
	// short-circuits before emit.)
	events := chain.snapshot()
	for i, ev := range events {
		var payload map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload["succeeded"] == false {
			errStr, _ := payload["error"].(string)
			if errStr == context.Canceled.Error() || errStr == context.DeadlineExceeded.Error() {
				t.Errorf("event %d: emitted failure audit with ctx-cancel error %q (forbidden)", i, errStr)
			}
		}
	}
}

func TestIngester_Ingest_AuditPayloadFormat_AllTenFields(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 3)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
	}
	idx := &recordingIndexer{}
	sym := &recordingSymbolIndex{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		SymbolIndex:  sym,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(chain.events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(chain.events))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(chain.events[0].Payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	// All 10 spec-required fields MUST be present on success-audit emit.
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
	for _, k := range required {
		if _, ok := got[k]; !ok {
			t.Errorf("audit payload missing required field %q (got keys: %v)", k, mapKeys(got))
		}
	}

	if v, ok := got["change_nodes_count"]; ok {

		if cnt, _ := v.(float64); cnt != 0 {
			t.Errorf("change_nodes_count = %v; want 0 (B-10 emits empty changes slice)", v)
		}
	}

	if _, ok := got["package_id"]; !ok {
		t.Errorf("audit payload missing package_id field")
	}
}

// IMPORTANT-2 fix-cycle test: started_at and completed_at MUST be distinct
// timestamps (started_at captured at processPackage entry; completed_at at
// emit time). Pre-fix: both used the SAME time.Now() at emit (identical
// timestamps semantically misleading for ops dashboards).
//
// Resolution timestamps emitted in RFC3339Nano (sub-second) so the
// processPackage→emit gap (≥ 1µs realistic) is measurable. With a docDelay
// of 10ms, the gap is unambiguously >> 1µs.
func TestIngester_Ingest_AuditTimestamps_Distinct(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
		docDelay:  10 * time.Millisecond,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(chain.events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(chain.events))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(chain.events[0].Payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	startedStr, _ := got["started_at"].(string)
	completedStr, _ := got["completed_at"].(string)
	if startedStr == "" || completedStr == "" {
		t.Fatalf("started_at=%q completed_at=%q; both MUST be non-empty", startedStr, completedStr)
	}

	startedTS, errS := time.Parse(time.RFC3339, startedStr)
	completedTS, errC := time.Parse(time.RFC3339, completedStr)
	if errS != nil || errC != nil {
		t.Fatalf("RFC3339 parse: started=%v completed=%v", errS, errC)
	}
	// completed_at MUST be > started_at (strict: post-fix uses
	// RFC3339Nano sub-second resolution + 10ms doc delay ensures the gap is
	// unambiguous; pre-fix uses the SAME time.Now() value → equal → FAIL).
	if !completedTS.After(startedTS) {
		t.Errorf("completed_at (%q) MUST be AFTER started_at (%q); pre-fix bug: identical timestamps", completedStr, startedStr)
	}
}

// IMPORTANT-2 fix-cycle test (defense-in-depth): started_at MUST be threaded
// from processPackage entry, not derived from emit-time time.Now(). With
// RFC3339Nano resolution + 10ms doc delay, the gap is unambiguously
// observable in the parsed timestamps.
//
// Bound check: started_at MUST be between (beforeIngest) and (afterIngest).
// Sub-second resolution + the controlled delay turn this into a load-bearing
// invariant — if started_at were emit-time, it would land near afterIngest,
// not near beforeIngest.
func TestIngester_Ingest_StartedAt_ThreadedFromEntry(t *testing.T) {
	src := &fakeSource{
		manifest:  makeFakeManifest(1),
		docsByPkg: map[string]*PackageDoc{"pkg-0": makeFakeDoc("pkg-0", 1)},
		eco:       EcoGo,
		kind:      SrcPackageDoc,
		docDelay:  20 * time.Millisecond,
	}
	idx := &recordingIndexer{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: src}},
		Indexer:      idx,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	beforeIngest := time.Now().UTC()
	_, err = ing.Ingest(context.Background(), IngestRequest{Ecosystem: EcoGo})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	afterIngest := time.Now().UTC()
	if len(chain.events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(chain.events))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(chain.events[0].Payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	startedStr, _ := got["started_at"].(string)
	completedStr, _ := got["completed_at"].(string)
	startedTS, perr := time.Parse(time.RFC3339, startedStr)
	if perr != nil {
		t.Fatalf("RFC3339 parse started_at: %v", perr)
	}
	completedTS, cerr := time.Parse(time.RFC3339, completedStr)
	if cerr != nil {
		t.Fatalf("RFC3339 parse completed_at: %v", cerr)
	}
	// started_at MUST be within [beforeIngest, afterIngest]. We give 1µs
	// tolerance on either side to account for clock-resolution edge cases.
	if startedTS.Before(beforeIngest.Add(-1 * time.Microsecond)) {
		t.Errorf("started_at (%v) < beforeIngest (%v); MUST be captured at processPackage entry, not earlier", startedTS, beforeIngest)
	}
	if startedTS.After(afterIngest.Add(1 * time.Microsecond)) {
		t.Errorf("started_at (%v) > afterIngest (%v); MUST be captured at processPackage entry, not later", startedTS, afterIngest)
	}
	// Defense-in-depth: completed_at - started_at MUST be ≥ docDelay (10ms)
	// minus a small slack. This proves started_at was captured BEFORE the
	// fetch, not at emit time (where the gap would be ~zero).
	gap := completedTS.Sub(startedTS)
	if gap < 5*time.Millisecond {
		t.Errorf("completed_at - started_at = %v; want ≥ 5ms (docDelay=20ms; pre-fix bug: identical → gap=0)", gap)
	}
}

func TestMarshalAuditPayload(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		got := marshalAuditPayload(map[string]interface{}{"k": "v", "n": 1})
		var parsed map[string]interface{}
		if err := json.Unmarshal(got, &parsed); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if parsed["k"] != "v" {
			t.Errorf("k = %v; want v", parsed["k"])
		}
	})
	t.Run("non-marshalable value falls back to empty object", func(t *testing.T) {

		ch := make(chan int)
		got := marshalAuditPayload(map[string]interface{}{"bad": ch})
		if string(got) != "{}" {
			t.Errorf("marshalAuditPayload(non-marshalable) = %q; want %q (fallback)", string(got), "{}")
		}
		// Result MUST still parse as JSON (chain-link continuity invariant).
		var parsed map[string]interface{}
		if err := json.Unmarshal(got, &parsed); err != nil {
			t.Fatalf("fallback body is not valid JSON: %v", err)
		}
	})
}

func mapKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
