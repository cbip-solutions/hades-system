// SPDX-License-Identifier: MIT
// Package cli — orchestrator_depth.go.
//
// `hades orchestrator depth` overrides the calculated depth/width for a
// given spec on a given project. --depth and --reset are mutually
// exclusive; --project + --spec are required.
package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func orchDepthCmd() *cobra.Command {
	var (
		project, spec string
		depth         int
		reset         bool
	)
	cmd := &cobra.Command{
		Use:   "depth",
		Short: "Override calculated depth/width for a spec (or reset)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if depth > 0 && reset {
				return errors.New("--depth and --reset are mutually exclusive")
			}
			if project == "" || spec == "" {
				return errors.New("--project and --spec are required")
			}
			req := client.DepthOverride{ProjectID: project, SpecPath: spec, Depth: depth, Reset: reset}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := plan5ClientFromCmd(cmd).OrchestratorDepth(ctx, req); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "depth override applied")
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project id (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "spec path (required)")
	cmd.Flags().IntVar(&depth, "depth", 0, "explicit depth value (>0)")
	cmd.Flags().BoolVar(&reset, "reset", false, "reset depth override (mutually exclusive with --depth)")
	return cmd
}
