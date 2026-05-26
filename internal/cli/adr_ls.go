// SPDX-License-Identifier: MIT
// Package cli — adr_ls.go (Plan 9 Phase I Task I-6).
//
// `zen adr ls [--status X] [--plan Y] [--risk-level Z]` calls
// GET /v1/adr/list with the supplied filter and renders results in a
// tabwriter table. Wire type: []client.ADR.
package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func adrLsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls",
		Short: "List ADRs filterable by status / plan / risk-level",
		Example: `  zen adr ls
  zen adr ls --status proposed
  zen adr ls --plan plan-9 --risk-level high`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, _ := cmd.Flags().GetString("status")
			plan, _ := cmd.Flags().GetString("plan")
			risk, _ := cmd.Flags().GetString("risk-level")
			limit, _ := cmd.Flags().GetInt("limit")

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).ADRList(ctx, client.ADRListClientFilter{
				Status:    status,
				Plan:      plan,
				RiskLevel: risk,
				Limit:     limit,
			})
			if err != nil {
				return err
			}
			renderAdrList(cmd, items)
			return nil
		},
	}
	c.Flags().String("status", "", "Filter by status (proposed|accepted|rejected|superseded|deprecated)")
	c.Flags().String("plan", "", "Filter by plan tag (e.g. plan-9)")
	c.Flags().String("risk-level", "", "Filter by risk level (low|medium|high)")
	c.Flags().Int("limit", 0, "Max rows (0 = server default 200)")
	return c
}

func renderAdrList(cmd *cobra.Command, items []client.ADR) {
	out := cmd.OutOrStdout()
	if len(items) == 0 {
		fmt.Fprintln(out, "(no rows)")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPLAN\tRISK\tTOPIC")
	for _, r := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.Status, r.Plan, r.RiskLevel, r.Topic)
	}
	_ = tw.Flush()
}
