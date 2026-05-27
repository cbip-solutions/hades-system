// SPDX-License-Identifier: MIT
// Package cli — day_autonomy.go.
//
// Renders the `[plan-5 autonomy]` section of the morning brief per
// spec §6.3. The renderer is graceful on daemon-down: rather than
// failing the whole brief, it emits a single "daemon unreachable"
// line. This matches day.go pattern (the brief
// always renders, even partially).
//
// Pulls from 4 daemon endpoints:
//
// GET /v1/autonomy/show — effective mode + resolution chain
// GET /v1/orchestrator/state — last session id + state + transitions
// GET /v1/doctrine/propose-list — pending amendments
// GET /v1/safetynet/status + /v1/orchestrator/pool — substrate + pool
//
// internal/cli/brief.go is a notImplementedCmd stub; the morning brief
// surface is `zen day`, so N-7 extends day.go's
// inline RunE rather than adding a new top-level command.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newMorningBriefAutonomyRenderer(baseURL string) func(ctx context.Context) (string, error) {

	httpC, urlBase := plan5DoctorHTTPClient(baseURL)
	return func(ctx context.Context) (string, error) {
		var b strings.Builder
		b.WriteString("[plan-5 autonomy]\n")

		var show client.AutonomyShow
		if err := getJSONP5(ctx, httpC, urlBase+"/v1/autonomy/show", &show); err != nil {
			b.WriteString("   daemon unreachable - skipping autonomy section\n")
			return b.String(), nil
		}

		if show.DoctrineMode != "" && show.ResolvedFrom != "" {
			fmt.Fprintf(&b, "   Mode: %s (effective via %s %s)\n",
				show.EffectiveMode, show.ResolvedFrom, show.DoctrineMode)
		} else {
			fmt.Fprintf(&b, "   Mode: %s (resolved from %s)\n",
				show.EffectiveMode, show.ResolvedFrom)
		}

		var sess client.SessionInfo
		if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/state", &sess); err == nil {
			fmt.Fprintf(&b, "   Last session: %s\n", sess.SessionID)
			if sess.State != "" && sess.State != "IDLE" {
				ts := time.Unix(sess.LastTransitionAt, 0).Format("15:04")
				fmt.Fprintf(&b, "     - State: %s since %s\n", sess.State, ts)
				if sess.State == "WAITING_FOR_CONFIRMATION" && len(sess.RecentTransitions) > 0 {
					fmt.Fprintf(&b, "     - Action needed: zen confirmation show %s\n",
						sess.RecentTransitions[len(sess.RecentTransitions)-1].Reason)
				}
			}
		}

		var list client.DoctrineProposalList
		if err := getJSONP5(ctx, httpC, urlBase+"/v1/doctrine/propose-list", &list); err == nil {
			pending := 0
			var firstID, firstTitle string
			for _, p := range list.Proposals {
				if p.Status == "proposed" {
					if pending == 0 {
						firstID, firstTitle = p.ID, p.Title
					}
					pending++
				}
			}
			fmt.Fprintf(&b, "   Pending amendments: %d", pending)
			if pending > 0 {
				fmt.Fprintf(&b, " (%s - %s; review: zen doctrine propose-show %s)",
					firstID, firstTitle, firstID)
			}
			b.WriteString("\n")
		}

		var sn client.SafetynetStatus
		if err := getJSONP5(ctx, httpC, urlBase+"/v1/safetynet/status", &sn); err == nil {
			b.WriteString("   Substrate health (last 24h):\n")
			fmt.Fprintf(&b, "     - Test pass rate (substrate): %.1f%%\n",
				sn.SubstratePassRate7d*100)
			divLine := "Divergence audits: 0"
			if sn.LastDivergenceAt > 0 {
				divLine = "Divergence audits: 1"
				if sn.LastDivergenceClean {
					divLine += " (clean)"
				} else {
					divLine += " (DIRTY)"
				}
			}
			fmt.Fprintf(&b, "     - Drift incidents: %d / %s\n",
				sn.DriftIncidents24h, divLine)
		}

		var pool client.PoolStatus
		if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/pool", &pool); err == nil {
			fmt.Fprintf(&b,
				"   Worktree pool: floor=%d, elastic-current=%d, orphans cleaned=%d\n",
				pool.Floor, pool.ElasticInUse, pool.OrphansCleaned)
		}

		return b.String(), nil
	}
}
