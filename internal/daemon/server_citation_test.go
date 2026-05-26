// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package daemon

import (
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon/citationadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

func TestSetCitationRegistryAndAccessor(t *testing.T) {
	t.Parallel()
	s := &Server{}
	if got := s.Citations(); got != nil {
		t.Fatalf("default Citations should be nil, got %v", got)
	}
	reg := citation.NewRegistry()
	s.SetCitationRegistry(reg)
	if got := s.Citations(); got != reg {
		t.Errorf("Citations() == reg: want true, got %v", got)
	}

	s.SetCitationRegistry(nil)
	if got := s.Citations(); got != nil {
		t.Errorf("after SetCitationRegistry(nil): want nil, got %v", got)
	}
}

func TestSetCitationRegistryGoroutineSafe(t *testing.T) {
	t.Parallel()
	s := &Server{}
	reg := citation.NewRegistry()

	var wg sync.WaitGroup
	const N = 32
	wg.Add(N * 2)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			s.SetCitationRegistry(reg)
		}()
		go func() {
			defer wg.Done()
			_ = s.Citations()
		}()
	}
	wg.Wait()
	if s.Citations() != reg {
		t.Errorf("final Citations(): want reg, got %v", s.Citations())
	}
}

type captureEmitter struct {
	mu     sync.Mutex
	events []handlers.AuditEventIn
}

func (c *captureEmitter) AuditEmit(ev handlers.AuditEventIn) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func (c *captureEmitter) Events() []handlers.AuditEventIn {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]handlers.AuditEventIn, len(c.events))
	copy(cp, c.events)
	return cp
}

func TestCitationRegistryWireUpEndToEnd(t *testing.T) {
	t.Parallel()
	emit := &captureEmitter{}
	bridge := citationadapter.New(emit)

	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(bridge))

	s := &Server{}
	s.SetCitationRegistry(reg)
	if s.Citations() != reg {
		t.Fatalf("Citations() != reg")
	}

	env := &citation.Envelope{
		ID:           "c-abc123",
		Type:         citation.CitationTypeFileSlice,
		Source:       citation.SourceManualOverride,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-7777",
		Confidence:   0.95,
		RRFScore:     0.123,
		RRFRank:      1,
		ProjectID:    "internal-platform-x",
		Payload:      "the substantive content for the citation",
	}
	sess := citation.SessionContext{
		Doctrine: "max-scope",
		Platform: "markdown",
	}
	out, err := reg.Dispatch(env, sess)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out == "" {
		t.Error("rendered output is empty")
	}

	evs := emit.Events()
	if len(evs) != 1 {
		t.Fatalf("emitter calls: want 1 got %d", len(evs))
	}
	ev := evs[0]
	if ev.Type != "CitationRendered" {
		t.Errorf("type: want CitationRendered got %q", ev.Type)
	}
	if ev.ProjectID != "internal-platform-x" {
		t.Errorf("project_id: want internal-platform-x got %q", ev.ProjectID)
	}
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type: want map[string]any got %T", ev.Payload)
	}
	if payload["citation_id"] != "c-abc123" {
		t.Errorf("payload.citation_id: want c-abc123 got %v", payload["citation_id"])
	}
	if payload["platform"] != "markdown" {
		t.Errorf("payload.platform: want markdown got %v", payload["platform"])
	}
	if payload["audit_event_link"] != "zen://audit/evt-7777" {
		t.Errorf("payload.audit_event_link: want zen://audit/evt-7777 got %v",
			payload["audit_event_link"])
	}
}
