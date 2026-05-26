// SPDX-License-Identifier: MIT
// Package cli — bypass.go (Plan 2 Phase L Task L-2).
//
// Wires the 23 spec §8.1 operations through 14 cobra subcommands. Each
// subcommand is a thin shim: parse flags → call internal/client over
// UDS → format → exit. The daemon owns the *bypass.Client; the CLI
// never instantiates it directly.
//
// Flag-counted operations (so 14 cobra commands cover 23 spec ops):
//
//	audit       --range / --inspect / --since   (3 ops)
//	update-config --diff / --check / apply       (3 ops)
//	extract-config --capture-only                (2 ops)
//	anomalies   --acknowledge                    (2 ops)
//	purge       --dry-run / --apply              (2 ops)
//	certs       --show / --rotate                (2 ops)
//	cf-range    --show / --refresh               (2 ops)
//	single-shot: status, probe, test, cross-validate,
//	             refresh-now, pin, unpin         (7 ops)
//	total                                        = 23
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewBypassCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bypass",
		Short: "In-house bypass module controls (spec §22, Plan 2)",
	}
	cmd.AddCommand(
		bypassStatusCmd(),
		bypassProbeCmd(),
		bypassAuditCmd(),
		bypassUpdateConfigCmd(),
		bypassTestCmd(),
		newBypassExtractCmd(),
		newBypassCrossValidateCmd(),
		bypassAnomaliesCmd(),
		bypassRefreshNowCmd(),
		bypassPinCmd(),
		bypassUnpinCmd(),
		bypassPurgeCmd(),
		bypassCertsCmd(),
		bypassCFRangeCmd(),
	)
	return cmd
}

func bypassNewClient(cmd *cobra.Command) *client.Client {
	uds, _ := cmd.Flags().GetString("uds")
	return client.New(uds)
}

func bypassStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Bypass health, 24h success rate, in-flight, queue depth, refresh, anomalies, pinned",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			st, err := bypassNewClient(cmd).BypassStatus(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("Tier:           %s\n", st.ActiveTier)
			fmt.Printf("Health:         %s\n", st.Health)
			fmt.Printf("Success 24h:    %.1f%%\n", st.SuccessRate24h*100)
			fmt.Printf("In-flight:      %d\n", st.InFlight)
			fmt.Printf("Queue depth:    %d\n", st.QueueDepth)
			fmt.Printf("Refresh in:     %s\n", st.RefreshExpiresIn)
			fmt.Printf("Anomalies:      %d unacknowledged\n", st.AnomaliesUnacked)
			fmt.Printf("Pinned convs:   %d\n", st.PinnedConversations)
			return nil
		},
	}
}

func bypassProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe",
		Short: "Manual health check across tiers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).BypassProbe(ctx)
			if err != nil {
				return err
			}
			status := "OK"
			if !r.OK {
				status = "FAIL"
			}
			fmt.Printf("%s  %dms  tier=%s\n", status, r.LatencyMs, r.TierUsed)
			if r.Error != "" {
				fmt.Fprintf(os.Stderr, "  error: %s\n", r.Error)
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("probe failed"))
			}
			return nil
		},
	}
}

func bypassAuditCmd() *cobra.Command {
	var rangeStr, inspect, since string
	c := &cobra.Command{
		Use:   "audit",
		Short: "Audit log inspection (--range, --inspect, --since)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, err := bypassNewClient(cmd).BypassAudit(ctx, client.BypassAuditQuery{
				Range:   rangeStr,
				Inspect: inspect,
				Since:   since,
			})
			if err != nil {
				return err
			}
			if inspect != "" {
				return json.NewEncoder(os.Stdout).Encode(out.Row)
			}
			fmt.Printf("%-12s %-8s %-7s %-10s %s\n", "TIER", "COUNT", "P50ms", "ERROR%", "TOP_ERR")
			for _, row := range out.Aggregated {
				fmt.Printf("%-12s %-8d %-7d %-10.1f %s\n",
					row.Tier, row.Count, row.P50Ms, row.ErrorPct*100, row.TopError)
			}
			return nil
		},
	}
	c.Flags().StringVar(&rangeStr, "range", "24h", "time window (e.g. 1h, 24h, 7d)")
	c.Flags().StringVar(&inspect, "inspect", "", "inspect a single audit row by id")
	c.Flags().StringVar(&since, "since", "", "stream tail from audit id forward")
	return c
}

func bypassUpdateConfigCmd() *cobra.Command {
	var diff, check bool
	c := &cobra.Command{
		Use:   "update-config",
		Short: "Fetch + diff + smoke-probe + apply bypass-config (Q8 B+a)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			r, err := bypassNewClient(cmd).BypassUpdateConfig(ctx, client.BypassUpdateOpts{
				DiffOnly:  diff,
				CheckOnly: check,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Current: %s\n", r.CurrentVersion)
			fmt.Printf("Latest:  %s\n", r.LatestVersion)
			if r.Diff != "" {
				fmt.Println("--- diff ---")
				fmt.Println(r.Diff)
			}
			if r.Applied {
				fmt.Println("Applied. Smoke probes passed.")
			} else if !diff && !check {
				fmt.Fprintln(os.Stderr, "Smoke probes blocked apply (no --force flag exists; investigate first)")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&diff, "diff", false, "show diff only, do not apply")
	c.Flags().BoolVar(&check, "check", false, "show available version, do not fetch full config")
	return c
}

func bypassTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Run the 6-probe smoke matrix against the active tier",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			r, err := bypassNewClient(cmd).BypassTest(ctx)
			if err != nil {
				return err
			}
			for _, p := range r.Probes {
				mark := "OK"
				if !p.Passed {
					mark = "FAIL"
				}
				fmt.Printf("%s  %-20s  %dms  %s\n", mark, p.Name, p.LatencyMs, p.Detail)
			}
			if !r.AllPassed {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("not all probes passed"))
			}
			return nil
		},
	}
}

func bypassAnomaliesCmd() *cobra.Command {
	var ack string
	c := &cobra.Command{
		Use:   "anomalies",
		Short: "Stratified-validation anomalies (unknown fields)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cli := bypassNewClient(cmd)
			if ack != "" {
				if err := cli.BypassAnomaliesAck(ctx, ack); err != nil {
					return err
				}
				fmt.Printf("acknowledged: %s\n", ack)
				return nil
			}
			rows, err := cli.BypassAnomalies(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("%-30s %-8s %-12s %s\n", "FIELD", "COUNT", "FIRST_SEEN", "PCT")
			for _, a := range rows {
				fmt.Printf("%-30s %-8d %-12s %.1f%%\n",
					a.Field, a.Count, a.FirstSeen.Format("2006-01-02"), a.ThresholdPct*100)
			}
			if len(rows) == 0 {
				fmt.Println("(no anomalies)")
			}
			return nil
		},
	}
	c.Flags().StringVar(&ack, "acknowledge", "", "mark a field as reviewed")
	return c
}

func bypassRefreshNowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh-now",
		Short: "Force OAuth refresh now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).BypassRefreshNow(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("Refreshed. New token expires in: %s\n", r.ExpiresIn)
			return nil
		},
	}
}

func bypassPinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <conversation_id>",
		Short: "Mark a conversation for permanent body retention",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := bypassNewClient(cmd).BypassPin(ctx, args[0]); err != nil {
				return err
			}
			fmt.Printf("pinned: %s\n", args[0])
			return nil
		},
	}
}

func bypassUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <conversation_id>",
		Short: "Remove pin from a conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := bypassNewClient(cmd).BypassUnpin(ctx, args[0]); err != nil {
				return err
			}
			fmt.Printf("unpinned: %s\n", args[0])
			return nil
		},
	}
}

func bypassPurgeCmd() *cobra.Command {
	var dryRun, apply bool
	c := &cobra.Command{
		Use:   "purge",
		Short: "Run retention purge (>30d, unpinned)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun && apply {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--dry-run and --apply are mutually exclusive; pass exactly one"))
			}
			if !dryRun && !apply {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("must pass --dry-run or --apply"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			r, err := bypassNewClient(cmd).BypassPurge(ctx, apply)
			if err != nil {
				return err
			}
			fmt.Printf("Bodies older than 30d, unpinned: %d\n", r.Candidates)
			fmt.Printf("Bytes freed: %s\n", humanBytes(r.BytesFreed))
			if !apply {
				fmt.Println("(dry run — no rows deleted)")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show candidates only")
	c.Flags().BoolVar(&apply, "apply", false, "actually delete")

	c.MarkFlagsMutuallyExclusive("dry-run", "apply")
	return c
}

func bypassCertsCmd() *cobra.Command {
	var show bool
	var rotate string
	c := &cobra.Command{
		Use:   "certs",
		Short: "Inspect / rotate the pinned intermediate certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cli := bypassNewClient(cmd)
			if rotate != "" {
				if err := cli.BypassCertsRotate(ctx, rotate); err != nil {
					return err
				}
				fmt.Printf("rotated to: %s\n", rotate)
				return nil
			}
			if show {
				r, err := cli.BypassCertsShow(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("Pinned intermediate SHA-256: %s\n", r.SHA256)
				fmt.Printf("Issued:                       %s\n", r.NotBefore)
				fmt.Printf("Expires:                      %s\n", r.NotAfter)
				return nil
			}
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("must pass --show or --rotate <hash>"))
		},
	}
	c.Flags().BoolVar(&show, "show", false, "display the pinned cert hash + dates")
	c.Flags().StringVar(&rotate, "rotate", "", "set a new pinned hash (manual when Anthropic rotates CA)")
	return c
}

func bypassCFRangeCmd() *cobra.Command {
	var refresh, show bool
	c := &cobra.Command{
		Use:   "cf-range",
		Short: "Cloudflare IP range cache controls",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cli := bypassNewClient(cmd)
			if refresh {
				r, err := cli.BypassCFRange(ctx, true)
				if err != nil {
					return err
				}
				fmt.Printf("Refreshed. %d v4 + %d v6 ranges, age: %s\n",
					r.V4Count, r.V6Count, r.Age)
				return nil
			}
			if show {
				r, err := cli.BypassCFRange(ctx, false)
				if err != nil {
					return err
				}
				fmt.Printf("Cached: %d v4 + %d v6 ranges, age: %s\n",
					r.V4Count, r.V6Count, r.Age)
				for _, p := range r.V4 {
					fmt.Println("  ", p)
				}
				for _, p := range r.V6 {
					fmt.Println("  ", p)
				}
				return nil
			}
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("must pass --refresh or --show"))
		},
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "force refresh CF IP range cache")
	c.Flags().BoolVar(&show, "show", false, "display cached ranges + age")
	return c
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024)
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/1024/1024/1024)
	}
}
