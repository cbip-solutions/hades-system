// SPDX-License-Identifier: MIT
package hra

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func WindowOf(records []eventlog.Record, since, until time.Time) []eventlog.Record {
	if since.After(until) {
		panic(fmt.Sprintf("hra.WindowOf: since (%v) > until (%v)", since, until))
	}
	sinceNs := since.UnixNano()
	untilNs := until.UnixNano()
	out := make([]eventlog.Record, 0, len(records))
	for _, r := range records {
		if r.Timestamp >= sinceNs && r.Timestamp < untilNs {
			out = append(out, r)
		}
	}
	return out
}

type Aggregator interface {
	Aggregate(records []eventlog.Record, since, until time.Time) Finding
}

// tacticalAggregator implements plurality semantics on
// EvtWorkerCheckpoint records. ack > fix → "ack"; fix > ack →
// "needs_fix"; ack == fix → "needs_fix" + Disagreement (pessimistic
// tie-break — escalating a tie costs operator attention but
// suppressing one risks shipping a bug). Sub-majority winners (winner
// strictly less than ⌈(N+1)/2⌉) flag Disagreement so
// confirmation policy can reflect the soft consensus.
//
// Collects DISTINCT fix-proposal payload strings for downstream
// FMV voting: duplicate proposals from concurrent reviewer
// assemblies de-dup via the props set. Order in FixProposals is map
// iteration order (non-deterministic by Go spec); callers MUST NOT
// depend on ordering.
type tacticalAggregator struct{}

func (tacticalAggregator) Aggregate(records []eventlog.Record, since, until time.Time) Finding {
	records = WindowOf(records, since, until)
	ack, fix := 0, 0
	props := map[string]struct{}{}
	for _, r := range records {
		v := payloadString(r, "verdict")
		switch v {
		case "ack":
			ack++
		case "needs_fix":
			fix++
			if p := payloadString(r, "proposal"); p != "" {
				props[p] = struct{}{}
			}
		}
	}
	f := Finding{
		Layer:      LayerTactical,
		EventCount: len(records),
		Split:      [2]int{ack, fix},
	}
	for p := range props {
		f.FixProposals = append(f.FixProposals, p)
	}
	classify(&f, ack, fix)
	return f
}

type strategicAggregator struct{}

func (strategicAggregator) Aggregate(records []eventlog.Record, since, until time.Time) Finding {
	records = WindowOf(records, since, until)
	ack, fix := 0, 0
	for _, r := range records {
		switch payloadString(r, "verdict") {
		case "ack":
			ack++
		case "needs_fix":
			fix++
		}
	}
	f := Finding{
		Layer:      LayerStrategic,
		EventCount: len(records),
		Split:      [2]int{ack, fix},
	}
	classify(&f, ack, fix)
	return f
}

type architecturalAggregator struct{}

func (architecturalAggregator) Aggregate(records []eventlog.Record, since, until time.Time) Finding {
	records = WindowOf(records, since, until)
	ack, fix := 0, 0
	var summaries []string
	for _, r := range records {
		switch payloadString(r, "verdict") {
		case "ack":
			ack++
		case "needs_fix":
			fix++
			if s := payloadString(r, "summary"); s != "" {
				summaries = append(summaries, s)
			}
		}
	}
	f := Finding{
		Layer:      LayerArchitectural,
		EventCount: len(records),
		Split:      [2]int{ack, fix},
	}
	switch {
	case fix > 0:
		f.Verdict = "needs_fix"
		f.NeedsFix = true
		if ack > 0 {
			f.Disagreement = true
		}
	case ack > 0:
		f.Verdict = "ack"
	default:
		f.Verdict = "ack"
	}
	if len(summaries) > 0 {
		f.Summary = summaries[0]
		for _, s := range summaries[1:] {
			f.Summary += "; " + s
		}
	}
	return f
}

func classify(f *Finding, ack, fix int) {
	switch {
	case ack == 0 && fix == 0:
		f.Verdict = "ack"
	case ack > fix:
		f.Verdict = "ack"
	case fix > ack:
		f.Verdict = "needs_fix"
		f.NeedsFix = true
	default:
		f.Verdict = "needs_fix"
		f.NeedsFix = true
		f.Disagreement = true
	}
}

func payloadString(rec eventlog.Record, key string) string {
	if len(rec.Payload) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Payload, &m); err != nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
