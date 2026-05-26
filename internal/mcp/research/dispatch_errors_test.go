package research

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type recordingAuditDetail struct {
	mu       sync.Mutex
	events   []string
	payloads []string
}

func (r *recordingAuditDetail) Emit(_ context.Context, t string, p []byte) error {
	r.mu.Lock()
	r.events = append(r.events, t)
	r.payloads = append(r.payloads, string(p))
	r.mu.Unlock()
	return nil
}

func (r *recordingAuditDetail) snapshot() (events []string, payloads []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events = append([]string(nil), r.events...)
	payloads = append([]string(nil), r.payloads...)
	return
}

func TestDispatchAllBackendsFailIncludesRootCauseInError(t *testing.T) {
	web := &fakeBackend{err: errors.New("ddg 503 service unavailable")}
	arx := &fakeBackend{err: errors.New("arxiv timeout")}
	rec := &recordingAuditDetail{}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:   web,
		Arxiv:       &arxivAdapter{arx},
		Cite:        passCite{},
		AuditClient: rec,
	})
	_, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err == nil {
		t.Fatal("expected error when all backends fail")
	}
	msg := err.Error()

	if !strings.Contains(msg, "ddg 503") {
		t.Errorf("error missing web_search root cause; got: %v", err)
	}
	if !strings.Contains(msg, "arxiv timeout") {
		t.Errorf("error missing arxiv root cause; got: %v", err)
	}

	events, payloads := rec.snapshot()
	if len(events) == 0 {
		t.Fatal("no audit events emitted on all-backends-fail")
	}
	foundAllFail := false
	for i, e := range events {
		if !strings.Contains(e, "all-fail") && !strings.Contains(e, "no-source") {
			continue
		}
		foundAllFail = true
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloads[i]), &payload); err != nil {
			t.Errorf("audit payload not valid JSON: %v\npayload=%s", err, payloads[i])
		}
	}
	if !foundAllFail {
		t.Errorf("expected an all-fail/no-source audit event; got events=%v", events)
	}
}

func TestDispatchAuditPayloadIsJSON(t *testing.T) {

	web := &fakeBackend{err: errors.New(`evil "injection", "extra":"value", "nested":{"a":1}`)}
	rec := &recordingAuditDetail{}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:   web,
		Cite:        passCite{},
		AuditClient: rec,
	})
	_, _ = d.Dispatch(context.Background(), DispatchQuery{Query: "q"})

	_, payloads := rec.snapshot()
	if len(payloads) == 0 {
		t.Fatal("no audit payloads recorded")
	}
	for i, p := range payloads {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(p), &raw); err != nil {
			t.Errorf("payload %d not valid JSON (C-13 regression): %v\npayload=%s", i, err, p)
		}

		var asMap map[string]any
		if err := json.Unmarshal([]byte(p), &asMap); err != nil {

			continue
		}
		// If it parses as a map, the evil "extra" key MUST NOT have
		// escaped to top level (C-13: json.Marshal escapes inner
		// quotes; pre-fix concat would let it through).
		if _, leaked := asMap["extra"]; leaked {
			t.Errorf("payload %d shows JSON injection — `extra` leaked to top level: %s", i, p)
		}
	}
}

func TestDispatchPerBackendErrorsCaptured(t *testing.T) {
	web := &fakeBackend{err: errors.New("web boom")}
	arx := &fakeBackend{err: errors.New("arx boom")}
	gh := &fakeBackend{err: errors.New("gh boom")}
	rec := &recordingAuditDetail{}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:   web,
		Arxiv:       &arxivAdapter{arx},
		GitHub:      &ghAdapter{gh},
		Cite:        passCite{},
		AuditClient: rec,
	})
	_, _ = d.Dispatch(context.Background(), DispatchQuery{Query: "q"})

	_, payloads := rec.snapshot()
	if len(payloads) == 0 {
		t.Fatal("no audit payloads")
	}
	combined := strings.Join(payloads, "\n")
	for _, want := range []string{"web boom", "arx boom", "gh boom"} {
		if !strings.Contains(combined, want) {
			t.Errorf("audit payloads missing %q (C-2 + C-19 regression); combined=%s",
				want, combined)
		}
	}
}
