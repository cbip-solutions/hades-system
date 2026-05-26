// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_rebuild.go (Plan 9 Phase I Task I-5).
//
// `zen knowledge-p9 rebuild` — re-embed + re-index one project asynchronously.
//
// The daemon enqueues the rebuild in a background goroutine and returns 202
// Accepted with a job_id immediately. The operator tracks progress via Plan 5
// audit events. --project is required; the daemon returns 400 when omitted.
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
		Short: "Re-embed + re-index one project's notes (async; returns job_id)",
		Long: `rebuild enqueues a full re-embed + re-index for one project. The daemon
returns 202 Accepted immediately with a job_id; the rebuild runs in a background
goroutine. On Mac M4 MPS GPU (mac-compute tier), embedding is ~230× faster than
daemon CPU — the daemon offloads if the mac-compute dispatcher is configured.

--project is required. Track progress via ` + "`zen audit-chain history`" + `.`,
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
			fmt.Fprintf(out, "rebuild enqueued: project=%s job_id=%s\n", project, resp.JobID)
			if resp.StartedAt > 0 {
				fmt.Fprintf(out, "started_at=%s\n", client.FormatUnix(resp.StartedAt))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project ID (required)")
	return cmd
}
