// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9.go.
//
// `hades knowledge` is the HADES design operator surface for the cross-project
// knowledge aggregator (spec §6.1 design choice C). Five leaves cover the full lifecycle:
//
// query <q> federated/pinned/chain-verified search
// promote <note-id> operator-gated pin to global index (invariant)
// unpromote <note-id> reverse a prior promote (invariant)
// ls list notes (per-project or pinned-only)
// rebuild re-embed + re-index one project
//
// DISTINCT's `hades knowledge` surface (knowledge.go: query/reindex/
// stats backed by FTS5 daemon routes). This group hits the HADES design
// aggregator endpoints /v1/knowledge/{query,promote,unpromote,list,rebuild}.
//
// Constructor NewKnowledge9Cmd() (not NewKnowledgeCmd) — zero-arg, registered
// on root as `knowledge-p9` to avoid shadowing the existing HADES design group.
// See root.go for the registration comment.
//
// invariant: promote and unpromote MUST require non-empty --reason; enforced
// via cobra MarkFlagRequired (presence gate) + TrimSpace check in RunE
// (non-empty gate). Cross-cutting compliance test in reason_flag_test.go.
//
// invariant: aggregator NEVER queries the web — HADES design territory. No
// --remote or --ecosystem flag is exposed here.
package cli

import "github.com/spf13/cobra"

func NewKnowledge9Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge-p9",
		Short: "Cross-project knowledge aggregator: query | promote | unpromote | ls | rebuild",
		Long:  "HADES design cross-project knowledge aggregator. Per-project SQLite sources\nmerged via FTS5 + sqlite-vec + wikilink graph + RRF; opt-in operator-gated\npromote pin (design choice C). NEVER queries the web — HADES design territory (invariant).\n\nFive leaves:\n  query     Federated/pinned/chain-verified search (HADES design H-2 endpoint)\n  promote   Operator-gated pin to global index (invariant mandatory reason)\n  unpromote Reverse a prior promote (invariant mandatory reason)\n  ls        List notes (per-project or pinned-only)\n  rebuild   Re-embed + re-index one project (async; returns job_id)",

		Example: " # Federated query (all scopes)\n  hades knowledge-p9 query \"audit chain integrity\"\n\n # Pinned-only across all projects\n  hades knowledge-p9 query \"max scope doctrine\" --pinned-only\n\n # Chain-verified notes only\n  hades knowledge-p9 query \"tessera vendor\" --audit-chain\n\n # Operator-gated promote (invariant)\n  hades knowledge-p9 promote internal-platform-x/M0-pattern-vault-format \\\n    --reason \"applies to all max-scope projects\"\n\n # List all pinned notes\n  hades knowledge-p9 ls --pinned-only\n\n # Rebuild one project's index async\n  hades knowledge-p9 rebuild --project hades-system",
	}
	cmd.AddCommand(knowledge9QueryCmd())
	cmd.AddCommand(knowledge9PromoteCmd())
	cmd.AddCommand(knowledge9UnpromoteCmd())
	cmd.AddCommand(knowledge9LsCmd())
	cmd.AddCommand(knowledge9RebuildCmd())
	return cmd
}
