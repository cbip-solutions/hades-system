// SPDX-License-Identifier: MIT
// Package cli — doctor_ecosystem.go
//
// Reports per-ecosystem DB size, package/version/chunk counts, invariant
// 4-state budget classification, release F CAS blobs shared, cron worker
// PID, last upstream-poll + weekly-sweep timestamps, symbol-index health,
// and verifier live-cmd health (go doc / pip show / npm view / cargo doc).
//
// Architecture follows release Task J-2 (mirror of doctor_state.go):
//
// - EcosystemProber interface declared in probe.go (CLI side)
// - RunEcosystemProbe orchestrator declared here
// - Production wiring in cmd/hades-ctld/main.go ( daemon-init or
// follow-up); buildDoctorDeps currently returns DoctorDeps with
// EcosystemProber=nil — RunEcosystemProbe surfaces that as a non-nil
// error so mis-wired daemon compositions fail loudly at first call
// rather than emitting an empty report
// - Tests inject fakeEcosystemProberG4 (zero internal/research/ecosystem
// import; invariant clean)
//
// Boundary (invariant): this file imports only cli-internal types + cobra +
// context + stdlib. Does NOT import internal/research/ecosystem concrete types.
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

// RunEcosystemProbe orchestrates the ecosystem doctor check (release
// Task G-4, spec §5 doctor surface).
//
// Delegates to DoctorDeps.EcosystemProber.Probe(ctx) within a 10-second
// inner deadline (the outer subcommand wraps with a 30-second timeout; the
// inner deadline guards against a single misbehaving probe hanging the whole
// invocation).
//
// Returns a non-nil error if:
//
// - ctx is already cancelled on entry (per probe.go contract — propagates
// the operator's interrupt signal up the call stack).
// - DoctorDeps.EcosystemProber is nil. The error message names the field
// so the operator's first remediation step is "wire
// DoctorDeps.EcosystemProber in cmd/hades-ctld/main.go" (matching
// the J-1/J-2 nil-deps error surface).
//
// Typical result count: ≥15 (see EcosystemProber docstring in probe.go).
// Callers MUST NOT branch on len(results) == N; the contract is "any non-zero
// count of valid ProbeResults" so future probe additions do not break callers.
func RunEcosystemProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.EcosystemProber == nil {
		return nil, errors.New("RunEcosystemProbe: EcosystemProber is nil — wire DoctorDeps.EcosystemProber in cmd/hades-ctld/main.go")
	}

	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return deps.EcosystemProber.Probe(pctx), nil
}

func NewDoctorEcosystemCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ecosystem",
		Short: "Ecosystem RAG health (Plan 14: DB sizes + budget + cron + symbol-index + verifier)",
		Long: `Run ecosystem RAG health checks (spec §5 doctor surface):

  ecosystem.{go,python,typescript,rust}.db_size   per-ecosystem DB size on disk
  ecosystem.budget                                 storage budget state (Green/Yellow/Red/Overflow)
  ecosystem.cas_blobs_shared                       Plan 9 F CAS blob dedup count + total size
  ecosystem.last_upstream_poll                     last 6h upstream poll timestamp (from cron)
  ecosystem.last_weekly_sweep                      last Sunday 03:00 integrity sweep timestamp
  ecosystem.cron.pid                               hades-docs-cron worker PID (or "not running")
  ecosystem.symbol_index.count                     in-memory symbol-existence set cardinality × 4 eco
  ecosystem.symbol_index.last_rebuild              last weekly symbol-index rebuild timestamp
  ecosystem.verifier.go                            live: go doc reachable + non-empty response
  ecosystem.verifier.python                        live: pip show reachable + non-empty response
  ecosystem.verifier.npm                           live: npm view reachable + non-empty response
  ecosystem.verifier.cargo                         live: cargo doc reachable + non-empty response

Budget states (inv-hades-199):
  GREEN     < 32 GB (< 80% of 40 GB target)   no action
  YELLOW    32-40 GB                          alert only; hades docs prune when convenient
  RED       40-60 GB                          new ingest blocked; prune required
  OVERFLOW  > 60 GB                           all writes blocked; operator must prune immediately

The --strict parent flag escalates Warn rows to non-zero exit so CI gates
can fail on the Yellow alert-only band.

Exit codes:
  0  every check OK (or only WARNs without --strict)
  1  any check FAIL OR (any WARN AND --strict)
  2  unrecoverable: prober wiring, transport`,
		Example: " # Probe the ecosystem RAG substrate\n  hades doctor ecosystem\n\n # CI gate: fail on Warn rows (Yellow budget band) too\n  hades doctor ecosystem --strict",

		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			probes, err := RunEcosystemProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("ecosystem probe: %w", err))
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Ecosystem:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
