// SPDX-License-Identifier: MIT
// Package cli — adr.go (Plan 9 Phase I Task I-6).
//
// `zen adr` is the Plan 9 operator surface for the ADR machine-readable
// index (spec §6.1 Q7 A). Ten leaves cover the full ADR lifecycle:
//
//	propose  <topic>              — draft + $EDITOR + daemon commit   [I-6]
//	show     <id>                 — frontmatter table + body           [I-6]
//	ls       [--status/plan/risk] — filter list                        [I-6]
//	graph    --from <id> [--depth]— supersede chain ASCII tree         [I-6]
//	history  <id>                 — transition log                     [I-7]
//	accept   <id> --reason        — emit adr.accepted event            [I-7]
//	reject   <id> --reason        — emit adr.rejected event            [I-7]
//	supersede <old> <new> --reason— link old→new chain                [I-7]
//	migrate                       — one-time 39-ADR frontmatter import [I-8]
//	index    [--check]            — dual manifest regenerate / CI gate [I-8]
//
// inv-zen-146: accept / reject / supersede MUST require non-empty --reason.
// Cross-cutting compliance test in reason_flag_test.go (I-12).
//
// Wire types: client.ADR, client.ADRGraph, client.ADRGraphNode,
// client.ADREdge, client.ADRTransition, client.ADRManifest (H-9 final
// shapes). The plan-file spec used fictitious plan-time type names
// (AdrProposeResp, AdrShowResp, AdrSummary, AdrGraphResp, AdrGraphNode
// with Children …); the implementation uses the actual shipped types.
package cli

import "github.com/spf13/cobra"

func NewAdrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adr",
		Short: "ADR machine-readable index (Plan 9)",
		Long: `Plan 9 ADR surface. Structured MADR YAML frontmatter + JSON Schema
validator + dual JSON manifest emitter (_index.json + _graph.json).
Transitions (accept | reject | supersede) require --reason (inv-zen-146)
and emit Plan 8 events anchored on Plan 9 chain.

Ten leaves:
  propose    Interactive draft (auto-assigns next id; opens $EDITOR)
  show       Frontmatter table + markdown body
  ls         Filter list by status / plan / risk-level
  graph      Supersede chain ASCII tree
  history    Transition log for one ADR
  accept     Mark accepted (--reason mandatory; inv-zen-146)
  reject     Mark rejected (--reason mandatory; inv-zen-146)
  supersede  Link old→new chain (--reason mandatory; inv-zen-146)
  migrate    One-time: parse 39 legacy headers → MADR frontmatter
  index      Regenerate dual manifest; --check for CI gate`,
		Example: `  zen adr propose tessera-batch-cadence-tuning
  zen adr show ADR-0042
  zen adr ls --status proposed --plan plan-9
  zen adr graph --from ADR-0001 --depth 3
  zen adr accept ADR-0070 --reason "Q4 B approved"
  zen adr index --check`,
	}
	cmd.AddCommand(adrProposeCmd())
	cmd.AddCommand(adrShowCmd())
	cmd.AddCommand(adrLsCmd())
	cmd.AddCommand(adrGraphCmd())
	cmd.AddCommand(adrHistoryCmd())
	cmd.AddCommand(adrAcceptCmd())
	cmd.AddCommand(adrRejectCmd())
	cmd.AddCommand(adrSupersedeCmd())
	cmd.AddCommand(adrMigrateCmd())
	cmd.AddCommand(adrIndexCmd())
	return cmd
}
