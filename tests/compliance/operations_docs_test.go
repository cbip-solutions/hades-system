package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRootDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("go.mod not found walking up from %s", cwd)
	return ""
}

func findADRFile(repoRoot, adrID string) string {
	dir := filepath.Join(repoRoot, "docs", "decisions")
	num := strings.TrimPrefix(adrID, "ADR-")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), num+"-") && strings.HasSuffix(e.Name(), ".md") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func checkWikilinks(t *testing.T, repoRoot, docName, body string) {
	t.Helper()
	re := regexp.MustCompile(`\[\[(ADR-\d{4})\]\]`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		adrID := m[1]
		if path := findADRFile(repoRoot, adrID); path == "" {
			t.Errorf("%s: wikilink [[%s]] — no matching file in docs/decisions/", docName, adrID)
		}
	}
}

func checkRelativeLinks(t *testing.T, docDir, docName, body string) {
	t.Helper()
	re := regexp.MustCompile(`\[(?:[^\]]*)\]\(\./([^)]+\.md)\)`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		target := filepath.Join(docDir, m[1])
		if _, err := os.Stat(target); err != nil {
			t.Errorf("%s: relative link ./%s — file not found: %s", docName, m[1], target)
		}
	}
}

func requireOpsDoc(t *testing.T, name string, sections []string) {
	t.Helper()
	root := repoRootDir(t)
	docPath := filepath.Join(root, "docs", "operations", name)
	body, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read docs/operations/%s: %v", name, err)
	}
	content := string(body)
	for _, h := range sections {
		if !strings.Contains(content, h) {
			t.Errorf("docs/operations/%s missing required section: %q", name, h)
		}
	}

	checkWikilinks(t, root, "docs/operations/"+name, content)
	checkRelativeLinks(t, filepath.Join(root, "docs", "operations"), name, content)
}

func TestOpsAuditCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "audit.md", []string{
		"# HADES system audit chain operations",
		"## Architecture overview",
		"## Tessera tile-log",
		"## Litestream replication",
		"## Cold archive",
		"## Recovery flow",
		"## Tamper response per-doctrine",
		"## Doctor checks (audit.*)",
		"## CLI commands (zen audit-chain)",
		"## Troubleshooting",
	})
}

func TestOpsKnowledgeAggregatorCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "knowledge-aggregator.md", []string{
		"# HADES system cross-project knowledge aggregator",
		"## Architecture",
		"## Hybrid retrieval (FTS5 + sqlite-vec + wikilink + RRF)",
		"## Per-project shard + opt-in promote",
		"## CLI commands (zen knowledge)",
		"## Boundary respect",
	})
}

func TestOpsAdrCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "adr.md", []string{
		"# HADES system ADR machine-readable index",
		"## Structured MADR format",
		"## Validator + dual JSON manifest",
		"## Migration tool (39 existing ADRs)",
		"## ADR transitions",
		"## CLI commands (zen adr)",
	})
}

func TestOpsResearchCacheCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "research-cache.md", []string{
		"# HADES system research findings global cache",
		"## Architecture",
		"## Content-addressed cache key",
		"## Per-source TTL + revalidation",
		"## Plan 4 MCP integration",
		"## CLI commands (zen research)",
	})
}

func TestOpsSystemStateCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "system-state.md", []string{
		"# HADES system system-state.toml manifest",
		"## Architecture (auto-derived + manual fields)",
		"## JSON Schema + verify gate",
		"## Manual field pinning + chain anchor",
		"## CLI commands (zen state)",
	})
}

func TestOpsBoundaryCrossRefsResolve(t *testing.T) {
	requireOpsDoc(t, "knowledge-aggregator-boundary.md", []string{
		"# HADES system knowledge aggregator boundary",
		"## 4-tier diagram",
		"## Ownership matrix per file/feature",
		"## Layer 1: Plan 7 per-project knowledge index",
		"## Layer 2: Plan 9 knowledge aggregator",
		"## Layer 3: Plan 9 research findings cache",
		"## Layer 4: Plan 14 ecosystem RAG (deferred)",
		"## caronte orthogonal",
	})
}

func TestAllOpsDocsWikilinksResolve(t *testing.T) {
	root := repoRootDir(t)
	opsDir := filepath.Join(root, "docs", "operations")
	entries, err := os.ReadDir(opsDir)
	if err != nil {
		t.Fatalf("read docs/operations: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(opsDir, e.Name()))
		if err != nil {
			t.Errorf("read %s: %v", e.Name(), err)
			continue
		}
		checkWikilinks(t, root, e.Name(), string(body))
		checkRelativeLinks(t, opsDir, e.Name(), string(body))
	}
}
