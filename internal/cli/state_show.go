// SPDX-License-Identifier: MIT
// Package cli — state_show.go (Plan 9 Phase I Task I-10).
//
// `zen state show` calls GET /v1/state/show and renders the StateManifest.
// The manifest exposes TomlContent (raw TOML) plus convenience scalars:
// LastRegenerateUnix, ManualFieldCount, MissingSourceCount.
//
// The TOML body is printed verbatim below the metadata header so operators
// can inspect field values without a separate cat call.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func newStateShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print system-state.toml manifest metadata + TOML body",
		Long: `show calls GET /v1/state/show and renders the StateManifest.
Metadata header (last regenerate, manual field count) is printed above the
raw TOML body for quick inspection.`,
		Example: "  zen state show",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			m, err := newClientFromCmd(cmd).StateShow(ctx)
			if err != nil {
				return err
			}
			return renderStateManifest(cmd, m)
		},
	}
}

func renderStateManifest(cmd *cobra.Command, m client.StateManifest) error {
	out := cmd.OutOrStdout()
	var lastRegen string
	if m.LastRegenerateUnix > 0 {
		lastRegen = client.FormatUnix(m.LastRegenerateUnix)
	} else {
		lastRegen = "never"
	}
	fmt.Fprintf(out, "last_regenerate:    %s\n", lastRegen)
	fmt.Fprintf(out, "manual_field_count: %d\n", m.ManualFieldCount)
	if m.MissingSourceCount > 0 {
		fmt.Fprintf(out, "missing_sources:    %d\n", m.MissingSourceCount)
	}
	if m.TomlContent != "" {
		fmt.Fprintln(out, "---")
		fmt.Fprint(out, m.TomlContent)
	}
	return nil
}
