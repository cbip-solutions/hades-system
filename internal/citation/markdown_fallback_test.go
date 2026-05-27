// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/citation/markdown_fallback_test.go — Task D-4.
//
// Render output tests + AuditEmitter integration (best-effort emission)
// + plain-text mode + golden-style footnote shape.
package citation_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type fakeAuditEmitter struct {
	mu     sync.Mutex
	events []citation.CitationRenderedEvent
	err    error
}

func (f *fakeAuditEmitter) EmitCitationRendered(ctx context.Context, ev citation.CitationRenderedEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return f.err
}

func (f *fakeAuditEmitter) snapshot() []citation.CitationRenderedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]citation.CitationRenderedEvent, len(f.events))
	copy(out, f.events)
	return out
}

func TestMarkdownFallbackRendersFootnote(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	r := citation.NewMarkdownFallback(emitter)

	env := newTestEnv()
	env.Payload = "MergeEngine.Score()"
	env.AuditEventID = "evt-0001"

	got, err := r.Render(env, citation.SessionContext{
		Doctrine: "max-scope", Platform: "markdown", Now: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "[^c-test0001]") {
		t.Errorf("missing footnote inline marker: %s", got)
	}
	if !strings.Contains(got, "[^c-test0001]: ") {
		t.Errorf("missing footnote definition: %s", got)
	}
	if !strings.Contains(got, "zen://audit/evt-0001") {
		t.Errorf("missing zen://audit URL: %s", got)
	}
	if !strings.Contains(got, "MergeEngine.Score()") {
		t.Errorf("missing payload: %s", got)
	}
	if !strings.Contains(got, "doctrine=max-scope") {
		t.Errorf("missing doctrine metadata: %s", got)
	}
	if !strings.Contains(got, "lane=semantic") {
		t.Errorf("missing lane metadata: %s", got)
	}
	if !strings.Contains(got, "conf=0.50") {
		t.Errorf("missing confidence metadata: %s", got)
	}
}

func TestMarkdownFallbackEmitsAuditEvent(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	r := citation.NewMarkdownFallback(emitter)

	env := newTestEnv()
	_, err := r.Render(env, citation.SessionContext{
		Platform: "markdown", Now: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	events := emitter.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(events))
	}
	ev := events[0]
	if ev.CitationID != env.ID {
		t.Errorf("CitationID: want %s got %s", env.ID, ev.CitationID)
	}
	if ev.Platform != "markdown" {
		t.Errorf("Platform: want markdown got %s", ev.Platform)
	}
	if ev.AuditEventURL != "zen://audit/evt-test" {
		t.Errorf("AuditEventURL: want zen://audit/evt-test got %s", ev.AuditEventURL)
	}
	if ev.ProjectID != env.ProjectID {
		t.Errorf("ProjectID: want %s got %s", env.ProjectID, ev.ProjectID)
	}
	if ev.RenderedAt != time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("RenderedAt: want %d got %d", time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC).Unix(), ev.RenderedAt)
	}
}

func TestMarkdownFallbackEmitterFailureDoesNotBlock(t *testing.T) {

	emitter := &fakeAuditEmitter{err: context.DeadlineExceeded}
	r := citation.NewMarkdownFallback(emitter)

	env := newTestEnv()
	got, err := r.Render(env, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render must succeed despite emitter failure: %v", err)
	}
	if got == "" {
		t.Error("Render produced empty output")
	}
}

func TestMarkdownFallbackNilEmitterAllowed(t *testing.T) {

	r := citation.NewMarkdownFallback(nil)
	env := newTestEnv()
	_, err := r.Render(env, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render with nil emitter: %v", err)
	}
}

func TestMarkdownFallbackPlainTextMode(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	env := newTestEnv()
	env.Payload = "MergeEngine.Score()"
	env.AuditEventID = "evt-0001"

	got, err := r.Render(env, citation.SessionContext{Platform: "plaintext", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render plaintext: %v", err)
	}
	if !strings.HasPrefix(got, "[citation:") {
		t.Errorf("plaintext output missing prefix: %s", got)
	}
	if strings.Contains(got, "[^") {
		t.Errorf("plaintext mode emitted footnote syntax: %s", got)
	}
	if !strings.Contains(got, "MergeEngine.Score()") {
		t.Errorf("plaintext missing payload: %s", got)
	}
	if !strings.Contains(got, "zen://audit/evt-0001") {
		t.Errorf("plaintext missing zen://audit URL: %s", got)
	}
}

func TestMarkdownFallbackInvalidEnvelopeRejects(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	env := &citation.Envelope{}
	_, err := r.Render(env, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err == nil {
		t.Error("Render accepted invalid envelope")
	}
}

func TestMarkdownFallbackNilEnvelopeRejects(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	_, err := r.Render(nil, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err == nil {
		t.Error("Render accepted nil envelope")
	}
}

func TestMarkdownFallbackPreservesSpecialChars(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	env := newTestEnv()
	env.Payload = "test ` *bold* [link](url) **end"
	got, err := r.Render(env, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(got, env.Payload) {
		t.Errorf("payload not preserved verbatim: %s (want substring %q)", got, env.Payload)
	}
}

func TestMarkdownFallbackExpirationRendered(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	env := newTestEnv()
	env.Expiration = time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	got, err := r.Render(env, citation.SessionContext{Platform: "markdown", Doctrine: "max-scope", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "expires=2026-05-11T12:00:00Z") {
		t.Errorf("missing expiration: %s", got)
	}
}

func TestMarkdownFallbackZeroExpirationNotRendered(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	env := newTestEnv()

	got, err := r.Render(env, citation.SessionContext{Platform: "markdown", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "expires=") {
		t.Errorf("zero-Expiration should not render: %s", got)
	}
}

func TestMarkdownFallbackPlatformNameInEvent(t *testing.T) {

	emitter := &fakeAuditEmitter{}
	r := citation.NewMarkdownFallback(emitter)
	env := newTestEnv()
	_, err := r.Render(env, citation.SessionContext{Platform: "ink", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	events := emitter.snapshot()
	if len(events) != 1 || events[0].Platform != "ink" {
		t.Errorf("emitter Platform should be 'ink' (session-active), got events=%v", events)
	}
}

func TestMarkdownFallbackEmptyPlatformFallsToMarkdown(t *testing.T) {

	emitter := &fakeAuditEmitter{}
	r := citation.NewMarkdownFallback(emitter)
	env := newTestEnv()
	_, err := r.Render(env, citation.SessionContext{Platform: "", Now: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	events := emitter.snapshot()
	if len(events) != 1 || events[0].Platform != "markdown" {
		t.Errorf("empty Platform should fall back to 'markdown' in emitter, got events=%v", events)
	}
}

func TestMarkdownFallbackStampsDoctrineInEvent(t *testing.T) {
	cases := []string{"max-scope", "default", "capa-firewall"}
	for _, doctrine := range cases {
		t.Run(doctrine, func(t *testing.T) {
			emitter := &fakeAuditEmitter{}
			r := citation.NewMarkdownFallback(emitter)
			env := newTestEnv()
			_, err := r.Render(env, citation.SessionContext{
				Doctrine: doctrine,
				Platform: "markdown",
				Now:      time.Now(),
			})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			events := emitter.snapshot()
			if len(events) != 1 {
				t.Fatalf("events: want 1 got %d", len(events))
			}
			if events[0].Doctrine != doctrine {
				t.Errorf("event.Doctrine: want %q got %q (m-3 regression: "+
					"renderer dropped sess.Doctrine before emit)",
					doctrine, events[0].Doctrine)
			}
		})
	}
}
