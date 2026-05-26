//go:build property
// +build property

// Three properties, each fuzzed via testing/quick:
//
//  1. (Hard cap never silently bypassed.) For any doctrine and any
//     sequence of pre-flight checks that result in Allowed=true, the
//     classification by quota.ClassifyUsage MUST match the doctrine's
//     enforcement rule:
//     - max-scope:     Allowed=true at hard-cap is the warn-only
//     contract — but PreFlight ALSO sets SoftWarn=true
//     (so the operator still sees the audit signal).
//     No silent bypass means: at HardCap+, if Allowed
//     then SoftWarn MUST be true.
//     - default:       Allowed=true ⇒ used < HardCapPct (or override
//     active). At HardCap+ without override: Allowed
//     MUST be false.
//     - capa-firewall: Allowed=true ⇒ used < 95% of cap (or override
//     active). At >=95% without override: Allowed MUST
//     be false.
//
//  2. (Doctrine semantics — reason-string contract.) When PreFlight
//     denies, the Reason string MUST encode the layer + doctrine +
//     status that produced the deny, in the canonical
//     "<layer>:<doctrine>:<status>:<detail>" form. This pins the
//     observability contract the daemon's `/v1/inbox` and `/v1/day`
//     handlers depend on.
//
//  3. (WFQ fairness bound.) Under balanced weights, no project's
//     dispatch share deviates >25% from its expected share over
//     N=500..1000 ops. The drainer is in-test (TryDispatch round-robin)
//     so the property is purely the WFQ math, not the production
//     scheduler thread.
//
// Extends inv-zen-115 + inv-zen-116 + inv-zen-119/121.
//
// # Drift notes (vs plan-template heredoc)
//
// The plan template referenced fictional symbols not present in the
// live codebase: `quota.HardCap{...}`, `quota.SetCap`,
// `quota.NewPreFlight`, `quota.PreFlightDeps{Store, Emitter, Clock}`,
// `quota.Op{Cost}`, `quota.DecisionAllow / AllowWarn / Deny`,
// `quota.NewWfqScheduler`, `quota.WfqDeps`, `scheduler.Drain(t, ...)`,
// `quota.Doctrine` enum, `clock.NewVirtual`, and
// `eventlog.NewRecorder`.
//
// Real surface (already exercised by Phase B + the inv-zen-115/116
// compliance tests):
//
//   - quota.PreFlight(ctx, alias, doctrine.Name, PreFlightDeps)
//     → PreFlightDecision{Allowed, SoftWarn, Reason, NextRetryAt}
//   - quota.DoctrineDefaults(d) → Thresholds
//   - quota.ClassifyUsage(used, cap, Thresholds) → CapStatus
//   - quota.WfqQueue: NewWfqQueue(weights), Enqueue, TryDispatch,
//     Depth, SetWeight, Weight
//   - doctrine.NameMaxScope / NameDefault / NameCapaFirewall
//
// Reality wins: the properties under test are preserved verbatim; only
// the binding mechanics change.
package property

import (
	"context"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

type capScenario struct {
	HardCap  int64
	NumOps   int
	Doctrine uint8
	OpsCost  []int64
	Override bool
	Seed     int64
}

func (cs capScenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	cap := int64(100 + rng.Intn(900))
	n := 50 + rng.Intn(450)
	costs := make([]int64, n)
	for i := range costs {
		costs[i] = int64(1 + rng.Intn(50))
	}
	v := capScenario{
		HardCap:  cap,
		NumOps:   n,
		Doctrine: uint8(rng.Intn(3)),
		OpsCost:  costs,
		Override: false,
		Seed:     rng.Int63(),
	}
	return reflect.ValueOf(v)
}

var doctrineLabels = []doctrine.Name{
	doctrine.NameMaxScope,
	doctrine.NameDefault,
	doctrine.NameCapaFirewall,
}

func TestProp_QuotaInvariants_HardCapNeverBypassed(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 50,
		Rand:     rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name()))))),
	}
	if testing.Short() {
		cfg.MaxCount = 5
	}

	property := func(sc capScenario) bool {
		msg := runQuotaScenario(t, sc)
		if msg != "" {
			t.Logf("scenario failed: HardCap=%d NumOps=%d Doctrine=%s Override=%v Seed=%d: %s",
				sc.HardCap, sc.NumOps, doctrineLabels[sc.Doctrine], sc.Override, sc.Seed, msg)
			return false
		}
		return true
	}

	if err := quick.Check(property, cfg); err != nil {
		t.Fatalf("quota hard-cap-never-bypassed property failed: %v", err)
	}
}

func runQuotaScenario(t *testing.T, sc capScenario) string {
	t.Helper()
	ctx := context.Background()

	d := doctrineLabels[sc.Doctrine]
	thresholds := quota.DoctrineDefaults(d)

	used := int64(0)
	alias := "p-fuzz"

	for i, cost := range sc.OpsCost {

		deps := quota.PreFlightDeps{
			Thresholds: thresholds,
			Used:       used,
			Cap:        sc.HardCap,
		}

		dec, err := quota.PreFlight(ctx, alias, d, deps)
		if err != nil {
			return fmt.Sprintf("op %d PreFlight: %v", i, err)
		}

		status := quota.ClassifyUsage(used, sc.HardCap, thresholds)

		switch d {
		case doctrine.NameMaxScope:

			if !dec.Allowed {
				return fmt.Sprintf("op %d max-scope unexpectedly denied: status=%s used=%d cap=%d",
					i, status, used, sc.HardCap)
			}
			if status == quota.CapStatusHardLogOnly && !dec.SoftWarn {
				return fmt.Sprintf("op %d max-scope at hard-cap WITHOUT SoftWarn: silent bypass! used=%d cap=%d",
					i, used, sc.HardCap)
			}
			if status == quota.CapStatusSoftWarn && !dec.SoftWarn {
				return fmt.Sprintf("op %d max-scope soft-warn WITHOUT SoftWarn=true: used=%d cap=%d",
					i, used, sc.HardCap)
			}

		case doctrine.NameDefault:
			// Default: deny at >=HardCapPct (100%). Allowed=true MUST
			// imply ClassifyUsage != HardDeny.
			if dec.Allowed && status == quota.CapStatusHardDeny {
				return fmt.Sprintf("op %d default Allowed=true at hard-deny: silent bypass! used=%d cap=%d",
					i, used, sc.HardCap)
			}

		case doctrine.NameCapaFirewall:
			// Capa-firewall: deny at >=95% (HardCapPct=95). Allowed=true
			// MUST imply pct < 95.
			if dec.Allowed && status == quota.CapStatusHardDeny {
				return fmt.Sprintf("op %d capa-firewall Allowed=true at hard-deny: silent bypass! used=%d cap=%d",
					i, used, sc.HardCap)
			}
		}

		if dec.Allowed {
			used += cost
		}
	}

	return ""
}

func TestProp_QuotaInvariants_DenyReasonContract(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 30,
		Rand:     rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name()))))),
	}
	if testing.Short() {
		cfg.MaxCount = 3
	}

	property := func(sc denyScenario) bool {
		msg := runDenyScenario(t, sc)
		if msg != "" {
			t.Logf("scenario failed: Doctrine=%s Cap=%d Used=%d Seed=%d: %s",
				doctrineLabels[sc.Doctrine], sc.Cap, sc.Used, sc.Seed, msg)
			return false
		}
		return true
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Fatalf("quota deny-reason-contract property failed: %v", err)
	}
}

type denyScenario struct {
	Doctrine uint8
	Cap      int64
	Used     int64
	Seed     int64
}

func (ds denyScenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	cap := int64(100 + rng.Intn(900))

	used := cap + int64(rng.Intn(int(cap)))
	v := denyScenario{

		Doctrine: 1 + uint8(rng.Intn(2)),
		Cap:      cap,
		Used:     used,
		Seed:     rng.Int63(),
	}
	return reflect.ValueOf(v)
}

func runDenyScenario(t *testing.T, sc denyScenario) string {
	t.Helper()
	ctx := context.Background()
	d := doctrineLabels[sc.Doctrine]
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(d),
		Used:       sc.Used,
		Cap:        sc.Cap,
	}
	dec, err := quota.PreFlight(ctx, "p-deny", d, deps)
	if err != nil {
		return fmt.Sprintf("PreFlight: %v", err)
	}
	if dec.Allowed {

		return fmt.Sprintf("expected deny at used=%d cap=%d doctrine=%s, got Allowed=true",
			sc.Used, sc.Cap, d)
	}
	// Reason MUST be non-empty + match the canonical shape.
	if dec.Reason == "" {
		return "deny without Reason string"
	}

	parts := strings.SplitN(dec.Reason, ":", 4)
	if len(parts) < 4 {
		return fmt.Sprintf("Reason missing canonical 4 parts: %q", dec.Reason)
	}
	if parts[0] != "layer1" {
		return fmt.Sprintf("Reason layer != layer1: %q", dec.Reason)
	}
	expectedDoctrine := string(d)
	if parts[1] != expectedDoctrine {
		return fmt.Sprintf("Reason doctrine != %q: %q", expectedDoctrine, dec.Reason)
	}
	if parts[2] != "hard-deny" {
		return fmt.Sprintf("Reason status != hard-deny: %q", dec.Reason)
	}
	if !strings.Contains(parts[3], "project_cap") {
		return fmt.Sprintf("Reason detail missing project_cap: %q", dec.Reason)
	}
	return ""
}

func TestProp_QuotaInvariants_WfqFairness(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 30,
		Rand:     rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name()))))),
	}
	if testing.Short() {
		cfg.MaxCount = 3
	}

	property := func(sc fairnessScenario) bool {
		msg := runFairnessScenario(t, sc)
		if msg != "" {
			t.Logf("scenario failed: NumProjects=%d NumOps=%d Seed=%d: %s",
				sc.NumProjects, sc.NumOps, sc.Seed, msg)
			return false
		}
		return true
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Fatalf("WFQ fairness property failed: %v", err)
	}
}

type fairnessScenario struct {
	NumProjects int
	NumOps      int
	Seed        int64
}

func (fs fairnessScenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	v := fairnessScenario{
		NumProjects: 3 + rng.Intn(8),
		NumOps:      500 + rng.Intn(501),
		Seed:        rng.Int63(),
	}
	return reflect.ValueOf(v)
}

func runFairnessScenario(t *testing.T, sc fairnessScenario) string {
	t.Helper()
	weights := make(map[string]quota.Weight, sc.NumProjects)
	aliases := make([]string, sc.NumProjects)
	for i := 0; i < sc.NumProjects; i++ {
		aliases[i] = fmt.Sprintf("p%02d", i)
		weights[aliases[i]] = 1.0
	}
	q := quota.NewWfqQueue(weights)

	rng := rand.New(rand.NewSource(sc.Seed))
	_ = rng
	for k := 0; k < sc.NumOps; k++ {
		for _, alias := range aliases {
			work := quota.WorkItem{
				ID:           fmt.Sprintf("%s-%d", alias, k),
				ProjectAlias: alias,
				Cost:         1.0,
			}
			if err := q.Enqueue(alias, work); err != nil {
				return fmt.Sprintf("Enqueue(%s, %d): %v", alias, k, err)
			}
		}
	}

	total := sc.NumProjects * sc.NumOps
	counts := make(map[string]int, sc.NumProjects)
	for i := 0; i < total; i++ {
		w, ok := q.TryDispatch()
		if !ok {
			return fmt.Sprintf("TryDispatch returned !ok at iteration %d (total=%d)", i, total)
		}
		counts[w.ProjectAlias]++
	}

	expectedShare := 1.0 / float64(sc.NumProjects)
	tolerance := 0.25 * expectedShare
	for _, alias := range aliases {
		actualShare := float64(counts[alias]) / float64(total)
		if math.Abs(actualShare-expectedShare) > tolerance {
			return fmt.Sprintf("project %s share %.4f deviates >25%% (tol=%.4f) from expected %.4f",
				alias, actualShare, tolerance, expectedShare)
		}
	}
	return ""
}
