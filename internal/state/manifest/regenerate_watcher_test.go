package manifest

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegenerateWatcher_TriggersOnSourceChange(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	regen := NewRegenerator(schema)
	manifestPath := filepath.Join(dir, "system-state.toml")
	if err := regen.RegenerateAndWrite(context.Background(), makeFreshManifest(), manifestPath); err != nil {
		t.Fatal(err)
	}

	var triggers int32
	w := NewRegenerateWatcher(WatcherConfig{
		ManifestPath: manifestPath,
		AuthSources:  []string{dir},
		Walker:       NewWalker(staticWalkerCfg(t)),
		Regenerator:  regen,
		Appender:     &fakeAppender{},
		Debounce:     50 * time.Millisecond,
		OnRegenerated: func() {
			atomic.AddInt32(&triggers, 1)
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	authSrc := filepath.Join(dir, "trigger.txt")
	if err := os.WriteFile(authSrc, []byte("change"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&triggers) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("expected at least 1 OnRegenerated callback within 2s; got %d", triggers)
}

func TestRegenerateWatcher_DebouncesBurst(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	regen := NewRegenerator(schema)
	manifestPath := filepath.Join(dir, "system-state.toml")
	if err := regen.RegenerateAndWrite(context.Background(), makeFreshManifest(), manifestPath); err != nil {
		t.Fatal(err)
	}

	var triggers int32
	w := NewRegenerateWatcher(WatcherConfig{
		ManifestPath: manifestPath,
		AuthSources:  []string{dir},
		Walker:       NewWalker(staticWalkerCfg(t)),
		Regenerator:  regen,
		Appender:     &fakeAppender{},
		Debounce:     200 * time.Millisecond,
		OnRegenerated: func() {
			atomic.AddInt32(&triggers, 1)
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "burst.txt"), []byte{byte(i)}, 0o644)
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)

	if got := atomic.LoadInt32(&triggers); got > 2 {
		t.Errorf("expected ≤2 regenerates from a 5-event burst, got %d (debounce broken)", got)
	}
}

func TestRegenerateWatcher_StopBeforeStartIsSafe(t *testing.T) {
	w := NewRegenerateWatcher(WatcherConfig{})
	w.Stop()
}

func TestRegenerateWatcher_StartNilWalkerReturnsError(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	regen := NewRegenerator(schema)

	w := NewRegenerateWatcher(WatcherConfig{
		Regenerator: regen,
	})
	if err := w.Start(context.Background()); err == nil {
		t.Error("Start with nil Walker should return error")
	}
	w.Stop()
}

func TestRegenerateWatcher_StartNilRegeneratorReturnsError(t *testing.T) {
	w := NewRegenerateWatcher(WatcherConfig{
		Walker: NewWalker(staticWalkerCfg(t)),
	})
	if err := w.Start(context.Background()); err == nil {
		t.Error("Start with nil Regenerator should return error")
	}
	w.Stop()
}

func TestDefaultAuthSources_ReturnsCanonicalPaths(t *testing.T) {
	root := "/repo"
	srcs := DefaultAuthSources(root)
	want := map[string]bool{
		filepath.Join(root, "docs", "decisions"):    true,
		filepath.Join(root, "go.mod"):               true,
		filepath.Join(root, "internal", "doctrine"): true,
	}
	if len(srcs) != len(want) {
		t.Fatalf("DefaultAuthSources returned %d entries, want %d", len(srcs), len(want))
	}
	for _, s := range srcs {
		if !want[s] {
			t.Errorf("unexpected source %q in DefaultAuthSources result", s)
		}
		delete(want, s)
	}
	for missing := range want {
		t.Errorf("missing expected source %q from DefaultAuthSources", missing)
	}
}

func TestRegenerateWatcher_EventsCh_NilWatcher(t *testing.T) {

	rw := &RegenerateWatcher{
		stopC: make(chan struct{}),
	}
	ch := rw.eventsCh()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("eventsCh nil-watcher channel should be closed (receive ok=false)")
		}
	default:
		t.Error("eventsCh nil-watcher channel should be immediately readable (closed)")
	}
}
