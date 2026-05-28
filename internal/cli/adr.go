// SPDX-License-Identifier: MIT
// Package cli — adr.go.
//
// `hades adr` is the HADES design operator surface for the ADR machine-readable
// index (spec §6.1 design choice A). Ten leaves cover the full ADR lifecycle:
//
// propose <topic> — draft + $EDITOR + daemon commit [I-6]
// show <id> — frontmatter table + body [I-6]
// ls [--status/plan/risk] — filter list [I-6]
// graph --from <id> [--depth]— supersede chain ASCII tree [I-6]
// history <id> — transition log [I-7]
// accept <id> --reason — emit adr.accepted event [I-7]
// reject <id> --reason — emit adr.rejected event [I-7]
// supersede <old> <new> --reason— link old→new chain [I-7]
// migrate — one-time 39-ADR frontmatter import [I-8]
// index [--check] — dual manifest regenerate / CI gate [I-8]
//
// invariant: accept / reject / supersede MUST require non-empty --reason.
// Cross-cutting compliance test in reason_flag_test.go.
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
		Short: "ADR machine-readable index (HADES design)",
		Long:  "HADES design ADR surface. Structured MADR YAML frontmatter + JSON Schema\nvalidator + dual JSON manifest emitter (_index.json + _graph.json).\nTransitions (accept | reject | supersede) require --reason (invariant)\nand emit HADES design events anchored on HADES design chain.\n\nTen leaves:\n  propose    Interactive draft (auto-assigns next id; opens $EDITOR)\n  show       Frontmatter table + markdown body\n  ls         Filter list by status / plan / risk-level\n  graph      Supersede chain ASCII tree\n  history    Transition log for one ADR\n  accept     Mark accepted (--reason mandatory; invariant)\n  reject     Mark rejected (--reason mandatory; invariant)\n  supersede  Link old→new chain (--reason mandatory; invariant)\n  migrate    One-time: parse 39 legacy headers → MADR frontmatter\n  index      Regenerate dual manifest; --check for CI gate",

		Example: "  hades adr propose tessera-batch-cadence-tuning\n  hades adr show ADR-0042\n  hades adr ls --status proposed --plan HADES design\n  hades adr graph --from ADR-0001 --depth 3\n  hades adr accept ADR-0070 --reason \"design choice B approved\"\n  hades adr index --check",
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
