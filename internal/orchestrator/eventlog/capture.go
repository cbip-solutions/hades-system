// SPDX-License-Identifier: MIT
// Package eventlog — capture.go.
//
// Capture serializes a session's event log to a JSONL stream with a
// signed-metadata header and a closing footer. LLM bodies are redacted
// by default (privacy-by-default per project instructions hard rule + spec §7.4);
// callers explicitly opt-out via CaptureOptions.Redact = &false.
//
// The on-disk envelope is frozen at version=1 (Task O-1):
//
// line 1 {"kind":"header", version, session_id, captured_at, redacted, metadata_sha256}
// line 2-N {"kind":"event", event_id, timestamp, event_type, payload}
// line N+1 {"kind":"footer", event_count, first_event_id, last_event_id}
//
// Replay (Task O-2) recomputes metadata_sha256 from header/footer fields
// and rejects fixtures with mismatching signatures (ErrCorruptedFixture).
//
// Invariant flow:
// - invariant (corruption bounded): replay tolerates ≤5 corrupted
// events before transitioning HARD_PAUSED. Capture writes the footer
// LAST so a mid-stream daemon kill produces a fixture that fails sha
// verification fast (rather than silently truncated).
//
// Decoupling this Capture is a pure value-shape function over the
// Record type and a Querier. It is independent of (and
// complementary to) internal/daemon/orchestrator_plan5_service.go's
// Capture method, which writes a per-row JSON envelope used by the
// daemon's HTTP API. Task O-1 ships the canonical fixture
// format used by the replay tier.
package eventlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

// CaptureOptions controls the Capture function's behavior.
//
// Redact uses *bool so the zero-value (nil) means "default-on"; this
// avoids the booltrap where struct-literal-without-redact-set silently
// disables redaction. Callers MUST explicitly set Redact = &false to
// opt out.
type CaptureOptions struct {
	SessionID string

	CapturedAt time.Time

	Output io.Writer
	// Redact defaults to true when nil. Set to a pointer-to-false for
	// opt-out — operators MUST explicitly choose to disable.
	Redact *bool

	Since int64
}

type CaptureResult struct {
	EventCount     int
	FirstEventID   int64
	LastEventID    int64
	MetadataSha256 string
}

type Querier interface {
	QueryRaw(ctx context.Context, sessionID string, since int64) ([]Record, error)
}

var ErrEmptyCapture = errors.New("eventlog: capture produced zero events")

func Capture(ctx context.Context, q Querier, opts CaptureOptions) (CaptureResult, error) {
	if opts.SessionID == "" {
		return CaptureResult{}, errors.New("eventlog.Capture: SessionID required")
	}
	if opts.Output == nil {
		return CaptureResult{}, errors.New("eventlog.Capture: Output required")
	}
	if opts.CapturedAt.IsZero() {
		return CaptureResult{}, errors.New("eventlog.Capture: CapturedAt required")
	}
	if q == nil {
		return CaptureResult{}, errors.New("eventlog.Capture: Querier required")
	}
	redactOn := true
	if opts.Redact != nil {
		redactOn = *opts.Redact
	}

	rows, err := q.QueryRaw(ctx, opts.SessionID, opts.Since)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("eventlog.Capture: query: %w", err)
	}
	if len(rows) == 0 {
		return CaptureResult{}, ErrEmptyCapture
	}

	first := rows[0].EventID
	last := rows[len(rows)-1].EventID

	capturedAtStr := opts.CapturedAt.UTC().Format(time.RFC3339)
	meta := map[string]any{
		"captured_at":    capturedAtStr,
		"event_count":    len(rows),
		"first_event_id": first,
		"last_event_id":  last,
		"redacted":       redactOn,
		"session_id":     opts.SessionID,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("eventlog.Capture: canonical metadata marshal: %w", err)
	}
	sum := sha256.Sum256(metaBytes)
	sig := hex.EncodeToString(sum[:])

	header := map[string]any{
		"kind":            "header",
		"version":         1,
		"session_id":      opts.SessionID,
		"captured_at":     capturedAtStr,
		"redacted":        redactOn,
		"metadata_sha256": sig,
	}
	if err := writeJSONLine(opts.Output, header); err != nil {
		return CaptureResult{}, fmt.Errorf("eventlog.Capture: write header: %w", err)
	}

	for _, r := range rows {
		payload := r.Payload
		if redactOn && needsBodyRedaction(r.EventType) {
			payload = redactLLMBody(payload)
		}

		var payloadJSON json.RawMessage
		if len(payload) == 0 {
			payloadJSON = json.RawMessage("null")
		} else {
			payloadJSON = json.RawMessage(payload)
		}

		tsStr := time.Unix(0, r.Timestamp).UTC().Format(time.RFC3339Nano)
		evt := map[string]any{
			"kind":       "event",
			"event_id":   r.EventID,
			"timestamp":  tsStr,
			"event_type": r.EventType.String(),
			"payload":    payloadJSON,
		}
		if err := writeJSONLine(opts.Output, evt); err != nil {
			return CaptureResult{}, fmt.Errorf("eventlog.Capture: write event %d: %w", r.EventID, err)
		}
	}

	footer := map[string]any{
		"kind":           "footer",
		"event_count":    len(rows),
		"first_event_id": first,
		"last_event_id":  last,
	}
	if err := writeJSONLine(opts.Output, footer); err != nil {
		return CaptureResult{}, fmt.Errorf("eventlog.Capture: write footer: %w", err)
	}

	return CaptureResult{
		EventCount:     len(rows),
		FirstEventID:   first,
		LastEventID:    last,
		MetadataSha256: sig,
	}, nil
}

func needsBodyRedaction(t EventType) bool {
	switch t {
	case EvtWorkerCheckpoint,
		EvtTacticalAggregation,
		EvtStrategicAggregation,
		EvtArchitecturalReview,
		EvtResearchCompleted:
		return true
	default:
		return false
	}
}

func redactLLMBody(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {

		return payload
	}
	mutated := false
	for _, key := range llmBodyFieldKeys {
		v, ok := raw[key]
		if !ok {
			continue
		}

		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			continue
		}
		marker := "<redacted-len-" + strconv.Itoa(len(s)) + ">"
		newVal, err := encodeNoEscape(marker)
		if err != nil {
			continue
		}
		raw[key] = newVal
		mutated = true
	}
	if !mutated {
		return payload
	}
	out, err := encodeNoEscape(raw)
	if err != nil {
		return payload
	}
	return out
}

func encodeNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}

var llmBodyFieldKeys = []string{
	"summary",
	"body",
	"text",
	"content",
	"rationale",
}

func writeJSONLine(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}
