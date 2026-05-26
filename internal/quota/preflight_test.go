package quota

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func baseDeps(t *testing.T) PreFlightDeps {
	t.Helper()
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	return PreFlightDeps{
		Thresholds:  DoctrineDefaults(doctrine.NameDefault),
		Used:        0,
		Cap:         10000,
		GlobalCap:   100000,
		GlobalUsed:  0,
		PerTierCaps: map[string]int64{"paygo": 5000, "bypass": 5000},
		PerTierUsed: map[string]int64{"paygo": 0, "bypass": 0},
		Wfq:         NewWfqQueue(map[string]Weight{"internal-platform-x": 1.0}),
		Now:         clock.Now,
	}
}

func TestPreFlightAllAllowedNoWarn(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	dec, err := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (zero usage)")
	}
	if dec.SoftWarn {
		t.Errorf("SoftWarn = true; want false")
	}
	if dec.Reason != "" {
		t.Errorf("Reason = %q; want empty (no flags)", dec.Reason)
	}
	if !dec.NextRetryAt.IsZero() {
		t.Errorf("NextRetryAt = %v; want zero", dec.NextRetryAt)
	}
}

func TestPreFlightSoftWarnAtSoftCap(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 8000
	dec, err := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (soft cap only)")
	}
	if !dec.SoftWarn {
		t.Errorf("SoftWarn = false; want true (soft cap crossed)")
	}
	if !strings.Contains(dec.Reason, "soft-warn") {
		t.Errorf("Reason missing soft-warn; got %q", dec.Reason)
	}
}

func TestPreFlightDefaultHardDeny(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (hard cap)")
	}
	if !strings.Contains(dec.Reason, "hard-deny") {
		t.Errorf("Reason missing hard-deny; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "project_cap") {
		t.Errorf("Reason missing project_cap; got %q", dec.Reason)
	}
}

func TestPreFlightMaxScopeHardLogOnlyAllows(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Thresholds = DoctrineDefaults(doctrine.NameMaxScope)
	deps.Used = 12000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameMaxScope, deps)
	if !dec.Allowed {
		t.Errorf("max-scope @ 120%% Allowed = false; want true (warn-only doctrine)")
	}
	if !dec.SoftWarn {
		t.Errorf("max-scope hard-log-only must set SoftWarn=true to surface the warn")
	}
	if !strings.Contains(dec.Reason, "hard-log-only") {
		t.Errorf("Reason missing hard-log-only; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "max-scope") {
		t.Errorf("Reason missing doctrine label; got %q", dec.Reason)
	}
}

func TestPreFlightCapaFirewallDeniesAt95(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Thresholds = DoctrineDefaults(doctrine.NameCapaFirewall)
	deps.Used = 9500
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameCapaFirewall, deps)
	if dec.Allowed {
		t.Errorf("capa-firewall @ 95%% Allowed = true; want false")
	}
	if !strings.Contains(dec.Reason, "capa-firewall") {
		t.Errorf("Reason missing doctrine label; got %q", dec.Reason)
	}
}

func TestPreFlightGlobalCapDenies(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)

	deps.GlobalUsed = 100000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (global cap reached)")
	}
	if !strings.Contains(dec.Reason, "daemon_cap") {
		t.Errorf("Reason should reference daemon_cap; got %q", dec.Reason)
	}
}

func TestPreFlightGlobalCapSoftWarn(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.GlobalUsed = 80000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (global SoftWarn only)")
	}
	if !dec.SoftWarn {
		t.Errorf("SoftWarn = false; want true (global cap crossed soft)")
	}
	if !strings.Contains(dec.Reason, "daemon_cap") {
		t.Errorf("Reason should reference daemon_cap; got %q", dec.Reason)
	}
}

func TestPreFlightPerTierCapDeniesPaygoOnly(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.PerTierUsed["paygo"] = 5000
	deps.RequestTier = "paygo"
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (paygo tier cap)")
	}
	if !strings.Contains(dec.Reason, "per_tier") {
		t.Errorf("Reason should reference per_tier; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "paygo") {
		t.Errorf("Reason should reference paygo; got %q", dec.Reason)
	}
}

func TestPreFlightPerTierCapBypassNotAffected(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.PerTierUsed["paygo"] = 5000
	deps.RequestTier = "bypass"
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (bypass tier has headroom)")
	}
}

func TestPreFlightPerTierSoftWarnDoesNotDeny(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.PerTierUsed["paygo"] = 4000
	deps.RequestTier = "paygo"
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (per-tier soft-warn only)")
	}
	if !dec.SoftWarn {
		t.Errorf("SoftWarn = false; want true (per-tier soft-warn)")
	}
}

func TestPreFlightPerTierMissingCapSkipped(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)

	deps.RequestTier = "unknown-tier"
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (no per-tier cap configured for tier)")
	}
}

func TestPreFlightPerTierZeroCapSkipped(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)

	deps.PerTierCaps["paygo"] = 0
	deps.PerTierUsed["paygo"] = 99999
	deps.RequestTier = "paygo"
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (zero cap = uncapped)")
	}
}

func TestPreFlightOverrideUnlocksDefaultHardDeny(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(1 * time.Hour),
		Reason:     "investigation",
		CreatedAt:  deps.Now(),
	}
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (override active)")
	}
	if !strings.Contains(dec.Reason, "boost-applied") {
		t.Errorf("Reason missing boost-applied; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "multiplier=3") {
		t.Errorf("Reason missing multiplier=3; got %q", dec.Reason)
	}
}

func TestPreFlightOverrideDoesNotUnlockGlobalCap(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.GlobalUsed = 100000
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(1 * time.Hour),
		Reason:     "investigation",
		CreatedAt:  deps.Now(),
	}
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (global cap doctrine-immutable; override does NOT unlock)")
	}
	if !strings.Contains(dec.Reason, "daemon_cap") {
		t.Errorf("Reason missing daemon_cap; got %q", dec.Reason)
	}
}

func TestPreFlightOverrideDoesNotUnlockPerTierCap(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.PerTierUsed["paygo"] = 5000
	deps.RequestTier = "paygo"
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(1 * time.Hour),
		Reason:     "investigation",
		CreatedAt:  deps.Now(),
	}
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (per-tier cap doctrine-immutable; override does NOT unlock)")
	}
	if !strings.Contains(dec.Reason, "per_tier") {
		t.Errorf("Reason missing per_tier; got %q", dec.Reason)
	}
}

func TestPreFlightExpiredOverrideIgnored(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(-1 * time.Hour),
		Reason:     "stale",
		CreatedAt:  deps.Now().Add(-2 * time.Hour),
	}

	SetNowFunc(deps.Now)
	t.Cleanup(func() { SetNowFunc(time.Now) })
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (expired override ignored)")
	}
	if !strings.Contains(dec.Reason, "hard-deny") {
		t.Errorf("Reason missing hard-deny; got %q", dec.Reason)
	}
}

func TestPreFlightWfqCongestionRetryAt(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)

	for i := 0; i < 51; i++ {
		_ = deps.Wfq.Enqueue("internal-platform-x", WorkItem{
			ID:           "id" + itemID(i),
			ProjectAlias: "internal-platform-x",
			EnqueuedAt:   deps.Now(),
			Cost:         1,
		})
	}
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (WFQ congested)")
	}
	if !strings.Contains(dec.Reason, "wfq-congested") {
		t.Errorf("Reason missing wfq-congested; got %q", dec.Reason)
	}
	expected := deps.Now().Add(30 * time.Second)
	if !dec.NextRetryAt.Equal(expected) {
		t.Errorf("NextRetryAt = %v, want %v (now+30s)", dec.NextRetryAt, expected)
	}
}

func TestPreFlightWfqCongestionBypassedByOverride(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)

	for i := 0; i < 51; i++ {
		_ = deps.Wfq.Enqueue("internal-platform-x", WorkItem{
			ID:           "id" + itemID(i),
			ProjectAlias: "internal-platform-x",
			EnqueuedAt:   deps.Now(),
			Cost:         1,
		})
	}
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(1 * time.Hour),
		Reason:     "urgent boost",
		CreatedAt:  deps.Now(),
	}
	SetNowFunc(deps.Now)
	t.Cleanup(func() { SetNowFunc(time.Now) })
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (override bypasses congestion)")
	}
	if !strings.Contains(dec.Reason, "boost-applied") {
		t.Errorf("Reason missing boost-applied; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "wfq_congestion_bypass") {
		t.Errorf("Reason missing wfq_congestion_bypass marker; got %q", dec.Reason)
	}
}

func TestPreFlightCustomCongestionThreshold(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.CongestionThreshold = 5
	for i := 0; i < 5; i++ {
		_ = deps.Wfq.Enqueue("internal-platform-x", WorkItem{
			ID:           "id" + itemID(i),
			ProjectAlias: "internal-platform-x",
			EnqueuedAt:   deps.Now(),
			Cost:         1,
		})
	}
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (custom threshold reached)")
	}
	if !strings.Contains(dec.Reason, "wfq-congested") {
		t.Errorf("Reason missing wfq-congested; got %q", dec.Reason)
	}
}

func TestPreFlightNilWfqSkipsLayer2(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Wfq = nil
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (no WFQ → no Layer 2 deny)")
	}
}

func TestPreFlightReasonStringSchema(t *testing.T) {

	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	parts := strings.SplitN(dec.Reason, ":", 4)
	if len(parts) < 4 {
		t.Fatalf("Reason missing 4 colon-segments; got %q", dec.Reason)
	}
	if parts[0] != "layer1" {
		t.Errorf("layer = %q, want layer1", parts[0])
	}
	if parts[1] != "default" {
		t.Errorf("doctrine = %q, want default", parts[1])
	}
	if parts[2] != "hard-deny" {
		t.Errorf("status = %q, want hard-deny", parts[2])
	}
}

func TestPreFlightMultipleCapStatusReportsLayerInOrder(t *testing.T) {

	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	deps.GlobalUsed = 100000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if dec.Allowed {
		t.Error("Allowed = true; want false (both layers fail)")
	}
	if !strings.Contains(dec.Reason, "project_cap") {
		t.Errorf("primary reason should be project_cap; got %q", dec.Reason)
	}
}

func TestPreFlightUnknownDoctrineFallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.Name("unknown-xyz"), deps)
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (unknown doctrine treated as default; hard-deny @ 100%%)")
	}
	if !strings.Contains(dec.Reason, "default") {
		t.Errorf("Reason should carry normalised default doctrine label; got %q", dec.Reason)
	}
}

func TestPreFlightZeroCapAlwaysAllowed(t *testing.T) {

	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 99999999
	deps.Cap = 0
	deps.GlobalCap = 0
	deps.PerTierCaps = nil
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (no caps configured)")
	}
}

func TestPreFlightEmptyAliasRejected(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	dec, err := PreFlight(ctx, "", doctrine.NameDefault, deps)
	if err == nil {
		t.Fatal("err = nil; want non-nil for empty alias")
	}
	if dec.Allowed {
		t.Error("Allowed = true on err; want false (zero value decision)")
	}
}

func TestPreFlightDefaultClock(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Now = nil
	dec, err := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if !dec.Allowed {
		t.Error("Allowed = false; want true (zero-usage; default clock)")
	}
}

func TestPreFlightWfqBypassWithoutCongestionNoOp(t *testing.T) {

	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 10000
	deps.Override = &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  deps.Now().Add(1 * time.Hour),
		Reason:     "boost",
		CreatedAt:  deps.Now(),
	}
	SetNowFunc(deps.Now)
	t.Cleanup(func() { SetNowFunc(time.Now) })
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (override unlocks project deny)")
	}
	if strings.Contains(dec.Reason, "wfq_congestion_bypass") {
		t.Errorf("Reason should NOT contain wfq_congestion_bypass when WFQ not congested; got %q", dec.Reason)
	}
}

func TestPreFlightAppendReasonStacking(t *testing.T) {
	ctx := context.Background()
	deps := baseDeps(t)
	deps.Used = 8000
	deps.GlobalUsed = 80000
	dec, _ := PreFlight(ctx, "internal-platform-x", doctrine.NameDefault, deps)
	if !dec.Allowed {
		t.Fatalf("Allowed = false; want true (both layers SoftWarn only)")
	}
	if !dec.SoftWarn {
		t.Fatalf("SoftWarn = false; want true (both layers SoftWarn)")
	}
	if !strings.Contains(dec.Reason, "project_cap") {
		t.Errorf("Reason missing project_cap; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "daemon_cap") {
		t.Errorf("Reason missing daemon_cap; got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, " | ") {
		t.Errorf("Reason missing stacking separator; got %q", dec.Reason)
	}
}

func TestIsCongested(t *testing.T) {
	if IsCongested(nil, "x", 0) {
		t.Error("IsCongested(nil, ...) = true; want false")
	}
	q := NewWfqQueue(map[string]Weight{"a": 1.0})
	for i := 0; i < 50; i++ {
		_ = q.Enqueue("a", WorkItem{ID: "x" + itemID(i), ProjectAlias: "a", Cost: 1})
	}
	if !IsCongested(q, "a", 0) {
		t.Error("IsCongested @ default threshold (50) for depth=50 = false; want true")
	}
	if !IsCongested(q, "a", 25) {
		t.Error("IsCongested @ custom threshold 25 for depth=50 = false; want true")
	}
	if IsCongested(q, "a", 1000) {
		t.Error("IsCongested @ huge threshold for depth=50 = true; want false")
	}
}

func TestPreFlightAdapterStaticDeps(t *testing.T) {
	ctx := context.Background()
	a := &PreFlightAdapter{Deps: baseDeps(t)}
	dec, err := a.PreFlight(ctx, "internal-platform-x", doctrine.NameDefault)
	if err != nil {
		t.Fatalf("adapter.PreFlight: %v", err)
	}
	if !dec.Allowed {
		t.Error("Allowed = false; want true (static deps, zero usage)")
	}
}

func TestPreFlightAdapterBuilder(t *testing.T) {
	ctx := context.Background()
	called := 0
	a := &PreFlightAdapter{
		DepsBuilder: func(ctx context.Context, alias string, d doctrine.Name) (PreFlightDeps, error) {
			called++
			deps := baseDeps(t)
			deps.Used = 10000
			return deps, nil
		},
	}
	dec, err := a.PreFlight(ctx, "internal-platform-x", doctrine.NameDefault)
	if err != nil {
		t.Fatalf("adapter.PreFlight: %v", err)
	}
	if dec.Allowed {
		t.Error("Allowed = true; want false (built deps put us at hard cap)")
	}
	if called != 1 {
		t.Errorf("DepsBuilder called %d times; want 1", called)
	}
}

func TestPctOfDefensiveBranches(t *testing.T) {
	cases := []struct {
		name string
		used int64
		cap  int64
		want int64
	}{
		{"zero cap returns zero", 50, 0, 0},
		{"negative cap returns zero", 50, -1, 0},
		{"negative used returns zero", -100, 1000, 0},
		{"happy path", 50, 100, 50},
		{"hundred percent", 100, 100, 100},
		{"over hundred", 150, 100, 150},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pctOf(c.used, c.cap)
			if got != c.want {
				t.Errorf("pctOf(%d, %d) = %d, want %d", c.used, c.cap, got, c.want)
			}
		})
	}
}

func TestPreFlightAdapterBuilderError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("ledger unavailable")
	a := &PreFlightAdapter{
		DepsBuilder: func(ctx context.Context, alias string, d doctrine.Name) (PreFlightDeps, error) {
			return PreFlightDeps{}, sentinel
		},
	}
	dec, err := a.PreFlight(ctx, "internal-platform-x", doctrine.NameDefault)
	if err == nil {
		t.Fatal("err = nil; want builder error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err does not wrap sentinel; got %v", err)
	}
	if dec.Allowed {
		t.Error("Allowed = true on builder error; want false (zero-value)")
	}
}
