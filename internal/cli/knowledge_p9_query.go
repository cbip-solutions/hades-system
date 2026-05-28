// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_query.go.
//
// `hades knowledge-p9 query <q>` — federated/pinned/chain-anchored search.
//
// Four orthogonal flags:
//
// --global query the global pin index across all projects
// --pinned-only restrict to pinned notes (Scope="pinned-only")
// --project restrict to one project (mutually exclusive with --global)
// --audit-chain filter to notes with a non-empty audit_chain_anchor
//
// --global and --project are enforced as mutually exclusive via cobra's
// MarkFlagsMutuallyExclusive (cobra ≥ v1.5); additional manual check in RunE
// ensures a consistent error message across cobra versions.
//
// Wire method: KnowledgeQueryP9 (client/knowledge_p9.go H-8).
// Response type: []client.KnowledgeResult (NoteID, ProjectID, Path, Snippet, Score, AuditChainAnchor).
package cli

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func knowledge9QueryCmd() *cobra.Command {
	var global, pinnedOnly, auditChain bool
	var project string
	var limit int

	cmd := &cobra.Command{
		Use:   "query <q>",
		Short: "Federated/pinned/chain-verified search against aggregator (Plan 9 H-2)",
		Args:  cobra.ExactArgs(1),
		Long: `Search the Plan 9 cross-project knowledge aggregator. The query is
forwarded to GET /v1/knowledge/query; the daemon runs FTS5 + sqlite-vec RRF
over all project vaults and the global pin index.

Scope flags (mutually exclusive):
  --global       Query only the global pin index across all projects.
  --pinned-only  Query only pinned notes in the per-project index.
  --project <id> Restrict to one project (cannot combine with --global).

  (No flag = default scope: federated across all project vaults.)

Chain flag:
  --audit-chain  Filter to notes whose audit_chain_anchor is present.

inv-hades-129: NEVER queries the web — Plan 14 territory.`,
		Example: " # Default federated query\n  hades knowledge-p9 query \"WFQ saturation\"\n\n # Global pin index only\n  hades knowledge-p9 query \"max scope\" --global\n\n # One project, chain-verified\n  hades knowledge-p9 query \"tessera\" --project internal-platform-x --audit-chain\n\n # Pinned-only across all projects, limit 5\n  hades knowledge-p9 query \"doctrine\" --pinned-only --limit 5",

		RunE: func(cmd *cobra.Command, args []string) error {

			if global && project != "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--global and --project are mutually exclusive"))
			}

			req := client.KnowledgeQueryReq{
				Q:          args[0],
				AuditChain: auditChain,
			}
			switch {
			case global:
				req.Scope = "global"
			case pinnedOnly:
				req.Scope = "pinned-only"
				req.PinnedOnly = true
			case project != "":
				req.Scope = "project"
				req.ProjectID = project
			}
			if limit > 0 {
				req.Limit = limit
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).KnowledgeQueryP9(ctx, req)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if len(items) == 0 {
				fmt.Fprintln(w, "(no results)")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NOTE_ID\tPROJECT\tSCORE\tCHAIN_OK\tSNIPPET")
			for _, r := range items {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.NoteID,
					r.ProjectID,
					strconv.FormatFloat(r.Score, 'f', 3, 64),
					strconv.FormatBool(r.AuditChainAnchor != ""),
					truncateKnowledge9(r.Snippet, 60),
				)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Query global pin index across all projects")
	cmd.Flags().BoolVar(&pinnedOnly, "pinned-only", false, "Only return pinned notes")
	cmd.Flags().StringVar(&project, "project", "", "Restrict to one project ID")
	cmd.Flags().BoolVar(&auditChain, "audit-chain", false, "Only return chain-verified notes")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max results (0 = server default 50)")

	cmd.MarkFlagsMutuallyExclusive("global", "project")

	return cmd
}

func truncateKnowledge9(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
