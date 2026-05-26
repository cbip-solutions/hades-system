// SPDX-License-Identifier: MIT
package amendment

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/templates"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type L4Reasoning struct {
	ContextNarrative  string
	DecisionStatement string
	Alternatives      []string
	SOTARefs          []string
}

type L4Reviewer interface {
	Reason(ctx context.Context, ev Evidence) (L4Reasoning, error)
}

type DrafterConfig struct {
	Reviewer L4Reviewer
	ADRID    int
	Clock    clock.Clock
}

type TemplateDrafter struct {
	cfg  DrafterConfig
	tmpl *template.Template
}

func NewTemplateDrafter(cfg DrafterConfig) *TemplateDrafter {
	if cfg.Reviewer == nil {
		panic("amendment: nil Reviewer")
	}
	if cfg.Clock == nil {
		panic("amendment: nil Clock")
	}
	t := template.Must(template.New("adr").Parse(templates.ADRProposal))
	return &TemplateDrafter{cfg: cfg, tmpl: t}
}

type eventSummary struct {
	Type             eventlog.EventType
	TimestampRFC3339 string
}

func (d *TemplateDrafter) Draft(ctx context.Context, ev Evidence) (ADRBody, error) {
	r, err := d.cfg.Reviewer.Reason(ctx, ev)
	if err != nil {
		return ADRBody{}, fmt.Errorf("amendment: L4 reasoning: %w", err)
	}
	samples := make([]eventSummary, 0, len(ev.Samples))
	for _, e := range ev.Samples {
		samples = append(samples, eventSummary{
			Type:             e.Type,
			TimestampRFC3339: e.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	title := fmt.Sprintf("doctrine amendment for %s (%s)", ev.Doctrine, ev.TriggerClass)
	data := struct {
		ADRID             int
		Date              string
		Title             string
		Doctrine          string
		TriggerClass      string
		Pattern           string
		WindowHours       int
		Count             int
		Threshold         int
		ProjectID         string
		ContextNarrative  string
		DecisionStatement string
		Alternatives      []string
		SOTARefs          []string
		Samples           []eventSummary
	}{
		ADRID:             d.cfg.ADRID,
		Date:              d.cfg.Clock.Now().UTC().Format("2006-01-02"),
		Title:             title,
		Doctrine:          ev.Doctrine,
		TriggerClass:      ev.TriggerClass,
		Pattern:           ev.Pattern,
		WindowHours:       ev.WindowHours,
		Count:             ev.Count,
		Threshold:         ev.Threshold,
		ProjectID:         ev.ProjectID,
		ContextNarrative:  r.ContextNarrative,
		DecisionStatement: r.DecisionStatement,
		Alternatives:      r.Alternatives,
		SOTARefs:          r.SOTARefs,
		Samples:           samples,
	}
	var buf bytes.Buffer
	if err := d.tmpl.Execute(&buf, data); err != nil {
		return ADRBody{}, fmt.Errorf("amendment: render ADR: %w", err)
	}
	return ADRBody{Title: title, Markdown: buf.String()}, nil
}
