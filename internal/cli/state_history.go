// SPDX-License-Identifier: MIT
// Package cli — state_history.go (Plan 9 Phase I Task I-10).
//
// `zen state history [--field <name>]` calls GET /v1/state/history and
// renders manual field change events from the Plan 9 chain as a table.
// Optional --field scopes output to one field name.
//
// Wire type: client.StateChange (Field, OldValue, NewValue, Reason, At, OperatorID).
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func newStateHistoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "history",
		Short: "Walk Plan 9 chain showing manual field changes",
		Long: `history calls GET /v1/state/history and renders state.manual_field_changed
events in chronological order. Use --field to scope output to one field.

Columns: FIELD | OLD | NEW | AT | REASON | OPERATOR`,
		Example: `  zen state history
  zen state history --field substrate_min_version
  zen state history --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			field, _ := cmd.Flags().GetString("field")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).StateHistory(ctx, field)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "FIELD", Field: func(r any) string { return r.(client.StateChange).Field }},
				{Header: "OLD", Field: func(r any) string { return r.(client.StateChange).OldValue }},
				{Header: "NEW", Field: func(r any) string { return r.(client.StateChange).NewValue }},
				{Header: "AT", Field: func(r any) string { return client.FormatUnix(r.(client.StateChange).At) }},
				{Header: "REASON", Field: func(r any) string { return r.(client.StateChange).Reason }},
				{Header: "OPERATOR", Field: func(r any) string { return r.(client.StateChange).OperatorID }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	format.AttachFlags(c)
	c.Flags().String("field", "", "Filter by field name")
	return c
}
