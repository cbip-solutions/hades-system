package amendment_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type stubReviewer struct {
	context  string
	decision string
	alts     []string
	sota     []string
	err      error
}

func (s *stubReviewer) Reason(_ context.Context, _ amendment.Evidence) (amendment.L4Reasoning, error) {
	if s.err != nil {
		return amendment.L4Reasoning{}, s.err
	}
	return amendment.L4Reasoning{
		ContextNarrative:  s.context,
		DecisionStatement: s.decision,
		Alternatives:      s.alts,
		SOTARefs:          s.sota,
	}, nil
}

func TestTemplateDrafterRendersAllSections(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC))
	d := amendment.NewTemplateDrafter(amendment.DrafterConfig{
		Reviewer: &stubReviewer{
			context:  "Operator overrode tier-select 5 times in 24h.",
			decision: "Promote local Ollama to default Tier 2.",
			alts:     []string{"Keep current", "Add new tier"},
			sota:     []string{"https://arxiv.org/abs/2511.03690v2"},
		},
		ADRID: 20,
		Clock: clk,
	})
	body, err := d.Draft(context.Background(), amendment.Evidence{
		Doctrine:     "max-scope",
		TriggerClass: "operator_override",
		Pattern:      "max-scope|operator_override|tier_select|p1",
		WindowHours:  24,
		Count:        6,
		Threshold:    5,
		ProjectID:    "p1",
		Samples: []eventlog.Event{
			{Type: eventlog.EvtOperatorOverrideApplied, Timestamp: clk.Now()},
		},
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	must := []string{
		"# ADR 0020",
		"**Status**: Proposed",
		"**Date**: 2026-04-30",
		"**Decision-maker**: orchestrator (L4 reviewer) — pending operator confirmation",
		"## Context",
		"Operator overrode tier-select 5 times in 24h.",
		"## Decision",
		"Promote local Ollama to default Tier 2.",
		"## Consequences",
		"### Positive",
		"### Negative",
		"### Risks",
		"## Doctrine alignment",
		"max-scope",
		"## SOTA References",
		"https://arxiv.org/abs/2511.03690v2",
		"## Plan impact",
		"## Related ADRs",
		"## Evidence",
		"window_hours=24",
		"count=6",
		"threshold=5",
		"## Alternatives considered",
		"Keep current",
		"Add new tier",
	}
	for _, m := range must {
		if !strings.Contains(body.Markdown, m) {
			t.Errorf("rendered ADR missing %q\n---\n%s", m, body.Markdown)
		}
	}
	if !strings.Contains(body.Title, "operator_override") {
		t.Errorf("title should reference trigger class, got %q", body.Title)
	}
}

func TestTemplateDrafterDeterministicGivenInputs(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC))
	rev := &stubReviewer{context: "x", decision: "y"}
	d1 := amendment.NewTemplateDrafter(amendment.DrafterConfig{Reviewer: rev, ADRID: 21, Clock: clk})
	d2 := amendment.NewTemplateDrafter(amendment.DrafterConfig{Reviewer: rev, ADRID: 21, Clock: clk})
	ev := amendment.Evidence{Doctrine: "max-scope", TriggerClass: "escalation"}
	a, err1 := d1.Draft(context.Background(), ev)
	b, err2 := d2.Draft(context.Background(), ev)
	if err1 != nil || err2 != nil {
		t.Fatalf("draft errors: %v %v", err1, err2)
	}
	if a.Markdown != b.Markdown || a.Title != b.Title {
		t.Fatal("TemplateDrafter rendering should be deterministic given identical inputs")
	}
}

func TestTemplateDrafterEmptyAlternativesAndSOTA(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC))
	d := amendment.NewTemplateDrafter(amendment.DrafterConfig{
		Reviewer: &stubReviewer{context: "ctx", decision: "dec"},
		ADRID:    25,
		Clock:    clk,
	})
	body, err := d.Draft(context.Background(), amendment.Evidence{
		Doctrine: "default", TriggerClass: "cost_degradation",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if !strings.Contains(body.Markdown, "(none cited by L4 reviewer)") {
		t.Errorf("missing 'none cited' fallback for SOTA refs")
	}
	if !strings.Contains(body.Markdown, "(none enumerated by L4 reviewer)") {
		t.Errorf("missing 'none enumerated' fallback for Alternatives")
	}
	if !strings.Contains(body.Markdown, "# ADR 0025") {
		t.Errorf("ADRID padding wrong, got %q", body.Markdown[:50])
	}
}

func TestTemplateDrafterSamplesRender(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 30, 45, 0, time.UTC))
	d := amendment.NewTemplateDrafter(amendment.DrafterConfig{
		Reviewer: &stubReviewer{context: "x", decision: "y"},
		ADRID:    22,
		Clock:    clk,
	})
	body, err := d.Draft(context.Background(), amendment.Evidence{
		Samples: []eventlog.Event{
			{Type: eventlog.EvtOperatorOverrideApplied, Timestamp: clk.Now()},
			{Type: eventlog.EvtBudgetDegradationApplied, Timestamp: clk.Now().Add(time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if !strings.Contains(body.Markdown, "Sample events (2 shown") {
		t.Errorf("samples count missing: %s", body.Markdown)
	}
	if !strings.Contains(body.Markdown, "2026-04-30T14:30:45Z") {
		t.Errorf("RFC3339 timestamp missing")
	}
}

func TestTemplateDrafterReviewerError(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC))
	d := amendment.NewTemplateDrafter(amendment.DrafterConfig{
		Reviewer: &stubReviewer{err: errors.New("L4 timeout")},
		ADRID:    23,
		Clock:    clk,
	})
	_, err := d.Draft(context.Background(), amendment.Evidence{})
	if err == nil || !strings.Contains(err.Error(), "L4 reasoning") {
		t.Fatalf("want wrapped L4 reasoning error, got %v", err)
	}
}

func TestTemplateDrafterExecuteError(t *testing.T) {

	clk := clock.NewFake(time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC))
	d := amendment.NewTemplateDrafter(amendment.DrafterConfig{
		Reviewer: &stubReviewer{context: "x", decision: "y"},
		ADRID:    99,
		Clock:    clk,
	})
	bad := template.Must(template.New("adr").Parse("{{.Nonexistent.Method}}"))
	amendment.SetDrafterTemplate(d, bad)
	_, err := d.Draft(context.Background(), amendment.Evidence{})
	if err == nil || !strings.Contains(err.Error(), "render ADR") {
		t.Fatalf("want wrapped render error, got %v", err)
	}
}

func TestNewTemplateDrafterPanicsOnNilCollaborator(t *testing.T) {
	clk := clock.NewFake(time.Now())
	rev := &stubReviewer{}
	cases := []struct {
		name string
		cfg  amendment.DrafterConfig
	}{
		{"nil reviewer", amendment.DrafterConfig{Clock: clk, ADRID: 24}},
		{"nil clock", amendment.DrafterConfig{Reviewer: rev, ADRID: 24}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic")
				}
			}()
			amendment.NewTemplateDrafter(c.cfg)
		})
	}
}
