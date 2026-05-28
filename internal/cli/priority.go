// SPDX-License-Identifier: MIT
// Package cli — priority.go.
//
// `hades project priority --boost / --reset / --ls` is the operator surface
// for Layer 3 WFQ priority overrides per spec §1 Q10 + §6.2.
//
// Cobra layout (single subcommand, three mutually-exclusive flag actions):
//
// hades project priority --boost <alias> --duration 4h --reason "..."
// hades project priority --reset <alias>
// hades project priority --ls
//
// Action mutex: exactly one of --boost / --reset / --ls must be set;
// any other combination errors out. The mutex check happens BEFORE
// any HTTP dispatch so operator typos surface immediately.
//
// Validation (client-side, before any daemon call):
// - alias non-empty (whitespace-trimmed)
// - --boost requires --duration ((0, 168h] window) + --reason
// (non-empty after trim)
// - --multiplier defaults 3.0; bounds (0, 100]
//
// Defence in depth: the same validation runs again at the daemon (in
// the OverrideStore.Set adapter) and once more at the WFQ hot-path read
// (quota.ApplyOverride) — three checks for one drift.
//
// Output formatting:
// - boost: ✓ priority boost queued + project/multiplier/duration/
// expires(RFC3339)/reason block (machine-parseable; stable across
// releases for `hades day` morning brief composition).
// - reset: ✓ priority override removed: <alias>
// - ls: ALIAS / MULT / EXPIRES / REASON / CREATED column header +
// row-per-override.
//
// Exit-code mapping:
// - 0 success
// - 1 operator-recoverable (validation reject, alias not found via
// daemon 404, daemon 422 quota.ErrInvalidOverride)
// - 2 unrecoverable (transport, decode, daemon 5xx)
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

const maxPriorityDuration = 168 * time.Hour

const minPriorityDuration = time.Second

const maxPriorityMultiplier = 100.0

const defaultPriorityMultiplier = 3.0

func NewPriorityCmd() *cobra.Command {
	var (
		boostAlias string
		resetAlias string
		ls         bool
		duration   string
		reason     string
		multiplier float64
	)
	cmd := &cobra.Command{
		Use:   "priority",
		Short: "Manage per-project WFQ priority overrides (boost/reset/list)",
		Long: `Manage per-project WFQ priority overrides per Plan 7 spec §1 Q10.

The Layer-3 weighted-fair-queue arbitrates LLM dispatch across
projects; an operator-issued boost temporarily raises a project's
weight so its scheduled fires win arbitration during the boost
window. Reset removes the override; list inspects what's active.

The boost multiplier defaults to 3.0; provide --multiplier to override.
Multiplier must be in (0, 100]; duration in [1s, 168h] (1 week max).
A --reason string is mandatory on --boost — the daemon audit-logs the
reason for post-hoc traceability.

Mutually exclusive: pass exactly one of --boost / --reset / --ls.

Exit codes (spec §6.2):
  0 success
  1 operator-recoverable (validation reject, alias not found, daemon
    422 quota.ErrInvalidOverride)
  2 unrecoverable (transport / decode / daemon 5xx)`,
		Example: " # Boost a project for 4 hours with a documented reason (audit-logged)\n  hades project priority --boost internal-platform-x --duration 4h --reason \"release prep\"\n\n # Specify a non-default multiplier (5x weight)\n  hades project priority --boost internal-platform-x --duration 1h --reason \"demo\" --multiplier 5\n\n # Reset (remove) an existing override\n  hades project priority --reset internal-platform-x\n\n # List active overrides\n  hades project priority --ls",

		RunE: func(cmd *cobra.Command, _ []string) error {
			actions := 0
			if boostAlias != "" {
				actions++
			}
			if resetAlias != "" {
				actions++
			}
			if ls {
				actions++
			}
			if actions != 1 {
				return errPriorityActionExclusive(cmd)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			c := newClientFromCmd(cmd)
			if boostAlias != "" {
				return runPriorityBoost(ctx, cmd, c, boostAlias, duration, reason, multiplier)
			}
			if resetAlias != "" {
				return runPriorityReset(ctx, cmd, c, resetAlias)
			}
			return runPriorityList(ctx, cmd, c)
		},
	}
	cmd.Flags().StringVar(&boostAlias, "boost", "", "project alias to boost")
	cmd.Flags().StringVar(&resetAlias, "reset", "", "project alias to reset")
	cmd.Flags().BoolVar(&ls, "ls", false, "list active priority overrides")
	cmd.Flags().StringVar(&duration, "duration", "", "boost duration (Go time.Duration; required with --boost)")
	cmd.Flags().StringVar(&reason, "reason", "", "operator reason (required with --boost; audit-logged)")
	cmd.Flags().Float64Var(&multiplier, "multiplier", defaultPriorityMultiplier, "boost weight multiplier (0, 100]; default 3.0")
	return cmd
}

func errPriorityActionExclusive(cmd *cobra.Command) error {
	fmt.Fprintln(cmd.ErrOrStderr(),
		"error: exactly one of --boost / --reset / --ls must be specified")
	return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("priority: exactly one of --boost / --reset / --ls is required"))
}

func runPriorityBoost(ctx context.Context, cmd *cobra.Command, c *client.Client, alias, durationStr, reason string, mult float64) error {
	if strings.TrimSpace(alias) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--boost alias is empty"))
	}
	if strings.TrimSpace(reason) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--reason is required (audit-logged)"))
	}
	if err := validatePriorityDuration(durationStr); err != nil {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "--duration invalid"))
	}
	if mult <= 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--multiplier must be > 0 (got %v)", mult))
	}
	if mult > maxPriorityMultiplier {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--multiplier %v exceeds sanity ceiling %v", mult, maxPriorityMultiplier))
	}
	d, _ := time.ParseDuration(durationStr)
	now := time.Now().UTC()
	expires := now.Add(d)
	if err := c.PriorityBoost(ctx, alias, mult, expires, reason); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "boost failed: %v\n", err)

		if client.IsHTTPStatus(err, http.StatusNotFound) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "daemon rejected override"))
		}
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
	}
	renderBoost(cmd.OutOrStdout(), alias, mult, d, reason, expires)
	return nil
}

func runPriorityReset(ctx context.Context, cmd *cobra.Command, c *client.Client, alias string) error {
	if strings.TrimSpace(alias) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--reset alias is empty"))
	}
	if err := c.PriorityReset(ctx, alias); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "reset failed: %v\n", err)
		if client.IsHTTPStatus(err, http.StatusNotFound) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ priority override removed: %s\n", alias)
	return nil
}

func runPriorityList(ctx context.Context, cmd *cobra.Command, c *client.Client) error {
	rows, err := c.PriorityList(ctx)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "list failed: %v\n", err)
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
	}
	renderList(cmd.OutOrStdout(), rows)
	return nil
}

func renderBoost(w io.Writer, alias string, mult float64, d time.Duration, reason string, expires time.Time) {
	fmt.Fprintln(w, "✓ priority boost queued")
	fmt.Fprintf(w, "  project:    %s\n", alias)
	fmt.Fprintf(w, "  multiplier: %g\n", mult)
	fmt.Fprintf(w, "  duration:   %s\n", d)
	fmt.Fprintf(w, "  expires:    %s\n", expires.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "  reason:     %s\n", reason)
}

func renderList(w io.Writer, rows []client.PriorityOverrideRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tMULT\tEXPIRES\tREASON\tCREATED")
	now := time.Now().UTC()
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%g\t%s\t%s\t%s\n",
			r.Alias,
			r.Multiplier,
			r.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC"),
			r.Reason,
			renderRelativeAgo(now, r.CreatedAt),
		)
	}
	_ = tw.Flush()
}

func renderRelativeAgo(now, then time.Time) string {
	d := now.Sub(then)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func validatePriorityDuration(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("--duration is required (e.g., 4h, 30m, 24h)")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("--duration %q invalid: %w", s, err)
	}
	if d < minPriorityDuration {
		return fmt.Errorf("--duration %v too small (>= %s required)", d, minPriorityDuration)
	}
	if d > maxPriorityDuration {
		return fmt.Errorf("--duration %v exceeds %s (1 week ceiling)", d, maxPriorityDuration)
	}
	return nil
}
