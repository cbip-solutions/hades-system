// SPDX-License-Identifier: MIT
// Package cli — doctor_adr.go
//
// `zen doctor adr index` delegates to ADRProber declared in probe.go;
// 3 results:
//
//   - adr.index.dual_manifest_freshness
//   - adr.index.json_schema_validation_status
//   - adr.index.id_collision_count
//
// Architecture (J-1 precedent):
//
//	ADRProber interface declared in probe.go (CLI side).
//	Substrate prober.go files (internal/adr/) are NOT modified per J-1 pattern.
//	Production wiring in cmd/zen-swarm-ctld/main.go.
//
// Naming
//
//	doctor_adr.go is a new file — Plan 4's doctor_audit.go covers audit events;
//	there is no prior Plan doctor_adr.go. No _p9 suffix needed.
//
// Boundary (inv-zen-031):
//
//	This file imports only cli-internal types + cobra + context + stdlib.
//	Does NOT import internal/adr concrete types.
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

// RunAdrIndexProbe orchestrates the adr.index doctor check (Plan 9 Phase J
// Task J-2, spec §6.2).
//
// Delegates to DoctorDeps.ADRProber.Probe(ctx) and returns the resulting
// ProbeResult slice unchanged. Returns a non-nil error if ADRProber is nil
// (mis-wired deps) or ctx is already cancelled.
//
// Typical result count: 3 (dual_manifest_freshness + json_schema_validation_status
// + id_collision_count). Callers MUST NOT branch on len(results) == 3.
func RunAdrIndexProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.ADRProber == nil {
		return nil, errors.New("RunAdrIndexProbe: ADRProber is nil — wire DoctorDeps.ADRProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return deps.ADRProber.Probe(pctx), nil
}

func NewDoctorAdrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adr",
		Short: "ADR index health (Plan 9: dual-manifest freshness + JSON schema + ID collision)",
		Long: `ADR index health checks per Plan 9 Phase J spec §6.2.

Sub-commands:
  index — dual-manifest freshness + JSON schema validation + ID collision count

Each check returns OK / WARN / FAIL severity with doctrine-tuned thresholds.
Use --strict (inherited from parent) to promote WARN to FAIL-equivalent for CI gating.

ADRProber interface declared in internal/cli/probe.go; production wiring in
cmd/zen-swarm-ctld/main.go. Until wired, invoking a sub-command returns an error.`,
	}
	cmd.AddCommand(newDoctorAdrIndexCmd())
	return cmd
}

func newDoctorAdrIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "ADR index checks (dual-manifest freshness + JSON schema validation + ID collision count)",
		Long: `Run 3 ADR index checks (spec §6.2):

  adr.index.dual_manifest_freshness       regenerate-and-diff against current _index.json + _graph.json
  adr.index.json_schema_validation_status OK if all 39+ ADRs valid; WARN if 1-3 violations; FAIL if >3
  adr.index.id_collision_count            OK if 0; FAIL if any ID collision

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
			probes, err := RunAdrIndexProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("adr.index probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "ADR index:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
