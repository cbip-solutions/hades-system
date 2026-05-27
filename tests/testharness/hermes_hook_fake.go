// SPDX-License-Identifier: MIT
// hermes_hook_fake.go — mock for the Hermes plugin pre_llm_call hook
// callback contract.
//
// Per 2026-05-11 spike artifact §4-5 (consolidating Q10=B Spike-2),
// the empirical Hermes v0.13.0 pre_llm_call contract is:
// - hook callback receives {messages, model, session_id, **kwargs}
// (kwargs include project_id, conversation_id, doctrine, tools).
// - hook callback returns None OR {"context": "<text>"} per
// hermes_cli/plugins.py:1097-1107.
// - returning {"context": "<text>"} causes Hermes to prepend <text>
// to the next LLM call as a system-context prefix (the plugin does
// NOT mutate messages directly — Hermes handles assembly).
//
// HermesHookFake records every invocation and lets tests inject the
// {"context":...} payload returned to Hermes. Used by:
// - tests/integration/zenswarm_transport_test.go (verifies hook called
// with correct payload)
// - tests/replay/augment_capture_test.go (records hook calls for
// replay-determinism check)
// - downstream chaos / adversarial tests that need a stable Hermes
// pre_llm_call stand-in.
package testharness

import (
	"context"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type HermesHookCall struct {
	Model        string
	Messages     []HookMessage
	Tools        []string
	ProjectID    string
	Doctrine     string
	SessionID    string
	AuditEventID string
}

type HookMessage struct {
	Role    string
	Content string
}

type HermesHookFake struct {
	mu sync.Mutex

	calls         []HermesHookCall
	envelopeQueue []*citation.Envelope
	contextQueue  []string
	errorQueue    []error
}

func NewHermesHookFake() *HermesHookFake {
	return &HermesHookFake{}
}

func (f *HermesHookFake) EnqueueEnvelope(env *citation.Envelope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envelopeQueue = append(f.envelopeQueue, env)
}

func (f *HermesHookFake) EnqueueContext(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contextQueue = append(f.contextQueue, text)
}

func (f *HermesHookFake) EnqueueError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorQueue = append(f.errorQueue, err)
}

func (f *HermesHookFake) Calls() []HermesHookCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]HermesHookCall(nil), f.calls...)
}

func (f *HermesHookFake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type HermesHookResponse struct {
	Envelope *citation.Envelope

	ContextText string
}

func (f *HermesHookFake) Invoke(ctx context.Context, call HermesHookCall) (HermesHookResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if len(f.errorQueue) > 0 {
		err := f.errorQueue[0]
		f.errorQueue = f.errorQueue[1:]
		return HermesHookResponse{}, err
	}
	if len(f.envelopeQueue) > 0 {
		env := f.envelopeQueue[0]
		f.envelopeQueue = f.envelopeQueue[1:]
		return HermesHookResponse{Envelope: env}, nil
	}
	if len(f.contextQueue) > 0 {
		txt := f.contextQueue[0]
		f.contextQueue = f.contextQueue[1:]
		return HermesHookResponse{ContextText: txt}, nil
	}

	return HermesHookResponse{}, nil
}

func (f *HermesHookFake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
	f.envelopeQueue = nil
	f.contextQueue = nil
	f.errorQueue = nil
}
