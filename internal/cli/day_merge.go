// SPDX-License-Identifier: MIT
// Package cli — day_merge.go.
//
// renderMergeMorningBrief writes the [release merge] section of the
// `hades day` morning brief per spec §6.3. Layout:
//
// [release merge]
// Cache size=N entries, hit_rate=P%
// Anomalies pending review: K
// ├─ [Severity] Type — Detail
// └─...
//
// Graceful degradation: a CacheStatus failure renders one
// "daemon unreachable: <err>" line and the section returns — same
// pattern as the bypass section in day.go so the brief continues to
// surface every other section's data even with the daemon down. An
// AnomalyList failure surfaces "unable to query: <err>" but keeps the
// preceding cache line so the operator still sees partial state.
//
// Wiring (C-2 fix, 2026-05-05): NewDayCmd in day.go calls this helper
// after the autonomy renderer + before the notifications block, using
// the production HTTP MergeClient (internal/client.MergeHTTPClient
// constructed from the same daemon URL the autonomy renderer consumes).
package cli

import (
	"context"
	"fmt"
	"io"
)

func renderMergeMorningBrief(ctx context.Context, client MergeClient, out io.Writer) {
	fmt.Fprintln(out, "[plan-6 merge]")

	cs, err := client.CacheStatus(ctx)
	if err != nil {
		fmt.Fprintf(out, "  daemon unreachable: %v\n", err)
		return
	}
	fmt.Fprintf(out, "  Cache: size=%d entries, hit_rate=%.1f%%\n", cs.Size, cs.HitRatePct)

	al, err := client.AnomalyList(ctx, "24h")
	if err != nil {
		fmt.Fprintf(out, "  Anomalies: unable to query: %v\n", err)
		return
	}
	if len(al.Anomalies) == 0 {
		fmt.Fprintln(out, "  Anomalies pending review: 0")
		return
	}
	fmt.Fprintf(out, "  Anomalies pending review: %d\n", len(al.Anomalies))

	for i, a := range al.Anomalies {
		glyph := "├─"
		if i == len(al.Anomalies)-1 {
			glyph = "└─"
		}
		fmt.Fprintf(out, "    %s [%s] %s — %s\n", glyph, a.Severity, a.Type, a.Detail)
	}
}
