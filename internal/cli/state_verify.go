// SPDX-License-Identifier: MIT
// Package cli — state_verify.go.
//
// `hades state verify` calls POST /v1/state/verify and is the CI gate for
// docs/system-state.toml freshness. On drift (Match=false) the command
// exits non-zero so `make verify-invariants` fails the build.
//
// invariant boundary: verify is read-only (no --reason required); only
// `pin` modifies state and enforces invariant.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func newStateVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "CI gate: regenerate-and-diff; exit non-zero on drift",
		Long: `verify calls POST /v1/state/verify (regenerate-into-memory + diff).

StateDiff.Match=true  → exit 0, prints "no drift"
StateDiff.Match=false → exit non-zero, prints diff + error message

Integrated with ` + "`make verify-invariants`" + ` for CI-time freshness enforcement.
Run ` + "`hades state regenerate`" + ` to resolve drift.`,
		Example: "  hades state verify",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			diff, err := newClientFromCmd(cmd).StateVerify(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !diff.Match {
				fmt.Fprintln(out, "DRIFT detected — system-state.toml differs from regenerated:")
				if diff.Diff != "" {
					fmt.Fprintln(out, diff.Diff)
				}
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("system-state.toml drift detected (CI gate); run 'hades state regenerate'"))
			}
			fmt.Fprintln(out, "no drift")
			return nil
		},
	}
}
