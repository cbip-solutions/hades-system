// tests/compliance/inv_zen_129_aggregator_no_web_test.go
//
// Compliance gate for inv-zen-129 (Plan 9 D-14 extension): the knowledge
// aggregator and embed packages MUST NOT make any web calls. This test
// covers the Plan 9 Phase D aggregator + embed subsystem, which is distinct
// from the Plan 7 knowledge package gate in inv_zen_129_knowledge_no_remote_test.go.
//
// Two defense-in-depth layers:
//
//	(a) AST-import scan: for every non-test .go file in
//	    internal/knowledge/aggregator/ and internal/knowledge/embed/,
//	    parse the file's import block and assert "net/http" is absent.
//	    This catches a direct import of the HTTP client surface.
//
//	(b) Callsite grep: raw file scan for http.{Get,Post,Do,PostForm,Head,
//	    Client} identifiers via regexp. Catches a case where net/http is
//	    imported transitively (and thus escaped layer (a)) but a callsite
//	    is present in the source text — defence in depth, not a substitute
//	    for the import scan.
//
// These two layers defend against distinct drift modes:
//   - Layer (a): developer adds `import "net/http"` directly.
//   - Layer (b): developer adds a vendor shim that re-exports http.Client
//     without the net/http import path appearing in the aggregator source.
//
// inv-zen-129: no web queries from the aggregator or embed worker.
// inv-zen-080: single-egress-point for LLM traffic (corollary: no ad-hoc HTTP).
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var httpCallPattern = regexp.MustCompile(`\bhttp\.(PostForm|Client|Head|Post|Get|Do)\b`)

func TestInvZen129AggregatorNoWeb(t *testing.T) {
	root := repoRoot(t)

	targetDirs := []string{
		filepath.Join(root, "internal", "knowledge", "aggregator"),
		filepath.Join(root, "internal", "knowledge", "embed"),
	}

	scanned := 0
	for _, dir := range targetDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {

			t.Logf("inv-zen-129 (aggregator): directory %s not found, skipping: %v", dir, err)
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".go") {
				continue
			}

			if strings.HasSuffix(name, "_test.go") {
				continue
			}

			absPath := filepath.Join(dir, name)
			scanned++

			checkAggregatorNoNetHTTPImport(t, absPath)

			checkAggregatorNoHTTPCallsite(t, absPath)
		}
	}

	if scanned == 0 {
		t.Fatal("inv-zen-129 (aggregator): sentinel failure — 0 non-test Go files found in aggregator+embed; " +
			"directory layout may have changed or both packages were accidentally deleted")
	}
}

func checkAggregatorNoNetHTTPImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("inv-zen-129 (aggregator): parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}

		if imp.Path.Value == `"net/http"` {
			t.Errorf("inv-zen-129 violated: %s imports net/http — "+
				"aggregator/embed packages must never make web calls", path)
		}
	}
}

func checkAggregatorNoHTTPCallsite(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("inv-zen-129 (aggregator): read %s: %v", path, err)
		return
	}
	matches := httpCallPattern.FindAllString(string(data), -1)
	if len(matches) > 0 {
		t.Errorf("inv-zen-129 violated: %s contains http callsite(s) %v — "+
			"aggregator/embed packages must never call net/http methods directly", path, matches)
	}
}
