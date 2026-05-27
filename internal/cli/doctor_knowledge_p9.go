// SPDX-License-Identifier: MIT
// Package cli — doctor_knowledge_p9.go
//
// Adds `zen doctor knowledge aggregator` subcommand — a leaf under the release
// `zen doctor knowledge` group. RunKnowledgeAggregatorProbe delegates to
// AggregatorProber declared in probe.go; 4 results:
//
// - knowledge.aggregator.sqlite_vec_loaded
// - knowledge.aggregator.embedding_model_active
// - knowledge.aggregator.fts5_index_size
// - knowledge.aggregator.pinned_notes_count
//
// Architecture (J-1 precedent):
//
// AggregatorProber interface declared in probe.go (CLI side).
// Substrate prober.go files are NOT modified per J-1 pattern.
// Production wiring in cmd/zen-swarm-ctld/main.go assigns the concrete
// substrate adapter satisfying AggregatorProber.
//
// Boundary:
//
// This file imports only cli-internal types + cobra + context + stdlib.
// Does NOT import internal/knowledge/aggregator concrete types.
//
// # Naming
//
// File suffix _p9 because doctor_knowledge.go already ships
// ( Task J-3: 5-aspect KnowledgeProber for the per-project knowledge
// index). This file extends the same `zen doctor knowledge` group with the
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

// RunKnowledgeAggregatorProbe orchestrates the knowledge.aggregator doctor
// check.
//
// Delegates to DoctorDeps.AggregatorProber.Probe(ctx) and returns the
// resulting ProbeResult slice unchanged. Returns a non-nil error if
// AggregatorProber is nil (mis-wired deps) or ctx is already cancelled.
//
// Typical result count: 4 (sqlite_vec_loaded + embedding_model_active +
// fts5_index_size + pinned_notes_count). Callers MUST NOT branch on
// len(results) == 4; the contract is "any non-zero count of valid
// ProbeResults" so a future probe addition does not break callers.
func RunKnowledgeAggregatorProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.AggregatorProber == nil {
		return nil, errors.New("RunKnowledgeAggregatorProbe: AggregatorProber is nil — wire DoctorDeps.AggregatorProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return deps.AggregatorProber.Probe(pctx), nil
}

func NewDoctorKnowledgeAggregatorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "aggregator",
		Short: "Knowledge aggregator health (Plan 9: sqlite-vec + embedding model + FTS5 index + pinned notes)",
		Long: `Run 4 knowledge aggregator checks (spec §6.2):

  knowledge.aggregator.sqlite_vec_loaded      OK if extension loaded at daemon start; FAIL otherwise
  knowledge.aggregator.embedding_model_active OK if Mac MPS or daemon CPU model loaded; WARN if fallback; FAIL if neither
  knowledge.aggregator.fts5_index_size        OK if size matches pinned-notes × avg-note-size (±20%); WARN if drift >20%; FAIL if index missing
  knowledge.aggregator.pinned_notes_count     informational; reports count + last-promote timestamp

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
			probes, err := RunKnowledgeAggregatorProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("knowledge.aggregator probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Knowledge aggregator:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
