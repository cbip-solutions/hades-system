// SPDX-License-Identifier: MIT
// Package cli — workforce.go.
//
// `zen workforce` exposes the in-flight workforce surface: live status,
// the OperatorGate state machine (pause/resume — security-grade), worker
// inspection, durable Kanban (checkpoints), fix-prompt queue, and the
// static spec catalog.
//
// Cobra layout (8 §6.1 entries; 9 cobra leaves):
//
// zen workforce
// status
// gate
// state
// pause --mode --reason --yes
// resume --reason --yes
// workers --status --limit
// checkpoints --task --limit
// specs
// list
// show <id>
//
// All inherit the universal flags from format.AttachFlags on the namespace.
//
// Note (Option A adaptation): the plan referred to a `subprocesses {list,
// inspect, kill}` triple but surfaces the canonical workforce
// primitive as `workers` (handlers.WorkforceWorkers). This wiring uses
// `workers` to match the daemon contract; spec §6.1 enumeration count
// remains 8 entries (status, gate{state,pause,resume}, workers, checkpoints,
// specs{list,show}).
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

var TestOnlyClientFactory func(uds string) *client.Client

type stderrDebugLogger struct {
	w interface{ Write([]byte) (int, error) }
}

func (d *stderrDebugLogger) Logf(format string, args ...any) {
	fmt.Fprintf(d.w, format, args...)
}

func newClientFromCmd(cmd *cobra.Command) *client.Client {
	uds, _ := cmd.Root().PersistentFlags().GetString("uds")
	if uds == "" {
		uds, _ = cmd.PersistentFlags().GetString("uds")
	}
	var c *client.Client
	if TestOnlyClientFactory != nil {
		c = TestOnlyClientFactory(uds)
	} else {
		c = client.New(uds)
	}

	if isVerboseSet(cmd) {
		c.SetDebugLogger(&stderrDebugLogger{w: cmd.ErrOrStderr()})
	}
	return c
}

func isVerboseSet(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup("verbose"); f != nil {
		v, _ := cmd.Flags().GetBool("verbose")
		return v
	}
	if f := cmd.PersistentFlags().Lookup("verbose"); f != nil {
		v, _ := cmd.PersistentFlags().GetBool("verbose")
		return v
	}
	return false
}

func NewWorkforceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workforce",
		Short: "Inspect and control the in-flight workforce (Plan 4)",
		Long: `Operator surface for workforce primitives:

  status         aggregated counts + gate state
  gate           pause/resume + state inspection (security-grade)
  workers        live worker rows (filterable by status)
  checkpoints    durable Kanban (read-only)
  specs          loaded WorkerSpec catalog`,
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(workforceStatusCmd())
	cmd.AddCommand(workforceGateCmd())
	cmd.AddCommand(workforceWorkersCmd())
	cmd.AddCommand(workforceCheckpointsCmd())
	cmd.AddCommand(workforceSpecsCmd())
	cmd.AddCommand(workforceFixPromptsCmd())
	return cmd
}

type kvRow struct {
	Key   string `json:"key" yaml:"key"`
	Value string `json:"value" yaml:"value"`
}

func kvColumns() []format.Column {
	return []format.Column{
		{Header: "FIELD", Field: func(r any) string { return r.(kvRow).Key }},
		{Header: "VALUE", Field: func(r any) string { return r.(kvRow).Value }},
	}
}

func workforceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show aggregated workforce health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			st, err := newClientFromCmd(cmd).WorkforceStatus(ctx)
			if err != nil {
				return err
			}
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.WorkforceStatusSnapshot{st}, nil)
			}
			rows := []kvRow{
				{"GateState", st.GateState},
				{"CanPause", strconv.FormatBool(st.CanPause)},
				{"CanResume", strconv.FormatBool(st.CanResume)},
				{"WorkersTotal", strconv.Itoa(st.WorkersTotal)},
				{"WorkersPending", strconv.Itoa(st.WorkersPending)},
				{"WorkersInProgress", strconv.Itoa(st.WorkersInProgress)},
				{"WorkersReview", strconv.Itoa(st.WorkersReview)},
				{"WorkersDone", strconv.Itoa(st.WorkersDone)},
				{"WorkersFailed", strconv.Itoa(st.WorkersFailed)},
				{"SpecsLoaded", strconv.Itoa(st.SpecsLoaded)},
				{"CheckpointsDepth", strconv.Itoa(st.CheckpointsDepth)},
				{"FixPromptsDepth", strconv.Itoa(st.FixPromptsDepth)},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, kvColumns())
		},
	}
}

func workforceGateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "OperatorGate state machine (state | pause | resume)",
	}
	cmd.AddCommand(workforceGateStateCmd())
	cmd.AddCommand(workforceGatePauseCmd())
	cmd.AddCommand(workforceGateResumeCmd())
	return cmd
}

func workforceGateStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state",
		Short: "Show current OperatorGate state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			st, err := newClientFromCmd(cmd).GateState(ctx)
			if err != nil {
				return err
			}
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.GateStateResp{st}, nil)
			}
			rows := []kvRow{
				{"State", st.State},
				{"CanPause", strconv.FormatBool(st.CanPause)},
				{"CanResume", strconv.FormatBool(st.CanResume)},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, kvColumns())
		},
	}
}

func workforceGatePauseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause workforce activity (security-grade; --mode required)",
		Long: `Halts new worker dispatch. Idempotent (already-paused state returns 200).

Modes:
  paused_descriptive   visible pause; emits notification
  paused_quiet         silent pause; no notification (automated test windows)
  paused_after_apply   pause kicks in after current Plan 5 apply commits`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			mode, _ := cmd.Flags().GetString("mode")
			if mode == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--mode required (paused_descriptive | paused_quiet | paused_after_apply)"))
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm pause (security-grade kill-switch)"))
			}
			reason, _ := cmd.Flags().GetString("reason")
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).GatePause(ctx, client.GatePauseReq{Mode: mode, Reason: reason})
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.GatePauseResp{resp}, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "state=%s paused=%t\n", resp.State, resp.Paused)
			return nil
		},
	}
	cmd.Flags().String("mode", "", "Pause mode: paused_descriptive | paused_quiet | paused_after_apply")
	cmd.Flags().String("reason", "", "Free-form audit reason (recorded in audit trail)")
	cmd.Flags().Bool("yes", false, "Confirm the security-grade pause")
	return cmd
}

func workforceGateResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume workforce activity (--yes confirmation required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm resume"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).GateResume(ctx)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.GateResumeResp{resp}, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "state=%s running=%t\n", resp.State, resp.Running)
			return nil
		},
	}
	cmd.Flags().Bool("yes", false, "Confirm resume")
	return cmd
}

func workforceWorkersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workers",
		Short: "List running and recent workers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			status, _ := cmd.Flags().GetString("status")
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).WorkforceWorkers(ctx, status, limit, offset)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return r.(client.WorkforceWorker).ID }},
				{Header: "SPEC", Field: func(r any) string { return r.(client.WorkforceWorker).SpecID }},
				{Header: "STATUS", Field: func(r any) string { return r.(client.WorkforceWorker).Status }},
				{Header: "TASK", Field: func(r any) string { return r.(client.WorkforceWorker).TaskID }},
				{Header: "THREAD", Field: func(r any) string { return r.(client.WorkforceWorker).ThreadID }},
				{Header: "STARTED", Field: func(r any) string { return client.FormatUnix(r.(client.WorkforceWorker).StartedAt) }},
				{Header: "UPDATED", Field: func(r any) string { return client.FormatUnix(r.(client.WorkforceWorker).UpdatedAt) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	cmd.Flags().String("status", "", "Filter by status (pending|in_progress|review|done|failed)")
	cmd.Flags().Int("offset", 0, "Pagination offset")
	return cmd
}

func workforceCheckpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoints",
		Short: "Read-only Kanban checkpoint view",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			task, _ := cmd.Flags().GetString("task")
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).WorkforceCheckpoints(ctx, task, limit, offset)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return r.(client.WorkforceCheckpoint).ID }},
				{Header: "TASK", Field: func(r any) string { return r.(client.WorkforceCheckpoint).TaskID }},
				{Header: "THREAD", Field: func(r any) string { return r.(client.WorkforceCheckpoint).ThreadID }},
				{Header: "CREATED", Field: func(r any) string { return client.FormatUnix(r.(client.WorkforceCheckpoint).CreatedAt) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	cmd.Flags().String("task", "", "Filter by task ID")
	cmd.Flags().Int("offset", 0, "Pagination offset")
	return cmd
}

func workforceSpecsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "specs",
		Short: "Loaded WorkerSpec catalog (list | show)",
	}
	cmd.AddCommand(workforceSpecsListCmd())
	cmd.AddCommand(workforceSpecsShowCmd())
	return cmd
}

func workforceSpecsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded WorkerSpec entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			variant, _ := cmd.Flags().GetString("variant")
			limit, _ := cmd.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).WorkforceSpecs(ctx, variant, limit, 0)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return r.(client.WorkforceSpec).ID }},
				{Header: "VARIANT", Field: func(r any) string { return r.(client.WorkforceSpec).Variant }},
				{Header: "TIER", Field: func(r any) string { return r.(client.WorkforceSpec).TaskTier }},
				{Header: "MODEL", Field: func(r any) string { return r.(client.WorkforceSpec).ModelClass }},
				{Header: "DOCTRINE", Field: func(r any) string { return r.(client.WorkforceSpec).DoctrineName }},
				{Header: "PROJECT", Field: func(r any) string { return r.(client.WorkforceSpec).ProjectID }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	cmd.Flags().String("variant", "", "Filter by variant")
	return cmd
}

func workforceSpecsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show full WorkerSpec by ID (filtered from list)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).WorkforceSpecs(ctx, "", 500, 0)
			if err != nil {
				return err
			}
			for _, s := range items {
				if s.ID == args[0] {
					opts := format.OptionsFromFlags(cmd)
					if opts.Format != "table" {
						return format.Render(cmd.OutOrStdout(), opts, []client.WorkforceSpec{s}, nil)
					}
					out := cmd.OutOrStdout()
					fmt.Fprintf(out, "ID:        %s\n", s.ID)
					fmt.Fprintf(out, "Variant:   %s\n", s.Variant)
					fmt.Fprintf(out, "Tier:      %s\n", s.TaskTier)
					fmt.Fprintf(out, "Model:     %s\n", s.ModelClass)
					fmt.Fprintf(out, "Doctrine:  %s\n", s.DoctrineName)
					fmt.Fprintf(out, "Project:   %s\n", s.ProjectID)
					fmt.Fprintf(out, "Created:   %s\n", client.FormatUnix(s.CreatedAt))
					if len(s.Tools) > 0 {
						fmt.Fprintln(out, "Tools:")
						for _, tool := range s.Tools {
							fmt.Fprintf(out, "  - %s\n", tool)
						}
					}
					return nil
				}
			}
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("spec %q not found", args[0]))
		},
	}
}

func workforceFixPromptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix-prompts",
		Short: "Show pending fix-prompt queue (L2→L3, L3→L4 handoffs)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			task, _ := cmd.Flags().GetString("task")
			limit, _ := cmd.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).WorkforceFixPrompts(ctx, task, limit, 0)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return r.(client.WorkforceFixPrompt).ID }},
				{Header: "TASK", Field: func(r any) string { return r.(client.WorkforceFixPrompt).TaskID }},
				{Header: "FROM_LAYER", Field: func(r any) string { return r.(client.WorkforceFixPrompt).FromLayer }},
				{Header: "CONSUMED", Field: func(r any) string { return strconv.FormatBool(r.(client.WorkforceFixPrompt).Consumed) }},
				{Header: "CREATED", Field: func(r any) string { return client.FormatUnix(r.(client.WorkforceFixPrompt).CreatedAt) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	cmd.Flags().String("task", "", "Filter by task ID")
	return cmd
}
