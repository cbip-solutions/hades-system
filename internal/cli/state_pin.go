// SPDX-License-Identifier: MIT
// Package cli — state_pin.go.
//
// `hades state pin <field> <value> --reason <X>` calls POST /v1/state/pin.
// Enforces invariant: --reason is both cobra.MarkFlagRequired and
// non-empty checked in RunE. The operator sees a confirmation prompt
// (privacy-by-default: blank input = abort, same as audit_chain_cold_archive.go).
//
// The daemon returns 204 No Content on success (StatePin returns nil error).
// The CLI renders a concise confirmation line: field=<X> value=<Y> pinned.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func newStatePinCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pin <field> <value>",
		Short: "Set a manual field in system-state.toml (invariant; emits HADES design event)",
		Args:  cobra.ExactArgs(2),
		Long: "pin sets a manual field in docs/system-state.toml and emits\nstate.manual_field_changed on the HADES design audit chain. The field MUST be\ntagged x-manual-field in docs/system-state.schema.json (daemon rejects\nunrecognised fields with 400).\n\n--reason is mandatory (invariant): it records the operator's rationale\nin the audit chain event and is visible in " +
			"`hades state history`" + `.

A confirmation prompt is shown before the daemon call.
Blank input or anything other than "y"/"yes" aborts without error.`,
		Example: "  hades state pin substrate_min_version 0.7.1 --reason \"worker runtime 0.7.0 has CVE-2026-X\"\n  hades state pin schema_version 24 --reason \"HADES design schema bump\"",

		RunE: func(cmd *cobra.Command, args []string) error {
			reason, _ := cmd.Flags().GetString("reason")
			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason required and must be non-empty (invariant)"))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "About to pin manual field %q = %q in docs/system-state.toml\n", args[0], args[1])
			fmt.Fprintf(out, "Reason: %s\n", reason)
			ok, err := promptYN(cmd.InOrStdin(), out, "Continue?")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(out, "Pin aborted by operator.")
				return nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			if err := newClientFromCmd(cmd).StatePin(ctx, client.StatePinReq{
				Field:  args[0],
				Value:  args[1],
				Reason: reason,
			}); err != nil {
				return err
			}
			fmt.Fprintf(out, "field=%s value=%s pinned\n", args[0], args[1])
			return nil
		},
	}
	c.Flags().String("reason", "", "Operator rationale for the pin (required, invariant)")
	_ = c.MarkFlagRequired("reason")
	return c
}
