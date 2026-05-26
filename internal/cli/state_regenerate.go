// SPDX-License-Identifier: MIT
// Package cli — state_regenerate.go (Plan 9 Phase I Task I-10).
//
// `zen state regenerate [--dry-run]` calls POST /v1/state/regenerate.
// With --dry-run the TOML file is NOT rewritten; a diff of what would
// change is returned and printed. Without --dry-run the file is atomically
// rewritten on the daemon side.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func newStateRegenerateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate system-state.toml from authoritative sources",
		Long: `regenerate calls POST /v1/state/regenerate.

With --dry-run: shows a diff of what would change WITHOUT writing the file.
Without --dry-run: atomically rewrites docs/system-state.toml on the daemon side.

Sources consulted: go.mod, git tags, doctrine registry, ADR _index.json.
Manual (pinned) fields are preserved unless explicitly overridden.`,
		Example: `  zen state regenerate --dry-run   # preview changes
  zen state regenerate             # write changes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).StateRegenerate(ctx, client.StateRegenerateReq{DryRun: dryRun})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if dryRun {
				fmt.Fprintln(out, "Dry-run diff (no file written):")
				if resp.Diff != "" {
					fmt.Fprintln(out, resp.Diff)
				} else {
					fmt.Fprintln(out, "(no changes)")
				}
				if len(resp.ChangedFields) > 0 {
					fmt.Fprintf(out, "changed fields: %s\n", strings.Join(resp.ChangedFields, ", "))
				}
				return nil
			}

			if len(resp.ChangedFields) > 0 {
				fmt.Fprintf(out, "regenerated: changed fields: %s\n", strings.Join(resp.ChangedFields, ", "))
			} else {
				fmt.Fprintln(out, "regenerated: no changes")
			}
			return nil
		},
	}
	c.Flags().Bool("dry-run", false, "Preview diff without writing docs/system-state.toml")
	return c
}
