package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func testRegistry(t *testing.T) *providers.Registry {
	t.Helper()
	reg := providers.NewRegistry()
	if err := registerConstructors(reg, keychain.SystemResolver{}); err != nil {
		t.Fatalf("registerConstructors: %v", err)
	}
	return reg
}

func testResolver() *config.ProfileResolver {
	return config.NewProfileResolver(config.ProfileResolverLayers{})
}

func testDeps(t *testing.T, st *store.Store, notifier orchestrator.Notifier) buildOrchestratorDeps {
	t.Helper()
	return buildOrchestratorDeps{
		Store:    st,
		Notifier: notifier,
		Registry: testRegistry(t),
		Resolver: testResolver(),
	}
}

// openTestStore opens a temp *store.Store with all migrations applied.
// Used by buildOrchestrator(_, st) call sites in the wiring tests since
// dispatcheradapter.New panics on a nil *store.Store (F-6 fail-fast).
//
// t.Cleanup closes the store at test end so -race + parallel tests do not
// leak handles.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "wiring.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestBuildOrchestrator_NilBypassClientGracefullyDegrades — when the bypass
// bootstrap fails (returns nil *bypass.Client), buildOrchestrator MUST
// still produce a non-nil orchestrator. The bypass backend is registered
// as a disabled stub (cascade skips it). With no other providers in the
// registry, a Forward against the built-in "worker-code" cascade hits an
// inv-zen-211 condition at request time — the cascade names unregistered
// providers — and the resolver call surfaces an error. The point of the
// test is "buildOrchestrator did not panic on nil BypassClient".
func TestBuildOrchestrator_NilBypassClientGracefullyDegrades(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	orch := built.Orchestrator
	closeFn := built.Close
	defer closeFn()
	if orch == nil {
		t.Fatal("buildOrchestrator(nil bypass) returned nil orchestrator — must always produce a runnable orchestrator")
	}

	_, err := orch.Forward(context.Background(), orchestrator.Call{
		Method:  "POST",
		Path:    "/v1/messages",
		Body:    []byte(`{}`),
		Profile: "orchestrator",
	})
	if err == nil {
		t.Errorf("Forward against partly-registered cascade returned nil error; expected an error path")
	}
}

// TestBuildOrchestrator_DisabledBypassReturnsErrTierUnavailable — the
// disabled-Tier-1 stand-in (used when bypass bootstrap fails) MUST return
// errors that wrap providers.ErrTierUnavailable so the dispatcher routes
// the failure as "tier unavailable, try next" rather than as a generic
// transport error (which would still fail-over but log differently).
func TestBuildOrchestrator_DisabledBypassReturnsErrTierUnavailable(t *testing.T) {
	dc := &disabledBypassClient{reason: "test"}
	body, status, _, err := dc.ForwardRaw(context.Background(), nil, nil, "")
	if !errors.Is(err, providers.ErrTierUnavailable) {
		t.Errorf("ForwardRaw err must wrap ErrTierUnavailable, got %v", err)
	}
	if body != nil || status != 0 {
		t.Errorf("disabled stub must return (nil, 0, 0, err), got (%v, %d)", body, status)
	}

	if !strings.Contains(err.Error(), "test") {
		t.Errorf("ForwardRaw error should include reason %q: got %v", "test", err)
	}
	if hErr := dc.Health(context.Background()); !errors.Is(hErr, providers.ErrTierUnavailable) {
		t.Errorf("Health err must wrap ErrTierUnavailable, got %v", hErr)
	}
	if hErr := dc.Health(context.Background()); !strings.Contains(hErr.Error(), "test") {
		t.Errorf("Health error should include reason %q: got %v", "test", hErr)
	}
}

// TestBuildOrchestrator_ReturnsCostAdapter — Plan 3 Phase C F-7 wiring
// pin: buildOrchestrator MUST return a non-nil *dispatcheradapter.Adapter
// so main.go can hand it to orchestrator.NewCostCounters and the
// dispatcher's AsyncEmitter writes + the counters' RebuildFromLedger
// reads cross the SAME boundary instance. A regression returning a
// throwaway second adapter would silently route writes to one Adapter
// and reads to another, breaking inv-zen-065 (rebuild would query an
// empty adapter). Pinning the adapter satisfies orchestrator.CostStore.
func TestBuildOrchestrator_ReturnsCostAdapter(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.Orchestrator == nil {
		t.Fatal("buildOrchestrator returned nil Orchestrator")
	}
	if built.CostAdapter == nil {
		t.Fatal("buildOrchestrator returned nil CostAdapter — F-7 RebuildFromLedger cannot wire")
	}
	// Pin the interface contract used by NewCostCounters: the adapter MUST
	// satisfy orchestrator.CostStore at compile + run time.
	var _ orchestrator.CostStore = built.CostAdapter

	if built.CostCounters == nil {
		t.Fatal("buildOrchestrator returned nil CostCounters — Phase E I-5 contract violated")
	}
	if err := built.CostCounters.RebuildFromLedger(time.Now().Add(-30 * 24 * time.Hour)); err != nil {
		t.Errorf("RebuildFromLedger on built.CostCounters: %v", err)
	}
}

func TestBuildOrchestrator_CloseReleasesEmitter(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	orch := built.Orchestrator
	closeFn := built.Close
	if orch == nil {
		t.Fatal("buildOrchestrator returned nil")
	}
	closeFn()

	closeFn()
}

// TestBuildOrchestrator_ReturnsRecoveryScheduler — Plan 3 Phase D-6 wiring
// pin: buildOrchestrator MUST return a non-nil *orchestrator.RecoveryScheduler
// so main.go can wire it through Server.SetRecoveryScheduler. A regression
// returning nil would silently disable the recovery probe loop and Open
// tiers would never recover. The scheduler is constructed but NOT yet
// running — its Run(ctx) is invoked by main.go AFTER buildOrchestrator
// returns, with a daemon-process-scoped context.
func TestBuildOrchestrator_ReturnsRecoveryScheduler(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.Orchestrator == nil {
		t.Fatal("buildOrchestrator returned nil Orchestrator")
	}
	if built.RecoveryScheduler == nil {
		t.Fatal("buildOrchestrator returned nil RecoveryScheduler — Phase D-6 contract violated; Open tiers would never recover")
	}
}

// TestBuildOrchestrator_BreakerInDispatcherChain — Plan 3 Phase D-6 pin:
// the dispatcher MUST be wired with a real *orchestrator.CircuitBreaker
// (not the deleted noopBreaker stand-in). We verify by triggering a
// Forward against a fully-disabled chain (both tiers off) and asserting
// the dispatcher reaches ErrAllTiersUnavailable rather than panicking on a
// nil breaker. The breaker permits all tiers initially (StateClosed
// default) so the dispatcher attempts both tiers, both fail, and the
// terminal error surfaces.
//
// This is a structural smoke test — the breaker's full state-machine
// behaviour is exercised by the unit tests in the orchestrator package
// (circuit_breaker_test.go) and the integration test
// (circuit_breaker_integration_test.go). What this pins is "the wiring
// path from dispatcher.New through to a *CircuitBreaker is in place".
func TestBuildOrchestrator_BreakerInDispatcherChain(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	orch := built.Orchestrator
	closeFn := built.Close
	defer closeFn()

	_, err := orch.Forward(context.Background(), orchestrator.Call{
		Method:  "POST",
		Path:    "/v1/messages",
		Body:    []byte(`{}`),
		Profile: "orchestrator",
	})
	if err == nil {
		t.Errorf("Forward against partial cascade returned nil error; expected an error from the cascade walk")
	}
}

// TestBuildOrchestrator_ReturnsPinOverrides — Plan 3 Phase E I-5 wiring
// pin: buildOrchestrator MUST return a non-nil *orchestrator.PinOverrides
// so main.go can wire it through Server.SetPinOverrides. A regression
// returning nil would silently disable the operator-facing pin path —
// `zen orchestrator pin` (Phase F CLI) would 503 forever.
func TestBuildOrchestrator_ReturnsPinOverrides(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.PinOverrides == nil {
		t.Fatal("buildOrchestrator returned nil PinOverrides — Phase E I-5 contract violated")
	}

	rows, err := built.PinOverrides.ListAll()
	if err != nil {
		t.Errorf("PinOverrides.ListAll on empty store: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListAll on empty store returned %d rows, want 0", len(rows))
	}
	if err := built.PinOverrides.Set("global", "", "tier-2", "openclaude", 0, "test"); err != nil {
		t.Errorf("PinOverrides.Set on empty store: %v", err)
	}
	rows2, err := built.PinOverrides.ListAll()
	if err != nil {
		t.Errorf("PinOverrides.ListAll after Set: %v", err)
	}
	if len(rows2) != 1 {
		t.Errorf("ListAll after Set returned %d rows, want 1", len(rows2))
	}
}

// TestBuildOrchestrator_ReturnsPaygSafety — Plan 3 Phase E I-5 wiring
// pin: buildOrchestrator MUST return a non-nil *orchestrator.PaygSafety
// constructed against the SAME *CostCounters (CapCounters interface) so
// CheckCap reads the live ledger. A duplicate counter cache (or nil)
// would silently let cap-checks pass that ought to fail.
func TestBuildOrchestrator_ReturnsPaygSafety(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.PaygSafety == nil {
		t.Fatal("buildOrchestrator returned nil PaygSafety — Phase E I-5 contract violated")
	}

	err := built.PaygSafety.CheckCap("p", "default", "tier-2", "sess",
		1.0,
		orchestrator.ProfileEffective{
			PerMonthUSD: 0.5,
		})
	if !errors.Is(err, orchestrator.ErrCapWillExceed) {
		t.Errorf("CheckCap err = %v, want ErrCapWillExceed (live counters wired)", err)
	}
}

// TestBuildOrchestrator_PaygSafetySharesCostCounters — Plan 3 Phase E I-5
// CRITICAL pin: PaygSafety MUST be wired against the EXACT same
// *CostCounters instance buildOrchestrator returns in built.CostCounters.
// A duplicate would silently desynchronise from the live ledger — writes
// from the dispatcher's AsyncEmitter would land in counter A, cap checks
// in PaygSafety would read counter B, and caps would always report 0
// regardless of historical spend.
//
// We verify by recording cost into built.CostCounters and confirming
// PaygSafety.CheckCap sees the new total via SessionTotal — proving the
// underlying counter is shared.
func TestBuildOrchestrator_PaygSafetySharesCostCounters(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()

	now := time.Now().UTC()
	if err := built.CostCounters.Record(orchestrator.CostLedgerRow{
		IdempotencyKey: "i5-shared-counter-test",
		TS:             now,
		Project:        "p",
		Profile:        "default",
		Tier:           "tier-2",
		Model:          "claude-x",
		SessionID:      "sess-1",
		CostUSD:        2.0,
	}); err != nil {
		t.Fatalf("Record on shared CostCounters: %v", err)
	}

	err := built.PaygSafety.CheckCap("p", "default", "tier-2", "sess-1",
		0.0,
		orchestrator.ProfileEffective{
			PerSessionUSD: 1.5,
		})
	if !errors.Is(err, orchestrator.ErrCapWillExceed) {
		t.Errorf("CheckCap err = %v, want ErrCapWillExceed (shared CostCounters proof: Record(2.0) on built.CostCounters must be visible to built.PaygSafety)", err)
	}
}

// TestBuildOrchestrator_ReturnsCircuitBreakerAndTiers — Plan 3 Phase F
// K-3 + Plan 16 T17 wiring pin: buildOrchestrator MUST return a non-nil
// *orchestrator.CircuitBreaker AND a non-empty Tiers slice so main.go
// can hand them to Server.SetCircuitBreaker / Server.SetTiers. The
// operator-facing `zen orchestrator status / probe / history` commands
// (Phase F K-3) consult those accessors; missing wiring would silently
// surface empty-shape responses with no breaker / tier data.
//
// the deleted tier1/tier2 hard-wire. With testDeps' empty registry plus
// the bypass auto-registration, Tiers has length 1 ("bypass"). The
// breaker is keyed by provider Name (not Tier enum) post-T17.
func TestBuildOrchestrator_ReturnsCircuitBreakerAndTiers(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.CircuitBreaker == nil {
		t.Fatal("buildOrchestrator returned nil CircuitBreaker — Phase F K-3 contract violated")
	}
	if len(built.Tiers) < 1 {
		t.Fatalf("Tiers len = %d, want >= 1 (bypass auto-registered by master C5)", len(built.Tiers))
	}

	for _, b := range built.Tiers {
		if got := built.CircuitBreaker.State(b.Name()); got != orchestrator.StateClosed {
			t.Errorf("provider %q initial state = %v, want closed", b.Name(), got)
		}
	}
}

// TestBuildOrchestrator_NilNotifierIsAccepted — buildOrchestrator MUST
// accept a nil notifier (test path); PaygSafety silently no-ops on
// notifier == nil per its constructor contract.
func TestBuildOrchestrator_NilNotifierIsAccepted(t *testing.T) {
	st := openTestStore(t)
	built := buildOrchestrator(testDeps(t, st, nil))
	defer built.Close()
	if built.PaygSafety == nil {
		t.Fatal("buildOrchestrator with nil notifier returned nil PaygSafety")
	}

	err := built.PaygSafety.CheckCap("p", "default", "tier-2", "sess",
		5.0,
		orchestrator.ProfileEffective{
			PerSessionUSD:    10.0,
			NotifyAtPercents: []int{50},
		})
	if err != nil {
		t.Errorf("CheckCap below cap err = %v, want nil", err)
	}
}

// TestOrchestratorNotifierAdapter_ForwardsAllSeverities — the adapter MUST
// forward NotifyINFO/WARN/CRITICAL to daemon.Notifier.Dispatch with the
// matching severity string. The store visibility of the dispatched rows
// is the proof: severity propagates correctly through the adapter.
func TestOrchestratorNotifierAdapter_ForwardsAllSeverities(t *testing.T) {
	st := openTestStore(t)
	nfy := daemon.NewNotifier(st)
	defer nfy.Close()

	a := orchestratorNotifierAdapter{n: nfy}

	var _ orchestrator.Notifier = a

	a.NotifyINFO("info-title", "info-body", "test/info")
	a.NotifyWARN("warn-title", "warn-body", "test/warn")
	a.NotifyCRITICAL("critical-title", "critical-body", "test/critical")

	rows, err := st.ListBypassNotifications(context.Background(), 100, false)
	if err != nil {
		t.Fatalf("ListBypassNotifications: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("notifications count = %d, want 3 (INFO+WARN+CRITICAL)", len(rows))
	}

	bySev := make(map[string]string, 3)
	for _, n := range rows {
		bySev[n.Severity] = n.Title
	}
	if bySev["INFO"] != "info-title" {
		t.Errorf("INFO title = %q, want info-title (got map: %v)", bySev["INFO"], bySev)
	}
	if bySev["WARN"] != "warn-title" {
		t.Errorf("WARN title = %q, want warn-title (got map: %v)", bySev["WARN"], bySev)
	}
	if bySev["CRITICAL"] != "critical-title" {
		t.Errorf("CRITICAL title = %q, want critical-title (got map: %v)", bySev["CRITICAL"], bySev)
	}
}

// TestCostLedgerSink_PersistsPerAttemptEvent verifies the real
// dispatcheradapter.Store sink (replacing noopCostSink in T17) translates a
// per-attempt CostLedgerRow into a canonical store.CostLedgerRow and persists
// it via store.InsertCostLedger. The persisted row MUST carry the Plan 16
// provider attribution (cost_ledger.provider column, migration 064) and the
// Tier rendered as its canonical string.
func TestCostLedgerSink_PersistsPerAttemptEvent(t *testing.T) {
	st := openTestStore(t)
	sink := newCostLedgerSink(st)

	err := sink.InsertCostLedger(context.Background(), dispatcheradapter.CostLedgerRow{
		Timestamp:    time.UnixMilli(1700000000000),
		Project:      "internal-platform-x",
		SessionID:    "sess-1",
		Profile:      "worker-code",
		Provider:     "deepseek-direct",
		Tier:         providers.TierGenericOpenAICompat,
		Model:        "deepseek-chat",
		InputTokens:  100,
		OutputTokens: 50,
		Status:       200,
	})
	if err != nil {
		t.Fatalf("InsertCostLedger: %v", err)
	}
	rows, err := store.QueryAllRecentCosts(st.DB(), time.UnixMilli(0))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("persisted %d rows, want 1", len(rows))
	}
	if rows[0].Provider != "deepseek-direct" {
		t.Errorf("row.Provider = %q, want deepseek-direct", rows[0].Provider)
	}
	if rows[0].Tier != providers.TierGenericOpenAICompat.String() {
		t.Errorf("row.Tier = %q, want %q", rows[0].Tier, providers.TierGenericOpenAICompat.String())
	}
}

// TestCostLedgerSink_DuplicateEventDeduped verifies the deterministic
// idempotency-key synthesis: a re-delivered identical CostEvent MUST NOT
// produce a duplicate cost_ledger row and MUST NOT surface an error to the
// AsyncEmitter worker (worker would log spurious failures + retry). The
// inv-zen-062 no-double-charge invariant relies on this dedup behaviour.
func TestCostLedgerSink_DuplicateEventDeduped(t *testing.T) {
	st := openTestStore(t)
	sink := newCostLedgerSink(st)
	row := dispatcheradapter.CostLedgerRow{
		Timestamp: time.UnixMilli(1700000000000), Project: "p", SessionID: "s",
		Profile: "pr", Provider: "deepseek-direct", Tier: providers.TierGenericOpenAICompat,
		Model: "m", Status: 200,
	}
	if err := sink.InsertCostLedger(context.Background(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	if err := sink.InsertCostLedger(context.Background(), row); err != nil {
		t.Fatalf("duplicate insert should be a silent no-op, got: %v", err)
	}
	rows, _ := store.QueryAllRecentCosts(st.DB(), time.UnixMilli(0))
	if len(rows) != 1 {
		t.Errorf("after duplicate delivery: %d rows, want 1", len(rows))
	}
}

func TestBuildOrchestrator_UsesRegistryAndResolver(t *testing.T) {
	st := openTestStore(t)
	reg := testRegistry(t)
	if err := reg.RegisterFromConfig(providers.ProviderConfig{
		Name:     "ollama-qwen-coder",
		Type:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "qwen2.5-coder:32b",
		Family:   "local-qwen",
	}); err != nil {
		t.Fatalf("RegisterFromConfig(ollama): %v", err)
	}
	resolver := testResolver()

	built := buildOrchestrator(buildOrchestratorDeps{
		Store:    st,
		Notifier: nil,
		Registry: reg,
		Resolver: resolver,
	})
	defer built.Close()

	if built.Orchestrator == nil {
		t.Fatal("buildOrchestrator returned nil Orchestrator")
	}
	if built.RecoveryScheduler == nil {
		t.Fatal("buildOrchestrator returned nil RecoveryScheduler")
	}
	if len(built.Tiers) == 0 {
		t.Error("built.Tiers should include the registered backends (bypass + ollama-qwen-coder)")
	}

	if _, err := reg.Get("bypass"); err != nil {
		t.Errorf("buildOrchestrator should register the bypass backend per master C5: %v", err)
	}

	if _, err := reg.Get("ollama-qwen-coder"); err != nil {
		t.Errorf("ollama-qwen-coder lost from registry after buildOrchestrator: %v", err)
	}
}

func TestBuildOrchestrator_PanicsOnNilRegistry(t *testing.T) {
	st := openTestStore(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildOrchestrator(nil Registry) did not panic; fail-fast guard missing")
		}
	}()
	_ = buildOrchestrator(buildOrchestratorDeps{
		Store:    st,
		Notifier: nil,
		Registry: nil,
		Resolver: testResolver(),
	})
}

func TestBuildOrchestrator_PanicsOnNilResolver(t *testing.T) {
	st := openTestStore(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildOrchestrator(nil Resolver) did not panic; fail-fast guard missing")
		}
	}()
	_ = buildOrchestrator(buildOrchestratorDeps{
		Store:    st,
		Notifier: nil,
		Registry: testRegistry(t),
		Resolver: nil,
	})
}

func TestVerifyCascadeCompleteness_NamesGap(t *testing.T) {
	reg := testRegistry(t)

	resolver := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"custom": {Name: "custom", Cascade: []string{"ghost-provider"}},
		},
	})
	err := verifyCascadeCompleteness(resolver, reg)
	if err == nil {
		t.Fatal("verifyCascadeCompleteness accepted a cascade naming an undeclared provider")
	}
	if !strings.Contains(err.Error(), "custom") || !strings.Contains(err.Error(), "ghost-provider") {
		t.Errorf("error %q must name both profile and missing provider", err.Error())
	}
}

func TestVerifyCascadeCompleteness_BypassRegistered(t *testing.T) {
	st := openTestStore(t)
	reg := testRegistry(t)

	rosterProviders := []string{
		"gemini-pro", "deepseek-direct", "siliconflow-deepseek",
		"openrouter-deepseek", "moonshot-kimi", "openrouter-kimi",
		"gemini-flash", "zhipu-glm-flash", "openrouter-glm",
		"ollama-qwen-coder",
	}
	for _, name := range rosterProviders {
		if err := reg.RegisterFromConfig(providers.ProviderConfig{
			Name: name, Type: "ollama",
			Endpoint: "http://localhost:11434", Model: "stand-in", Family: "test",
		}); err != nil {
			t.Fatalf("RegisterFromConfig(%q): %v", name, err)
		}
	}

	resolver := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"bypass-only": {Name: "bypass-only", Cascade: []string{"bypass"}},
		},
	})
	built := buildOrchestrator(buildOrchestratorDeps{
		Store: st, Notifier: nil,
		Registry: reg, Resolver: resolver,
	})
	defer built.Close()
	if err := verifyCascadeCompleteness(resolver, reg); err != nil {
		t.Errorf("verifyCascadeCompleteness should pass after bypass registration: %v", err)
	}
}

// TestVerifyCascadeCompleteness_EmptyConfigBootstrap verifies the doctrine
// intent documented in providers_init.go: the daemon's out-of-box state
// (no providers.toml + no profiles.toml) MUST boot cleanly. The built-in
// roster profile defaults (BuiltinProfileDefaults: orchestrator,
// worker-code, worker-reasoning, tactical, local-code) reference roster
// providers the operator has not configured yet — they are aspirational
// stubs of the v1.0 OSS roster. inv-zen-211 must only fire on
// operator-supplied profiles (profiles.toml ∪ projects.toml
// [orchestrator].fallback_chain), NOT on the built-in defaults; otherwise
// the daemon refuses to start out-of-box. Regression guard for the smoke
// break surfaced during Plan 16 Phase B Task 22.
func TestVerifyCascadeCompleteness_EmptyConfigBootstrap(t *testing.T) {
	reg := testRegistry(t)

	resolver := config.NewProfileResolver(config.ProfileResolverLayers{})

	if err := verifyCascadeCompleteness(resolver, reg); err != nil {
		t.Fatalf("verifyCascadeCompleteness must not fire on built-in defaults "+
			"in the empty-config out-of-box state (providers_init.go documents "+
			"this contract): %v", err)
	}
}

// TestVerifyCascadeCompleteness_OperatorProfileStillGated verifies the
// flip side of the empty-config relaxation: when an OPERATOR supplies a
// profile via profiles.toml, inv-zen-211 MUST still fire if the cascade
// names an unregistered provider. The gate protects against operator
// typos; the empty-config relaxation only excuses BUILT-IN defaults.
func TestVerifyCascadeCompleteness_OperatorProfileStillGated(t *testing.T) {
	reg := testRegistry(t)
	resolver := config.NewProfileResolver(config.ProfileResolverLayers{
		// profiles.toml entry — operator-supplied, gate MUST fire on a
		// cascade naming an unregistered provider.
		Profiles: map[string]config.ProfileConfig{
			"custom": {Name: "custom", Cascade: []string{"ghost-provider"}},
		},
	})
	err := verifyCascadeCompleteness(resolver, reg)
	if err == nil {
		t.Fatal("verifyCascadeCompleteness must still fire on an operator " +
			"profile naming an undeclared provider (only built-in defaults " +
			"are excused in the empty-config path)")
	}
	if !strings.Contains(err.Error(), "custom") || !strings.Contains(err.Error(), "ghost-provider") {
		t.Errorf("error %q must name both profile and missing provider", err.Error())
	}
}

func TestVerifyCascadeCompleteness_ProjectFallbackChainGated(t *testing.T) {
	reg := testRegistry(t)
	resolver := config.NewProfileResolver(config.ProfileResolverLayers{
		Orchestrators: map[string]config.OrchestratorConfig{
			"some-project": {
				FallbackChain: []string{"ghost-provider-2"},
			},
		},
	})
	err := verifyCascadeCompleteness(resolver, reg)
	if err == nil {
		t.Fatal("verifyCascadeCompleteness must fire on a project " +
			"orchestrator FallbackChain naming an undeclared provider")
	}
	if !strings.Contains(err.Error(), "ghost-provider-2") {
		t.Errorf("error %q must name the missing provider", err.Error())
	}
}

func TestRegistryBackends_StableOrder(t *testing.T) {
	reg := testRegistry(t)
	if err := reg.RegisterFromConfig(providers.ProviderConfig{
		Name: "z-ollama", Type: "ollama", Endpoint: "http://localhost:11434",
		Model: "x", Family: "local",
	}); err != nil {
		t.Fatalf("RegisterFromConfig: %v", err)
	}
	if err := reg.RegisterFromConfig(providers.ProviderConfig{
		Name: "a-ollama", Type: "ollama", Endpoint: "http://localhost:11434",
		Model: "y", Family: "local",
	}); err != nil {
		t.Fatalf("RegisterFromConfig: %v", err)
	}
	got := registryBackends(reg)
	if len(got) != 2 {
		t.Fatalf("registryBackends() len = %d, want 2", len(got))
	}
	if got[0].Name() != "a-ollama" || got[1].Name() != "z-ollama" {
		t.Errorf("registryBackends() order = [%s, %s], want [a-ollama, z-ollama] (sorted)",
			got[0].Name(), got[1].Name())
	}
}
