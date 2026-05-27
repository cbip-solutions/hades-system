// SPDX-License-Identifier: MIT
// cmd/hades-ctld/doctrine_eval_wiring.go
//
// into the daemon composition root. The mcpgateway.Dispatcher consumes
// the evaluator via the DoctrineEvaluator interface declared in
// internal/daemon/mcpgateway/dispatcher.go; this file ships the
// production adapter that bridges eval.Evaluator → mcpgateway.DoctrineEvaluator
// + the production TierPolicy that reads from the active doctrine
// accessor (internal/doctrine/active).
//
// Boundary (invariant): this file lives in cmd/hades-ctld (the
// composition root) — it consumes mcpgateway + doctrine/eval +
// internal/onboard/mcp, the only place that's allowed. None of those
// three packages depend on each other; the daemon root wires them via
// interface seams.
//
// escalation matrix (high→confirm, medium→audit, low→allow) is
// catalog-driven instead of the pre-canonical "always unknown" stub.
// ADR-0085 + CHANGELOG promise the per-MCP risk-tier surface ships in
// operator override via TOML `[capa_firewall.tiers]` namespace remains
// out of scope; documented in CHANGELOG + ADR-0085.
package main

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/eval"
	"github.com/cbip-solutions/hades-system/internal/onboard/mcp"
)

type dispatcherDoctrineEvalAdapter struct {
	e *eval.RuntimeEvaluator
}

func (a *dispatcherDoctrineEvalAdapter) EvaluateCall(
	ctx context.Context, mcpName, toolName string, params any,
) (decisionLabel string, evidence string, err error) {
	decision, evidence, err := a.e.EvaluateCall(ctx, mcpName, toolName, params)
	return decision.String(), evidence, err
}

type tierResolver func(mcpName string) string

func catalogTierResolver(mcpName string) string {
	entry, ok := mcp.ByName(mcpName)
	if !ok {
		return "unknown"
	}
	return entry.RiskTier
}

type activeTierPolicy struct {
	resolveTier tierResolver
}

func newActiveTierPolicy() *activeTierPolicy {
	return &activeTierPolicy{resolveTier: catalogTierResolver}
}

func (p *activeTierPolicy) RiskTierFor(mcpName, _ string) string {
	if p.resolveTier == nil {
		return "unknown"
	}
	return p.resolveTier(mcpName)
}

func (p *activeTierPolicy) ActiveProfile() string {
	sch := active.Active()
	if sch == nil {
		return "default"
	}
	if name, ok := active.NameFor(sch); ok {
		return name
	}
	return "default"
}

func (p *activeTierPolicy) AllowList() []string { return nil }

func (p *activeTierPolicy) DenyList() []string { return nil }

func buildDoctrineEvaluator(emitter eval.Emitter) mcpgateway.DoctrineEvaluator {
	if emitter == nil {
		return nil
	}
	runtime := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  newActiveTierPolicy(),
		Emitter: emitter,
	})
	return &dispatcherDoctrineEvalAdapter{e: runtime}
}

type auditEmitterAdapter struct {
	a mcpgateway.AuditEmitter
}

func (a *auditEmitterAdapter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	if a == nil || a.a == nil {
		return "", nil
	}
	a.a.Emit(eventType, payload)
	return "", nil
}

func newAuditEmitterAdapter(a mcpgateway.AuditEmitter) eval.Emitter {
	return &auditEmitterAdapter{a: a}
}
