// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// EventEmitter narrows the eventlog.Appender to the surface
// Proposer (and the rest of the amendment package) needs. The narrower
// signature (returning only error) lets tests substitute lightweight
// fakes that do not need to fabricate event_ids; production wiring
// adapts eventlog.Log.Append (which returns
// (int64, error)) to this interface by discarding the id.
//
// The full eventlog.Event Validation contract (SessionID + ProjectID
// non-empty, IsValid() type, etc.) is enforced upstream by the real
// adapter; tests do not depend on those checks.
type EventEmitter interface {
	Append(ctx context.Context, e eventlog.Event) error
}

type L4Drafter interface {
	Draft(ctx context.Context, ev Evidence) (ADRBody, error)
}

type RangeAllocator interface {
	NextAvailableID(ctx context.Context, decisionsDir string) (int, error)
}

type CooldownView interface {
	Suppressed(pattern string) bool
	Arm(pattern, doctrine string)
}

type Evidence struct {
	Doctrine     string
	TriggerClass string
	Pattern      string
	WindowHours  int
	Count        int
	Threshold    int
	Samples      []eventlog.Event
	ProjectID    string
}

type ADRBody struct {
	Title    string
	Markdown string
}

type ProposerConfig struct {
	DecisionsDir string
	Doctrine     string
	Emitter      EventEmitter
	Drafter      L4Drafter
	Allocator    RangeAllocator
	Cooldown     CooldownView
	Clock        clock.Clock
}

type AmendmentProposer struct {
	cfg     ProposerConfig
	mu      sync.Mutex
	windows map[string][]eventlog.Event
}

func NewProposer(cfg ProposerConfig) *AmendmentProposer {
	if cfg.Emitter == nil {
		panic("amendment: nil Emitter")
	}
	if cfg.Drafter == nil {
		panic("amendment: nil Drafter")
	}
	if cfg.Allocator == nil {
		panic("amendment: nil Allocator")
	}
	if cfg.Cooldown == nil {
		panic("amendment: nil Cooldown")
	}
	if cfg.Clock == nil {
		panic("amendment: nil Clock")
	}
	return &AmendmentProposer{cfg: cfg, windows: map[string][]eventlog.Event{}}
}

func thresholds(doctrine, class string) (count, hours int) {
	switch doctrine {
	case "max-scope":
		switch class {
		case "operator_override":
			return 5, 24
		case "cost_degradation":
			return 3, 24
		case "escalation":
			return 2, 24
		}
	case "default":
		switch class {
		case "operator_override":
			return 8, 72
		case "cost_degradation":
			return 5, 72
		case "escalation":
			return 3, 72
		}
	case "capa-firewall":
		switch class {
		case "operator_override":
			return 12, 168
		case "cost_degradation":
			return 8, 168
		case "escalation":
			return 5, 168
		}
	}
	return 0, 0
}

func classify(e eventlog.Event) (class, sub string, ok bool) {
	switch e.Type {
	case eventlog.EvtOperatorOverrideApplied:
		s, _ := e.Payload["override_class"].(string)
		return "operator_override", s, true
	case eventlog.EvtBudgetDegradationApplied:
		s, _ := e.Payload["severity"].(string)
		if s == "medium" || s == "hard" || s == "emergency" {
			return "cost_degradation", s, true
		}
	case eventlog.EvtEscalationDecision:
		s, _ := e.Payload["destination"].(string)
		if s == "L4" {
			return "escalation", s, true
		}
	}
	return "", "", false
}

func patternKey(doctrine, class, sub, projectID string) string {
	h := sha256.Sum256([]byte(doctrine + "|" + class + "|" + sub + "|" + projectID))
	return doctrine + "|" + class + "|" + sub + "|" + projectID + "#" + hex.EncodeToString(h[:4])
}

func shortPatternKey(doctrine, class, sub, projectID string) string {
	return doctrine + "|" + class + "|" + sub + "|" + projectID
}

func (p *AmendmentProposer) OnEvent(ctx context.Context, e eventlog.Event) error {
	class, sub, ok := classify(e)
	if !ok {
		return nil
	}
	projectID, _ := e.Payload["project_id"].(string)
	count, hours := thresholds(p.cfg.Doctrine, class)
	if count == 0 {
		return nil
	}
	shortKey := shortPatternKey(p.cfg.Doctrine, class, sub, projectID)
	window := time.Duration(hours) * time.Hour
	now := p.cfg.Clock.Now()
	cutoff := now.Add(-window)

	p.mu.Lock()
	cur := append(p.windows[shortKey], e)
	sort.Slice(cur, func(i, j int) bool { return cur[i].Timestamp.Before(cur[j].Timestamp) })
	pruned := cur[:0]
	for _, ev := range cur {
		if !ev.Timestamp.Before(cutoff) {
			pruned = append(pruned, ev)
		}
	}

	persisted := make([]eventlog.Event, len(pruned))
	copy(persisted, pruned)
	p.windows[shortKey] = persisted
	cnt := len(persisted)
	p.mu.Unlock()

	if cnt < count {
		return nil
	}

	pattern := shortKey

	resetWindow := func() {
		p.mu.Lock()
		delete(p.windows, shortKey)
		p.mu.Unlock()
	}
	if p.cfg.Cooldown.Suppressed(pattern) {
		resetWindow()
		return p.cfg.Emitter.Append(ctx, eventlog.Event{
			Type:      eventlog.EvtDoctrineAmendmentSuppressed,
			Timestamp: now,
			Payload: map[string]any{
				"reason":   "cooldown",
				"pattern":  pattern,
				"doctrine": p.cfg.Doctrine,
			},
		})
	}

	samples := make([]eventlog.Event, len(persisted))
	copy(samples, persisted)
	ev := Evidence{
		Doctrine:     p.cfg.Doctrine,
		TriggerClass: class,
		Pattern:      pattern,
		WindowHours:  hours,
		Count:        cnt,
		Threshold:    count,
		Samples:      samples,
		ProjectID:    projectID,
	}
	body, err := p.cfg.Drafter.Draft(ctx, ev)
	if err != nil {
		resetWindow()
		return p.cfg.Emitter.Append(ctx, eventlog.Event{
			Type:      eventlog.EvtDoctrineAmendmentSuppressed,
			Timestamp: now,
			Payload: map[string]any{
				"reason":   "drafter_failed",
				"pattern":  pattern,
				"doctrine": p.cfg.Doctrine,
				"error":    err.Error(),
			},
		})
	}
	id, err := p.cfg.Allocator.NextAvailableID(ctx, p.cfg.DecisionsDir)
	if err != nil {
		resetWindow()
		return p.cfg.Emitter.Append(ctx, eventlog.Event{
			Type:      eventlog.EvtDoctrineAmendmentSuppressed,
			Timestamp: now,
			Payload: map[string]any{
				"reason":   "range_exhausted",
				"pattern":  pattern,
				"doctrine": p.cfg.Doctrine,
				"error":    err.Error(),
			},
		})
	}

	proposedDir := filepath.Join(p.cfg.DecisionsDir, "proposed")
	if err := os.MkdirAll(proposedDir, 0o755); err != nil {
		return fmt.Errorf("amendment: mkdir proposed: %w", err)
	}
	slug := slugify(body.Title)
	fname := fmt.Sprintf("%04d-%s.md", id, slug)
	fpath := filepath.Join(proposedDir, fname)
	if err := os.WriteFile(fpath, []byte(body.Markdown), 0o644); err != nil {
		return fmt.Errorf("amendment: write ADR: %w", err)
	}

	p.mu.Lock()
	delete(p.windows, shortKey)
	p.mu.Unlock()

	return p.cfg.Emitter.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentProposed,
		Timestamp: now,
		Payload: map[string]any{
			"adr_id":        id,
			"adr_path":      fpath,
			"pattern":       pattern,
			"pattern_hash":  patternKey(p.cfg.Doctrine, class, sub, projectID),
			"doctrine":      p.cfg.Doctrine,
			"trigger_class": class,
			"window_hours":  hours,
			"count":         cnt,
			"threshold":     count,
			"project_id":    projectID,
			"diff_summary":  body.Title,
		},
	})
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "amendment"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
