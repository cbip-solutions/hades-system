// SPDX-License-Identifier: MIT
// Package cli — budget.go.
//
// `zen budget` is the operator's authoritative interface to the
// budget engine.
//
// Cobra layout (8 leaves + 2 group):
//
// zen budget cap-status --axis --value
// zen budget record (operator manual axis-tagging; rare)
// zen budget axes --cost-id
// zen budget anomaly --scope --value --window
// zen budget events --since --limit
// zen budget pause --scope --value --reason --yes
// zen budget resume --scope --value --yes
// zen budget pause-modes
// zen budget rollup --since --limit (synthetic from events)
//
// Option A adaptation: the plan-doc uses `caps {show, set}` whereas the
// daemon exposes `cap_status` (read) plus `record` (write). The CLI
// commands are renamed to match the actual daemon surface; the spec §6.1
// count of 9 leaves is preserved (cap-status, record, axes, anomaly,
// events, pause, resume, pause-modes, rollup).
package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewBudgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Multi-axis cost rollup, caps, anomalies, hierarchical pause (Plan 4)",
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(budgetCapStatusCmd())
	cmd.AddCommand(budgetRecordCmd())
	cmd.AddCommand(budgetAxesCmd())
	cmd.AddCommand(budgetAnomalyCmd())
	cmd.AddCommand(budgetEventsCmd())
	cmd.AddCommand(budgetPauseCmd())
	cmd.AddCommand(budgetResumeCmd())
	cmd.AddCommand(budgetPauseModesCmd())
	cmd.AddCommand(budgetRollupCmd())
	return cmd
}

func budgetCapStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cap-status",
		Short: "Pre-call cap check on an axis+value",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			axis, _ := cmd.Flags().GetString("axis")
			value, _ := cmd.Flags().GetString("value")
			if axis == "" || value == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--axis and --value required"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			cs, err := newClientFromCmd(cmd).BudgetCapStatusCall(ctx, axis, value)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.BudgetCapStatus{cs}, nil)
			}
			rows := []kvRow{
				{"Axis", axis},
				{"Value", value},
				{"RemainingUSD", fmt.Sprintf("%.4f", cs.RemainingUSD)},
				{"Blocked", strconv.FormatBool(cs.Blocked)},
				{"BlockedScope", cs.BlockedScope},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, kvColumns())
		},
	}
	cmd.Flags().String("axis", "", "Axis: project | doctrine | stage | worker_id")
	cmd.Flags().String("value", "", "Axis value (e.g. internal-platform-x, design)")
	return cmd
}

func budgetRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manual axis-tag a cost row (operator escape hatch)",
		Long: `Record a cost row with explicit axis tags. The daemon writes the row
into cost_axis_tags; the same path is normally hit by workers via the
Plan 4 budget engine. Operators rarely need this; provided for
post-incident recovery and debugging only.

Required: --cost-id, --amount-usd, --tag (axis=value, repeatable).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			costID, _ := cmd.Flags().GetString("cost-id")
			amount, _ := cmd.Flags().GetFloat64("amount-usd")
			tagPairs, _ := cmd.Flags().GetStringSlice("tag")
			operationID, _ := cmd.Flags().GetString("operation-id")
			workerID, _ := cmd.Flags().GetString("worker-id")
			if costID == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--cost-id required"))
			}
			tags := make(map[string]string, len(tagPairs))
			for _, p := range tagPairs {
				eq := -1
				for i, ch := range p {
					if ch == '=' {
						eq = i
						break
					}
				}
				if eq < 1 || eq == len(p)-1 {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--tag %q must be axis=value", p))
				}
				tags[p[:eq]] = p[eq+1:]
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			err := newClientFromCmd(cmd).BudgetRecord(ctx, client.BudgetRecordReq{
				CostID: costID, AmountUSD: amount, AxisTags: tags,
				OperationID: operationID, WorkerID: workerID,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "recorded cost_id=%s amount=$%.4f tags=%v\n", costID, amount, tags)
			return nil
		},
	}
	cmd.Flags().String("cost-id", "", "Cost ledger row ID")
	cmd.Flags().Float64("amount-usd", 0, "Amount in USD")
	cmd.Flags().StringSlice("tag", nil, "Axis tag (repeat): project=internal-platform-x")
	cmd.Flags().String("operation-id", "", "Operation ID (optional)")
	cmd.Flags().String("worker-id", "", "Worker ID (optional)")
	return cmd
}

func budgetAxesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "axes",
		Short: "Show axis tags attached to a cost row",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			costID, _ := cmd.Flags().GetString("cost-id")
			if costID == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--cost-id required"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			tags, err := newClientFromCmd(cmd).BudgetAxes(ctx, costID)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "AXIS", Field: func(r any) string { return r.(client.BudgetAxisTag).AxisName }},
				{Header: "VALUE", Field: func(r any) string { return r.(client.BudgetAxisTag).AxisValue }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, tags, cols)
		},
	}
	cmd.Flags().String("cost-id", "", "Cost ledger row ID")
	return cmd
}

func budgetAnomalyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "anomaly",
		Short: "Inspect z-score on an axis+value (sliding window)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			scope, _ := cmd.Flags().GetString("scope")
			value, _ := cmd.Flags().GetString("value")
			if scope == "" || value == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope and --value required"))
			}
			windowSec := int64(0)
			if v, _ := cmd.Flags().GetString("window"); v != "" {
				d, err := format.ParseDuration(v)
				if err != nil {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--window: %w", err))
				}
				windowSec = int64(d.Seconds())
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			a, err := newClientFromCmd(cmd).BudgetAnomalyCall(ctx, scope, value, windowSec)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.BudgetAnomaly{a}, nil)
			}
			rows := []kvRow{
				{"Scope", scope},
				{"Value", value},
				{"ZScore", fmt.Sprintf("%.4f", a.ZScore)},
				{"Mean", fmt.Sprintf("%.4f", a.Mean)},
				{"StdDev", fmt.Sprintf("%.4f", a.StdDev)},
				{"Samples", strconv.FormatInt(a.Samples, 10)},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, kvColumns())
		},
	}
	cmd.Flags().String("scope", "", "Scope: project | doctrine | stage | worker_id")
	cmd.Flags().String("value", "", "Scope value")
	cmd.Flags().String("window", "1h", "Sliding window (e.g. 1h, 24h, 7d)")
	return cmd
}

func budgetEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Recent budget event log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			sinceStr, _ := cmd.Flags().GetString("since")
			var sinceUnix int64
			if sinceStr != "" {
				t, err := format.ParseSince(sinceStr)
				if err != nil {
					return err
				}
				if !t.IsZero() {
					sinceUnix = t.Unix()
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			events, err := newClientFromCmd(cmd).BudgetEvents(ctx, sinceUnix, limit)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return r.(client.BudgetEvent).ID }},
				{Header: "TYPE", Field: func(r any) string { return r.(client.BudgetEvent).EventType }},
				{Header: "SCOPE", Field: func(r any) string { return r.(client.BudgetEvent).Scope }},
				{Header: "VALUE", Field: func(r any) string { return r.(client.BudgetEvent).Value }},
				{Header: "USD", Field: func(r any) string { return fmt.Sprintf("%.4f", r.(client.BudgetEvent).AmountUSD) }},
				{Header: "OCCURRED", Field: func(r any) string { return client.FormatUnix(r.(client.BudgetEvent).OccurredAt) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, events, cols)
		},
	}
}

func budgetPauseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause an axis+value scope (security-grade; --yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, _ := cmd.Flags().GetString("scope")
			value, _ := cmd.Flags().GetString("value")
			reason, _ := cmd.Flags().GetString("reason")
			yes, _ := cmd.Flags().GetBool("yes")
			if scope == "" || value == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope and --value required"))
			}
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm pause"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).BudgetPauseCall(ctx, client.BudgetPauseReq{
				Scope: scope, Value: value, Reason: reason,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "state=%s scope=%s value=%s\n", resp.State, resp.Scope, resp.Value)
			return nil
		},
	}
	cmd.Flags().String("scope", "", "Scope: project | doctrine | stage | worker_id")
	cmd.Flags().String("value", "", "Scope value")
	cmd.Flags().String("reason", "", "Free-form audit reason")
	cmd.Flags().Bool("yes", false, "Confirm pause")
	return cmd
}

func budgetResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume an axis+value scope (--yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, _ := cmd.Flags().GetString("scope")
			value, _ := cmd.Flags().GetString("value")
			yes, _ := cmd.Flags().GetBool("yes")
			if scope == "" || value == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope and --value required"))
			}
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm resume"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).BudgetResumeCall(ctx, client.BudgetResumeReq{
				Scope: scope, Value: value,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "state=%s scope=%s value=%s\n", resp.State, resp.Scope, resp.Value)
			return nil
		},
	}
	cmd.Flags().String("scope", "", "Scope")
	cmd.Flags().String("value", "", "Value")
	cmd.Flags().Bool("yes", false, "Confirm resume")
	return cmd
}

func budgetPauseModesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause-modes",
		Short: "Doctrine [budget].pause_mode catalog (set in zenswarm.toml; not a flag on 'zen budget pause')",
		Long: `Lists the pause-mode values the active doctrine recognises in
[budget].pause_mode (zenswarm.toml). These are CONFIGURATION values
read by the daemon at doctrine load — NOT command-line flags on
'zen budget pause'. Switching pause-mode requires editing the
project's zenswarm.toml [budget].pause_mode field and reloading the
daemon (or 'zen doctrine reload --yes').

The 'zen budget pause' command does NOT accept --mode; the active
mode is whichever the doctrine resolver selects (default: descriptive).

Review M-6: pre-fix the Short text read "Doctrine-resolved pause-mode
catalog" which operators interpreted as flags; this clarification
prevents that misread.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			modes := client.PauseModes()
			cols := []format.Column{
				{Header: "NAME", Field: func(r any) string { return r.(client.PauseMode).Name }},
				{Header: "DEFAULT", Field: func(r any) string { return strconv.FormatBool(r.(client.PauseMode).Default) }},
				{Header: "DESCRIPTION", Field: func(r any) string { return r.(client.PauseMode).Description }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, modes, cols)
		},
	}
}

func budgetRollupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollup",
		Short: "Aggregate spend over a time window (synthetic from events)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			sinceStr, _ := cmd.Flags().GetString("since")
			var sinceUnix int64
			if sinceStr != "" {
				t, err := format.ParseSince(sinceStr)
				if err != nil {
					return err
				}
				if !t.IsZero() {
					sinceUnix = t.Unix()
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			events, err := newClientFromCmd(cmd).BudgetEvents(ctx, sinceUnix, limit)
			if err != nil {
				return err
			}
			type rollupRow struct {
				Scope    string  `json:"scope" yaml:"scope"`
				Value    string  `json:"value" yaml:"value"`
				TotalUSD float64 `json:"total_usd" yaml:"total_usd"`
				Count    int     `json:"count" yaml:"count"`
			}
			byKey := map[string]*rollupRow{}
			var totalUSD float64
			for _, e := range events {
				k := e.Scope + "/" + e.Value
				row := byKey[k]
				if row == nil {
					row = &rollupRow{Scope: e.Scope, Value: e.Value}
					byKey[k] = row
				}
				row.TotalUSD += e.AmountUSD
				row.Count++
				totalUSD += e.AmountUSD
			}
			rows := make([]rollupRow, 0, len(byKey))
			for _, r := range byKey {
				rows = append(rows, *r)
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, rows, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Total: $%.4f (events=%d)\n\n", totalUSD, len(events))
			cols := []format.Column{
				{Header: "SCOPE", Field: func(r any) string { return r.(rollupRow).Scope }},
				{Header: "VALUE", Field: func(r any) string { return r.(rollupRow).Value }},
				{Header: "USD", Field: func(r any) string { return fmt.Sprintf("%.4f", r.(rollupRow).TotalUSD) }},
				{Header: "COUNT", Field: func(r any) string { return strconv.Itoa(r.(rollupRow).Count) }},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, cols)
		},
	}
	return cmd
}
