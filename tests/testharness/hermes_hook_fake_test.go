package testharness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestHermesHookFake_RecordsCall(t *testing.T) {
	fake := testharness.NewHermesHookFake()

	call := testharness.HermesHookCall{
		Model:     "anthropic/claude-opus-4-7",
		ProjectID: "p-test",
		Doctrine:  "default",
		SessionID: "sess-1",
		Messages: []testharness.HookMessage{
			{Role: "user", Content: "what does Engine.Select do?"},
		},
		Tools: []string{"research.codegraph", "research.context"},
	}

	resp, err := fake.Invoke(context.Background(), call)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Envelope != nil || resp.ContextText != "" {
		t.Errorf("empty queue → both Envelope and ContextText empty, got %+v", resp)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("len(Calls()) = %d, want 1", len(calls))
	}
	if calls[0].ProjectID != "p-test" || calls[0].Doctrine != "default" {
		t.Errorf("call[0] fields mismatch: %+v", calls[0])
	}
}

func TestHermesHookFake_EnvelopePriority(t *testing.T) {
	fake := testharness.NewHermesHookFake()
	env := &citation.Envelope{
		ID:           citation.CitationID("c-abc123"),
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteContext,
		Lane:         citation.LaneGraph,
		AuditEventID: "evt-1",
		ProjectID:    "p-test",
	}
	fake.EnqueueEnvelope(env)

	resp, err := fake.Invoke(context.Background(), testharness.HermesHookCall{ProjectID: "p-test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Envelope == nil || string(resp.Envelope.ID) != "c-abc123" {
		t.Errorf("envelope mismatch: %+v", resp.Envelope)
	}
}

func TestHermesHookFake_ContextFallback(t *testing.T) {
	fake := testharness.NewHermesHookFake()
	fake.EnqueueContext("# Augmented context\n\n## Engine.Select\n...")

	resp, err := fake.Invoke(context.Background(), testharness.HermesHookCall{ProjectID: "p-test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.ContextText == "" || resp.Envelope != nil {
		t.Errorf("context fallback: ContextText=%q Envelope=%v", resp.ContextText, resp.Envelope)
	}
}

func TestHermesHookFake_ErrorInjection(t *testing.T) {
	fake := testharness.NewHermesHookFake()
	injected := errors.New("daemon: 503 service unavailable")
	fake.EnqueueError(injected)

	_, err := fake.Invoke(context.Background(), testharness.HermesHookCall{})
	if err == nil || err.Error() != injected.Error() {
		t.Errorf("err = %v, want %v", err, injected)
	}
}

func TestHermesHookFake_ResetClearsQueues(t *testing.T) {
	fake := testharness.NewHermesHookFake()
	fake.EnqueueContext("foo")
	fake.EnqueueError(errors.New("bar"))
	_, _ = fake.Invoke(context.Background(), testharness.HermesHookCall{})
	fake.Reset()

	if fake.CallCount() != 0 {
		t.Errorf("after Reset, CallCount = %d, want 0", fake.CallCount())
	}
	resp, err := fake.Invoke(context.Background(), testharness.HermesHookCall{})
	if err != nil || resp.Envelope != nil || resp.ContextText != "" {
		t.Errorf("after Reset, expected empty response, got %+v err=%v", resp, err)
	}
}
