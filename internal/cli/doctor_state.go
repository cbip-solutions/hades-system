// SPDX-License-Identifier: MIT
// Package cli — doctor_state.go
//
// `hades doctor state-system` delegates to StateProber declared in probe.go;
// 3 results:
//
// - state.last_regenerate_age
// - state.manual_field_count
// - state.missing_source_count
//
// Architecture (J-1 precedent):
//
// StateProber interface declared in probe.go (CLI side).
// Substrate prober.go files (internal/state/manifest/) are NOT modified
// per J-1 pattern.
// Production wiring in cmd/hades-ctld/main.go.
//
// # Naming
//
// Use="state-system" to avoid collision with:
// - `hades state`
// - `hades doctor` child commands from other Plans
// The cobra Use field is "state-system"; file is doctor_state.go.
//
// Boundary (inv-hades-031):
//
// This file imports only cli-internal types + cobra + context + stdlib.
// Does NOT import internal/state/manifest concrete types.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

// RunSystemStateProbe orchestrates the system-state doctor check (release
// Task J-2, spec §6.2).
//
// Delegates to DoctorDeps.StateProber.Probe(ctx) and returns the resulting
// ProbeResult slice unchanged. Returns a non-nil error if StateProber is nil
// (mis-wired deps) or ctx is already cancelled.
//
// Typical result count: 3 (last_regenerate_age + manual_field_count +
// missing_source_count). Callers MUST NOT branch on len(results) == 3.
//
// Doctrine reference: last_regenerate_age thresholds per inv-hades-149:
//
// max-scope doctrine: 24h; default doctrine: 168h; capa-firewall: 24h.
// WARN at 1×-2× threshold; FAIL at >2× threshold (stale TOML).
func RunSystemStateProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.StateProber == nil {
		return nil, errors.New("RunSystemStateProbe: StateProber is nil — wire DoctorDeps.StateProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return deps.StateProber.Probe(pctx), nil
}

func NewDoctorStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state-system",
		Short: "System state health (Plan 9: regenerate age + manual fields + missing sources per inv-hades-149)",
		Long: `Run 3 system-state checks (spec §6.2):

  state.last_regenerate_age    OK if <doctrine threshold (max-scope=24h, default=168h, capa-firewall=24h);
                               WARN 1×-2×; FAIL >2× — flags stale TOML per inv-hades-149
  state.manual_field_count     informational; reports count + names of x-manual-field: true fields
  state.missing_source_count   OK if 0; WARN if 1-2; FAIL if 3+ — flags broken auto-derive walker

Exit codes:
  0  every check OK (or only WARNs without --strict)
  1  any check FAIL OR (any WARN AND --strict)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := RunSystemStateProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("system-state probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "System state:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
