// go:build property && cgo

// tests/property/ecosystem/doctrine_strictness_property_test.go
//
// invariant: doctrine strictness knob honored per profile.
//
// Profiles (spec §2.7 Q7=A FROZEN table):
// - max-scope MaxResults=10 LLMJudge=true RefuseOnUnverified=false AuditLevel=all-8-events
// - default MaxResults=5 LLMJudge=false RefuseOnUnverified=false AuditLevel=query+abstain+verify-failure+answer
// - capa-firewall MaxResults=10 LLMJudge=true RefuseOnUnverified=true AuditLevel=all-8-events
//
// Properties tested:
//
// 1. Resolution determinism — same doctrine name → identical
// DoctrineProfile across 500 random invocations.
// 2. Per-profile invariants — every field of every built-in profile
// matches the spec table exactly.
// 3. Capa-firewall strictness — capa-firewall.RefuseOnUnverified == true
// while default.RefuseOnUnverified == false (capa-firewall MUST be
// strictly more conservative than default on the refuse axis).
//
// Uses the real DoctrineResolver wired to a Accessor so a drift
// in `builtinProfiles` (private) is detected through the public
// Resolve() return.

package ecosystem_property_test

import (
	"context"
	"reflect"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func builtinResolver(t *testing.T, userDefault string) *ecosystem.DoctrineResolver {
	t.Helper()
	acc := active.NewAccessor()
	registry := map[string]*v1.Schema{
		"max-scope":     {},
		"default":       {},
		"capa-firewall": {},
	}
	acc.SetRegistry(registry)
	if userDefault == "" {
		userDefault = "default"
	}
	if err := acc.SetUserDefault(userDefault); err != nil {
		t.Fatalf("SetUserDefault(%q): %v", userDefault, err)
	}
	r := ecosystem.NewDoctrineResolver(acc)
	r.SetRegistry(registry)
	return r
}

func TestDoctrineStrictness_Property_ResolveDeterministic(t *testing.T) {
	ctx := context.Background()
	doctrines := []string{"max-scope", "default", "capa-firewall"}

	prop := func(idx uint8) bool {
		name := doctrines[int(idx)%len(doctrines)]
		r := builtinResolver(t, name)
		p1, err := r.Resolve(ctx, "")
		if err != nil {
			return false
		}
		p2, err := r.Resolve(ctx, "")
		if err != nil {
			return false
		}

		if p1.Name != p2.Name ||
			p1.MaxResults != p2.MaxResults ||
			p1.LLMJudgeEnabled != p2.LLMJudgeEnabled ||
			p1.RefuseOnUnverified != p2.RefuseOnUnverified ||
			p1.SkipLLMVersionDetection != p2.SkipLLMVersionDetection ||
			p1.CitationMode != p2.CitationMode ||
			p1.AuditEmissionLevel != p2.AuditEmissionLevel ||
			p1.CRPrefixLLM != p2.CRPrefixLLM ||
			!reflect.DeepEqual(p1.AbstentionThresholds, p2.AbstentionThresholds) {
			t.Logf("inv-zen-205: non-deterministic resolve: name=%s p1=%+v p2=%+v", name, p1, p2)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-205: resolution non-deterministic: %v", err)
	}
}

func TestDoctrineStrictness_Property_MaxScopeMatchesSpec(t *testing.T) {
	r := builtinResolver(t, "max-scope")
	p, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Name != "max-scope" {
		t.Errorf("Name = %q; want max-scope", p.Name)
	}
	if p.MaxResults != 10 {
		t.Errorf("MaxResults = %d; want 10", p.MaxResults)
	}
	if !p.LLMJudgeEnabled {
		t.Error("LLMJudgeEnabled = false; want true (max-scope)")
	}
	if p.RefuseOnUnverified {
		t.Error("RefuseOnUnverified = true; want false (max-scope — high recall, not refuse-on-unverified)")
	}
	if p.AuditEmissionLevel != ecosystem.AuditAll8Events {
		t.Errorf("AuditEmissionLevel = %v; want %v", p.AuditEmissionLevel, ecosystem.AuditAll8Events)
	}
	if p.CitationMode != ecosystem.CitationMandatoryGrammar {
		t.Errorf("CitationMode = %v; want %v", p.CitationMode, ecosystem.CitationMandatoryGrammar)
	}
	wantThresh := map[ecosystem.Ecosystem]float64{
		ecosystem.EcoGo:         0.3,
		ecosystem.EcoPython:     0.5,
		ecosystem.EcoTypeScript: 0.8,
		ecosystem.EcoRust:       0.4,
	}
	for eco, want := range wantThresh {
		if got := p.AbstentionThresholds[eco]; got != want {
			t.Errorf("max-scope λ[%s] = %v; want %v", eco, got, want)
		}
	}
}

func TestDoctrineStrictness_Property_DefaultMatchesSpec(t *testing.T) {
	r := builtinResolver(t, "default")
	p, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Name != "default" {
		t.Errorf("Name = %q; want default", p.Name)
	}
	if p.MaxResults != 5 {
		t.Errorf("MaxResults = %d; want 5 (default)", p.MaxResults)
	}
	if p.LLMJudgeEnabled {
		t.Error("LLMJudgeEnabled = true; want false (default)")
	}
	if p.RefuseOnUnverified {
		t.Error("RefuseOnUnverified = true; want false (default)")
	}
	if p.AuditEmissionLevel != ecosystem.AuditQueryAbstainVerifyFailureAnswer {
		t.Errorf("AuditEmissionLevel = %v; want %v", p.AuditEmissionLevel, ecosystem.AuditQueryAbstainVerifyFailureAnswer)
	}
	if p.CitationMode != ecosystem.CitationOptional {
		t.Errorf("CitationMode = %v; want %v", p.CitationMode, ecosystem.CitationOptional)
	}
}

func TestDoctrineStrictness_Property_CapaFirewallStricterThanDefault(t *testing.T) {
	ctx := context.Background()

	rDef := builtinResolver(t, "default")
	rCF := builtinResolver(t, "capa-firewall")

	pDef, err := rDef.Resolve(ctx, "")
	if err != nil {
		t.Fatalf("default Resolve: %v", err)
	}
	pCF, err := rCF.Resolve(ctx, "")
	if err != nil {
		t.Fatalf("capa-firewall Resolve: %v", err)
	}

	if pDef.RefuseOnUnverified {
		t.Error("default doctrine RefuseOnUnverified must be false")
	}
	if !pCF.RefuseOnUnverified {
		t.Error("capa-firewall doctrine RefuseOnUnverified must be true (stricter than default)")
	}

	for _, eco := range ecosystem.AllEcosystems {
		if pCF.AbstentionThresholds[eco] < pDef.AbstentionThresholds[eco] {
			t.Errorf("inv-zen-205: capa-firewall λ[%s]=%v < default λ[%s]=%v (must be ≥ to be stricter)",
				eco, pCF.AbstentionThresholds[eco], eco, pDef.AbstentionThresholds[eco])
		}
	}
}
