// SPDX-License-Identifier: MIT
// Package cli — docs_pin.go.
//
// `hades docs pin --ecosystem <X> --version <Y>` sets
// ecosystem_versions.indefinite_retain=true for the (ecosystem, version)
// tuple via daemon POST /v1/ecosystem/pin. Pinned versions are:
//
// - Never archived (excluded from the 2-prior-stable retention window
// governed by the Q9=A cron sweep).
// - Never auto-pruned by background storage-budget enforcement.
// - Only hard-removable via explicit `hades docs unpin` followed by
// `hades docs prune --confirm`; the daemon refuses to prune pinned
// versions with 409 Conflict.
//
// Architecture CLI calls daemon HTTP; daemon owns the write transaction.
// Boundary (invariant): does NOT import internal/research/ecosystem.
//
// Operator gate: requires explicit --ecosystem + --version flags (no
// positional arg, reducing typo-driven misuse). A promptYN confirmation
// prompt is shown before the daemon call; blank input aborts.
//
// G-5 SUPERSEDES F-6: F-6 shipped `hades docs pin <chunk-id>` (positional,
// chunk granularity). G-5 evolves to version granularity per spec §2.9
// Q9=A (retention operates on ecosystem_versions rows, not chunks).
// History NewDocsPinCmd previously took a factory + ExactArgs(1) chunk
// id; now takes a factory + flag-based ecosystem/version with promptYN.
//
// Exit codes (per spec §6.2):
//
// 0 success (pin committed by daemon, or operator aborted at prompt)
// 1 recoverable: missing/invalid --ecosystem, missing --version, daemon
// 404 (unknown tuple), daemon 409 (already pinned — operator can
// accept no-op)
// 2 unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const docsPinTimeout = 10 * time.Second

var validDocsEcosystems = []string{"go", "python", "typescript", "rust"}

func isValidEcosystemArg(eco string) bool {
	return slices.Contains(validDocsEcosystems, eco)
}

type DocsPinFlags struct {
	Ecosystem string
	Version   string
}

func NewDocsPinCmd(factory DocsClientFactory) *cobra.Command {
	flags := DocsPinFlags{}
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Pin an ecosystem version (never archived, never auto-pruned)",
		Long: `Pin sets ecosystem_versions.indefinite_retain=true for the given
version via the daemon. Pinned versions are excluded from the 2-prior-stable
retention window and cannot be auto-pruned. The daemon refuses to prune a
pinned version (409 Conflict); the operator must run ` + "`hades docs unpin`" + `
first.

Operator-confirmation prompt: a y/N gate is shown before the daemon call.
Blank input aborts without side-effect (privacy-by-default).

Required flags:
  --ecosystem    one of: go, python, typescript, rust
  --version      semver string (e.g. "1.22.0", "3.11.9", "1.70.0")`,
		Example: `  hades docs pin --ecosystem go --version 1.22.0
  hades docs pin --ecosystem python --version 3.11.9`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsPinTimeout)
			defer cancel()
			return RunDocsPin(ctx, c, flags, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Ecosystem, "ecosystem", "", "ecosystem to pin (go|python|typescript|rust)")
	cmd.Flags().StringVar(&flags.Version, "version", "", "version string to pin (e.g. 1.22.0)")
	_ = cmd.MarkFlagRequired("ecosystem")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func RunDocsPin(ctx context.Context, c DocsClient, flags DocsPinFlags, in io.Reader, w io.Writer) error {
	eco := strings.TrimSpace(flags.Ecosystem)
	ver := strings.TrimSpace(flags.Version)

	if eco == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs pin: --ecosystem is required"))
	}
	if !isValidEcosystemArg(eco) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs pin: invalid --ecosystem %q; must be one of: %s",
			eco, strings.Join(validDocsEcosystems, ", ")))
	}
	if ver == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs pin: --version is required and must be non-empty"))
	}

	fmt.Fprintf(w, "About to pin %s@%s (indefinite_retain=true).\n", eco, ver)
	fmt.Fprintln(w, "Pinned versions are never archived and never auto-pruned.")
	fmt.Fprintf(w, "To remove the pin later: hades docs unpin --ecosystem %s --version %s\n", eco, ver)

	ok, err := promptYN(in, w, "Continue?")
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("docs pin: prompt: %w", err))
	}
	if !ok {
		fmt.Fprintln(w, "Pin aborted by operator.")
		return nil
	}

	if err := c.EcosystemPin(ctx, eco, ver); err != nil {
		return classifyDocsError(err, "pin")
	}
	fmt.Fprintf(w, "pinned: %s@%s (indefinite_retain=true)\n", eco, ver)
	return nil
}
