// SPDX-License-Identifier: MIT
// Package main — orchestrator_wiring.go ( Task 17 cutover:
// the 2-tier hard-wire is gone; the dispatcher now resolves a profile to
// an ordered provider-name cascade against the runtime registry).
//
// Assembles the full LLM-traffic chain at daemon boot:
//
// bypass.Client
// ↓ (adapter: bypass.Request ↔ providers.TierRequest semantics)
// BypassClient impl
// ↓
// providers.BypassBackend ────┐
// │
// providers.toml cascade ──────┤── dispatcher.Dispatcher ← AsyncEmitter
// (anthropic-paygo, gemini, │ → costLedgerSink (cost_ledger)
// deepseek-direct, ollama, │ ← *CircuitBreaker (per-Name)
// moonshot-kimi, …) │ ← *RecoveryScheduler
// │
// ↓
// orchestrator.Orchestrator
// ↓
// Server.SetOrchestrator → POST /v1/messages
//
// The cascade composition lives in *config.ProfileResolver (built-in
// defaults ∪ profiles.toml ∪ projects.toml[orchestrator] ∪
// .zen-swarm.toml — merge per invariant). The runtime registry
// (*providers.Registry) is built by providers_init.go from
// ~/.config/zen-swarm/providers/providers.toml; the daemon-startup gate
// invariant (verifyCascadeCompleteness in main.go) refuses to start
// if any cascade names an unregistered provider.
//
// Master frozen contract C5: the bypass backend is registered
// HERE, not in providers_init.go — it needs the *bypass.Client built by
// main.go's bootstrap, which providers_init deliberately does not touch.
// So buildOrchestrator owns one and only one Register() call: the
// "bypass" entry. Every other backend is registered by providers_init
// from providers.toml.
//
// Graceful-degradation contract preserved end-to-end:
//
// - bypass.Client unavailable (no creds, no config, Keychain locked) →
// "bypass" backend is registered with a disabled stub that returns
// ErrTierUnavailable on every Forward. The cascade simply skips it.
// - A providers.toml entry whose Keychain key is absent → providers_init
// registers it as a disabled stub (cascade skips, `zen providers
// verify` surfaces it). Same end-shape as the bypass-disabled path.
// - Every backend in a cascade fails → dispatcher returns
// ErrAllTiersUnavailable; the proxy maps it to 503 (same operator-
// visible behaviour as "/v1/messages returns 503" contract).
//
// invariant: this file does the cross-package wiring (orchestrator +
// dispatcher + providers + dispatcheradapter + bypass.Client) that no
// internal/* package is allowed to do itself.
//
// PHASE D-6 KILL: noopBreaker was search-and-replaced with the real
// *orchestrator.CircuitBreaker.
// The dispatcher consults the live state machine (closed → suspect →
// open) on every Permit and updates it on every Record{Success,Failure}.
// The breaker's recovery-probe loop is the *orchestrator.RecoveryScheduler
// constructed alongside; its lifecycle is owned by Server.
//
// PLAN 16 T17 KILL: the `tier1/tier2` hard-wire + the
// ZEN_OPENCLAUDE_ENDPOINT/TOKEN env-var path + the inlineTwoTier*
// compile-keep shims + the noopCostSink placeholder — all deleted.
// CostSink path now lands in cost_ledger via costLedgerSink (
// Task 18).
//
// PHASE 8 PLACEHOLDER: CircuitBreakerConfig is constructed with zero values
// here so NewCircuitBreaker applies its defaults (FailureThreshold=3,
// Window=5m, Cooldown=10m). doctrine-implementation will read the
// per-doctrine TOML schema and override these knobs. Until then defaults
// are the contract.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type disabledBypassClient struct{ reason string }

func (d *disabledBypassClient) ForwardRaw(_ context.Context, _ []byte, _ map[string]string, _ string) ([]byte, int, time.Duration, error) {
	return nil, 0, 0, fmt.Errorf("%w: bypass tier disabled: %s", providers.ErrTierUnavailable, d.reason)
}

func (d *disabledBypassClient) Health(_ context.Context) error {
	return fmt.Errorf("%w: bypass tier disabled: %s", providers.ErrTierUnavailable, d.reason)
}

type orchestratorNotifierAdapter struct {
	n *daemon.Notifier
}

func (a orchestratorNotifierAdapter) NotifyINFO(title, body, source string) {
	_, _ = a.n.Dispatch(context.Background(), "INFO", title, body, source)
}

func (a orchestratorNotifierAdapter) NotifyWARN(title, body, source string) {
	_, _ = a.n.Dispatch(context.Background(), "WARN", title, body, source)
}

func (a orchestratorNotifierAdapter) NotifyCRITICAL(title, body, source string) {
	_, _ = a.n.Dispatch(context.Background(), "CRITICAL", title, body, source)
}

var _ orchestrator.Notifier = orchestratorNotifierAdapter{}

type Built struct {
	Orchestrator      *orchestrator.Orchestrator
	CostAdapter       *dispatcheradapter.Adapter
	CostCounters      *orchestrator.CostCounters
	RecoveryScheduler *orchestrator.RecoveryScheduler
	PinOverrides      *orchestrator.PinOverrides
	PaygSafety        *orchestrator.PaygSafety

	CircuitBreaker *orchestrator.CircuitBreaker

	Tiers []providers.TierBackend
	Close func()
}

type buildOrchestratorDeps struct {
	Store *store.Store

	Notifier orchestrator.Notifier

	Registry *providers.Registry

	Resolver *config.ProfileResolver
}

// buildOrchestrator assembles the full LLM-traffic chain plus the
// I-5 operator-facing pin overrides + PAYG safety net.
//
// The pre- positional signature (bypassClient, st, notifier) is
// gone — at 5 inputs a struct (buildOrchestratorDeps) is materially
// cleaner. Registry + Resolver are now load-bearing: the dispatcher
// resolves a cascade per request against deps.Registry / deps.Resolver
// (no more hard-coded tier1/tier2).
//
// Compatibility contract: a disabled "bypass" backend is registered into
// deps.Registry HERE for operator profiles created before the
// sidecar extraction. The real Tier 1 provider is "bypass-sidecar", wired by
// dispatcheradapter.RegisterSidecars in main.go when sidecars.toml declares a
// healthy localhost sidecar.
//
// Returns Built (see field docstrings above). Never returns an error:
// every input shape (including nil BypassClient, nil Notifier) produces
// a runnable chain. Graceful-degradation states are runtime
// ErrTierUnavailable / ErrAllTiersUnavailable surfaced by Forward, mapped
// by the proxy to 503.
//
// invariant boundary: orchestrator + dispatcher + providers MUST NOT
// import internal/store. dispatcheradapter is the bridge; PinStoreAdapter
// (I-4) is its sibling for the PinStore path (Go method-name collision
// forced a separate type — Adapter.Insert vs PinStoreAdapter.Insert have
// incompatible signatures).
func buildOrchestrator(deps buildOrchestratorDeps) Built {
	if deps.Registry == nil {
		panic("buildOrchestrator: deps.Registry is nil (providers_init must run before buildOrchestrator)")
	}
	if deps.Resolver == nil {
		panic("buildOrchestrator: deps.Resolver is nil (config.NewProfileResolver must run before buildOrchestrator)")
	}

	tier1Client := &disabledBypassClient{reason: "in-process bypass extracted to sidecar; configure ~/.config/hades/sidecars.toml and use provider bypass-sidecar"}
	if err := deps.Registry.Register("bypass", providers.NewBypassBackend(tier1Client)); err != nil {

		slog.Info("buildOrchestrator: bypass backend already registered (providers.toml override?); keeping existing", "error", err)
	}

	costSink := newCostLedgerSink(deps.Store)
	costAdapter := dispatcheradapter.New(costSink, deps.Store)
	emitter := dispatcher.NewAsyncEmitter(costAdapter, 0)

	breaker := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	backends := registryBackends(deps.Registry)
	scheduler := orchestrator.NewRecoveryScheduler(breaker, backends, 0)

	// tier_health_samples row per probe so the operator dashboard +
	// scheduler heuristics can observe provider health over time.
	// dispatcheradapter.TierHealthSampleAdapter is the boundary bridge
	// .
	scheduler.SetHealthSink(dispatcheradapter.NewTierHealthSampleAdapter(deps.Store))

	disp := dispatcher.New(deps.Registry, deps.Resolver, emitter, breaker)

	orch := orchestrator.New(disp, "")

	pinAdapter := dispatcheradapter.NewPinStoreAdapter(deps.Store)
	pinOverrides := orchestrator.NewPinOverrides(pinAdapter)

	costCounters := orchestrator.NewCostCounters(costAdapter)

	paygSafety := orchestrator.NewPaygSafety(orchestrator.PaygSafetyOptions{
		Counters: costCounters,
		Notifier: deps.Notifier,
	})

	closeFn := func() {
		emitter.Close()
		_ = deps.Registry.Close()
	}
	return Built{
		Orchestrator:      orch,
		CostAdapter:       costAdapter,
		CostCounters:      costCounters,
		RecoveryScheduler: scheduler,
		PinOverrides:      pinOverrides,
		PaygSafety:        paygSafety,
		CircuitBreaker:    breaker,
		Tiers:             backends,
		Close:             closeFn,
	}
}

func registryBackends(reg *providers.Registry) []providers.TierBackend {
	names := reg.List()
	out := make([]providers.TierBackend, 0, len(names))
	for _, n := range names {
		b, err := reg.Get(n)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

func verifyCascadeCompleteness(resolver *config.ProfileResolver, reg *providers.Registry) error {

	for _, prof := range resolver.OperatorProfileNames() {
		cascade, err := resolver.Resolve(prof, "")
		if err != nil {
			return fmt.Errorf("inv-zen-211: resolve profile %q: %w", prof, err)
		}
		for _, name := range cascade {
			if _, gerr := reg.Get(name); gerr != nil {
				return fmt.Errorf("inv-zen-211: profile %q cascade names provider %q which is not registered", prof, name)
			}
		}
	}

	for _, proj := range resolver.OperatorOrchestratorProjects() {

		chain, err := resolver.ProjectFallbackChain(proj)
		if err != nil {
			return fmt.Errorf("inv-zen-211: project %q fallback_chain: %w", proj, err)
		}
		for _, name := range chain {
			if _, gerr := reg.Get(name); gerr != nil {
				return fmt.Errorf("inv-zen-211: project %q fallback_chain names provider %q which is not registered", proj, name)
			}
		}
	}
	return nil
}

type costLedgerSink struct {
	st    *store.Store
	rates *providers.RateCardRegistry
}

func newCostLedgerSink(st *store.Store) *costLedgerSink {
	if st == nil {
		panic("newCostLedgerSink: store is nil")
	}
	return &costLedgerSink{st: st, rates: nil}
}

var _ dispatcheradapter.Store = (*costLedgerSink)(nil)

func (s *costLedgerSink) InsertCostLedger(ctx context.Context, row dispatcheradapter.CostLedgerRow) error {
	costUSD := s.computeCost(row)
	storeRow := store.CostLedgerRow{
		IdempotencyKey: synthIdempotencyKey(row),
		TS:             row.Timestamp,
		Project:        row.Project,
		Profile:        row.Profile,
		Provider:       row.Provider,
		Tier:           row.Tier.String(),
		Model:          row.Model,
		InputTokens:    row.InputTokens,
		OutputTokens:   row.OutputTokens,
		CostUSD:        costUSD,
		SessionID:      row.SessionID,
	}
	if storeRow.Project == "" {
		storeRow.Project = "unknown"
	}
	if storeRow.Profile == "" {
		storeRow.Profile = "unknown"
	}
	if storeRow.Model == "" {
		storeRow.Model = "unknown"
	}
	_, err := store.InsertCostLedger(s.st.DB(), storeRow)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateIdempotency) {

			return nil
		}
		return fmt.Errorf("costLedgerSink: insert cost_ledger: %w", err)
	}
	return nil
}

func (s *costLedgerSink) computeCost(row dispatcheradapter.CostLedgerRow) float64 {
	if s.rates == nil || row.Err != "" {
		return 0
	}
	card, err := s.rates.Lookup(row.Provider, row.Model)
	if err != nil {
		slog.Debug("costLedgerSink: rate card missing — cost_usd recorded as 0",
			"provider", row.Provider, "model", row.Model)
		return 0
	}
	return card.Calculate(providers.TierResponse{
		InputTokens:  row.InputTokens,
		OutputTokens: row.OutputTokens,
	})
}

func synthIdempotencyKey(row dispatcheradapter.CostLedgerRow) string {
	errMark := "ok"
	if row.Err != "" {
		errMark = "err"
	}
	return fmt.Sprintf("attempt:%s:%s:%s:%d:%d:%s",
		row.Project, row.SessionID, row.Provider,
		row.Timestamp.UnixMilli(), row.Status, errMark)
}
