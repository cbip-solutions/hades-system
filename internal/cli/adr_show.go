// SPDX-License-Identifier: MIT
// Package cli — adr_show.go.
//
// `hades adr show <id>` calls GET /v1/adr/show?id=... and renders the ADR
// frontmatter as a table plus the markdown body. Wire type: client.ADR.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

func adrShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <id>",
		Short:   "Render frontmatter as table + body",
		Args:    cobra.ExactArgs(1),
		Example: `  hades adr show ADR-0042`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).ADRShow(ctx, args[0])
			if err != nil {
				return err
			}
			renderAdrShow(cmd, resp)
			return nil
		},
	}
}

func renderAdrShow(cmd *cobra.Command, resp client.ADR) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "ID:        %s\n", resp.ID)
	fmt.Fprintf(out, "Status:    %s\n", resp.Status)
	fmt.Fprintf(out, "Topic:     %s\n", resp.Topic)
	fmt.Fprintf(out, "Plan:      %s\n", resp.Plan)
	fmt.Fprintf(out, "Risk:      %s\n", resp.RiskLevel)

	if len(resp.Frontmatter) > 0 {

		keys := make([]string, 0, len(resp.Frontmatter))
		for k := range resp.Frontmatter {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pretty, _ := json.MarshalIndent(resp.Frontmatter, "  ", "  ")
		fmt.Fprintln(out, "Frontmatter:")
		fmt.Fprintln(out, "  "+string(pretty))
	}
	if resp.Body != "" {
		fmt.Fprintln(out, "Body:")
		fmt.Fprint(out, resp.Body)
	}
}
