package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func failingParser() *fakeParser {
	return &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
		return errors.New("synthetic parse fail")
	}}
}

func TestStorm_NoSuppressionBelowThreshold(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	acc := &recordingActive{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: acc,
		Parser:         failingParser(),
		StormThreshold: 5,
		StormWindow:    60 * time.Second,
		StormCooldown:  60 * time.Second,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		w.RunReloadActionForTest(context.Background(), path)
		clk.Advance(1 * time.Second)
	}
	suppressed := 0
	for _, ev := range evlog.Snapshot() {
		if _, ok := ev.(reload.DoctrineRevertSuppressedCooldown); ok {
			suppressed++
		}
	}
	if suppressed != 0 {
		t.Errorf("got %d suppress events at threshold-1; want 0", suppressed)
	}
}

func TestStorm_SuppressionTriggeredAtThreshold(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser:         failingParser(),
		StormThreshold: 5,
		StormWindow:    60 * time.Second,
		StormCooldown:  60 * time.Second,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		w.RunReloadActionForTest(context.Background(), path)
		clk.Advance(1 * time.Second)
	}

	w.RunReloadActionForTest(context.Background(), path)

	failed, suppressed := 0, 0
	for _, ev := range evlog.Snapshot() {
		switch ev.(type) {
		case reload.DoctrineReloadFailed:
			failed++
		case reload.DoctrineRevertSuppressedCooldown:
			suppressed++
		}
	}
	if failed != 5 {
		t.Errorf("DoctrineReloadFailed count = %d; want 5 (one per real attempt)", failed)
	}
	if suppressed != 1 {
		t.Errorf("DoctrineRevertSuppressedCooldown count = %d; want 1 (the 6th attempt)", suppressed)
	}
}

func TestStorm_CooldownExpiresAfterWindow(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser:         failingParser(),
		StormThreshold: 5,
		StormWindow:    60 * time.Second,
		StormCooldown:  60 * time.Second,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		w.RunReloadActionForTest(context.Background(), path)
		clk.Advance(1 * time.Second)
	}

	w.RunReloadActionForTest(context.Background(), path)

	clk.Advance(70 * time.Second)

	w.RunReloadActionForTest(context.Background(), path)

	events := evlog.Snapshot()
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	last := events[len(events)-1]
	if _, ok := last.(reload.DoctrineReloadFailed); !ok {
		t.Errorf("last event = %T; want DoctrineReloadFailed (cooldown expired)", last)
	}
}

func TestStorm_SuccessfulReloadResetsCounter(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	failParse := true
	parser2 := &fakeParser{parseFn: func(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
		if failParse {
			return errors.New("fail")
		}
		*target = v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
		return nil
	}}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
		Parser:           parser2,
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
		StormThreshold:   5,
		StormWindow:      60 * time.Second,
		StormCooldown:    60 * time.Second,
		Clock:            clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 4; i++ {
		w.RunReloadActionForTest(context.Background(), path)
		clk.Advance(1 * time.Second)
	}

	failParse = false
	w.RunReloadActionForTest(context.Background(), path)

	failParse = true
	for i := 0; i < 4; i++ {
		w.RunReloadActionForTest(context.Background(), path)
		clk.Advance(1 * time.Second)
	}
	suppressed := 0
	for _, ev := range evlog.Snapshot() {
		if _, ok := ev.(reload.DoctrineRevertSuppressedCooldown); ok {
			suppressed++
		}
	}
	if suppressed != 0 {
		t.Errorf("DoctrineRevertSuppressedCooldown count = %d; want 0 (counter reset by success)", suppressed)
	}
}

func TestStorm_SlidingWindowDropsOldFailures(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser:         failingParser(),
		StormThreshold: 5,
		StormWindow:    60 * time.Second,
		StormCooldown:  60 * time.Second,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 4; i++ {
		w.RunReloadActionForTest(context.Background(), path)
	}

	clk.Advance(120 * time.Second)

	for i := 0; i < 4; i++ {
		w.RunReloadActionForTest(context.Background(), path)
	}
	suppressed := 0
	for _, ev := range evlog.Snapshot() {
		if _, ok := ev.(reload.DoctrineRevertSuppressedCooldown); ok {
			suppressed++
		}
	}
	if suppressed != 0 {
		t.Errorf("DoctrineRevertSuppressedCooldown count = %d; want 0 (old failures aged out)", suppressed)
	}
}
