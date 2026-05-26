// tests/compliance/inv_zen_284_test.go
//
// Compliance gate for inv-zen-284 (v0.20.1 fix #4): `Engine.IndexProject`
// auto-triggers the per-project `semantic.Resolver.ResolveProject` pass
// at the end of a successful walk, so the IndexReport.EdgesCreated
// surfaces a non-zero count and the per-project caronte.db ends a
// reindex in a fully populated state (nodes + edges + decomposition).
//
// Why: before v0.20.1, IndexProject walked the source tree + populated
// nodes via the parser indexer but NEVER called ResolveProject. The
// inline comment at engine_ops.go was honest about it:
//
//	// Edges are populated by the per-project semantic.Resolver
//	// (Phase C of the original Caronte design); the IndexProject
//	// surface reports zero edges until that wiring catches up
//	// (the resolver's call-graph fan-out runs as a separate pass —
//	// Phase E of Plan v0.20.0 may auto-trigger it after IndexProject;
//	// until then EdgesCreated stays 0 — documented at
//	// result_types.go::IndexReport).
//
// the walk, captures the ResolutionStats, and populates EdgesCreated
// from `CallEdges + ImplementsEdges + LLMHintEdges`. Failure of the
// resolver (loadGoPackages errors, broken module, etc.) does NOT fail
// the IndexProject — per spec §15 never-hard-fail; the walk's node
// population is still reported, just with EdgesCreated=0.
//
// Two source-regex anchors:
//
//  1. internal/caronte/engine_ops.go calls `pe.resolver.ResolveProject(`
//     after the walk completes. Removing this call regresses
//     EdgesCreated to permanently 0.
//  2. The returned ResolutionStats are summed into rep.EdgesCreated
//     (anchor on the assignment shape). Without this assignment, the
//     report would understate edges even after a successful resolve.
//
// Sister-test bite check: revert the ResolveProject call OR drop the
// EdgesCreated assignment; this test MUST fail. Behavioural test for
// the end-to-end pipeline (real Go module fixture → IndexProject →
// EdgesCreated > 0) lives at internal/caronte/engine_ops_test.go
// (TestIndexProjectAutoResolvesEdges).
//
// inv-zen-284 (v0.20.1 fix #4).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen284SourceRegex_IndexProjectCallsResolver(t *testing.T) {
	src := readEngineOpsSource(t)
	const needle = `pe.resolver.ResolveProject(`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-284 violated: engine_ops.go IndexProject does not call %q after the walk; EdgesCreated will remain 0 forever", needle)
	}
}

func TestInvZen284SourceRegex_IndexProjectAssignsEdgesCreated(t *testing.T) {
	src := readEngineOpsSource(t)

	const fieldNeedle = `rep.EdgesCreated`
	const statsNeedle = `CallEdges`
	if !strings.Contains(src, fieldNeedle) {
		t.Errorf("inv-zen-284 violated: engine_ops.go IndexProject does not assign to %s; the report will not surface resolver-populated edges", fieldNeedle)
	}
	if !strings.Contains(src, statsNeedle) {
		t.Errorf("inv-zen-284 violated: engine_ops.go IndexProject does not reference %s in EdgesCreated computation; the assignment is not sourced from ResolutionStats", statsNeedle)
	}
}

// TestInvZen284SourceRegex_NeverHardFailOnResolverError asserts the
// §15 never-hard-fail contract: a resolver failure during IndexProject
// MUST NOT fail the IndexReport (the walk's node population is the
// load-bearing output; the resolver's edge fan-out is an enrichment
// step). Anchor on the inline-fix comment pattern that documents the
// degraded path.
func TestInvZen284SourceRegex_NeverHardFailOnResolverError(t *testing.T) {
	src := readEngineOpsSource(t)

	if !strings.Contains(src, "never-hard-fail") && !strings.Contains(src, "§15") {
		t.Errorf("inv-zen-284 violated: engine_ops.go IndexProject's resolver call site lacks an explicit §15 never-hard-fail / 'never-hard-fail' comment marker; a future refactor may turn a resolver error into a hard IndexProject failure, breaking the inv-zen-273 contract that node walks survive resolver hiccups")
	}
}

func readEngineOpsSource(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "internal", "caronte", "engine_ops.go")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve engine_ops.go: %v", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	return string(b)
}
