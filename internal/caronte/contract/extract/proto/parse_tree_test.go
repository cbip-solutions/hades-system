//go:build cgo
// +build cgo

package proto

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestParseTreeDeregistersOnClose is the I-1 bite-check: the parsedSources
// side-channel MUST not grow monotonically as proto extraction loops. The
// runtime.AddCleanup hook wired by parseTree deregisters the (tree, source)
// pair so the daemon's long-running ingestion path keeps a bounded map.
//
// Pre-fix behaviour: 50 parse+close cycles → +50 permanent entries in
// parsedSources.m. Post-fix: after GC the entries are gone.
//
// Sister-test for parse_tree.go's cleanup claim (per
// feedback_sister_test_pattern.md: load-bearing doc-comment about
// deregistration MUST be gated by a test that asserts the deregistration).
func TestParseTreeDeregistersOnClose(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()

	parsedSources.RLock()
	start := len(parsedSources.m)
	parsedSources.RUnlock()

	parseOne := func() {
		tree, err := e.parseTree(context.Background(), src)
		if err != nil {
			t.Fatalf("parseTree: %v", err)
		}
		tree.Close()
	}

	const N = 50
	for i := 0; i < N; i++ {
		parseOne()
	}

	deadline := time.Now().Add(5 * time.Second)
	var end int
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.GC()
		parsedSources.RLock()
		end = len(parsedSources.m)
		parsedSources.RUnlock()
		if end <= start {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if end > start {
		t.Fatalf("parsedSources grew %d -> %d after %d parse+close cycles; want bounded (cleanup should deregister)", start, end, N)
	}
}
