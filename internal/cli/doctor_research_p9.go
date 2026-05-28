// SPDX-License-Identifier: MIT
// Package cli — doctor_research_p9.go
//
// Filename uses _p9 suffix because doctor_research.go already ships from
// reachability and size checks). This file adds the HADES design RunResearchCacheProbe
// orchestrator and `hades doctor research cache` subcommand.
//
// `hades doctor research cache` delegates to ResearchCacheProber declared
// in probe.go; 5 results:
//
// - research.cache.hit_rate
// - research.cache.volume
// - research.cache.freshness_lag
// - research.cache.revalidation_queue_depth
// - research.cache.stuck_queries_count
//
// Architecture (J-1 precedent):
//
// ResearchCacheProber interface declared in probe.go (CLI side).
// Substrate prober.go files (internal/research/cache/) are NOT modified
// per J-1 pattern.
// Production wiring in cmd/hades-ctld/main.go.
//
// Boundary (invariant):
//
// This file imports only cli-internal types + cobra + context + stdlib.
// Does NOT import internal/research/cache concrete types.
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

// RunResearchCacheProbe orchestrates the research.cache doctor check (HADES design
// task, spec §6.2).
//
// Delegates to DoctorDeps.ResearchCacheProber.Probe(ctx) and returns the
// resulting ProbeResult slice unchanged. Returns a non-nil error if
// ResearchCacheProber is nil (mis-wired deps) or ctx is already cancelled.
//
// Typical result count: 5 (hit_rate + volume + freshness_lag +
// revalidation_queue_depth + stuck_queries_count). Callers MUST NOT branch
// on len(results) == 5.
func RunResearchCacheProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.ResearchCacheProber == nil {
		return nil, errors.New("RunResearchCacheProbe: ResearchCacheProber is nil — wire DoctorDeps.ResearchCacheProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return deps.ResearchCacheProber.Probe(pctx), nil
}

func NewDoctorResearchCacheCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cache",
		Short: "Research cache health (HADES design: hit rate + volume + freshness lag + revalidation queue + stuck queries)",
		Long: `Run 5 research cache checks (spec §6.2):

  research.cache.hit_rate                rolling 24h; OK >50%; WARN 25-50%; FAIL <25% (broken cache)
  research.cache.volume                  informational; total dispatch_count + total findings + CAS size
  research.cache.freshness_lag           median age of cached findings vs TTL doctrine
  research.cache.revalidation_queue_depth OK <10; WARN 10-50; FAIL >50 (revalidation worker stuck)
  research.cache.stuck_queries_count     count of queries with status=pending >1h; OK if 0; FAIL if any

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
			probes, err := RunResearchCacheProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("research.cache probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Research cache:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
