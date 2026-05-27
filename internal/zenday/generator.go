// SPDX-License-Identifier: MIT
// Package zenday — method-bound façade over the per-call free
// functions.
//
// Generator is the canonical HTTP handler + CLI consumption
// surface. It closes over the dependency bundle so handlers and
// dispatchers can hold a single value-type and invoke briefs without
// re-assembling deps each call.
//
// The free-function form (`zenday.GenerateMorningBrief(ctx, deps,
// force)` etc.) is preserved for direct callers and tests; the
// Generator struct is the method-bound dispatch layer that closes over
// MorningDeps + EODDeps + CheckPendingDeps at construction time.
//
// review CRITICAL #6 reconciliation (2026-05-01): HTTP
// handlers (`internal/daemon/handlers/zenday.go`) reference
// `zenday.NewGenerator(...)` returning `*Generator` with methods
// `(*Generator).GenerateMorningBrief` / `(*Generator).GenerateEODDigest`
// / `(*Generator).CheckPending`. Both surfaces live alongside each
// other.
//
// Construction is pure data-shape: NewGenerator does not validate the
// supplied deps. A daemon-bootstrap caller that composes Generator
// before optional deps are ready (e.g. cost-ledger lazy init) gets a
// non-nil *Generator that may fail at first method invocation rather
// than at construction — the bootstrap ordering is the operator's
// responsibility, not the façade's.
package zenday

import "context"

type Generator struct {
	morningDeps      MorningDeps
	eodDeps          EODDeps
	checkPendingDeps CheckPendingDeps
}

type GeneratorDeps struct {
	Morning MorningDeps

	EOD EODDeps

	CheckPending CheckPendingDeps
}

func NewGenerator(deps GeneratorDeps) *Generator {
	return &Generator{
		morningDeps:      deps.Morning,
		eodDeps:          deps.EOD,
		checkPendingDeps: deps.CheckPending,
	}
}

func (g *Generator) GenerateMorningBrief(ctx context.Context, force bool) (BriefDoc, error) {
	return GenerateMorningBrief(ctx, g.morningDeps, force)
}

func (g *Generator) GenerateEODDigest(ctx context.Context, force bool) (BriefDoc, error) {
	return GenerateEODDigest(ctx, g.eodDeps, force)
}

func (g *Generator) CheckPending(ctx context.Context) (BriefDoc, error) {
	return CheckPending(ctx, g.checkPendingDeps)
}
