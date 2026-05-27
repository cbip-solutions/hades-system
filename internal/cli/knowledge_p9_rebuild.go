// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_rebuild.go.
//
// `zen knowledge-p9 rebuild` — re-embed + re-index one project's promoted pins.
//
// The daemon refreshes the global pin index synchronously and returns
// 202 Accepted with a receipt for wire compatibility. --project is required;
// the daemon returns 400 when omitted.
//
// Wire method: KnowledgeRebuildP9(ctx, projectID) → (KnowledgeRebuildResp, error).
// KnowledgeRebuildResp JobID (string), StartedAt (int64 unix).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func knowledge9RebuildCmd() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Re-embed + re-index one project's promoted pins",
		Long: `rebuild refreshes the Plan 9 global pin index for one project. The daemon
returns 202 Accepted with a receipt after the promoted-pin FTS and vector rows
have been rewritten.

--project is required.`,
		Example: `  zen knowledge-p9 rebuild --project zen-swarm
  zen knowledge-p9 rebuild --project internal-platform-x`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project required"))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()

			resp, err := newClientFromCmd(cmd).KnowledgeRebuildP9(ctx, project)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "rebuild completed: project=%s job_id=%s\n", project, resp.JobID)
			if resp.RebuiltCount > 0 {
				fmt.Fprintf(out, "rebuilt=%d\n", resp.RebuiltCount)
			}
			if resp.StartedAt > 0 {
				fmt.Fprintf(out, "started_at=%s\n", client.FormatUnix(resp.StartedAt))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project ID (required)")
	return cmd
}
