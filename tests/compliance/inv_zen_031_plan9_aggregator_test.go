// tests/compliance/inv_zen_031_plan9_aggregator_test.go
//
// Compliance gate for invariant: the knowledge
// aggregator and embed packages MUST NOT import internal/store. The daemon
// package is the only legitimate layer that spans both internal/store (daemon
// DB) and the aggregator subsystem, with server_knowledge_aggregator.go as the
// glue point.
//
// D-12 architecture deviation from the original plan-file:
//
// The plan-file described knowledgeadapter as having constructor
// NewAdapter(*store.Store, vaultDir) and importing internal/store directly.
// The actual implementation uses NewAdapterFromDB(*sql.DB) and does NOT
// import internal/store. This is intentional (CGO driver conflict isolation):
// keeping internal/store out of knowledgeadapter means the adapter's test
// binary only links mattn/go-sqlite3; ncruces/go-sqlite3 is never pulled in.
// The daemon glue file (server_knowledge_aggregator.go) is the single point
// that calls s.store.DB() and forwards the *sql.DB to knowledgeadapter.
//
// Two tests in this file:
//
// 1. TestInvZen031Plan9AggregatorNoStoreImport — walks internal/knowledge/
// aggregator/ and internal/knowledge/embed/ (ALL.go files, including
// tests). Asserts zero imports containing "internal/store". Test files are
// included because even test code in the aggregator package must not reach
// into the daemon's store layer — that would break the CGO isolation.
//
// 2. TestInvZen031KnowledgeAdapterLegitimateBridge — asserts that the daemon
// package collectively spans both layers: there must exist at least one
// .go file in internal/daemon/ that imports internal/store, AND at least
// one.go file in internal/daemon/ that imports internal/daemon/
// knowledgeadapter (or aggregatorbridge or internal/knowledge/aggregator).
// Together these two files (which may be different files in the same
// package) form the legitimate cross-layer bridge.
//
// Rationale for two-file check instead of single-file: the D-12
// architecture intentionally splits the bridge across two files in the
// daemon package (server.go imports internal/store; server_knowledge_
// aggregator.go imports knowledgeadapter). Requiring a single file to
// import both would be an artificial constraint that the architecture
// deliberately avoids for CGO isolation reasons. The package-level
// assertion is the correct invariant bridge test for D-12.
//
// invariant: aggregator/embed do not import internal/store.
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen031Plan9AggregatorNoStoreImport(t *testing.T) {
	root := repoRoot(t)

	targetDirs := []string{
		filepath.Join(root, "internal", "knowledge", "aggregator"),
		filepath.Join(root, "internal", "knowledge", "embed"),
	}

	scanned := 0
	for _, dir := range targetDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Logf("inv-zen-031 (plan9 aggregator): directory %s not found, skipping: %v", dir, err)
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

			absPath := filepath.Join(dir, name)
			scanned++

			checkAggregatorNoStoreImport(t, absPath)
		}
	}

	if scanned == 0 {
		t.Fatal("inv-zen-031 (plan9 aggregator): sentinel failure — 0 Go files found in " +
			"aggregator+embed; directory layout may have changed or packages were deleted")
	}
}

func checkAggregatorNoStoreImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("inv-zen-031 (plan9 aggregator): parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}

		importPath := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(importPath, "internal/store") {
			t.Errorf("inv-zen-031 violated: %s imports %q — "+
				"aggregator/embed packages must NOT import internal/store; "+
				"bridge via internal/daemon/knowledgeadapter + server_knowledge_aggregator.go",
				path, importPath)
		}
	}
}

func TestInvZen031KnowledgeAdapterLegitimateBridge(t *testing.T) {
	root := repoRoot(t)
	daemonDir := filepath.Join(root, "internal", "daemon")

	const storeMarker = "internal/store"

	aggregatorMarkers := []string{
		"internal/daemon/knowledgeadapter",
		"internal/daemon/aggregatorbridge",
		"internal/knowledge/aggregator",
	}

	var filesWithStore []string
	var filesWithAggregator []string

	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		t.Fatalf("inv-zen-031 (bridge): ReadDir internal/daemon/: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		absPath := filepath.Join(daemonDir, name)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
		if err != nil {

			t.Logf("inv-zen-031 (bridge): parse %s: %v (skipping)", absPath, err)
			continue
		}

		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, `"`)

			if strings.Contains(importPath, storeMarker) {
				filesWithStore = append(filesWithStore, name)
				break
			}
		}

		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, `"`)

			for _, marker := range aggregatorMarkers {
				if strings.Contains(importPath, marker) {
					filesWithAggregator = append(filesWithAggregator, name)
					goto nextFileAgg
				}
			}
		}
	nextFileAgg:
	}

	if len(filesWithStore) == 0 {
		t.Errorf("inv-zen-031 (bridge): no file in internal/daemon/ imports internal/store — " +
			"daemon must own the store layer as the legitimate bridge spanning store + aggregator")
	} else {
		t.Logf("inv-zen-031 (bridge): store layer present in daemon: %v", filesWithStore)
	}

	if len(filesWithAggregator) == 0 {
		t.Errorf("inv-zen-031 (bridge): no file in internal/daemon/ imports any aggregator-side seam "+
			"(%v) — Plan 9 Phase D bridge missing; daemon must wire the aggregator at this layer",
			aggregatorMarkers)
	} else {
		t.Logf("inv-zen-031 (bridge): aggregator seam present in daemon: %v", filesWithAggregator)
	}
}
