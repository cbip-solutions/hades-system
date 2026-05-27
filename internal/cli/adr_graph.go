// SPDX-License-Identifier: MIT
// Package cli — adr_graph.go.
//
// `zen adr graph --from <id> [--depth <n>]` calls
// GET /v1/adr/graph?from=...&depth=... and renders the supersede-chain DAG
// as an ASCII tree rooted at the --from node.
//
// Wire type: client.ADRGraph (flat Nodes []ADRGraphNode + Edges []ADREdge).
// The flat DAG is converted to an adjacency tree locally and rendered with
// box-drawing characters (├─ / └─) at 2-space indent per depth level.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func adrGraphCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "graph",
		Short: "Render supersede chain ASCII tree from a root ID",
		Long: `graph calls GET /v1/adr/graph and renders the supersede-chain DAG
as a Unicode box-drawing tree. The --from flag is required; --depth limits
how many levels are included (default 5; 0 = server default 1).`,
		Example: `  zen adr graph --from ADR-0001
  zen adr graph --from ADR-0001 --depth 3`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, _ := cmd.Flags().GetString("from")
			depth, _ := cmd.Flags().GetInt("depth")
			if from == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--from required"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).ADRGraph(ctx, from, depth)
			if err != nil {
				return err
			}
			renderADRGraph(cmd.OutOrStdout(), from, resp)
			return nil
		},
	}
	c.Flags().String("from", "", "Root ADR ID (required)")
	c.Flags().Int("depth", 5, "Max depth (0 = server default 1)")
	return c
}

func renderADRGraph(out io.Writer, rootID string, g client.ADRGraph) {

	statusOf := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		statusOf[n.ID] = n.Status
	}

	children := make(map[string][]string, len(g.Edges))
	for _, e := range g.Edges {
		children[e.From] = append(children[e.From], e.To)
	}

	var walk func(id, prefix string, isLast bool)
	walk = func(id, prefix string, isLast bool) {
		branch := "├─"
		if isLast {
			branch = "└─"
		}
		status := statusOf[id]
		if status != "" {
			fmt.Fprintf(out, "%s%s %s [%s]\n", prefix, branch, id, status)
		} else {
			fmt.Fprintf(out, "%s%s %s\n", prefix, branch, id)
		}
		kids := children[id]
		childPrefix := prefix + "│  "
		if isLast {
			childPrefix = prefix + "   "
		}
		for i, child := range kids {
			walk(child, childPrefix, i == len(kids)-1)
		}
	}

	status := statusOf[rootID]
	if status != "" {
		fmt.Fprintf(out, "%s [%s]\n", rootID, status)
	} else {
		fmt.Fprintf(out, "%s\n", rootID)
	}

	kids := children[rootID]
	for i, child := range kids {
		walk(child, "", i == len(kids)-1)
	}

	if len(g.Nodes) == 0 && len(g.Edges) == 0 {
		fmt.Fprintf(out, "%s (no graph data)\n", strings.Repeat("─", 20))
	}
}
