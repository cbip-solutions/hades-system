// SPDX-License-Identifier: MIT
// Package cli — autonomy.go.
//
// `hades autonomy` exposes the 3-layer autonomy resolution (design choice C) and
// the design choice D pre-flight check matrix:
//
// hades autonomy show — effective mode + chain
// hades autonomy --check [--verbose] — design choice D check matrix
// hades autonomy --check --allow-soft-warnings — proceed past soft fails
// hades autonomy mode <manual|semi|full> — write override
// hades autonomy mode --reset — clear override
//
// Capa-firewall doctrine (invariant) hard-blocks any non-manual
// override at the daemon. The CLI surfaces the daemon's 403 response
// as a human-readable "capa-firewall" error so the operator knows
// exactly why the request was refused.
package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewAutonomyCmd() *cobra.Command {
	var (
		runCheck         bool
		allowSoftWarning bool
		verbose          bool
	)
	cmd := &cobra.Command{
		Use:   "autonomy",
		Short: "Inspect and configure orchestrator autonomy mode (design choice C / design choice D)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !runCheck {
				return cmd.Help()
			}
			return runAutonomyCheck(cmd, verbose, allowSoftWarning)
		},
	}
	attachPlan5DaemonURLFlag(cmd)
	cmd.Flags().BoolVar(&runCheck, "check", false, "run pre-flight check matrix (design choice D)")
	cmd.Flags().BoolVar(&allowSoftWarning, "allow-soft-warnings", false, "proceed past soft-tier failures (operator explicit consent)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "include passing rows in output")

	cmd.AddCommand(autonomyShowCmd())
	cmd.AddCommand(autonomyModeCmd())
	return cmd
}

func autonomyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show effective autonomy mode + 3-layer resolution chain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := plan5ClientFromCmd(cmd).AutonomyShowCall(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Effective mode: %s (resolved from %s)\n", s.EffectiveMode, s.ResolvedFrom)
			fmt.Fprintf(out, "  doctrine: %s\n", s.DoctrineMode)
			fmt.Fprintf(out, "  hadessystem.toml: %s\n", s.HadesSystemTOMLMode)
			fmt.Fprintf(out, "  flag: %s\n", s.FlagMode)
			fmt.Fprintf(out, "  capa-firewall lock: %v\n", s.CapaFirewallLock)
			fmt.Fprintf(out, "Cost-degradation tier: %s (budget %.1f%%)\n",
				s.CostDegradation.CurrentTier, s.CostDegradation.BudgetPct)
			return nil
		},
	}
}

func autonomyModeCmd() *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "mode <manual|semi|full>",
		Short: "Set autonomy mode override (subject to capa-firewall hard guard)",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.AutonomyModeRequest{Reset: reset}
			if reset {
				if len(args) > 0 {
					return errors.New("--reset and a mode argument are mutually exclusive")
				}
			} else {
				if len(args) != 1 {
					return errors.New("exactly one mode argument required (or --reset)")
				}
				switch args[0] {
				case "manual", "semi", "full":
					req.Mode = args[0]
				default:
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("invalid mode %q (want manual|semi|full)", args[0]))
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := plan5ClientFromCmd(cmd).AutonomyMode(ctx, req); err != nil {
				return mapAutonomyModeError(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "autonomy mode updated")
			return nil
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "clear override; revert to doctrine default")
	return cmd
}

func mapAutonomyModeError(err error) error {
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "capa-firewall") || strings.Contains(low, "forbidden") || strings.Contains(low, "403") {
		return ierrors.Wrap(ierrors.Code("daemon.auth-failed"), fmt.Errorf("capa-firewall: mode override forbidden — doctrine pin holds (%s)", msg))
	}
	return err
}

func runAutonomyCheck(cmd *cobra.Command, verbose, allowSoft bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := plan5ClientFromCmd(cmd).AutonomyCheck(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, row := range res.Rows {
		if !row.Pass || verbose {
			status := "PASS"
			if !row.Pass {
				status = "FAIL"
			}
			fmt.Fprintf(out, "[%s] %-12s %-40s %s\n", status, row.Tier, row.Name, row.Detail)
		}
	}
	fmt.Fprintf(out, "\nSummary: hard=%d soft=%d info=%d  overall_pass=%v\n",
		res.HardFailed, res.SoftFailed, res.InfoFailed, res.OverallPass)
	if res.HardFailed > 0 {
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("hard tier check failed (%d row(s)); orchestrator will refuse to start", res.HardFailed))
	}
	if res.SoftFailed > 0 && !allowSoft {
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("soft tier check failed (%d row(s)); pass --allow-soft-warnings to proceed", res.SoftFailed))
	}
	return nil
}
