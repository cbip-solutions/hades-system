package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestNotifyForce_BypassesDebounce_FiresImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
		DebounceWindow:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if err := w.NotifyForce(path); err != nil {
		t.Fatalf("NotifyForce: %v", err)
	}
	elapsed := time.Since(t0)
	if elapsed > 500*time.Millisecond {
		t.Errorf("NotifyForce took %v; should be ~immediate (no debounce)", elapsed)
	}
	if got := len(evlog.Snapshot()); got != 1 {
		t.Errorf("event count = %d; want 1 (DoctrineReloaded)", got)
	}
}

func TestNotifyForce_SourceLabelledAmendmentApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
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
	if err := w.NotifyForce(path); err != nil {
		t.Fatal(err)
	}
	events := evlog.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events; want 1", len(events))
	}
	r, ok := events[0].(reload.DoctrineReloaded)
	if !ok {
		t.Fatalf("event type = %T; want DoctrineReloaded", events[0])
	}
	if r.Source != "amendment-apply" {
		t.Errorf("Source = %q; want %q", r.Source, "amendment-apply")
	}
}

func TestNotifyForce_UnregisteredPath_ReturnsError(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.NotifyForce("/never/registered"); err == nil {
		t.Error("expected error for unregistered path; got nil")
	}
}

func TestSubscribeReloadEvents_DeliversOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
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
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	sub := w.SubscribeReloadEvents()
	defer w.UnsubscribeReloadEvents(sub)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = w.NotifyForce(path)
	}()

	select {
	case ev := <-sub:
		if ev.Path != path {
			t.Errorf("event Path = %q; want %q", ev.Path, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive reload event within 2s")
	}
}

func TestSubscribeReloadFailedEvents_DeliversOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
		Validator:      &fakeValidator{validateErr: errors.New("synthetic")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	sub := w.SubscribeReloadFailedEvents()
	defer w.UnsubscribeReloadFailedEvents(sub)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = w.NotifyForce(path)
	}()

	select {
	case ev := <-sub:
		if ev.Path != path {
			t.Errorf("event Path = %q; want %q", ev.Path, path)
		}
		if ev.Phase != "validate" {
			t.Errorf("event Phase = %q; want validate", ev.Phase)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive failure event within 2s")
	}
}

func TestSubscribeReloadFailedEvents_DeliversOnTightenViolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   &fakeEventlog{},
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{validateTightenErr: errors.New("loosens")},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	sub := w.SubscribeReloadFailedEvents()
	defer w.UnsubscribeReloadFailedEvents(sub)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = w.NotifyForce(path)
	}()

	select {
	case ev := <-sub:
		if ev.Path != path {
			t.Errorf("event Path = %q; want %q", ev.Path, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive tighten-violation failure event within 2s")
	}
}

func TestSubscribeReloadEvents_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
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
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	subs := []<-chan reload.DoctrineReloaded{
		w.SubscribeReloadEvents(),
		w.SubscribeReloadEvents(),
		w.SubscribeReloadEvents(),
	}
	defer func() {
		for _, s := range subs {
			w.UnsubscribeReloadEvents(s)
		}
	}()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = w.NotifyForce(path)
	}()
	var received int
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	for _, s := range subs {
		wg.Add(1)
		go func(c <-chan reload.DoctrineReloaded) {
			defer wg.Done()
			select {
			case <-c:
				mu.Lock()
				received++
				mu.Unlock()
			case <-time.After(2 * time.Second):
			}
		}(s)
	}
	wg.Wait()
	if received != 3 {
		t.Errorf("only %d/3 subscribers received the event", received)
	}
}

func TestSubscribeReloadEvents_SlowSubscriberDoesNotBlockOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
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
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	slow := w.SubscribeReloadEvents()
	fast := w.SubscribeReloadEvents()
	defer w.UnsubscribeReloadEvents(slow)
	defer w.UnsubscribeReloadEvents(fast)

	for i := 0; i < 20; i++ {
		_ = w.NotifyForce(path)
	}

	select {
	case <-fast:

	case <-time.After(2 * time.Second):
		t.Fatal("fast subscriber starved by slow subscriber")
	}
}

func TestNotifyForce_CancelsPendingDebounceTimer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	var parses atomic.Int32
	parseFn := func(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
		parses.Add(1)
		*target = v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
		return nil
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{parseFn: parseFn},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},

		DebounceWindow: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}

	w.TriggerDebounceForTest(context.Background(), path)
	if !w.PendingDebounceForTest(path) {
		t.Fatal("expected pending debounce timer after Trigger; got none")
	}

	if err := w.NotifyForce(path); err != nil {
		t.Fatal(err)
	}

	if w.PendingDebounceForTest(path) {
		t.Error("expected pending debounce cancelled by NotifyForce; still pending")
	}

	time.Sleep(1200 * time.Millisecond)
	if got := parses.Load(); got != 1 {
		t.Errorf("parses = %d; want 1 (debounce should NOT fire after NotifyForce cancel)", got)
	}
}

func TestNotifyForce_RaceWithDebounce(t *testing.T) {
	if testing.Short() {
		t.Skip("race-stress skipped in -short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	var parses atomic.Int32
	parseFn := func(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
		parses.Add(1)
		*target = v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
		return nil
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   &fakeEventlog{},
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{parseFn: parseFn},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},

		DebounceWindow: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}

	const N = 50
	for i := 0; i < N; i++ {
		w.TriggerDebounceForTest(context.Background(), path)

		time.Sleep(time.Microsecond)
		if err := w.NotifyForce(path); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	if got := parses.Load(); int(got) != N {
		t.Errorf("parses = %d; want %d (one per iteration; cancel must be deterministic)", got, N)
	}
}
