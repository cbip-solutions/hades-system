package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fallbackFixture struct {
	dispatcher        *Dispatcher
	auditChain        *recordingAuditChain
	sourceManifest    map[Ecosystem]*Manifest
	sourcePackageDocs map[string]*PackageDoc
	revalidator       *fallbackFakeRevalidator
	researchMCP       *fakeResearchMCP
	findingsCache     *fakeFindingsCache
	indexer           *fakeIndexerDelta
	source            *fallbackFakeSource
}

type fallbackFakeRevalidator struct {
	forceRefreshSeen bool
	err              error
}

type fallbackFakeSource struct {
	fix *fallbackFixture
	eco Ecosystem
}

func (s *fallbackFakeSource) Ecosystem() Ecosystem { return s.eco }
func (s *fallbackFakeSource) Kind() SourceType     { return SrcPackageDoc }

func (s *fallbackFakeSource) FetchManifest(ctx context.Context) (*Manifest, error) {
	if resolveForceRefresh(ctx) {
		s.fix.revalidator.forceRefreshSeen = true
	}
	if s.fix.revalidator.err != nil {
		return nil, s.fix.revalidator.err
	}
	m, ok := s.fix.sourceManifest[s.eco]
	if !ok {
		return &Manifest{}, nil
	}
	return m, nil
}

func (s *fallbackFakeSource) FetchPackageDoc(ctx context.Context, pkg PackageRef) (*PackageDoc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := fakeKey(s.eco, pkg.Name, pkg.LatestStableVersion)
	if doc, ok := s.fix.sourcePackageDocs[key]; ok {
		return doc, nil
	}
	return nil, ErrPackageNotFound
}

func (s *fallbackFakeSource) FetchChangelog(_ context.Context, pkg PackageRef, _ string) (*Changelog, error) {
	return &Changelog{Package: pkg, FormatDetected: "not-available"}, nil
}

type fakeResearchMCP struct {
	mu               sync.Mutex
	synthesizeOutput string
	synthesizeCalled bool
	err              error
}

func (f *fakeResearchMCP) Synthesize(ctx context.Context, query string, eco Ecosystem) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.synthesizeCalled = true
	if f.err != nil {
		return "", f.err
	}
	return f.synthesizeOutput, nil
}

type fakeFindingsCache struct {
	mu      sync.Mutex
	records *cacheRecords
	err     error
}

type cacheRecords struct {
	set map[string]struct{}
}

func (r *cacheRecords) has(key string) bool {
	if r == nil {
		return false
	}
	_, ok := r.set[key]
	return ok
}

func (c *fakeFindingsCache) Cache(ctx context.Context, key, query, answer string, eco Ecosystem, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.records == nil {
		c.records = &cacheRecords{set: map[string]struct{}{}}
	}
	if c.err != nil {
		return c.err
	}
	c.records.set[key] = struct{}{}
	return nil
}

type fakeIndexerDelta struct {
	mu              sync.Mutex
	indexedPackages *packageSet
	writeDeltaCalls int
	err             error
}

type packageSet struct {
	set map[string]struct{}
}

func (p *packageSet) has(name, version string) bool {
	if p == nil {
		return false
	}
	_, ok := p.set[name+"@"+version]
	return ok
}

func (i *fakeIndexerDelta) WriteDelta(ctx context.Context, eco Ecosystem, doc *PackageDoc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.writeDeltaCalls++
	if i.err != nil {
		return i.err
	}
	if i.indexedPackages == nil {
		i.indexedPackages = &packageSet{set: map[string]struct{}{}}
	}
	i.indexedPackages.set[doc.Package.Name+"@"+doc.Version] = struct{}{}
	return nil
}

func buildFallbackFixture(t *testing.T) *fallbackFixture {
	t.Helper()
	fix := &fallbackFixture{
		auditChain:        &recordingAuditChain{},
		sourceManifest:    map[Ecosystem]*Manifest{},
		sourcePackageDocs: map[string]*PackageDoc{},
		revalidator:       &fallbackFakeRevalidator{},
		researchMCP:       &fakeResearchMCP{},
		findingsCache:     &fakeFindingsCache{records: &cacheRecords{set: map[string]struct{}{}}},
		indexer:           &fakeIndexerDelta{indexedPackages: &packageSet{set: map[string]struct{}{}}},
	}

	fix.source = &fallbackFakeSource{fix: fix, eco: EcoGo}
	fix.dispatcher = &Dispatcher{
		sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {SrcPackageDoc: fix.source},
		},
		researchMCP:   fix.researchMCP,
		findingsCache: fix.findingsCache,
		indexerDelta:  fix.indexer,
		auditEmitter: NewRAGAuditEmitter(
			fix.auditChain,
			&DoctrineProfile{Name: "default", AuditEmissionLevel: AuditAll8Events},
		),
	}
	return fix
}

func fakeKey(eco Ecosystem, name, version string) string {
	return string(eco) + ":" + name + ":" + version
}

func syntheticFindingKey() string {
	return computeFindingKey("obscure package nobody indexed", EcoGo)
}

func TestLiveFallback_PackageFound_IndexesDelta(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.sourceManifest[EcoGo] = &Manifest{
		Packages: []ManifestPackage{
			{Name: "github.com/new/pkg", Versions: []string{"v0.1.0"}, LatestStableVersion: "v0.1.0"},
		},
	}
	fix.sourcePackageDocs[fakeKey(EcoGo, "github.com/new/pkg", "v0.1.0")] = &PackageDoc{
		Package: PackageRef{Name: "github.com/new/pkg", Ecosystem: EcoGo},
		Version: "v0.1.0",
		RawBody: "package fresh new docs here",
	}
	res, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query:       "newly-released github.com/new/pkg usage",
		Ecosystem:   EcoGo,
		PackageHint: "github.com/new/pkg",
	})
	if err != nil {
		t.Fatalf("LiveFallback: %v", err)
	}
	if !res.Provenance.FreshDispatch {
		t.Errorf("FreshDispatch must be true on delta-index path")
	}
	if !fix.indexer.indexedPackages.has("github.com/new/pkg", "v0.1.0") {
		t.Errorf("delta-index must record new package")
	}
	if fix.researchMCP.synthesizeCalled {
		t.Errorf("research MCP must NOT be called when package found in manifest")
	}
	if res.Provenance.DoctrineApplied != "fresh-dispatch" {
		t.Errorf("DoctrineApplied=%q; want fresh-dispatch", res.Provenance.DoctrineApplied)
	}
	if res.AuditChainSeq <= 0 {
		t.Errorf("AuditChainSeq=%d; want >0", res.AuditChainSeq)
	}
}

func TestLiveFallback_PackageNotFound_SynthesizesViaResearchMCP(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.sourceManifest[EcoGo] = &Manifest{Packages: nil}
	fix.researchMCP.synthesizeOutput = "synthesized answer from web sources"
	res, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query:     "obscure package nobody indexed",
		Ecosystem: EcoGo,
	})
	if err != nil {
		t.Fatalf("LiveFallback: %v", err)
	}
	if !res.Provenance.FreshDispatch {
		t.Errorf("FreshDispatch must be true on synthesis path")
	}
	if !fix.findingsCache.records.has(syntheticFindingKey()) {
		t.Errorf("research finding must be cached with cache_hit_reason='fresh_dispatch'; have keys=%v",
			fix.findingsCache.records)
	}
	if !fix.researchMCP.synthesizeCalled {
		t.Errorf("research MCP synthesize must be called on package-not-found path")
	}
	if len(res.Chunks) == 0 || res.Chunks[0].ContentText != "synthesized answer from web sources" {
		t.Errorf("synthesis path must surface MCP output in result.Chunks; got %+v", res.Chunks)
	}
}

func TestLiveFallback_RevalidatorRespected_ForceRefresh(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.researchMCP.synthesizeOutput = "fallback"
	_, _ = fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "any", Ecosystem: EcoGo,
	})
	if !fix.revalidator.forceRefreshSeen {
		t.Errorf("ForceRefresh hint must be visible to Source via resolveForceRefresh(ctx)")
	}
}

func TestLiveFallback_EmitsQueryEventWithFreshDispatchFlag(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.researchMCP.synthesizeOutput = "x"
	_, _ = fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "any", Ecosystem: EcoGo,
	})
	found := false
	for _, evt := range fix.auditChain.snapshot() {
		if evt.EventType == uint32(eventlog.EvtRAGQuery) {
			var p RAGQueryPayload
			if err := jsonUnmarshalForTest(evt.Payload, &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.FreshDispatch {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("EvtRAGQuery with fresh_dispatch=true must be emitted (inv-zen-203)")
	}
}

func TestLiveFallback_RevalidatorError_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.revalidator.err = errors.New("network down")
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "q", Ecosystem: EcoGo,
	})
	if err == nil {
		t.Fatalf("must propagate revalidator error; got nil")
	}
	if !fallbackErrContains(err, "network down") {
		t.Errorf("error must wrap original: %v", err)
	}
}

func TestLiveFallback_ContextCancel(t *testing.T) {
	fix := buildFallbackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fix.dispatcher.LiveFallback(ctx, LiveFallbackRequest{
		Query: "q", Ecosystem: EcoGo,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled; got %v", err)
	}
}

func TestLiveFallback_UnknownEcosystem_Errors(t *testing.T) {
	fix := buildFallbackFixture(t)

	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "q", Ecosystem: EcoRust,
	})
	if err == nil {
		t.Fatalf("expected error for unknown ecosystem; got nil")
	}
	if !fallbackErrContains(err, "no source for ecosystem") {
		t.Errorf("error must identify missing-source root cause: %v", err)
	}
}

func TestLiveFallback_IndexerDeltaError_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.sourceManifest[EcoGo] = &Manifest{
		Packages: []ManifestPackage{
			{Name: "p", LatestStableVersion: "v1.0.0"},
		},
	}
	fix.sourcePackageDocs[fakeKey(EcoGo, "p", "v1.0.0")] = &PackageDoc{
		Package: PackageRef{Name: "p", Ecosystem: EcoGo}, Version: "v1.0.0", RawBody: "x",
	}
	fix.indexer.err = errors.New("disk full")
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "p", Ecosystem: EcoGo, PackageHint: "p",
	})
	if err == nil || !fallbackErrContains(err, "disk full") {
		t.Errorf("indexer-delta error must propagate; got %v", err)
	}
}

func TestLiveFallback_ResearchMCPError_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.researchMCP.err = errors.New("MCP timeout")
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "x", Ecosystem: EcoGo,
	})
	if err == nil || !fallbackErrContains(err, "MCP timeout") {
		t.Errorf("MCP error must propagate; got %v", err)
	}
}

func TestLiveFallback_FindingsCacheError_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.researchMCP.synthesizeOutput = "ok"
	fix.findingsCache.err = errors.New("sqlite locked")
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "x", Ecosystem: EcoGo,
	})
	if err == nil || !fallbackErrContains(err, "sqlite locked") {
		t.Errorf("findings-cache error must propagate; got %v", err)
	}
}

func TestLiveFallback_ResearchMCPUnwired_Errors(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.dispatcher.researchMCP = nil
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "x", Ecosystem: EcoGo,
	})
	if err == nil || !fallbackErrContains(err, "research MCP not wired") {
		t.Errorf("missing-MCP error must surface; got %v", err)
	}
}

func TestLiveFallback_EmitFails_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)

	failChain := &failingAuditChain{err: errors.New("chain disk full")}
	fix.dispatcher.auditEmitter = NewRAGAuditEmitter(
		failChain,
		&DoctrineProfile{Name: "default", AuditEmissionLevel: AuditAll8Events},
	)
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "x", Ecosystem: EcoGo,
	})
	if err == nil || !fallbackErrContains(err, "chain disk full") {
		t.Errorf("emit-failure must propagate before network; got %v", err)
	}

	if fix.revalidator.forceRefreshSeen {
		t.Errorf("manifest fetch must NOT run when audit-emit fails (inv-zen-203 ordering)")
	}
}

func TestLiveFallback_NoAuditEmitter_Errors(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.dispatcher.auditEmitter = nil
	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "x", Ecosystem: EcoGo,
	})
	if err == nil || !fallbackErrContains(err, "auditEmitter not wired") {
		t.Errorf("missing-emitter error must surface; got %v", err)
	}
}

func TestLiveFallback_HappyPath_PayloadFieldsPopulated(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.researchMCP.synthesizeOutput = "x"
	_, _ = fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query:       "obscure package nobody indexed",
		Ecosystem:   EcoGo,
		ProjectPath: "/some/project",
	})
	var payload RAGQueryPayload
	for _, evt := range fix.auditChain.snapshot() {
		if evt.EventType == uint32(eventlog.EvtRAGQuery) {
			_ = json.Unmarshal(evt.Payload, &payload)
		}
	}
	if payload.Query != "obscure package nobody indexed" {
		t.Errorf("payload.Query=%q; want canonical query", payload.Query)
	}
	if payload.Ecosystem != EcoGo {
		t.Errorf("payload.Ecosystem=%q; want EcoGo", payload.Ecosystem)
	}
	if !payload.FreshDispatch {
		t.Errorf("payload.FreshDispatch must be true")
	}
	if payload.ProjectPath != "/some/project" {
		t.Errorf("payload.ProjectPath=%q; want /some/project", payload.ProjectPath)
	}

	if payload.Doctrine == "" {
		t.Errorf("payload.Doctrine must be non-empty per spec §4.6")
	}
}

func TestExtractPackageNameFromQuery_Variants(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"", ""},
		{"how do I use github.com/foo/bar in my project", "github.com/foo/bar"},
		{"some text with no recognizable package", ""},
		{"info about @scope/pkg please", "@scope/pkg"},
		{"crate `tokio-util` usage examples", "tokio-util"},
	}
	for _, tc := range cases {
		got := extractPackageNameFromQuery(tc.query)
		if got != tc.want {
			t.Errorf("extractPackageNameFromQuery(%q)=%q; want %q", tc.query, got, tc.want)
		}
	}
}

func TestComputeFindingKey_StableAcrossCalls(t *testing.T) {
	a := computeFindingKey("hello world", EcoGo)
	b := computeFindingKey("hello world", EcoGo)
	if a != b {
		t.Errorf("computeFindingKey must be deterministic; got %q vs %q", a, b)
	}
	c := computeFindingKey("hello world", EcoPython)
	if a == c {
		t.Errorf("computeFindingKey must differ across ecosystems; got identical %q", a)
	}
}

func TestResolveForceRefresh_AbsentReturnsFalse(t *testing.T) {
	if got := resolveForceRefresh(context.Background()); got {
		t.Errorf("resolveForceRefresh on plain ctx = %v; want false", got)
	}
	ctx := contextWithForceRefresh(context.Background())
	if got := resolveForceRefresh(ctx); !got {
		t.Errorf("resolveForceRefresh after contextWithForceRefresh = false; want true")
	}
}

func TestLiveFallback_DoctrineResolverConsulted(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.dispatcher.doctrineResolver = &fixtureDoctrineResolver{}
	fix.researchMCP.synthesizeOutput = "x"
	_, _ = fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "any", Ecosystem: EcoGo, ProjectPath: "/p",
	})

	var payload RAGQueryPayload
	for _, evt := range fix.auditChain.snapshot() {
		if evt.EventType == uint32(eventlog.EvtRAGQuery) {
			_ = json.Unmarshal(evt.Payload, &payload)
		}
	}
	if payload.Doctrine == "" {
		t.Errorf("payload.Doctrine must be populated when resolver wired")
	}
}

// TestLiveFallback_FetchPackageDocError_Propagates — package found in
// manifest but FetchPackageDoc surfaces an error. The dispatcher MUST NOT
// silently swallow the failure; the error wraps the inner cause.
func TestLiveFallback_FetchPackageDocError_Propagates(t *testing.T) {
	fix := buildFallbackFixture(t)
	fix.sourceManifest[EcoGo] = &Manifest{
		Packages: []ManifestPackage{
			{Name: "ghost", LatestStableVersion: "v0.0.1"},
		},
	}

	_, err := fix.dispatcher.LiveFallback(context.Background(), LiveFallbackRequest{
		Query: "ghost", Ecosystem: EcoGo, PackageHint: "ghost",
	})
	if err == nil {
		t.Fatalf("FetchPackageDoc failure must propagate; got nil")
	}
	if !fallbackErrContains(err, "fetch package doc") {
		t.Errorf("error must identify FetchPackageDoc step: %v", err)
	}
}

func TestSourceFor_EmptySources(t *testing.T) {
	d := &Dispatcher{}
	if src, ok := d.sourceFor(EcoGo); ok || src != nil {
		t.Errorf("sourceFor on nil-sources Dispatcher = (%v, %v); want (nil, false)", src, ok)
	}
}

func TestSourceFor_EmptyEcoMap(t *testing.T) {
	d := &Dispatcher{
		sources: map[Ecosystem]map[SourceType]Source{
			EcoGo: {},
		},
	}
	if src, ok := d.sourceFor(EcoGo); ok || src != nil {
		t.Errorf("sourceFor on empty-eco-map = (%v, %v); want (nil, false)", src, ok)
	}
}

func TestFindInManifest_Variants(t *testing.T) {
	m := &Manifest{Packages: []ManifestPackage{
		{Name: "alpha", LatestStableVersion: "1.0.0", UpstreamURL: "u1"},
		{Name: "beta", LatestStableVersion: "2.0.0"},
	}}
	if _, ok := findInManifest(nil, "alpha", ""); ok {
		t.Errorf("nil manifest must surface not-found")
	}
	if _, ok := findInManifest(m, "", "no recognizable token here"); ok {
		t.Errorf("no-hint + no-token-extraction must surface not-found")
	}
	ref, ok := findInManifest(m, "alpha", "")
	if !ok || ref.Name != "alpha" || ref.LatestStableVersion != "1.0.0" {
		t.Errorf("hint-match: got ok=%v ref=%+v", ok, ref)
	}
	if _, ok := findInManifest(m, "gamma", ""); ok {
		t.Errorf("absent-hint must surface not-found")
	}
}

func jsonUnmarshalForTest(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}

func fallbackErrContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return fallbackStringContains(err.Error(), substr)
}

func fallbackStringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type failingAuditChain struct {
	err error
}

func (f *failingAuditChain) Append(_ context.Context, _ eventlog.EventType, _ []byte, _ string) (int64, error) {
	return 0, f.err
}
func (f *failingAuditChain) LastHash(_ context.Context) (string, error) { return "", nil }
func (f *failingAuditChain) SealPartition(_ context.Context, _ string) error {
	return nil
}
