package adr_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func fixedNow() func() time.Time {
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return ts }
}

func writeProposedADR(t *testing.T, dir, filename, id, title string) string {
	t.Helper()
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: proposed\n" +
		"date: \"2026-01-01\"\n" +
		"plan: \"plan-9\"\n" +
		"tags: []\n" +
		"---\n\n" +
		"## Context\n\nSome decision context.\n"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeProposedADR: %v", err)
	}
	return path
}

func writeAcceptedADR(t *testing.T, dir, filename, id, title string) string {
	t.Helper()
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: accepted\n" +
		"date: \"2026-01-01\"\n" +
		"plan: \"plan-9\"\n" +
		"tags: []\n" +
		"---\n\n" +
		"## Context\n\nSome decision context.\n"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeAcceptedADR: %v", err)
	}
	return path
}

func writeReservedADR(t *testing.T, dir, filename, id, title string) string {
	t.Helper()
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: Reserved\n" +
		"date: \"2026-01-01\"\n" +
		"plan: \"plan-9\"\n" +
		"tags: []\n" +
		"---\n\n" +
		"## Context\n\nSome decision context.\n"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeReservedADR: %v", err)
	}
	return path
}

func TestAcceptHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0001-test.md", "ADR-0001", "Test Decision")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "approved in review", sink, fixedNow())
	if err != nil {
		t.Fatalf("Accept: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after Accept: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusAccepted {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusAccepted)
	}

	if len(sink.Recorded) != 1 {
		t.Fatalf("events recorded: got %d, want 1", len(sink.Recorded))
	}
	ev := sink.Recorded[0]
	if ev.Type != adr.EvtADRAccepted {
		t.Errorf("event type: got %q, want %q", ev.Type, adr.EvtADRAccepted)
	}
	if ev.Payload.ADRID != "ADR-0001" {
		t.Errorf("event adr_id: got %q, want %q", ev.Payload.ADRID, "ADR-0001")
	}
	if ev.Payload.StatusFrom != adr.StatusProposed {
		t.Errorf("event status_from: got %q, want %q", ev.Payload.StatusFrom, adr.StatusProposed)
	}
	if ev.Payload.StatusTo != adr.StatusAccepted {
		t.Errorf("event status_to: got %q, want %q", ev.Payload.StatusTo, adr.StatusAccepted)
	}
	if ev.Payload.OperatorID != "op-alice" {
		t.Errorf("event operator_id: got %q, want %q", ev.Payload.OperatorID, "op-alice")
	}
	if ev.Payload.Reason != "approved in review" {
		t.Errorf("event reason: got %q, want %q", ev.Payload.Reason, "approved in review")
	}
}

func TestAcceptInvalidTransition(t *testing.T) {
	dir := t.TempDir()
	path := writeAcceptedADR(t, dir, "0002-test.md", "ADR-0002", "Already Accepted")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "should fail", sink, fixedNow())
	if err == nil {
		t.Fatal("Accept: expected error, got nil")
	}
	if !errors.Is(err, adr.ErrInvalidTransition) {
		t.Errorf("error: got %v, want wrapping ErrInvalidTransition", err)
	}

	if len(sink.Recorded) != 0 {
		t.Errorf("events recorded on failure: got %d, want 0", len(sink.Recorded))
	}
}

func TestAcceptEmptyReasonRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0003-test.md", "ADR-0003", "Empty Reason Test")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "", sink, fixedNow())
	if err == nil {
		t.Fatal("Accept: expected error for empty reason, got nil")
	}
	if !errors.Is(err, adr.ErrEmptyReason) {
		t.Errorf("error: got %v, want wrapping ErrEmptyReason", err)
	}

	if len(sink.Recorded) != 0 {
		t.Errorf("events recorded: got %d, want 0", len(sink.Recorded))
	}
}

func TestRejectHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0004-test.md", "ADR-0004", "Reject Me")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Reject(ctx, path, "op-bob", "superseded by better approach", sink, fixedNow())
	if err != nil {
		t.Fatalf("Reject: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after Reject: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusRejected {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusRejected)
	}

	if len(sink.Recorded) != 1 {
		t.Fatalf("events recorded: got %d, want 1", len(sink.Recorded))
	}
	ev := sink.Recorded[0]
	if ev.Type != adr.EvtADRRejected {
		t.Errorf("event type: got %q, want %q", ev.Type, adr.EvtADRRejected)
	}
	if ev.Payload.StatusFrom != adr.StatusProposed {
		t.Errorf("event status_from: got %q, want %q", ev.Payload.StatusFrom, adr.StatusProposed)
	}
	if ev.Payload.StatusTo != adr.StatusRejected {
		t.Errorf("event status_to: got %q, want %q", ev.Payload.StatusTo, adr.StatusRejected)
	}
}

func TestSupersedeHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeAcceptedADR(t, dir, "0005-test.md", "ADR-0005", "Old Decision")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Supersede(ctx, path, "ADR-0042", "op-carol", "replaced by ADR-0042", sink, fixedNow())
	if err != nil {
		t.Fatalf("Supersede: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after Supersede: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusSuperseded {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusSuperseded)
	}
	if a.Frontmatter.SupersededBy != "ADR-0042" {
		t.Errorf("superseded-by: got %q, want %q", a.Frontmatter.SupersededBy, "ADR-0042")
	}

	if len(sink.Recorded) != 1 {
		t.Fatalf("events recorded: got %d, want 1", len(sink.Recorded))
	}
	ev := sink.Recorded[0]
	if ev.Type != adr.EvtADRSuperseded {
		t.Errorf("event type: got %q, want %q", ev.Type, adr.EvtADRSuperseded)
	}
	if ev.Payload.StatusFrom != adr.StatusAccepted {
		t.Errorf("event status_from: got %q, want %q", ev.Payload.StatusFrom, adr.StatusAccepted)
	}
	if ev.Payload.StatusTo != adr.StatusSuperseded {
		t.Errorf("event status_to: got %q, want %q", ev.Payload.StatusTo, adr.StatusSuperseded)
	}
}

func TestSupersedeInvalidNewIDFormat(t *testing.T) {
	dir := t.TempDir()
	path := writeAcceptedADR(t, dir, "0006-test.md", "ADR-0006", "Accepted Decision")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	badIDs := []string{
		"",
		"ADR-42",
		"adr-0042",
		"ADR-00420",
		"ADR-004X",
		"ADR0042",
		"something-else",
	}
	for _, id := range badIDs {
		err := adr.Supersede(ctx, path, id, "op-carol", "test", sink, fixedNow())
		if err == nil {
			t.Errorf("Supersede(%q): expected error, got nil", id)
		}
	}

	if len(sink.Recorded) != 0 {
		t.Errorf("events recorded: got %d, want 0", len(sink.Recorded))
	}
}

func TestDeprecateHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeAcceptedADR(t, dir, "0007-test.md", "ADR-0007", "Old Feature Decision")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Deprecate(ctx, path, "op-dave", "feature removed in v2", sink, fixedNow())
	if err != nil {
		t.Fatalf("Deprecate: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after Deprecate: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusDeprecated {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusDeprecated)
	}

	if len(sink.Recorded) != 1 {
		t.Fatalf("events recorded: got %d, want 1", len(sink.Recorded))
	}
	ev := sink.Recorded[0]
	if ev.Type != adr.EvtADRDeprecated {
		t.Errorf("event type: got %q, want %q", ev.Type, adr.EvtADRDeprecated)
	}
	if ev.Payload.StatusFrom != adr.StatusAccepted {
		t.Errorf("event status_from: got %q, want %q", ev.Payload.StatusFrom, adr.StatusAccepted)
	}
	if ev.Payload.StatusTo != adr.StatusDeprecated {
		t.Errorf("event status_to: got %q, want %q", ev.Payload.StatusTo, adr.StatusDeprecated)
	}
}

func TestTransitionFromReservedRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeReservedADR(t, dir, "0008-reserved.md", "ADR-0008", "Reserved Slot")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	t.Run("Accept on Reserved", func(t *testing.T) {
		err := adr.Accept(ctx, path, "op-x", "should fail", sink, fixedNow())
		if !errors.Is(err, adr.ErrReservedStatusNotTransitionable) {
			t.Errorf("Accept on Reserved: got %v, want ErrReservedStatusNotTransitionable", err)
		}
	})

	t.Run("Reject on Reserved", func(t *testing.T) {
		err := adr.Reject(ctx, path, "op-x", "should fail", sink, fixedNow())
		if !errors.Is(err, adr.ErrReservedStatusNotTransitionable) {
			t.Errorf("Reject on Reserved: got %v, want ErrReservedStatusNotTransitionable", err)
		}
	})

	t.Run("Deprecate on Reserved", func(t *testing.T) {
		err := adr.Deprecate(ctx, path, "op-x", "should fail", sink, fixedNow())
		if !errors.Is(err, adr.ErrReservedStatusNotTransitionable) {
			t.Errorf("Deprecate on Reserved: got %v, want ErrReservedStatusNotTransitionable", err)
		}
	})

	t.Run("Supersede on Reserved", func(t *testing.T) {
		err := adr.Supersede(ctx, path, "ADR-0042", "op-x", "should fail", sink, fixedNow())
		if !errors.Is(err, adr.ErrReservedStatusNotTransitionable) {
			t.Errorf("Supersede on Reserved: got %v, want ErrReservedStatusNotTransitionable", err)
		}
	})

	if len(sink.Recorded) != 0 {
		t.Errorf("events recorded: got %d, want 0", len(sink.Recorded))
	}
}

func TestTransitionUpdatesDate(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0009-date.md", "ADR-0009", "Date Update Test")

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "approved", sink, fixedNow())
	if err != nil {
		t.Fatalf("Accept: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after Accept: %v", err)
	}

	wantDate := "2026-05-10"
	if a.Frontmatter.Date != wantDate {
		t.Errorf("date: got %q, want %q", a.Frontmatter.Date, wantDate)
	}
}

func TestIsValidTransitionTable(t *testing.T) {
	cases := []struct {
		from  adr.Status
		to    adr.Status
		valid bool
		desc  string
	}{

		{adr.StatusProposed, adr.StatusAccepted, true, "proposed→accepted"},
		{adr.StatusProposed, adr.StatusRejected, true, "proposed→rejected"},

		{adr.StatusProposed, adr.StatusSuperseded, false, "proposed→superseded"},
		{adr.StatusProposed, adr.StatusDeprecated, false, "proposed→deprecated"},

		{adr.StatusAccepted, adr.StatusSuperseded, true, "accepted→superseded"},
		{adr.StatusAccepted, adr.StatusDeprecated, true, "accepted→deprecated"},

		{adr.StatusAccepted, adr.StatusRejected, false, "accepted→rejected"},
		{adr.StatusAccepted, adr.StatusProposed, false, "accepted→proposed"},

		{adr.StatusRejected, adr.StatusAccepted, false, "rejected→accepted (terminal)"},
		{adr.StatusSuperseded, adr.StatusAccepted, false, "superseded→accepted (terminal)"},
		{adr.StatusDeprecated, adr.StatusAccepted, false, "deprecated→accepted (terminal)"},

		{adr.StatusProposed, adr.StatusProposed, false, "proposed→proposed (no-op)"},

		{adr.StatusProposed, adr.StatusReserved, false, "proposed→Reserved (gated target)"},
		{adr.StatusAccepted, adr.StatusReserved, false, "accepted→Reserved (gated target)"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := adr.IsValidTransition(tc.from, tc.to)
			if got != tc.valid {
				t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.valid)
			}
		})
	}
}

type errSink struct{ err error }

func (s errSink) Emit(_ adr.EventType, _ adr.EventPayload) error { return s.err }

func TestApplyTransitionContextCancelled(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0010-ctx.md", "ADR-0010", "Context Test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adr.Accept(ctx, path, "op-alice", "should fail on ctx", nil, fixedNow())
	if err == nil {
		t.Fatal("Accept with cancelled context: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error: got %v, want wrapping context.Canceled", err)
	}
}

func TestApplyTransitionNilSink(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0011-nilsink.md", "ADR-0011", "Nil Sink Test")

	ctx := context.Background()
	err := adr.Accept(ctx, path, "op-alice", "approved", nil, fixedNow())
	if err != nil {
		t.Fatalf("Accept with nil sink: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after nil-sink Accept: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusAccepted {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusAccepted)
	}
}

func TestApplyTransitionNilClock(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0014-nilclock.md", "ADR-0014", "Nil Clock Test")

	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "approved", nil, nil)
	if err != nil {
		t.Fatalf("Accept with nil clock: unexpected error: %v", err)
	}

	a, err := adr.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after nil-clock Accept: %v", err)
	}
	if a.Frontmatter.Status != adr.StatusAccepted {
		t.Errorf("status: got %q, want %q", a.Frontmatter.Status, adr.StatusAccepted)
	}

	if a.Frontmatter.Date == "" || a.Frontmatter.Date == "2026-01-01" {
		t.Errorf("date: got %q — expected a newly set date (not the fixture value)", a.Frontmatter.Date)
	}
}

func TestApplyTransitionEmitError(t *testing.T) {
	dir := t.TempDir()
	path := writeProposedADR(t, dir, "0012-emiterr.md", "ADR-0012", "Emit Error Test")

	ctx := context.Background()
	sentinel := errors.New("sink: downstream unavailable")
	sink := errSink{err: sentinel}

	err := adr.Accept(ctx, path, "op-alice", "approved", sink, fixedNow())
	if err == nil {
		t.Fatal("Accept with failing sink: expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error: got %v, want wrapping sentinel", err)
	}
}

func TestApplyTransitionWriteError(t *testing.T) {
	dir := t.TempDir()

	path := writeProposedADR(t, dir, "0013-writeerr.md", "ADR-0013", "Write Error Test")

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {

		_ = os.Chmod(dir, 0o755)
	})

	sink := &adr.RecordingEventSink{}
	ctx := context.Background()

	err := adr.Accept(ctx, path, "op-alice", "approved", sink, fixedNow())
	if err == nil {
		t.Fatal("Accept with read-only dir: expected error, got nil")
	}

	if len(sink.Recorded) != 0 {
		t.Errorf("events recorded: got %d, want 0", len(sink.Recorded))
	}
}

func TestApplyTransitionFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.md")

	ctx := context.Background()
	err := adr.Accept(ctx, path, "op-alice", "reason", nil, fixedNow())
	if err == nil {
		t.Fatal("Accept with missing file: expected error, got nil")
	}
	if !errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("error: got %v, want wrapping ErrFileNotFound", err)
	}
}
