// SPDX-License-Identifier: MIT
package inbox

import (
	"fmt"
	"sort"
	"time"
)

// CollapseRule is the cross-project collapse policy ( spec §3.3,
// Datadog Simple Alert pattern). When N distinct projects emit the
// same EventType within Window, the N per-project notifications are
// summarised as a single SimpleAlert.
//
// Invariant callers MUST construct rules via DefaultCollapseRule (or
// supply Window>0 + MinProjects>0 explicitly). A zero-value rule with
// MinProjects=0 would always trip on the first event of the configured
// EventType, producing alert-storms — exactly what the collapse mechanic
// is meant to prevent.
type CollapseRule struct {
	EventType   string
	Window      time.Duration
	MinProjects int
}

// DefaultCollapseRule returns the canonical rule per spec: 60s window,
// 3 distinct projects minimum, exact EventType match. Production
// emitters that don't override the threshold MUST use this constructor
// so the policy stays consistent across call sites.
func DefaultCollapseRule(eventType string) CollapseRule {
	return CollapseRule{
		EventType:   eventType,
		Window:      60 * time.Second,
		MinProjects: 3,
	}
}

// SimpleAlert is the collapsed cross-project summary. Carries the union
// of distinct ProjectIDs (sorted ascending), the maximum severity
// observed, and a human-readable Message — but NOT any per-project
// payload. Downstream renderers treat this
// as an opaque bundle: they MUST NOT inspect ProjectIDs to fan-out
// per-project payloads, because the whole point of the collapse is to
// suppress that fan-out and avoid leaking cross-project state into a
// single channel render.
type SimpleAlert struct {
	EventType  string
	ProjectIDs []string
	Severity   Severity
	Message    string
	WindowEnd  time.Time
}

// severityRank assigns numeric weight for max-severity selection during
// collapse: urgent=4 > action-needed=3 > info-immediate=2 > info-digest=1.
// Unknown severities fall back to 0 (the lowest), so a misconfigured
// emitter can never accidentally elevate the SimpleAlert severity.
//
// The rank is a private detail of the collapse algorithm; do not export
// it. Other call sites that need to compare severities should add their
// own ordering or use AllSeverities() (which is index-ordered max→min).
func severityRank(s Severity) int {
	switch s {
	case SeverityUrgent:
		return 4
	case SeverityActionNeeded:
		return 3
	case SeverityInfoImmediate:
		return 2
	case SeverityInfoDigest:
		return 1
	default:
		return 0
	}
}

func DetectCollapse(events []Notification, rule CollapseRule, now time.Time) (*SimpleAlert, bool) {
	windowStart := now.Add(-rule.Window)
	distinct := make(map[string]Severity)
	for _, e := range events {
		if e.EventType != rule.EventType {
			continue
		}
		if e.CreatedAt.Before(windowStart) || e.CreatedAt.After(now) {
			continue
		}

		if prev, ok := distinct[e.ProjectID]; ok {
			if severityRank(e.Severity) > severityRank(prev) {
				distinct[e.ProjectID] = e.Severity
			}
		} else {
			distinct[e.ProjectID] = e.Severity
		}
	}
	if len(distinct) < rule.MinProjects {
		return nil, false
	}

	pids := make([]string, 0, len(distinct))
	maxSev := Severity("")
	maxRank := -1
	for pid, sev := range distinct {
		pids = append(pids, pid)
		if r := severityRank(sev); r > maxRank {
			maxRank = r
			maxSev = sev
		}
	}
	sort.Strings(pids)

	return &SimpleAlert{
		EventType:  rule.EventType,
		ProjectIDs: pids,
		Severity:   maxSev,
		Message:    fmt.Sprintf("%d projects affected by %q (Datadog Simple Alert)", len(pids), rule.EventType),
		WindowEnd:  now,
	}, true
}
