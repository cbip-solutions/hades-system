//go:build chaos

// Drives the contract from spec §4.5 row "File watcher CPU spike":
//   - knowledge.Watcher honours an internal cpuBudget = 25% (constant
//     MaxCPUBudgetPct in internal/knowledge); when CPU exceeds budget,
//     the watcher's debounce loop re-arms its timer instead of
//     dispatching, keeping the queue intact while the spike lasts.
//   - When CPU clears, the queued events drain and reach the
//     IndexerSink — no events lost.
//   - The watcher does NOT crash, leak goroutines, or hang under the
//     pressure of 1000+ rapid file events.
//
// Cross-package surface only: the Watcher's CPUSampler interface is
// package-private (internal/knowledge), so this test does not inject
// a fake CPU profile — it asserts the high-level integration contract:
// real fsnotify events on real markdown files reach the IndexerSink
// even under CPU pressure. The throttle path is exercised by
// internal/knowledge unit tests (Phase G); the chaos surface here
// asserts the integration boundary survives operator-level load.
//
// The test creates a tmpdir, a single Watcher subscribed to the dir,
// then writes/removes 200 files in a tight loop. After the watcher's
// debounce window elapses, every Reindex/Delete call MUST reach the
// IndexerSink (modulo OS-level event coalescing — fsnotify squashes
// repeated Writes on the same path, which is the watcher's contract).
package chaos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

type recordingIndexer struct {
	mu        sync.Mutex
	cond      *sync.Cond
	reindexed map[string]int
	deleted   map[string]int
}

func newRecordingIndexer() *recordingIndexer {
	r := &recordingIndexer{
		reindexed: map[string]int{},
		deleted:   map[string]int{},
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *recordingIndexer) Reindex(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reindexed[path]++
	r.cond.Broadcast()
	return nil
}

func (r *recordingIndexer) Delete(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted[path]++
	r.cond.Broadcast()
	return nil
}

func (r *recordingIndexer) reindexedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reindexed)
}

func (r *recordingIndexer) deletedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.deleted)
}

func (r *recordingIndexer) waitForReindexedAtLeast(t *testing.T, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.reindexedCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitForReindexed: got %d distinct paths, want >=%d (timeout)",
		r.reindexedCount(), want)
}

func TestChaos_KnowledgeWatcher_BurstOfEventsAllReachSink(t *testing.T) {
	root := t.TempDir()
	indexer := newRecordingIndexer()
	w, err := knowledge.NewWatcher(indexer)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := w.AddProject(root); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- w.Run(ctx) }()

	const N = 200
	for i := 0; i < N; i++ {
		p := filepath.Join(root, "memory", fmt.Sprintf("file-%03d.md", i))
		if err := os.WriteFile(p, []byte("# burst"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	indexer.waitForReindexedAtLeast(t, N, 10*time.Second)

	cancel()
	if err := <-runErrCh; err != nil && err != context.Canceled {
		t.Fatalf("Watcher.Run returned unexpected error: %v", err)
	}

	if got := indexer.reindexedCount(); got < N {
		t.Fatalf("expected >=%d distinct paths reindexed, got %d", N, got)
	}
}

func TestChaos_KnowledgeWatcher_DeletePropagatesToSink(t *testing.T) {
	root := t.TempDir()
	indexer := newRecordingIndexer()
	w, err := knowledge.NewWatcher(indexer)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := w.AddProject(root); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- w.Run(ctx) }()

	const N = 30
	paths := make([]string, N)
	for i := 0; i < N; i++ {
		paths[i] = filepath.Join(root, "memory", fmt.Sprintf("rm-%03d.md", i))
		if err := os.WriteFile(paths[i], []byte("# rm"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	indexer.waitForReindexedAtLeast(t, N, 10*time.Second)

	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Fatalf("Remove %s: %v", p, err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if indexer.deletedCount() >= N {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-runErrCh; err != nil && err != context.Canceled {
		t.Fatalf("Watcher.Run returned unexpected error: %v", err)
	}

	if got := indexer.deletedCount(); got < N {
		t.Fatalf("expected >=%d distinct paths deleted, got %d", N, got)
	}
}

func TestChaos_KnowledgeWatcher_NonMarkdownIgnored(t *testing.T) {
	root := t.TempDir()
	indexer := newRecordingIndexer()
	w, err := knowledge.NewWatcher(indexer)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := w.AddProject(root); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- w.Run(ctx) }()

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(
			filepath.Join(root, "memory", fmt.Sprintf("md-%d.md", i)),
			[]byte("# md"), 0o644,
		); err != nil {
			t.Fatalf("WriteFile md: %v", err)
		}
	}
	for i := 0; i < 50; i++ {
		ext := ".txt"
		if i%2 == 0 {
			ext = ".yaml"
		}
		if err := os.WriteFile(
			filepath.Join(root, "memory", fmt.Sprintf("noise-%d%s", i, ext)),
			[]byte("noise"), 0o644,
		); err != nil {
			t.Fatalf("WriteFile noise: %v", err)
		}
	}

	indexer.waitForReindexedAtLeast(t, 5, 10*time.Second)

	time.Sleep(4 * time.Second)

	cancel()
	if err := <-runErrCh; err != nil && err != context.Canceled {
		t.Fatalf("Watcher.Run returned unexpected error: %v", err)
	}

	if got := indexer.reindexedCount(); got != 5 {
		t.Fatalf("non-markdown filter violated: got %d distinct reindex paths, want 5", got)
	}
}
