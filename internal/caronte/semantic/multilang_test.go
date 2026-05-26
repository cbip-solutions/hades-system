//go:build cgo
// +build cgo

package semantic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func newMemStore(t *testing.T) *store.Store {
	t.Helper()
	db := openInMemoryCaronteDB(t)
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

func seedTSWidget(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	nodes := []store.Node{
		{NodeID: "src/app/widget.Renderer", Name: "Renderer", Kind: string(store.KindInterface), Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 1, ContentHash: "h1"},
		{NodeID: "src/app/widget.Widget", Name: "Widget", Kind: string(store.KindStruct), Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 5, ContentHash: "h2"},
		{NodeID: "src/app/widget.Renderer.render", Name: "render", Kind: string(store.KindField), Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 2, ContentHash: "h3"},
		{NodeID: "src/app/widget.Widget.render", Name: "render", Kind: string(store.KindMethod), Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 6, ContentHash: "h4"},
	}
	for _, n := range nodes {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.NodeID, err)
		}
	}
}

func TestResolveLanguageSCIPPath(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	runner := &fakeSCIPRunner{
		available: map[IndexerKind]bool{IndexerSCIPTypeScript: true},
		index:     []byte(scipFixtureJSON),
	}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(scip): %v", err)
	}
	if stats.Mode != ModeSCIP {
		t.Errorf("Mode = %q; want scip", stats.Mode)
	}
	if stats.SCIPEdges == 0 {
		t.Error("SCIPEdges = 0; want >0 from the fixture")
	}
	if runner.lastKind != IndexerSCIPTypeScript {
		t.Errorf("runner invoked with kind %q; want scip-typescript", runner.lastKind)
	}

	edges, err := s.ListEdgesByTarget(context.Background(), "src/app/widget.Renderer", store.EdgeImplements)
	if err != nil {
		t.Fatalf("ListEdgesByTarget: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.SourceID == "src/app/widget.Widget" && e.Confidence == store.ConfSCIPImpl {
			found = true
		}
	}
	if !found {
		t.Errorf("scip_impl Widget→Renderer implements edge not persisted; got %v", edges)
	}
}

func TestResolveLanguageDegradesToHeuristic(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(absent indexer) hard-failed: %v; must degrade (inv-zen-234)", err)
	}
	if stats.Mode != ModeHeuristic {
		t.Errorf("Mode = %q; want heuristic (degraded)", stats.Mode)
	}
	if stats.HeuristicEdges == 0 {
		t.Error("HeuristicEdges = 0; want the Widget→Renderer name-coverage link")
	}
	edges, _ := s.ListEdgesByTarget(context.Background(), "src/app/widget.Renderer", store.EdgeImplements)
	found := false
	for _, e := range edges {
		if e.SourceID == "src/app/widget.Widget" && e.Confidence == store.ConfHeuristicName {
			found = true
		}
	}
	if !found {
		t.Errorf("heuristic_name Widget→Renderer edge not persisted; got %v", edges)
	}
}

func TestResolveLanguageSCIPErrorDegrades(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	runner := &fakeSCIPRunner{
		available: map[IndexerKind]bool{IndexerSCIPTypeScript: true},
		runErr:    errors.New("indexer crashed"),
	}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(scip error) hard-failed: %v; must degrade", err)
	}
	if stats.Mode != ModeHeuristic {
		t.Errorf("Mode = %q; want heuristic (degraded from scip error)", stats.Mode)
	}
}

func TestResolveLanguageNilRunnerDegrades(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	r := NewMultiLangResolver(s, nil, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(nil runner): %v", err)
	}
	if stats.Mode != ModeHeuristic {
		t.Errorf("Mode = %q; want heuristic", stats.Mode)
	}
}

func TestMultiLangLLMTailRoutesViaSeam(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)

	_ = s.UpsertNode(context.Background(), store.Node{NodeID: "src/app/widget.Pluggable", Name: "Pluggable", Kind: string(store.KindInterface), Language: "typescript", FilePath: "src/app/widget.ts", ContentHash: "h5"})
	_ = s.UpsertNode(context.Background(), store.Node{NodeID: "src/app/widget.Pluggable.plug", Name: "plug", Kind: string(store.KindField), Language: "typescript", FilePath: "src/app/widget.ts", ContentHash: "h6"})
	fd := &fakeDispatcher{resp: &providers.TierResponse{
		Status: 200,
		Body:   []byte(`{"content":[{"type":"text","text":"{\"resolutions\":[{\"from_id\":\"src/app/widget.Widget\",\"to_id\":\"src/app/widget.Pluggable\",\"site_file\":\"src/app/widget.ts\",\"site_line\":1}]}"}]}`),
	}}
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}
	r := NewMultiLangResolver(s, runner, fd, MultiLangOpts{MaxTailSites: 8, EnableLLMTail: true})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(with tail): %v", err)
	}
	if fd.lastCall.Profile != DefaultLLMProfile {
		t.Errorf("LLM-tail Profile = %q; want %q (single-egress seam)", fd.lastCall.Profile, DefaultLLMProfile)
	}
	if fd.lastCall.Path != "/v1/messages" {
		t.Errorf("LLM-tail Path = %q; want /v1/messages", fd.lastCall.Path)
	}
	if stats.LLMHintEdges == 0 {
		t.Error("LLMHintEdges = 0; want the dispatcher-proposed edge persisted")
	}
}

func TestResolveLanguageConfidenceOrderedDedup(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	// Both SCIP (fixture has Widget→Renderer) and heuristic (name covers) would
	// produce the same Widget→Renderer edge. With SCIP available the stored edge
	// MUST be at scip_impl, NOT heuristic_name (dedup keeps higher tier).
	// We need the store to have position data so SCIP lookup resolves.
	// Seed nodes with matching positions from the fixture:
	// fixture: Widget at range[4,...] = line 5, Renderer at range[0,...] = line 1
	// These nodes already exist from seedTSWidget; we update start_line to match fixture.
	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{
		NodeID: "src/app/widget.Renderer", Name: "Renderer", Kind: string(store.KindInterface),
		Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 1, ContentHash: "h1",
	})
	_ = s.UpsertNode(ctx, store.Node{
		NodeID: "src/app/widget.Widget", Name: "Widget", Kind: string(store.KindStruct),
		Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 5, ContentHash: "h2",
	})

	runner := &fakeSCIPRunner{
		available: map[IndexerKind]bool{IndexerSCIPTypeScript: true},
		index:     []byte(scipFixtureJSON),
	}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(ctx, "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(dedup test): %v", err)
	}
	if stats.Mode != ModeSCIP {
		t.Errorf("Mode = %q; want scip (SCIP succeeded)", stats.Mode)
	}

	edges, err := s.ListEdgesByTarget(ctx, "src/app/widget.Renderer", store.EdgeImplements)
	if err != nil {
		t.Fatalf("ListEdgesByTarget: %v", err)
	}
	var scipEdge, heuristicEdge bool
	for _, e := range edges {
		if e.SourceID == "src/app/widget.Widget" {
			if e.Confidence == store.ConfSCIPImpl {
				scipEdge = true
			}
			if e.Confidence == store.ConfHeuristicName {
				heuristicEdge = true
			}
		}
	}
	if !scipEdge {
		t.Error("dedup: scip_impl edge Widget→Renderer not found; it must be the winner")
	}
	// In SCIP mode we do NOT also run the heuristic, so heuristic_name for the
	// same pair must not exist. (Note: UpsertEdge is an upsert keyed on
	// (source,target,kind), so SCIP edge replaces any prior heuristic edge.)
	if heuristicEdge {
		t.Error("dedup violation: heuristic_name edge survived alongside scip_impl for the same Widget→Renderer pair")
	}
}

func TestResolveLanguageDeterminism(t *testing.T) {

	s1 := newMemStore(t)
	s2 := newMemStore(t)
	seedTSWidget(t, s1)
	seedTSWidget(t, s2)

	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}
	r1 := NewMultiLangResolver(s1, runner, nil, MultiLangOpts{})
	r2 := NewMultiLangResolver(s2, runner, nil, MultiLangOpts{})

	ctx := context.Background()
	stats1, err := r1.ResolveLanguage(ctx, "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	stats2, err := r2.ResolveLanguage(ctx, "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("run2: %v", err)
	}

	if stats1.HeuristicEdges != stats2.HeuristicEdges {
		t.Errorf("non-deterministic edge count: run1=%d run2=%d", stats1.HeuristicEdges, stats2.HeuristicEdges)
	}

	e1, _ := s1.ListEdgesByTarget(ctx, "src/app/widget.Renderer", store.EdgeImplements)
	e2, _ := s2.ListEdgesByTarget(ctx, "src/app/widget.Renderer", store.EdgeImplements)
	if len(e1) != len(e2) {
		t.Fatalf("non-deterministic edge count in store: %d vs %d", len(e1), len(e2))
	}
	for i := range e1 {
		if e1[i].SourceID != e2[i].SourceID || e1[i].Confidence != e2[i].Confidence {
			t.Errorf("non-deterministic edge[%d]: %+v vs %+v", i, e1[i], e2[i])
		}
	}
}

func TestResolveLanguageNoDanglingEdges(t *testing.T) {
	s := newMemStore(t)

	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{
		NodeID: "src/app/widget.Widget", Name: "Widget", Kind: string(store.KindStruct),
		Language: "typescript", FilePath: "src/app/widget.ts", StartLine: 5, ContentHash: "hW",
	})
	runner := &fakeSCIPRunner{
		available: map[IndexerKind]bool{IndexerSCIPTypeScript: true},
		index:     []byte(scipFixtureJSON),
	}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	_, err := r.ResolveLanguage(ctx, "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(no-dangling): %v", err)
	}

	edges, _ := s.ListEdgesByTarget(ctx, "src/app/widget.Renderer", store.EdgeImplements)
	for _, e := range edges {
		if e.SourceID == "" || e.TargetID == "" {
			t.Errorf("dangling edge with empty endpoint: %+v", e)
		}
	}

	if len(edges) > 0 {
		t.Errorf("implements edge written with unresolved Renderer endpoint (dangling): %+v", edges)
	}
}

func TestResolveLanguageUnsupportedLanguageDegrades(t *testing.T) {
	s := newMemStore(t)
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "ruby")
	if err != nil {
		t.Fatalf("ResolveLanguage(unknown lang): %v", err)
	}
	if stats.Mode != ModeHeuristic {
		t.Errorf("Mode = %q; want heuristic for unknown language", stats.Mode)
	}
}

func TestResolveLanguageTailSkippedWhenNilDispatcher(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)

	_ = s.UpsertNode(context.Background(), store.Node{
		NodeID: "src/app/widget.Lonely", Name: "Lonely", Kind: string(store.KindInterface),
		Language: "typescript", FilePath: "src/app/widget.ts", ContentHash: "hL",
	})
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}

	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{EnableLLMTail: true})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(nil dispatcher, EnableLLMTail=true): %v", err)
	}
	if stats.LLMHintEdges != 0 {
		t.Errorf("LLMHintEdges = %d; want 0 (nil dispatcher skips tail)", stats.LLMHintEdges)
	}

	if stats.Unresolved != 0 {
		t.Errorf("Unresolved = %d; want 0 (tail section skipped when dispatcher is nil)", stats.Unresolved)
	}
}

func TestResolveLanguageTailMaxSitesBound(t *testing.T) {
	s := newMemStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := store.Node{
			NodeID: fmt.Sprintf("m.I%d", i), Name: fmt.Sprintf("I%d", i),
			Kind: string(store.KindInterface), Language: "typescript", FilePath: "m.ts",
			ContentHash: fmt.Sprintf("h%d", i),
		}
		if err := s.UpsertNode(ctx, id); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	fd := &fakeDispatcher{resp: &providers.TierResponse{
		Status: 200,
		Body:   []byte(`{"content":[{"type":"text","text":"{}"}]}`),
	}}
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}

	r := NewMultiLangResolver(s, runner, fd, MultiLangOpts{MaxTailSites: 2, EnableLLMTail: true})
	stats, err := r.ResolveLanguage(ctx, "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(cap test): %v", err)
	}
	if stats.Unresolved > 2 {
		t.Errorf("Unresolved = %d; want ≤2 (MaxTailSites cap)", stats.Unresolved)
	}
}

func TestResolveLanguageSCIPParseErrorDegrades(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)
	runner := &fakeSCIPRunner{
		available: map[IndexerKind]bool{IndexerSCIPTypeScript: true},
		index:     []byte("not json at all"),
	}
	r := NewMultiLangResolver(s, runner, nil, MultiLangOpts{})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(scip parse error) hard-failed: %v; want degrade", err)
	}
	if stats.Mode != ModeHeuristic {
		t.Errorf("Mode = %q; want heuristic (degraded from SCIP parse error)", stats.Mode)
	}
}

func TestFilterByLanguageEmptyString(t *testing.T) {
	nodes := []store.Node{
		{NodeID: "a", Language: "go"},
		{NodeID: "b", Language: "typescript"},
	}
	out := filterByLanguage(nodes, "")
	if len(out) != len(nodes) {
		t.Errorf("filterByLanguage(empty) = %d nodes; want %d (pass-through)", len(out), len(nodes))
	}
}

func TestResolveLanguageLLMTailDispatchError(t *testing.T) {
	s := newMemStore(t)
	seedTSWidget(t, s)

	_ = s.UpsertNode(context.Background(), store.Node{
		NodeID: "src/app/widget.Pluggable", Name: "Pluggable", Kind: string(store.KindInterface),
		Language: "typescript", FilePath: "src/app/widget.ts", ContentHash: "hP",
	})
	fd := &fakeDispatcher{err: errors.New("ollama down")}
	runner := &fakeSCIPRunner{available: map[IndexerKind]bool{}}
	r := NewMultiLangResolver(s, runner, fd, MultiLangOpts{MaxTailSites: 8, EnableLLMTail: true})
	stats, err := r.ResolveLanguage(context.Background(), "proj", "/repo", "typescript")
	if err != nil {
		t.Fatalf("ResolveLanguage(tail dispatch error) hard-failed: %v; want degrade", err)
	}
	if stats.LLMHintEdges != 0 {
		t.Errorf("LLMHintEdges = %d; want 0 when dispatcher errors", stats.LLMHintEdges)
	}
}
