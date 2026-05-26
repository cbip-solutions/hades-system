package reload_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type fakeEventlog struct {
	mu     sync.Mutex
	events []any
}

func (f *fakeEventlog) Emit(_ context.Context, evt any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evt)
	return nil
}

func (f *fakeEventlog) Snapshot() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, len(f.events))
	copy(out, f.events)
	return out
}

type fakeActiveAccessor struct {
	mu              sync.Mutex
	perProjectCalls map[string]int
	userDefault     int
	lastSchema      *v1.Schema
}

func newFakeActiveAccessor() *fakeActiveAccessor {
	return &fakeActiveAccessor{perProjectCalls: map[string]int{}}
}

func (f *fakeActiveAccessor) SetForProject(projectID string, s *v1.Schema) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perProjectCalls[projectID]++
	f.lastSchema = s
}

func (f *fakeActiveAccessor) SetUserDefault(s *v1.Schema) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userDefault++
	f.lastSchema = s
}

func (f *fakeActiveAccessor) ClearForProject(projectID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.perProjectCalls, projectID)
}

type fakeParser struct {
	parseFn func(data []byte, source string, target *v1.Schema, opts parser.ParseOpts) error
}

func (p *fakeParser) ParseStrict(data []byte, source string, target *v1.Schema, opts parser.ParseOpts) error {
	if p.parseFn != nil {
		return p.parseFn(data, source, target, opts)
	}
	*target = v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.0.0",
		AutoUpgrade:     "patch",
	}
	return nil
}

func TestWatcherNew_RequiresEventlog(t *testing.T) {
	_, err := reload.New(reload.WatcherOpts{
		EventlogClient: nil,
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err == nil {
		t.Fatal("expected error when EventlogClient is nil; got nil")
	}
}

func TestWatcherNew_RequiresActiveAccessor(t *testing.T) {
	_, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: nil,
		Parser:         &fakeParser{},
	})
	if err == nil {
		t.Fatal("expected error when ActiveAccessor is nil; got nil")
	}
}

func TestWatcherNew_RequiresParser(t *testing.T) {
	_, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         nil,
	})
	if err == nil {
		t.Fatal("expected error when Parser is nil; got nil")
	}
}

func TestWatcherNew_DefaultsApplied(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if got, want := w.DebounceWindow(), 2*time.Second; got != want {
		t.Errorf("DebounceWindow default = %v; want %v", got, want)
	}
	if got, want := w.StormCooldown(), 1*time.Minute; got != want {
		t.Errorf("StormCooldown default = %v; want %v", got, want)
	}
	if got, want := w.StormThreshold(), 5; got != want {
		t.Errorf("StormThreshold default = %v; want %v", got, want)
	}
}

func TestWatcherAddPath_RegistersAndUnregisters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte("schema_version=\"1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatalf("AddPath: %v", err)
	}
	if got := w.PathsCount(); got != 1 {
		t.Errorf("PathsCount = %d; want 1", got)
	}

	if err := w.AddPath(path, "proj-B"); err != nil {
		t.Fatalf("AddPath re-arm: %v", err)
	}
	if got := w.PathsCount(); got != 1 {
		t.Errorf("PathsCount after re-arm = %d; want 1", got)
	}
}

func TestWatcherAddPath_MissingFile(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath("/nonexistent/path", ""); err == nil {
		t.Fatal("expected error for missing file; got nil")
	}
}

func TestWatcherAddPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(filepath.Join(dir, "..", "etc", "passwd"), ""); err == nil {
		t.Errorf("expected AddPath to reject path with .. traversal; got nil")
	}
}

func TestWatcherStart_BlocksUntilCtxCancel(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: newFakeActiveAccessor(),
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Start returned %v; want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s of ctx cancel")
	}
}
