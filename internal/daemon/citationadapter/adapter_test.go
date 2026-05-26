// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package citationadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon/citationadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeAuditEmitter struct {
	calls    []handlers.AuditEventIn
	returnEr error
}

func (f *fakeAuditEmitter) AuditEmit(ev handlers.AuditEventIn) error {
	f.calls = append(f.calls, ev)
	return f.returnEr
}

func TestAdapterForwardsRenderedEventThroughAuditEmit(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)

	ev := citation.CitationRenderedEvent{
		CitationID:    "c-abc123def",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/evt-9001",
		ProjectID:     "internal-platform-x",
		RenderedAt:    1715299200,
	}
	if err := a.EmitCitationRendered(context.Background(), ev); err != nil {
		t.Fatalf("EmitCitationRendered: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("emit calls: want 1 got %d", len(fake.calls))
	}
	got := fake.calls[0]
	if got.Type != "CitationRendered" {
		t.Errorf("type: want CitationRendered got %q", got.Type)
	}
	if got.ProjectID != "internal-platform-x" {
		t.Errorf("project_id: want internal-platform-x got %q", got.ProjectID)
	}
	if got.ID == "" {
		t.Errorf("ID should be stamped by adapter (internal emit path bypasses HTTP handler)")
	}
	if got.EmittedAt <= 0 {
		t.Errorf("EmittedAt should be > 0 (CHECK constraint at migration-055); got %d", got.EmittedAt)
	}

	if got.EmittedAt != 1715299200 {
		t.Errorf("EmittedAt: want 1715299200 (caller-supplied) got %d", got.EmittedAt)
	}

	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type: want map[string]any got %T", got.Payload)
	}
	if payload["citation_id"] != "c-abc123def" {
		t.Errorf("payload.citation_id: want c-abc123def got %v", payload["citation_id"])
	}
	if payload["platform"] != "markdown" {
		t.Errorf("payload.platform: want markdown got %v", payload["platform"])
	}
	if payload["audit_event_link"] != "zen://audit/evt-9001" {
		t.Errorf("payload.audit_event_link: want zen://audit/evt-9001 got %v",
			payload["audit_event_link"])
	}
}

func TestAdapterSurfacesAuditEmitError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("disk full")
	fake := &fakeAuditEmitter{returnEr: sentinel}
	a := citationadapter.New(fake)

	err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-x1",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/evt-1",
		ProjectID:     "p",
		RenderedAt:    1,
	})
	if err == nil {
		t.Fatal("want error from emitter, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

func TestAdapterRejectsMalformedCitationID(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)

	// Various malformed IDs; the adapter MUST not forward to AuditEmit
	// when the ID can't satisfy citation.CitationID.Validate.
	bad := []citation.CitationID{
		"",
		"xx",
		"c",
		"c-",
		"c-A",
		"c-AB",
		"c-a-b-c",
	}
	for _, id := range bad {
		t.Run(string(id), func(t *testing.T) {
			err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
				CitationID:    id,
				Platform:      "markdown",
				AuditEventURL: "zen://audit/evt-1",
				ProjectID:     "p",
				RenderedAt:    1,
			})
			if err == nil {
				t.Fatalf("want validation error for id=%q, got nil", id)
			}
		})
	}
	if len(fake.calls) != 0 {
		t.Errorf("emit should NOT be called for malformed ids; got %d calls",
			len(fake.calls))
	}
}

func TestNewPanicsOnNilEmitter(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("want panic on nil emitter, got none")
		}
	}()
	citationadapter.New(nil)
}

func TestAdapterPlatformPassThrough(t *testing.T) {
	t.Parallel()
	platforms := []string{"markdown", "ink", "telegram", "slack", "html_email", "voice", "web_html"}
	for _, p := range platforms {
		t.Run(p, func(t *testing.T) {
			fake := &fakeAuditEmitter{}
			a := citationadapter.New(fake)
			err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
				CitationID:    "c-test01",
				Platform:      p,
				AuditEventURL: "zen://audit/x",
				ProjectID:     "p",
				RenderedAt:    1,
			})
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			if len(fake.calls) != 1 {
				t.Fatalf("calls: want 1 got %d", len(fake.calls))
			}
			pl := fake.calls[0].Payload.(map[string]any)
			if pl["platform"] != p {
				t.Errorf("platform: want %s got %v", p, pl["platform"])
			}
		})
	}
}

func TestAdapterSatisfiesCitationInterface(t *testing.T) {
	t.Parallel()
	var _ citation.AuditEmitter = citationadapter.New(&fakeAuditEmitter{})
}

func TestAdapterStampsFreshIDPerEmit(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)

	for i := 0; i < 5; i++ {
		if err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
			CitationID:    "c-same01",
			Platform:      "markdown",
			AuditEventURL: "zen://audit/evt-x",
			ProjectID:     "p",
			RenderedAt:    1,
		}); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	seen := map[string]bool{}
	for _, ev := range fake.calls {
		if ev.ID == "" {
			t.Fatal("ID empty")
		}
		if seen[ev.ID] {
			t.Errorf("duplicate ID: %s", ev.ID)
		}
		seen[ev.ID] = true
	}
}

func TestAdapterFallsBackToClockWhenRenderedAtZero(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake).WithClock(func() time.Time {
		return time.Unix(1234567890, 0)
	})

	if err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-zero01",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/x",
		ProjectID:     "p",
		RenderedAt:    0,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls: want 1 got %d", len(fake.calls))
	}
	if fake.calls[0].EmittedAt != 1234567890 {
		t.Errorf("EmittedAt: want 1234567890 (clock fallback) got %d",
			fake.calls[0].EmittedAt)
	}
}

func TestAdapterDefaultClockUsesTimeNow(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)

	before := time.Now().Unix()
	if err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-nowdef",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/x",
		ProjectID:     "p",
		RenderedAt:    0,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	after := time.Now().Unix()
	got := fake.calls[0].EmittedAt
	if got < before || got > after {
		t.Errorf("EmittedAt: want in [%d, %d] got %d", before, after, got)
	}
}

func TestAdapterDefaultIDUsesUUID(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)

	if err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-uuid01",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/x",
		ProjectID:     "p",
		RenderedAt:    1,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	id := fake.calls[0].ID
	if len(id) != 36 {
		t.Errorf("uuid length: want 36 got %d (%q)", len(id), id)
	}
}

func TestAdapterWithIDGenerator(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake).WithIDGenerator(func() string {
		return "fixed-test-id-001"
	})
	if err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-gen01",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/x",
		ProjectID:     "p",
		RenderedAt:    1,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if fake.calls[0].ID != "fixed-test-id-001" {
		t.Errorf("ID: want fixed-test-id-001 got %q", fake.calls[0].ID)
	}
}

func TestAdapterStampsDoctrineIntoPayload(t *testing.T) {
	t.Parallel()
	doctrines := []string{"max-scope", "default", "capa-firewall", "future-doctrine"}
	for _, d := range doctrines {
		t.Run(d, func(t *testing.T) {
			fake := &fakeAuditEmitter{}
			a := citationadapter.New(fake)
			err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
				CitationID:    "c-doc001",
				Platform:      "markdown",
				AuditEventURL: "zen://audit/x",
				ProjectID:     "p",
				Doctrine:      d,
				RenderedAt:    1,
			})
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			if len(fake.calls) != 1 {
				t.Fatalf("calls: want 1 got %d", len(fake.calls))
			}
			pl, ok := fake.calls[0].Payload.(map[string]any)
			if !ok {
				t.Fatalf("payload type: want map[string]any got %T", fake.calls[0].Payload)
			}
			if pl["doctrine"] != d {
				t.Errorf("payload[doctrine]: want %q got %v", d, pl["doctrine"])
			}
		})
	}
}

func TestAdapterOmitsEmptyDoctrineFromPayload(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEmitter{}
	a := citationadapter.New(fake)
	err := a.EmitCitationRendered(context.Background(), citation.CitationRenderedEvent{
		CitationID:    "c-empty1",
		Platform:      "markdown",
		AuditEventURL: "zen://audit/x",
		ProjectID:     "p",
		Doctrine:      "",
		RenderedAt:    1,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	pl := fake.calls[0].Payload.(map[string]any)
	if got, present := pl["doctrine"]; present {
		t.Errorf("payload should NOT carry doctrine key for empty Doctrine; "+
			"extractor must fail-closed (got: %v)", got)
	}
}
