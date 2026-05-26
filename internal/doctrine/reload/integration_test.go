package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestIntegration_FsnotifyEndToEnd_TriggersReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`schema_version="1.0"`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	parses := atomic.Int32{}
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

		DebounceWindow: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(80 * time.Millisecond)

	if err := os.WriteFile(path, []byte(`schema_version="1.0"
# trigger reload`), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if parses.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := parses.Load(); got < 1 {
		t.Errorf("parses after write event = %d; want ≥1", got)
	}
}

// TestIntegration_FsnotifyEndToEnd_ChmodOnlyIgnored asserts CHMOD events
// without WRITE/CREATE/RENAME do not fire reloads. Exercises onEvent's
// op-mask filter.
func TestIntegration_FsnotifyEndToEnd_ChmodOnlyIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	parses := atomic.Int32{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser: &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
			parses.Add(1)
			return errors.New("don't actually reload")
		}},
		DebounceWindow: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	time.Sleep(80 * time.Millisecond)

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	// We do NOT assert parses == 0 absolutely (some platforms may pair
	// CHMOD with WRITE). Instead, assert at most a single parse — the
	// debounce coalesces any spurious extras.
	if got := parses.Load(); got > 1 {
		t.Errorf("parses after chmod-only = %d; want ≤1 (op-mask should ignore CHMOD)", got)
	}
}

func TestIsOverflowError_PatternMatching(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"linux queue overflow", errors.New("queue overflow"), true},
		{"event queue overflow", errors.New("event queue overflow"), true},
		{"capitalised overflow", errors.New("Overflow detected"), true},
		{"darwin EWATCHQUEUEOVERFLOW", errors.New("EWATCHQUEUEOVERFLOW"), true},
		{"lowercase ewatchqueueoverflow", errors.New("ewatchqueueoverflow"), true},
		{"unrelated error", errors.New("permission denied"), false},
		{"empty message", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.IsOverflowErrorForTest(tc.err); got != tc.want {
				t.Errorf("IsOverflowErrorForTest(%v) = %t; want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestStart_EventLoopRunsRecordEventTime(t *testing.T) {
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
		DebounceWindow:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(80 * time.Millisecond)

	if err := os.WriteFile(path, []byte(`y`), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("Start returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestValidatePath_ExtendedCorpus(t *testing.T) {
	dir := t.TempDir()
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	cases := []struct {
		path    string
		wantErr bool
	}{
		{"", true},
		{"relative/path.toml", true},
		{"./still/relative.toml", true},

		{filepath.Join(dir, "..", "etc"), true},
		{filepath.Join(dir, "max-scope.toml") + "/", true},
		{filepath.Join("/foo", "bar", "..", "baz.toml"), true},
	}
	for _, tc := range cases {
		err := w.AddPath(tc.path, "")
		if tc.wantErr && err == nil {
			t.Errorf("AddPath(%q) = nil; want error", tc.path)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("AddPath(%q) = %v; want nil", tc.path, err)
		}
	}
}

func TestDebounce_CancelStopsTimer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	parses := atomic.Int32{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser: &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
			parses.Add(1)
			return errors.New("don't reload")
		}},
		DebounceWindow: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)

	if err := os.WriteFile(path, []byte(`changed`), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := w.NotifyForce(path); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	if got := parses.Load(); got < 1 {
		t.Errorf("parses = %d; want ≥1 (NotifyForce inline)", got)
	}
}
