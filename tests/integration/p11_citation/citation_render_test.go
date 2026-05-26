//go:build integration

package p11_citation_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type fakeAuditEmitter struct {
	mu     sync.Mutex
	events []citation.CitationRenderedEvent
}

func (f *fakeAuditEmitter) EmitCitationRendered(_ context.Context, ev citation.CitationRenderedEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeAuditEmitter) snapshot() []citation.CitationRenderedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]citation.CitationRenderedEvent, len(f.events))
	copy(out, f.events)
	return out
}

func newRenderEnv() *citation.Envelope {
	return &citation.Envelope{
		ID:           "c-render01",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-render-test",
		Confidence:   0.85,
		RRFScore:     0.0123,
		RRFRank:      0,
		ProjectID:    "p-test",
		Payload:      "Engine.Run()",
	}
}

func TestCitationRender_MarkdownFallbackOutput(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	r := citation.NewMarkdownFallback(emitter)

	env := newRenderEnv()
	got, err := r.Render(env, citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "[^c-render01]") {
		t.Errorf("missing footnote inline marker in:\n%s", got)
	}
	if !strings.Contains(got, "Engine.Run()") {
		t.Errorf("missing payload in:\n%s", got)
	}
	if !strings.Contains(got, "evt-render-test") {
		t.Errorf("missing audit_event_id in:\n%s", got)
	}

	if len(emitter.snapshot()) != 1 {
		t.Errorf("emitter saw %d events, want 1", len(emitter.snapshot()))
	}
}

func TestCitationRender_RoundtripJSONThenMarkdown(t *testing.T) {
	original := newRenderEnv()

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var recovered citation.Envelope
	if err := json.Unmarshal(b, &recovered); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	r := citation.NewMarkdownFallback(nil)
	ctx := citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}
	out1, err := r.Render(original, ctx)
	if err != nil {
		t.Fatalf("Render original: %v", err)
	}
	out2, err := r.Render(&recovered, ctx)
	if err != nil {
		t.Fatalf("Render recovered: %v", err)
	}
	if out1 != out2 {
		t.Errorf("render-after-roundtrip diverged:\nout1=%q\nout2=%q", out1, out2)
	}
}

func TestCitationRender_MultipleEnvelopes(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	ctx := citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	envs := []*citation.Envelope{
		{
			ID:           "c-multi01",
			Type:         citation.CitationTypeFileSlice,
			Source:       citation.SourceAggregatorFTS,
			Lane:         citation.LaneLexical,
			AuditEventID: "evt-1",
			ProjectID:    "p-multi",
			Payload:      "first slice",
			Confidence:   0.7,
		},
		{
			ID:           "c-multi02",
			Type:         citation.CitationTypeKGEdge,
			Source:       citation.SourceCaronteContext,
			Lane:         citation.LaneGraph,
			AuditEventID: "evt-2",
			ProjectID:    "p-multi",
			Payload:      "second slice",
			Confidence:   0.8,
		},
	}

	seen := map[string]bool{}
	for _, env := range envs {
		out, err := r.Render(env, ctx)
		if err != nil {
			t.Fatalf("Render %s: %v", env.ID, err)
		}
		if !strings.Contains(out, string(env.ID)) {
			t.Errorf("envelope %s: output missing ID", env.ID)
		}
		seen[string(env.ID)] = true
	}
	if len(seen) != 2 {
		t.Errorf("rendered %d envelopes, want 2", len(seen))
	}
}
