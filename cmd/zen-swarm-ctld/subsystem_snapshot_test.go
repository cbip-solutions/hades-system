package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type recordedLine struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type captureSink struct {
	lines []recordedLine
}

type sinkHandler struct {
	sink  *captureSink
	attrs []slog.Attr
}

func (h *sinkHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *sinkHandler) Handle(_ context.Context, r slog.Record) error {
	line := recordedLine{Level: r.Level, Message: r.Message, Attrs: map[string]any{}}
	for _, a := range h.attrs {
		line.Attrs[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		line.Attrs[a.Key] = a.Value.Any()
		return true
	})
	h.sink.lines = append(h.sink.lines, line)
	return nil
}

func (h *sinkHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	combined = append(combined, h.attrs...)
	combined = append(combined, attrs...)
	return &sinkHandler{sink: h.sink, attrs: combined}
}

func (h *sinkHandler) WithGroup(_ string) slog.Handler { return h }

func newCaptureLogger() (*slog.Logger, *captureSink) {
	sink := &captureSink{}
	return slog.New(&sinkHandler{sink: sink}), sink
}

type fakeSubsystemProberMain struct {
	rows []daemon.ProbeRow
	err  error
}

func (f *fakeSubsystemProberMain) SubsystemProbe(_ context.Context) ([]daemon.ProbeRow, error) {
	return f.rows, f.err
}

func newSnapshotTestServer(t *testing.T) *daemon.Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return daemon.New(st, daemon.Config{DisableAuditInfra: true})
}

func TestSnapshotSubsystemUnwiredLogsInfo(t *testing.T) {
	srv := newSnapshotTestServer(t)
	logger, sink := newCaptureLogger()
	snapshotSubsystem(context.Background(), srv, "knowledge", logger)
	if len(sink.lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(sink.lines))
	}
	got := sink.lines[0]
	if got.Level != slog.LevelInfo {
		t.Errorf("level = %v, want Info", got.Level)
	}
	if got.Message != "subsystem unwired" {
		t.Errorf("message = %q, want %q", got.Message, "subsystem unwired")
	}
	if got.Attrs["subsystem"] != "knowledge" {
		t.Errorf("subsystem attr = %v, want \"knowledge\"", got.Attrs["subsystem"])
	}
	for _, k := range []string{"ok", "warn", "fail", "total"} {
		v, ok := got.Attrs[k].(int64)
		if !ok {
			vInt, intOK := got.Attrs[k].(int)
			if !intOK {
				t.Errorf("attr %q missing or wrong type: %T = %v", k, got.Attrs[k], got.Attrs[k])
				continue
			}
			v = int64(vInt)
		}
		if v != 0 {
			t.Errorf("attr %q = %d, want 0 (unwired surface)", k, v)
		}
	}
}

func TestSnapshotSubsystemAllOkLogsInfo(t *testing.T) {
	srv := newSnapshotTestServer(t)
	srv.SetKnowledgeProber(&fakeSubsystemProberMain{rows: []daemon.ProbeRow{
		{Name: "knowledge.index.integrity", Status: "ok"},
		{Name: "knowledge.index.last_indexed", Status: "ok"},
	}})
	logger, sink := newCaptureLogger()
	snapshotSubsystem(context.Background(), srv, "knowledge", logger)
	if len(sink.lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(sink.lines))
	}
	got := sink.lines[0]
	if got.Level != slog.LevelInfo {
		t.Errorf("level = %v, want Info", got.Level)
	}
	if got.Message != "subsystem health snapshot" {
		t.Errorf("message = %q, want %q", got.Message, "subsystem health snapshot")
	}
}

func TestSnapshotSubsystemWarnPromotesLevel(t *testing.T) {
	srv := newSnapshotTestServer(t)
	srv.SetKnowledgeProber(&fakeSubsystemProberMain{rows: []daemon.ProbeRow{
		{Name: "knowledge.index.last_indexed", Status: "warn"},
		{Name: "knowledge.indexer.cpu_budget", Status: "ok"},
	}})
	logger, sink := newCaptureLogger()
	snapshotSubsystem(context.Background(), srv, "knowledge", logger)
	if len(sink.lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(sink.lines))
	}
	got := sink.lines[0]
	if got.Level != slog.LevelWarn {
		t.Errorf("level = %v, want Warn (1 warn probe)", got.Level)
	}
}

func TestSnapshotSubsystemFailPromotesLevelOverWarn(t *testing.T) {
	srv := newSnapshotTestServer(t)
	srv.SetKnowledgeProber(&fakeSubsystemProberMain{rows: []daemon.ProbeRow{
		{Name: "knowledge.index.integrity", Status: "fail"},
		{Name: "knowledge.index.last_indexed", Status: "warn"},
		{Name: "knowledge.indexer.cpu_budget", Status: "ok"},
	}})
	logger, sink := newCaptureLogger()
	snapshotSubsystem(context.Background(), srv, "knowledge", logger)
	if len(sink.lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(sink.lines))
	}
	got := sink.lines[0]
	if got.Level != slog.LevelError {
		t.Errorf("level = %v, want Error (1 fail probe)", got.Level)
	}
}

func TestSnapshotSubsystemPropagatesProberError(t *testing.T) {
	srv := newSnapshotTestServer(t)
	srv.SetKnowledgeProber(&fakeSubsystemProberMain{err: context.DeadlineExceeded})
	logger, sink := newCaptureLogger()
	snapshotSubsystem(context.Background(), srv, "knowledge", logger)
	if len(sink.lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(sink.lines))
	}
	got := sink.lines[0]
	if got.Level != slog.LevelWarn {
		t.Errorf("level = %v, want Warn (probe error)", got.Level)
	}
	if got.Message != "subsystem probe error" {
		t.Errorf("message = %q, want %q", got.Message, "subsystem probe error")
	}
}

func TestSnapshotSubsystemsCanonicalListMatchesSpec(t *testing.T) {
	want := []string{"knowledge", "scheduler", "inbox", "tmux"}
	if len(snapshotSubsystems) != len(want) {
		t.Fatalf("snapshotSubsystems = %v, want %v", snapshotSubsystems, want)
	}
	for i, n := range want {
		if snapshotSubsystems[i] != n {
			t.Errorf("snapshotSubsystems[%d] = %q, want %q", i, snapshotSubsystems[i], n)
		}
	}
}

func TestSnapshotIntervalIs5Minutes(t *testing.T) {
	if snapshotInterval != 300_000_000_000 {
		t.Errorf("snapshotInterval = %v, want 5 minutes (per spec §J-7)", snapshotInterval)
	}
}

func TestRunSubsystemSnapshotLoggerExitsOnCtxCancel(t *testing.T) {
	srv := newSnapshotTestServer(t)
	logger, _ := newCaptureLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		runSubsystemSnapshotLogger(ctx, srv, logger)
		close(done)
	}()
	select {
	case <-done:

	case <-time.After(30 * time.Second):
		t.Error("runSubsystemSnapshotLogger did not exit within 30s of ctx cancel")
	}
}
