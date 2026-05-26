package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type fakeValidator struct {
	validateErr        error
	validateTightenErr error
}

func (v *fakeValidator) Validate(_ *v1.Schema) error           { return v.validateErr }
func (v *fakeValidator) ValidateTighten(_, _ *v1.Schema) error { return v.validateTightenErr }

type fakeBaselineProvider struct {
	err error
}

func (f fakeBaselineProvider) BaselineFor(_ string) (*v1.Schema, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}, nil
}

type recordingActive struct {
	mu               sync.Mutex
	perProjectCalls  []recordedCall
	userDefaultCalls []*v1.Schema
}

type recordedCall struct {
	projectID string
	schema    *v1.Schema
}

func (r *recordingActive) SetForProject(projectID string, s *v1.Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.perProjectCalls = append(r.perProjectCalls, recordedCall{projectID, s})
}

func (r *recordingActive) SetUserDefault(s *v1.Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userDefaultCalls = append(r.userDefaultCalls, s)
}

func (r *recordingActive) ClearForProject(projectID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.perProjectCalls {
		if c.projectID == projectID {
			r.perProjectCalls = append(r.perProjectCalls[:i], r.perProjectCalls[i+1:]...)
			return
		}
	}
}

func (r *recordingActive) PerProjectCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.perProjectCalls)
}

func (r *recordingActive) UserDefaultCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.userDefaultCalls)
}

func TestPipeline_HappyPath_UserDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`schema_version="1.0"`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.UserDefaultCallCount(); got != 1 {
		t.Errorf("UserDefault swap count = %d; want 1", got)
	}
	if got := active.PerProjectCallCount(); got != 0 {
		t.Errorf("PerProject swap count = %d; want 0 (user-default file)", got)
	}
	events := evlog.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events; want 1: %#v", len(events), events)
	}
	r, ok := events[0].(reload.DoctrineReloaded)
	if !ok {
		t.Fatalf("event type = %T; want reload.DoctrineReloaded", events[0])
	}
	if r.Source != "operator-edit" {
		t.Errorf("Source = %q; want operator-edit", r.Source)
	}
	if r.DoctrineName != "max-scope" {
		t.Errorf("DoctrineName = %q; want max-scope (derived from filename)", r.DoctrineName)
	}
}

func TestPipeline_HappyPath_PerProjectOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`schema_version="1.0"`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.PerProjectCallCount(); got != 1 {
		t.Errorf("PerProject swap count = %d; want 1", got)
	}
	if got := active.UserDefaultCallCount(); got != 0 {
		t.Errorf("UserDefault swap count = %d; want 0 (per-project file)", got)
	}
}

func TestPipeline_ReadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.UserDefaultCallCount(); got != 0 {
		t.Errorf("swap should not happen on read failure; got %d", got)
	}
	events := evlog.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events; want 1", len(events))
	}
	rf, ok := events[0].(reload.DoctrineReloadFailed)
	if !ok {
		t.Fatalf("event type = %T; want DoctrineReloadFailed", events[0])
	}
	if rf.Phase != "read" {
		t.Errorf("Phase = %q; want read", rf.Phase)
	}
}

func TestPipeline_ParseFailure_KeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`malformed`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: active,
		Parser: &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
			return errors.New("parse boom")
		}},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.UserDefaultCallCount(); got != 0 {
		t.Errorf("swap should not happen on parse failure; got %d calls", got)
	}
	events := evlog.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events; want 1", len(events))
	}
	rf, ok := events[0].(reload.DoctrineReloadFailed)
	if !ok {
		t.Fatalf("event type = %T; want reload.DoctrineReloadFailed", events[0])
	}
	if rf.Phase != "parse" {
		t.Errorf("Phase = %q; want parse", rf.Phase)
	}
}

func TestPipeline_ValidateFailure_KeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{validateErr: errors.New("schema range violation")},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.UserDefaultCallCount(); got != 0 {
		t.Errorf("swap should not happen on validate failure; got %d", got)
	}
	if len(evlog.Snapshot()) != 1 {
		t.Errorf("expected 1 DoctrineReloadFailed event; got %d", len(evlog.Snapshot()))
	}
}

func TestPipeline_TightenViolation_PerProjectRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{validateTightenErr: errors.New("loosens flake_rerun_budget from 1 to 5")},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.PerProjectCallCount(); got != 0 {
		t.Errorf("swap should not happen on tighten violation; got %d", got)
	}
	events := evlog.Snapshot()

	if len(events) != 1 {
		t.Fatalf("got %d events; want 1: %#v", len(events), events)
	}
	if _, ok := events[0].(reload.DoctrineTightenViolationRejected); !ok {
		t.Errorf("event type = %T; want reload.DoctrineTightenViolationRejected", events[0])
	}
}

func TestPipeline_PerProjectMissingBaselineProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: active,
		Parser:         &fakeParser{},
		Validator:      &fakeValidator{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.PerProjectCallCount(); got != 0 {
		t.Errorf("swap should not happen on missing BaselineProvider; got %d", got)
	}
	events := evlog.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events; want 1", len(events))
	}
	rf, ok := events[0].(reload.DoctrineReloadFailed)
	if !ok {
		t.Fatalf("event type = %T; want DoctrineReloadFailed", events[0])
	}
	if rf.Phase != "load" {
		t.Errorf("Phase = %q; want load", rf.Phase)
	}
}

func TestPipeline_BaselineLookupError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   active,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{err: errors.New("registry not loaded")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if got := active.PerProjectCallCount(); got != 0 {
		t.Errorf("swap should not happen on baseline-lookup error; got %d", got)
	}
}

func TestPipeline_CtxCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	active := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: active,
		Parser:         &fakeParser{},
		Validator:      &fakeValidator{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.RunReloadActionForTest(ctx, path)
	if got := active.UserDefaultCallCount(); got != 0 {
		t.Errorf("cancelled-ctx run should not swap; got %d", got)
	}
	if got := len(evlog.Snapshot()); got != 0 {
		t.Errorf("cancelled-ctx run should not emit events; got %d", got)
	}
}

func TestPipeline_AtomicSwap_RaceConcurrent10(t *testing.T) {
	if testing.Short() {
		t.Skip("race-stress skipped in -short mode")
	}
	dir := t.TempDir()
	const N = 50
	paths := make([]string, N)
	for i := 0; i < N; i++ {
		f := filepath.Join(dir, "f"+strconv.Itoa(i)+".toml")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[i] = f
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   &fakeEventlog{},
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	for _, p := range paths {
		if err := w.AddPath(p, ""); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			w.RunReloadActionForTest(ctx, p)
		}(p)
	}
	wg.Wait()

}
