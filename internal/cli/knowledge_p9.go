// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9.go.
//
// `zen knowledge` is the operator surface for the cross-project
// knowledge aggregator (spec §6.1 Q6 C). Five leaves cover the full lifecycle:
//
// query <q> federated/pinned/chain-verified search
// promote <note-id> operator-gated pin to global index
// unpromote <note-id> reverse a prior promote
// ls list notes (per-project or pinned-only)
// rebuild re-embed + re-index one project
//
// DISTINCT's `zen knowledge` surface (knowledge.go: query/reindex/
// stats backed by FTS5 daemon routes). This group hits the
// aggregator endpoints /v1/knowledge/{query,promote,unpromote,list,rebuild}.
//
// Constructor NewKnowledge9Cmd() (not NewKnowledgeCmd) — zero-arg, registered
// on root as `knowledge-p9` to avoid shadowing the existing group.
// See root.go for the registration comment.
//
// invariant: promote and unpromote MUST require non-empty --reason; enforced
// via cobra MarkFlagRequired (presence gate) + TrimSpace check in RunE
// (non-empty gate). Cross-cutting compliance test in reason_flag_test.go (I-12).
//
// invariant: aggregator NEVER queries the web — territory. No
// --remote or --ecosystem flag is exposed here.
package cli

import "github.com/spf13/cobra"

func NewKnowledge9Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge-p9",
		Short: "Cross-project knowledge aggregator: query | promote | unpromote | ls | rebuild",
		Long: `Plan 9 cross-project knowledge aggregator. Per-project SQLite sources
merged via FTS5 + sqlite-vec + wikilink graph + RRF; opt-in operator-gated
promote pin (Q6 C). NEVER queries the web — Plan 14 territory (inv-zen-129).

Five leaves:
  query     Federated/pinned/chain-verified search (Plan 9 H-2 endpoint)
  promote   Operator-gated pin to global index (inv-zen-146 mandatory reason)
  unpromote Reverse a prior promote (inv-zen-146 mandatory reason)
  ls        List notes (per-project or pinned-only)
  rebuild   Re-embed + re-index one project (async; returns job_id)`,
		Example: `  # Federated query (all scopes)
  zen knowledge-p9 query "audit chain integrity"

  # Pinned-only across all projects
  zen knowledge-p9 query "max scope doctrine" --pinned-only

  # Chain-verified notes only
  zen knowledge-p9 query "tessera vendor" --audit-chain

  # Operator-gated promote (inv-zen-146)
  zen knowledge-p9 promote internal-platform-x/M0-pattern-vault-format \
    --reason "applies to all max-scope projects"

  # List all pinned notes
  zen knowledge-p9 ls --pinned-only

  # Rebuild one project's index async
  zen knowledge-p9 rebuild --project zen-swarm`,
	}
	cmd.AddCommand(knowledge9QueryCmd())
	cmd.AddCommand(knowledge9PromoteCmd())
	cmd.AddCommand(knowledge9UnpromoteCmd())
	cmd.AddCommand(knowledge9LsCmd())
	cmd.AddCommand(knowledge9RebuildCmd())
	return cmd
}
