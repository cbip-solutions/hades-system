// SPDX-License-Identifier: MIT
package semantic

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// CaronteDispatcher is the C-2 single-egress seam (master §C-2). The semantic
// resolver routes its residual-tail LLM disambiguation (reflection / DI /
// dynamic dispatch) through this narrow interface; the daemon composition
// root wires the real *orchestrator.Orchestrator. Declaring the
// interface HERE (consumer-side) — not importing a concrete dispatcher —
// keeps internal/caronte persistence- and transport-agnostic (inv-hades-031)
// and preserves single-egress (inv-hades-088/236): every Caronte LLM call is a
// dispatcher.Forward, never a direct backend dial.
//
// The signature mirrors orchestrator.Orchestrator.Forward EXACTLY so the
// production type satisfies it without an adapter (anchor below).
//
// Implementations MUST be safe for concurrent invocation (the resolver may
// fan out tail disambiguations); the production orchestrator documents that
// guarantee.
type CaronteDispatcher interface {
	Forward(ctx context.Context, call orchestrator.Call) (*providers.TierResponse, error)
}

// inv-hades-236 compile anchor: the production *orchestrator.Orchestrator MUST
// satisfy CaronteDispatcher. If this stops compiling the daemon wiring is
// broken — fix the orchestrator signature or the wiring, do NOT relax this
// seam (that would breach single-egress). Mirrors the
// orchestrator.Forwarder ↔ *dispatcher.Dispatcher anchor pattern.
var _ CaronteDispatcher = (*orchestrator.Orchestrator)(nil)

type ResolveMode string

const (
	ModeVTA ResolveMode = "vta"

	ModeCHA ResolveMode = "cha"

	ModeStaleSnapshot ResolveMode = "stale_snapshot"
)

type ResolutionStats struct {
	CallEdges       int
	ImplementsEdges int
	LLMHintEdges    int
	ResolvedFuncs   int
	UnresolvedSites int
	Mode            ResolveMode
	Stale           bool
}

type Implementation struct {
	InterfaceID string
	ImplID      string
	Confidence  string
	Reachable   bool
}

type CallPathHop struct {
	FromID     string
	ToID       string
	Confidence string
	SiteFile   string
	SiteLine   int
	Depth      int
}
