package reload_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
)

type failingEventlog struct {
	mu      sync.Mutex
	err     error
	emitted []any
}

func (f *failingEventlog) Emit(_ context.Context, evt any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emitted = append(f.emitted, evt)
	return f.err
}

func (f *failingEventlog) Snapshot() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, len(f.emitted))
	copy(out, f.emitted)
	return out
}

func captureSlogOutput(t *testing.T, fn func()) string {
	t.Helper()
	prev := slog.Default()
	defer slog.SetDefault(prev)
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	fn()
	return buf.String()
}

func TestEmit_LogsWarnOnEventlogError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &failingEventlog{err: errors.New("sqlite: database is locked")}

	var output string
	output = captureSlogOutput(t, func() {
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
		// Drive the success path; emit returns the synthetic error so
		// the helper should log a warning.
		w.RunReloadActionForTest(context.Background(), path)
	})

	if !strings.Contains(output, "doctrine reload: eventlog emit failed") {
		t.Errorf("slog output missing emit-failure warning; got:\n%s", output)
	}
	if !strings.Contains(output, "sqlite: database is locked") {
		t.Errorf("slog output missing underlying error; got:\n%s", output)
	}

	if got := len(evlog.Snapshot()); got == 0 {
		t.Errorf("evlog.Snapshot empty; expected emit attempt count >0")
	}
}

func TestEmit_LogsWarnOnReloadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &failingEventlog{err: errors.New("eventlog: connection refused")}

	output := captureSlogOutput(t, func() {
		w, err := reload.New(reload.WatcherOpts{
			EventlogClient: evlog,
			ActiveAccessor: &recordingActive{},
			Parser:         &fakeParser{},
			Validator:      &fakeValidator{validateErr: errors.New("schema range")},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer w.Close()
		if err := w.AddPath(path, ""); err != nil {
			t.Fatal(err)
		}
		w.RunReloadActionForTest(context.Background(), path)
	})

	if !strings.Contains(output, "doctrine reload: eventlog emit failed") {
		t.Errorf("slog output missing emit-failure warning on reload-failure; got:\n%s", output)
	}
	if !strings.Contains(output, "eventlog: connection refused") {
		t.Errorf("slog output missing underlying error on reload-failure; got:\n%s", output)
	}
}
