// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/orchestrator.go
//
// Orchestrator is the public entry point for LLM traffic from zen-swarm-side
// consumers (anthropic_proxy handler in Plan 2/3, Plan 4 MCPs, Plan 5 workforce
// orchestrator subagents). It accepts a high-level Call, resolves the routing
// profile (explicit on the call OR daemon default), injects context-keyed
// X-Zen-Project / X-Zen-Session / X-Zen-Profile headers, and forwards to the
// dispatcher.
//
// Boundary (inv-zen-031): this package MUST NOT import internal/store. The
// orchestrator talks to a Forwarder interface (satisfied by *dispatcher.Dispatcher
// in production wiring); the dispatcher itself bridges to the store via
// dispatcheradapter (Phase B-7). Keeping the orchestrator persistence-agnostic
// is what makes the Plan 3 architecture testable end-to-end with in-memory
// fakes.
//
// Concurrency Orchestrator holds no mutable state after construction
// (forwarder + defaultProfile are read-only). Forward is therefore safe for
// concurrent invocation from arbitrarily many goroutines as long as the
// supplied Forwarder is. The Plan 3 dispatcher documents this guarantee at
// its package level.
//
// Phase scope (B-6): profile resolution + dispatcher invocation. Phase E
// adds a budget pre-check that consults the Plan 4 budget MCP `cap_status`
// before dispatch; that integration replaces the no-op pre-check stub here
// without changing the public surface.

package orchestrator

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// Forwarder is the contract Orchestrator depends on. It deliberately mirrors
// dispatcher.Dispatcher.Forward so the production *dispatcher.Dispatcher
// satisfies it without an adapter; tests substitute an in-memory fake.
//
// Implementations MUST be safe for concurrent invocation (the orchestrator
// fans out one Forward per inbound HTTP request). The Plan 3 dispatcher
// documents this guarantee at its package level.
type Forwarder interface {
	Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error)
}

// Compile-time guard: production *dispatcher.Dispatcher MUST satisfy
// Forwarder. If this stops compiling the daemon wiring is broken;
// fix the dispatcher signature, do not relax the orchestrator interface.
var _ Forwarder = (*dispatcher.Dispatcher)(nil)

type Call struct {
	Project   string
	SessionID string
	Profile   string

	Model string

	Method  string
	Path    string
	Headers map[string]string
	Body    []byte

	IdempotencyKey string
	ConversationID string
}

type Orchestrator struct {
	forwarder      Forwarder
	defaultProfile string
}

func New(forwarder Forwarder, defaultProfile string) *Orchestrator {
	if forwarder == nil {
		panic("orchestrator.New: forwarder is required")
	}
	return &Orchestrator{
		forwarder:      forwarder,
		defaultProfile: defaultProfile,
	}
}

func (o *Orchestrator) Forward(ctx context.Context, call Call) (*providers.TierResponse, error) {
	profile := call.Profile
	if profile == "" {
		profile = o.defaultProfile
	}
	ctx = dispatcher.WithProject(ctx, call.Project)
	ctx = dispatcher.WithSession(ctx, call.SessionID)
	ctx = dispatcher.WithProfile(ctx, profile)

	req := providers.TierRequest{

		Method: call.Method,
		Path:   call.Path,

		Headers: dispatcher.MergeHeaders(ctx, call.Headers),
		Body:    call.Body,

		Project:   call.Project,
		SessionID: call.SessionID,
		Profile:   profile,
		Model:     call.Model,

		IdempotencyKey: call.IdempotencyKey,
		ConversationID: call.ConversationID,
	}
	return o.forwarder.Forward(ctx, req)
}
