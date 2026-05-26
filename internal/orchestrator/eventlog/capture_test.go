package eventlog_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeQuerier struct {
	rows []eventlog.Record
	err  error
}

func (f *fakeQuerier) QueryRaw(ctx context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]eventlog.Record, 0, len(f.rows))
	for _, r := range f.rows {
		if r.SessionID != "" && r.SessionID != sessionID {
			continue
		}
		if r.EventID > since {
			out = append(out, r)
		}
	}
	return out, nil
}

func newFakeQuerier(t *testing.T, sessionID string) *fakeQuerier {
	t.Helper()
	t0 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	mkPayload := func(p eventlog.PayloadEncoder) []byte {
		t.Helper()
		b, err := p.Payload()
		if err != nil {
			t.Fatalf("payload: %v", err)
		}
		return b
	}
	return &fakeQuerier{rows: []eventlog.Record{
		{
			EventID:   42,
			SessionID: sessionID,
			ProjectID: "proj-1",
			EventType: eventlog.EvtOrchestratorStarted,
			Payload:   mkPayload(eventlog.OrchestratorStarted{SessionID: sessionID, ProjectID: "proj-1", AutonomyMode: "semi"}),
			Timestamp: t0.UnixNano(),
		},
		{
			EventID:   43,
			SessionID: sessionID,
			ProjectID: "proj-1",
			EventType: eventlog.EvtWorkerDispatched,
			Payload:   mkPayload(eventlog.WorkerDispatched{WorkerID: "W1", TaskID: "T-1", Tier: "t1_bypass"}),
			Timestamp: t0.Add(time.Second).UnixNano(),
		},
		{
			EventID:   44,
			SessionID: sessionID,
			ProjectID: "proj-1",
			EventType: eventlog.EvtWorkerCheckpoint,
			Payload: mkPayload(eventlog.WorkerCheckpoint{
				WorkerID:      "W1",
				CheckpointSHA: "deadbeef",
				Summary:       "LLM said: do not commit secrets AKIA-NEVER-LAND-HERE",
			}),
			Timestamp: t0.Add(2 * time.Second).UnixNano(),
		},
	}}
}

func TestCapture_DefaultRedacted_Envelope(t *testing.T) {
	q := newFakeQuerier(t, "sess-abc")
	var buf bytes.Buffer
	res, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-abc",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if res.EventCount != 3 {
		t.Fatalf("EventCount = %d, want 3", res.EventCount)
	}
	if res.FirstEventID != 42 || res.LastEventID != 44 {
		t.Fatalf("FirstEventID=%d LastEventID=%d, want 42/44", res.FirstEventID, res.LastEventID)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("len(lines)=%d, want 5; got: %q", len(lines), buf.String())
	}

	var header struct {
		Kind           string `json:"kind"`
		Version        int    `json:"version"`
		SessionID      string `json:"session_id"`
		Redacted       bool   `json:"redacted"`
		MetadataSha256 string `json:"metadata_sha256"`
		CapturedAt     string `json:"captured_at"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Kind != "header" || header.Version != 1 {
		t.Fatalf("header kind/version: %+v", header)
	}
	if header.SessionID != "sess-abc" {
		t.Fatalf("session_id mismatch: %s", header.SessionID)
	}
	if !header.Redacted {
		t.Fatalf("Redacted should default to true (privacy-by-default)")
	}
	if len(header.MetadataSha256) != 64 {
		t.Fatalf("sha256 length = %d, want 64 hex chars", len(header.MetadataSha256))
	}
	if header.CapturedAt != "2026-04-30T12:00:00Z" {
		t.Fatalf("captured_at format mismatch: %q", header.CapturedAt)
	}

	var footer struct {
		Kind         string `json:"kind"`
		EventCount   int    `json:"event_count"`
		FirstEventID int64  `json:"first_event_id"`
		LastEventID  int64  `json:"last_event_id"`
	}
	if err := json.Unmarshal([]byte(lines[4]), &footer); err != nil {
		t.Fatalf("unmarshal footer: %v", err)
	}
	if footer.Kind != "footer" || footer.EventCount != 3 || footer.FirstEventID != 42 || footer.LastEventID != 44 {
		t.Fatalf("footer mismatch: %+v", footer)
	}
}

func TestCapture_DefaultRedacted_BodyScrubbed(t *testing.T) {
	q := newFakeQuerier(t, "sess-abc")
	var buf bytes.Buffer
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-abc",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if strings.Contains(buf.String(), "AKIA-NEVER-LAND-HERE") {
		t.Fatalf("redaction failed: secret leaked into capture stream")
	}
	if !strings.Contains(buf.String(), "<redacted-len-") {
		t.Fatalf("redacted marker missing: redaction not applied; output=%q", buf.String())
	}
}

func TestCapture_NoRedact_BodyPreserved(t *testing.T) {
	q := newFakeQuerier(t, "sess-abc")
	var buf bytes.Buffer
	noRedact := false
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-abc",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
		Redact:     &noRedact,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !strings.Contains(buf.String(), "AKIA-NEVER-LAND-HERE") {
		t.Fatalf("opt-out failed: body should be preserved when Redact=false; output=%q", buf.String())
	}

	first := strings.SplitN(buf.String(), "\n", 2)[0]
	if !strings.Contains(first, `"redacted":false`) {
		t.Fatalf("header should have redacted=false; got: %q", first)
	}
}

func TestCapture_Signature_RoundTrip(t *testing.T) {
	q := newFakeQuerier(t, "sess-abc")
	var buf bytes.Buffer
	res, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-abc",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	canonical, err := json.Marshal(map[string]any{
		"captured_at":    "2026-04-30T12:00:00Z",
		"event_count":    3,
		"first_event_id": int64(42),
		"last_event_id":  int64(44),
		"redacted":       true,
		"session_id":     "sess-abc",
	})
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	sum := sha256.Sum256(canonical)
	want := hex.EncodeToString(sum[:])
	if res.MetadataSha256 != want {
		t.Fatalf("sha mismatch: got %s, want %s", res.MetadataSha256, want)
	}
}

func TestCapture_Validation_RejectsZeroOptions(t *testing.T) {
	q := newFakeQuerier(t, "sess-abc")

	cases := []struct {
		name string
		opts eventlog.CaptureOptions
		want string
	}{
		{
			name: "no session",
			opts: eventlog.CaptureOptions{Output: &bytes.Buffer{}, CapturedAt: time.Now()},
			want: "SessionID required",
		},
		{
			name: "no output",
			opts: eventlog.CaptureOptions{SessionID: "x", CapturedAt: time.Now()},
			want: "Output required",
		},
		{
			name: "no captured_at",
			opts: eventlog.CaptureOptions{SessionID: "x", Output: &bytes.Buffer{}},
			want: "CapturedAt required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := eventlog.Capture(context.Background(), q, c.opts)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestCapture_NilQuerier(t *testing.T) {
	_, err := eventlog.Capture(context.Background(), nil, eventlog.CaptureOptions{
		SessionID: "x", Output: &bytes.Buffer{}, CapturedAt: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "Querier required") {
		t.Fatalf("expected Querier required error, got %v", err)
	}
}

func TestCapture_EmptyResult(t *testing.T) {
	q := &fakeQuerier{}
	_, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID: "sess-empty", Output: &bytes.Buffer{}, CapturedAt: time.Now(),
	})
	if !errors.Is(err, eventlog.ErrEmptyCapture) {
		t.Fatalf("expected ErrEmptyCapture, got %v", err)
	}
}

func TestCapture_QuerierError(t *testing.T) {
	want := errors.New("synthetic-db-error")
	q := &fakeQuerier{err: want}
	_, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID: "x", Output: &bytes.Buffer{}, CapturedAt: time.Now(),
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped synthetic error, got %v", err)
	}
}

func TestCapture_NonLLMEventType_Unredacted(t *testing.T) {

	t0 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	disp, err := eventlog.WorkerDispatched{WorkerID: "W42", TaskID: "T-42", Tier: "t1_bypass"}.Payload()
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	q := &fakeQuerier{rows: []eventlog.Record{{
		EventID: 1, SessionID: "s", ProjectID: "p",
		EventType: eventlog.EvtWorkerDispatched, Payload: disp, Timestamp: t0.UnixNano(),
	}}}
	var buf bytes.Buffer
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID: "s", CapturedAt: t0, Output: &buf,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if !strings.Contains(buf.String(), `"worker_id":"W42"`) {
		t.Fatalf("non-LLM event payload was unexpectedly altered; output=%q", buf.String())
	}
}

func TestRedactLLMBody_PreservesUnknownFields(t *testing.T) {

	t0 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	cp, err := eventlog.WorkerCheckpoint{
		WorkerID:      "W7",
		CheckpointSHA: "abc123",
		Summary:       "long body content here",
	}.Payload()
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	q := &fakeQuerier{rows: []eventlog.Record{{
		EventID: 1, SessionID: "s", ProjectID: "p",
		EventType: eventlog.EvtWorkerCheckpoint, Payload: cp, Timestamp: t0.UnixNano(),
	}}}
	var buf bytes.Buffer
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID: "s", CapturedAt: t0, Output: &buf,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"worker_id":"W7"`) {
		t.Fatalf("non-redacted field worker_id missing: %q", out)
	}
	if !strings.Contains(out, `"checkpoint_sha":"abc123"`) {
		t.Fatalf("non-redacted field checkpoint_sha missing: %q", out)
	}
	if strings.Contains(out, "long body content") {
		t.Fatalf("LLM body content leaked: %q", out)
	}
	if !strings.Contains(out, "<redacted-len-22>") {
		t.Fatalf("expected length-preserving marker for body of length 22; got: %q", out)
	}
}
