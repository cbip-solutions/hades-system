// go:build cgo
//go:build cgo
// +build cgo

package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func writeADR(t *testing.T, root, name, id, title string, citedPaths []string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nid: " + id + "\ntitle: " + title + "\nstatus: accepted\ndate: 2026-05-22\nplan: plan-19\ntags: [caronte]\n---\n\n# " + id + ": " + title + "\n\n## Context\n\n"
	for _, p := range citedPaths {
		body += "This decision governs `" + p + "` and its boundary.\n"
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func writeCodeFile(t *testing.T, root, repoRel, content string) {
	t.Helper()
	full := filepath.Join(root, repoRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func adrLinkerStore(t *testing.T) (*store.Store, *ADRLinker, string, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"internal/caronte/intent.GetWhy", "internal/caronte/intent.NewEngine"} {
		n := store.Node{NodeID: id, Name: "X", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/caronte/intent/getwhy.go", PackageID: "internal/caronte/intent", ContentHash: "h"}
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seed node %s: %v", id, err)
		}
	}
	root := t.TempDir()
	linker := NewADRLinker(s, root)
	return s, linker, root, ctx
}

func TestExplicitRefFromCodeCitingADR(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)
	writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture", nil)
	writeCodeFile(t, root, "internal/caronte/intent/getwhy.go",
		"package intent\n// GetWhy implements the get_why surface per ADR-0111.\nfunc GetWhy() {}\n")

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}
	links, err := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	var found bool
	for _, l := range links {
		if l.ADRID == "docs/decisions/0100-caronte.md" && l.LinkKind == string(store.LinkExplicitRef) {
			found = true
		}
	}
	if !found {
		t.Errorf("no explicit_ref link to ADR-0111 for GetWhy; got %+v", links)
	}
}

func TestExplicitRefFromADRCitingPath(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)
	writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture",
		[]string{"internal/caronte/intent"})

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}
	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.NewEngine")
	var found bool
	for _, l := range links {
		if l.ADRID == "docs/decisions/0100-caronte.md" && l.LinkKind == string(store.LinkExplicitRef) {
			found = true
		}
	}
	if !found {
		t.Errorf("no explicit_ref link from ADR-0111 citing the package path; got %+v", links)
	}
}

func TestCoverageManifestLinks(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)
	writeADR(t, root, "0102-federation.md", "ADR-0113", "Federation seam", nil)

	zenDir := filepath.Join(root, ".zen")
	if err := os.MkdirAll(zenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version = 1\n[[coverage]]\npackage = \"internal/caronte/intent\"\nadrs = [\"ADR-0113\"]\n"
	if err := os.WriteFile(filepath.Join(zenDir, "caronte-intent.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}

	var n int
	err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM adr_links WHERE link_kind = ? AND package_id = ? AND adr_id = ?`,
		string(store.LinkCoverageManifest), "internal/caronte/intent", "docs/decisions/0102-federation.md",
	).Scan(&n)
	if err != nil {
		t.Fatalf("count coverage links: %v", err)
	}
	if n != 1 {
		t.Errorf("coverage_manifest link count = %d; want 1", n)
	}
}

func TestIndexAndLinkSkipsUnderscoreFiles(t *testing.T) {
	_, linker, root, ctx := adrLinkerStore(t)
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_index.json"), []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink with only _index.json: %v", err)
	}
}

func TestParseADRRefFrontmatter(t *testing.T) {
	_, linker, root, _ := adrLinkerStore(t)
	path := writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture",
		[]string{"internal/caronte/store", "internal/caronte/intent"})
	ref, err := linker.parseADRRef(path)
	if err != nil {
		t.Fatalf("parseADRRef: %v", err)
	}
	if ref.ID != "ADR-0111" {
		t.Errorf("ID = %q; want ADR-0111", ref.ID)
	}
	if ref.Title != "Caronte architecture" {
		t.Errorf("Title = %q; want Caronte architecture", ref.Title)
	}
	if len(ref.CitedPaths) != 2 {
		t.Errorf("CitedPaths = %v; want 2 entries", ref.CitedPaths)
	}
}

func TestParseADRRefNoFrontmatter(t *testing.T) {
	_, linker, root, _ := adrLinkerStore(t)
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "0200-nodoc.md")
	if err := os.WriteFile(path, []byte("# No frontmatter\n\nSee `internal/cmd/main` for wiring.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := linker.parseADRRef(path)
	if err != nil {
		t.Fatalf("parseADRRef: %v", err)
	}

	if ref.ID == "" {
		t.Error("ID should not be empty for a doc without frontmatter")
	}

	if len(ref.CitedPaths) == 0 {
		t.Error("expected at least one cited path from no-frontmatter body")
	}
}

func TestParseADRRefUnterminatedFrontmatter(t *testing.T) {
	_, linker, root, _ := adrLinkerStore(t)
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "0201-unterminated.md")
	if err := os.WriteFile(path, []byte("---\nid: ADR-0201\ntitle: unterminated\n# Never closes frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := linker.parseADRRef(path)
	if err != nil {
		t.Fatalf("parseADRRef on unterminated frontmatter: %v", err)
	}

	if ref.ID == "" {
		t.Error("ID should not be empty even with unterminated frontmatter")
	}
}

func TestParseADRRefUnreadable(t *testing.T) {
	_, linker, _, _ := adrLinkerStore(t)
	_, err := linker.parseADRRef("/nonexistent/path/to/doc.md")
	if err == nil {
		t.Error("parseADRRef on missing file should return error; got nil")
	}
}

func TestLooksLikeRepoPathEdgeCases(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"internal/foo", true},
		{"cmd/zen/main.go", true},
		{"docs/decisions/0100.md", true},
		{"scripts/build.sh", true},
		{"no_slash", false},
		{"vendor/lib/foo", false},
		{"go test", false},
		{"WAL/file", false},
	}
	for _, c := range cases {
		got := looksLikeRepoPath(c.path)
		if got != c.want {
			t.Errorf("looksLikeRepoPath(%q) = %v; want %v", c.path, got, c.want)
		}
	}
}

func TestPackageOfCitedPathGoFile(t *testing.T) {
	got := packageOfCitedPath("internal/caronte/intent/getwhy.go")
	want := "internal/caronte/intent"
	if got != want {
		t.Errorf("packageOfCitedPath(.go) = %q; want %q", got, want)
	}
}

func TestPackageOfCitedPathDir(t *testing.T) {
	got := packageOfCitedPath("internal/caronte/store/")
	if got != "internal/caronte/store" {
		t.Errorf("packageOfCitedPath(dir/) = %q; want no trailing slash", got)
	}
}

func TestCitedCodePathsDedup(t *testing.T) {
	body := "See `internal/foo/bar` and again `internal/foo/bar` in the design.\n"
	paths := citedCodePaths(body)
	if len(paths) != 1 {
		t.Errorf("citedCodePaths dedup: got %d paths; want 1: %v", len(paths), paths)
	}
}

func TestCitedCodePathsFiltersNonRepoPaths(t *testing.T) {
	body := "Run `go test` and `vendor/lib/foo` and `WAL` in the terminal.\nSee `internal/real/path`.\n"
	paths := citedCodePaths(body)
	for _, p := range paths {
		if p == "go test" || p == "vendor/lib/foo" || p == "WAL" {
			t.Errorf("citedCodePaths returned non-repo path %q", p)
		}
	}

	var found bool
	for _, p := range paths {
		if p == "internal/real/path" {
			found = true
		}
	}
	if !found {
		t.Errorf("citedCodePaths: did not find internal/real/path in %v", paths)
	}
}

func TestDedupeEmpty(t *testing.T) {
	if got := dedupe(nil); got != nil {
		t.Errorf("dedupe(nil) = %v; want nil", got)
	}
	if got := dedupe([]string{}); len(got) != 0 {
		t.Errorf("dedupe([]) = %v; want []", got)
	}
}

func TestDedupeWithDuplicates(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b"}
	got := dedupe(in)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupe: got %v; want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("dedupe[%d] = %q; want %q", i, got[i], v)
		}
	}
}

func TestExplicitRefFromCodeNoCitations(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)
	writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture", nil)

	writeCodeFile(t, root, "internal/caronte/intent/getwhy.go",
		"package intent\n// GetWhy has no ADR reference.\nfunc GetWhy() {}\n")

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}

	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	for _, l := range links {
		if l.LinkKind == string(store.LinkExplicitRef) {
			t.Errorf("unexpected explicit_ref link in code with no citations: %+v", l)
		}
	}
}

func TestExplicitRefFromCodeUnknownADRCitation(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)

	writeCodeFile(t, root, "internal/caronte/intent/getwhy.go",
		"package intent\n// Cites ADR-9998 which does not exist.\nfunc GetWhy() {}\n")

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink with unknown ADR citation: %v", err)
	}

	links, _ := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if len(links) != 0 {
		t.Errorf("expected 0 links for unknown ADR citation; got %+v", links)
	}
	_ = s
}

func TestIndexAndLinkEmptyCorpus(t *testing.T) {
	_, linker, _, ctx := adrLinkerStore(t)

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink on empty root: %v", err)
	}
}

func TestParseCorpusSkipsNonMdFiles(t *testing.T) {
	_, linker, root, ctx := adrLinkerStore(t)
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "schema.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture", nil)

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink with non-.md file: %v", err)
	}
}

func TestIndexAndLinkManifestBadTOML(t *testing.T) {
	_, linker, root, ctx := adrLinkerStore(t)
	zenDir := filepath.Join(root, ".zen")
	if err := os.MkdirAll(zenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zenDir, "caronte-intent.toml"),
		[]byte("schema_version = [[[[["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := linker.IndexAndLink(ctx); err == nil {
		t.Error("IndexAndLink: expected error for malformed manifest TOML; got nil")
	}
}

func TestIndexAndLinkManifestMissingADR(t *testing.T) {
	_, linker, root, ctx := adrLinkerStore(t)

	zenDir := filepath.Join(root, ".zen")
	if err := os.MkdirAll(zenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version = 1\n[[coverage]]\npackage = \"internal/caronte/intent\"\nadrs = [\"ADR-9999\"]\n"
	if err := os.WriteFile(filepath.Join(zenDir, "caronte-intent.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := linker.IndexAndLink(ctx); err == nil {
		t.Error("IndexAndLink: expected error for manifest referencing missing ADR; got nil")
	}
}

func TestExplicitRefBothDirections(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)

	writeADR(t, root, "0100-caronte.md", "ADR-0111", "Caronte architecture",
		[]string{"internal/caronte/intent"})

	writeCodeFile(t, root, "internal/caronte/intent/getwhy.go",
		"package intent\n// GetWhy per ADR-0111.\nfunc GetWhy() {}\n")

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}

	links, err := s.ListADRLinksForNode(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	var count int
	for _, l := range links {
		if l.ADRID == "docs/decisions/0100-caronte.md" && l.LinkKind == string(store.LinkExplicitRef) {
			count++
		}
	}

	if count != 1 {
		t.Errorf("explicit_ref link count (both directions) = %d; want 1 (idempotent upsert); links: %+v", count, links)
	}
}

// TestADRPathIndexFrontmatterIDWins is the load-bearing assertion for the
// invariant contract: when an ADR's filename and frontmatter `id:` field
// disagree (the legitimate renumber-on-merge case where the canonical id
// shifts but the filename is deferred), adrPathIndex MUST resolve ADR-NNNN
// via the frontmatter id, NOT via the first four characters of the filename.
// This is the exact failure mode that caused TestCoverageManifestLinks
// to silently drop manifest entries on renumbered ADRs pre-v0.20.2.
func TestADRPathIndexFrontmatterIDWins(t *testing.T) {
	_, linker, root, _ := adrLinkerStore(t)

	writeADR(t, root, "0102-federation.md", "ADR-0113", "Federation seam", nil)

	idx, err := linker.adrPathIndex()
	if err != nil {
		t.Fatalf("adrPathIndex: %v", err)
	}
	// Canonical frontmatter id MUST be the key, mapping to the on-disk path.
	got, ok := idx["ADR-0113"]
	if !ok {
		t.Fatalf("ADR-0113 (frontmatter id) missing from index; got keys = %v", keysOf(idx))
	}
	if got != "docs/decisions/0102-federation.md" {
		t.Errorf("ADR-0113 → %q; want docs/decisions/0102-federation.md", got)
	}
	// Conversely the filename-derived ADR-0102 MUST NOT appear (we resolved
	// via frontmatter, not via filename; the broken pre-v0.20.2 behaviour
	// would have indexed ADR-0102 instead of ADR-0113).
	if _, leaked := idx["ADR-0102"]; leaked {
		t.Errorf("filename-derived ADR-0102 leaked into index; want only frontmatter id ADR-0113")
	}
}

func TestADRPathIndexFilenameFallback(t *testing.T) {
	_, linker, root, _ := adrLinkerStore(t)
	dir := filepath.Join(root, "docs", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "0299-no-frontmatter.md"),
		[]byte("# Legacy ADR\n\nNo frontmatter on this file.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx, err := linker.adrPathIndex()
	if err != nil {
		t.Fatalf("adrPathIndex: %v", err)
	}
	got, ok := idx["ADR-0299"]
	if !ok {
		t.Fatalf("ADR-0299 (filename-fallback) missing from index; got keys = %v", keysOf(idx))
	}
	if got != "docs/decisions/0299-no-frontmatter.md" {
		t.Errorf("ADR-0299 → %q; want docs/decisions/0299-no-frontmatter.md", got)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestIndexAndLinkNoFrontmatterSpecFile(t *testing.T) {
	s, linker, root, ctx := adrLinkerStore(t)

	specDir := filepath.Join(root, "docs", "superpowers", "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "design.md"),
		[]byte("# Design\n\nSee `internal/caronte/intent` for the intent layer.\n"),
		0o600); err != nil {
		t.Fatal(err)
	}

	if err := linker.IndexAndLink(ctx); err != nil {
		t.Fatalf("IndexAndLink: %v", err)
	}

	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM adr_links WHERE package_id = ? AND link_kind = ?`,
		"internal/caronte/intent", string(store.LinkExplicitRef),
	).Scan(&n); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if n == 0 {
		t.Error("expected explicit_ref links from spec file citing the intent package; got 0")
	}
}
