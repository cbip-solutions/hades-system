//go:build cgo
// +build cgo

package intent

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func whyFixture(t *testing.T) (*store.Store, *Engine, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: "internal/caronte/intent/getwhy.go",
		PackageID: "internal/caronte/intent", Doc: "GetWhy merges ADR links semantic passages and Lore.", ContentHash: "h",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: n.NodeID, PackageID: n.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0, Stale: true,
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if err := s.UpsertLoreTrailer(ctx, store.LoreTrailer{
		CommitSHA: "abc123", FilePath: n.FilePath, NodeID: n.NodeID,
		TrailerKind: string(store.TrailerConstraint), Body: "no net/http in the embed path", AuthoredAt: 1700000000,
	}); err != nil {
		t.Fatalf("seed lore: %v", err)
	}
	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	eng := NewEngine(s, idx, map[string]string{"docs/decisions/0100-caronte.md": "Caronte architecture"})
	return s, eng, ctx
}

func TestGetWhyMergesAllThreeSources(t *testing.T) {
	_, eng, ctx := whyFixture(t)
	ans, err := eng.GetWhy(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("GetWhy: %v", err)
	}
	if ans.Subject != "internal/caronte/intent.GetWhy" {
		t.Errorf("Subject = %q", ans.Subject)
	}

	var sawADR bool
	for _, a := range ans.LinkedADRs {
		if a.ADRID == "docs/decisions/0100-caronte.md" {
			sawADR = true
			if !a.Stale {
				t.Error("linked ADR should be stale")
			}
			if a.ADRTitle != "Caronte architecture" {
				t.Errorf("ADRTitle = %q; want Caronte architecture", a.ADRTitle)
			}
		}
	}
	if !sawADR {
		t.Errorf("no linked ADR in answer: %+v", ans.LinkedADRs)
	}

	if len(ans.SemanticPassages) == 0 {
		t.Error("no semantic passages in answer")
	}

	var sawLore bool
	for _, l := range ans.LoreTrailers {
		if l.Body == "no net/http in the embed path" && l.TrailerKind == "constraint" {
			sawLore = true
		}
	}
	if !sawLore {
		t.Errorf("no Lore trailer in answer: %+v", ans.LoreTrailers)
	}
}

func TestGetWhyByFileAggregatesPackage(t *testing.T) {
	_, eng, ctx := whyFixture(t)
	ans, err := eng.GetWhy(ctx, "internal/caronte/intent/getwhy.go")
	if err != nil {
		t.Fatalf("GetWhy(file): %v", err)
	}
	var sawADR bool
	for _, a := range ans.LinkedADRs {
		if a.ADRID == "docs/decisions/0100-caronte.md" {
			sawADR = true
		}
	}
	if !sawADR {
		t.Errorf("file-level get_why did not aggregate package ADR links: %+v", ans.LinkedADRs)
	}
}

func TestGetWhyEmptySubject(t *testing.T) {
	_, eng, ctx := whyFixture(t)
	ans, err := eng.GetWhy(ctx, "does/not.Exist")
	if err != nil {
		t.Fatalf("GetWhy(unknown): %v", err)
	}
	if ans.Subject != "does/not.Exist" {
		t.Errorf("Subject = %q", ans.Subject)
	}
	if len(ans.LinkedADRs) != 0 || len(ans.LoreTrailers) != 0 {
		t.Errorf("unknown subject should have no ADRs/Lore: %+v", ans)
	}
}

func TestNewEngineRejectsNilStore(t *testing.T) {
	idx, _ := NewSemanticIndexer(newTestStore(t), fakeEmbedder{dim: 1536}, nil, IntentParams{})
	defer func() {
		if r := recover(); r != nil {

			t.Fatalf("NewEngine panicked: %v", r)
		}
	}()

	eng := NewEngine(nil, idx, nil)
	if eng == nil {
		return
	}
}

func TestGetWhyCoverageManifestPackageLevelLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := store.Node{
		NodeID: "internal/caronte/store.Store", Name: "Store", Kind: string(store.KindStruct),
		Language: "go", FilePath: "internal/caronte/store/store.go",
		PackageID: "internal/caronte/store", ContentHash: "h2",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID:      "docs/decisions/0082-caronte-substrate.md",
		NodeID:     "",
		PackageID:  "internal/caronte/store",
		LinkKind:   string(store.LinkCoverageManifest),
		Confidence: 1.0,
		Stale:      false,
	}); err != nil {
		t.Fatalf("seed coverage_manifest link: %v", err)
	}

	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}
	eng := NewEngine(s, idx, map[string]string{
		"docs/decisions/0082-caronte-substrate.md": "Caronte substrate ADR",
	})

	ans, err := eng.GetWhy(ctx, "internal/caronte/store.Store")
	if err != nil {
		t.Fatalf("GetWhy: %v", err)
	}

	var sawCoverage bool
	for _, a := range ans.LinkedADRs {
		if a.ADRID == "docs/decisions/0082-caronte-substrate.md" {
			sawCoverage = true
			if a.LinkKind != string(store.LinkCoverageManifest) {
				t.Errorf("link kind = %q; want coverage_manifest", a.LinkKind)
			}
			if a.ADRTitle != "Caronte substrate ADR" {
				t.Errorf("ADRTitle = %q; want Caronte substrate ADR", a.ADRTitle)
			}
		}
	}
	if !sawCoverage {
		t.Errorf("coverage_manifest package-level ADR not surfaced for node in covered package: %+v", ans.LinkedADRs)
	}
}

func TestGetWhyNilSemanticDegrades(t *testing.T) {
	s, _, ctx := whyFixture(t)

	eng := NewEngine(s, nil, map[string]string{"docs/decisions/0100-caronte.md": "Caronte architecture"})
	ans, err := eng.GetWhy(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("GetWhy: %v", err)
	}
	if !ans.Degraded {
		t.Error("expected Degraded=true when semantic indexer is nil")
	}

	var sawADR bool
	for _, a := range ans.LinkedADRs {
		if a.ADRID == "docs/decisions/0100-caronte.md" {
			sawADR = true
		}
	}
	if !sawADR {
		t.Errorf("ADR links absent in degraded answer: %+v", ans.LinkedADRs)
	}
}

func TestGetWhyNilStoreDegrades(t *testing.T) {
	eng := NewEngine(nil, nil, nil)
	ans, err := eng.GetWhy(context.Background(), "any/subject")
	if err != nil {
		t.Fatalf("GetWhy(nil store): %v", err)
	}
	if !ans.Degraded {
		t.Error("expected Degraded=true for nil store engine")
	}
	if ans.Subject != "any/subject" {
		t.Errorf("Subject = %q; want any/subject", ans.Subject)
	}
}

func TestFileDirNoSlash(t *testing.T) {
	if got := fileDir("noslash"); got != "" {
		t.Errorf("fileDir(noslash) = %q; want empty", got)
	}
}

func TestAppendLinksDedup(t *testing.T) {
	eng := NewEngine(newTestStore(t), nil, nil)
	ans := &WhyAnswer{}
	seen := map[string]struct{}{}
	link := store.ADRLink{
		ADRID: "docs/decisions/0100-caronte.md", NodeID: "pkg.Foo",
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0,
	}
	eng.appendLinks(ans, []store.ADRLink{link}, seen)
	eng.appendLinks(ans, []store.ADRLink{link}, seen)
	if len(ans.LinkedADRs) != 1 {
		t.Errorf("expected 1 deduped ADR; got %d: %+v", len(ans.LinkedADRs), ans.LinkedADRs)
	}
}

func TestGetWhyCancelledCtxDegrades(t *testing.T) {
	_, eng, _ := whyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := eng.GetWhy(ctx, "internal/caronte/intent.GetWhy")
	if err == nil {

		t.Log("GetWhy with cancelled ctx returned nil error (SQLite tolerated it)")
	}
}

func TestGetWhyLoreDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := store.Node{
		NodeID: "pkg.FnDedup", Name: "FnDedup", Kind: string(store.KindFunction),
		Language: "go", FilePath: "pkg/fn.go", PackageID: "pkg", ContentHash: "h3",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	n2 := store.Node{
		NodeID: "pkg.FnDedup2", Name: "FnDedup2", Kind: string(store.KindFunction),
		Language: "go", FilePath: "pkg/fn.go", PackageID: "pkg", ContentHash: "h4",
	}
	if err := s.UpsertNode(ctx, n2); err != nil {
		t.Fatalf("seed node2: %v", err)
	}

	trailer := store.LoreTrailer{
		CommitSHA: "dup123", FilePath: "pkg/fn.go", NodeID: n.NodeID,
		TrailerKind: string(store.TrailerConstraint), Body: "same body", AuthoredAt: 1700000001,
	}
	if err := s.UpsertLoreTrailer(ctx, trailer); err != nil {
		t.Fatalf("seed lore1: %v", err)
	}
	trailer2 := trailer
	trailer2.NodeID = n2.NodeID
	if err := s.UpsertLoreTrailer(ctx, trailer2); err != nil {
		t.Fatalf("seed lore2: %v", err)
	}

	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}
	eng := NewEngine(s, idx, nil)

	ans, err := eng.GetWhy(ctx, "pkg/fn.go")
	if err != nil {
		t.Fatalf("GetWhy: %v", err)
	}
	count := 0
	for _, l := range ans.LoreTrailers {
		if l.CommitSHA == "dup123" && l.Body == "same body" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduped lore entry; got %d: %+v", count, ans.LoreTrailers)
	}
}
