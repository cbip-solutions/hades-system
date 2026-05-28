// SPDX-License-Identifier: MIT
// Package cli — adr_index.go.
//
// `hades adr index [--check]` calls POST /v1/adr/index. Without --check it
// regenerates _index.json + _graph.json on disk. With --check it
// regenerates in memory and returns a non-zero exit when drift is detected
// (CI gate — invoked by `make verify-invariants`).
//
// Wire type: client.ADRManifest.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func adrIndexCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "index",
		Short: "Regenerate dual manifest (--check exits non-zero on drift; CI use)",
		Long:  "index regenerates architecture records (flat list) and\n_graph.json (supersede edges). Without --check, regenerates and overwrites\nthe committed files. With --check, regenerates in memory and diffs against\ncommitted; any drift returns exit code 1 (CI gate).\n\nUsed by 'make verify-invariants' to enforce ADR index freshness.",

		Example: `  hades adr index           # regenerate + overwrite (developer use)
  hades adr index --check  # CI gate: exit non-zero on drift`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			check, _ := cmd.Flags().GetBool("check")
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).ADRIndex(ctx, check)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if check {

				if resp.Manifest == "" {
					fmt.Fprintf(out, "DRIFT detected — regenerated manifest differs from committed files\n")
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("ADR index drift detected (CI gate)"))
				}
				fmt.Fprintln(out, "no drift")
				return nil
			}
			fmt.Fprintf(out, "adr_count=%d generated_at=%s\n",
				resp.ADRCount, client.FormatUnix(resp.GeneratedAt))
			return nil
		},
	}
	c.Flags().Bool("check", false, "Dry-run: exit non-zero on drift (CI use)")
	return c
}
