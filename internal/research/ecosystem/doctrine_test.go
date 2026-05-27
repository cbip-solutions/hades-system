// internal/research/ecosystem/doctrine_test.go
//
// Tests for DoctrineProfile + DoctrineResolver ( Task A-8;
// see plan-file lines 5005-5253 for canonical test set, revised
// 2026-05-17 amendment to reflect v1.Schema Name-field absence).
//
// Plan-template drift reconciliation: the original plan-file assumed
// v1.Schema carried a `Name string` field. It does not (verified via
// `internal/doctrine/schema/v1/schema.go`). The resolver therefore
// takes its own copy of the registry via SetRegistry + reverses the
// name via pointer-equality scan (same posture as
// `internal/daemon/server_doctrine.go doctrineNameForSchema`). The
// test seed (`builtinAccessor`) constructs both the Accessor's
// registry AND the resolver's registry from the SAME map of pointers,
// so pointer-equality holds.
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files require ≥90% per-function coverage.
// The doctrine knob governs hallucination-mitigation behaviour (refuse-on-
// unverified, LLM-judge, abstention thresholds, audit emission level); a
// silently mis-resolved doctrine would silently weaken the hallucination
// guard rails. Tests cover Resolve's ctx-err / builtin-hit / custom-fallback /
// project-scoped branches AND NewDoctrineResolver's nil-accessor panic.

package ecosystem

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestDoctrineProfileFields(t *testing.T) {
	p := DoctrineProfile{
		Name:                    "max-scope",
		MaxResults:              10,
		AbstentionThresholds:    map[Ecosystem]float64{EcoGo: 0.3, EcoPython: 0.5, EcoTypeScript: 0.8, EcoRust: 0.4},
		LLMJudgeEnabled:         true,
		RefuseOnUnverified:      false,
		SkipLLMVersionDetection: false,
		CitationMode:            CitationMandatoryGrammar,
		AuditEmissionLevel:      AuditAll8Events,
		CRPrefixLLM:             "qwen2.5:7b",
	}
	if p.MaxResults != 10 || len(p.AbstentionThresholds) != 4 {
		t.Errorf("DoctrineProfile field-set mismatch: %+v", p)
	}
}

func TestAuditLevelStringers(t *testing.T) {
	cases := []struct {
		got  AuditLevel
		want string
	}{
		{AuditAll8Events, "all-8-events"},
		{AuditQueryAbstainVerifyFailureAnswer, "query+abstain+verify-failure+answer"},
		{AuditMinimal, "minimal"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("AuditLevel %v string = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func TestCitationModeStringers(t *testing.T) {
	cases := []struct {
		got  CitationMode
		want string
	}{
		{CitationMandatoryGrammar, "mandatory_grammar"},
		{CitationOptional, "optional"},
		{CitationNone, "none"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("CitationMode %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func builtinAccessor(t *testing.T) (*active.Accessor, map[string]*v1.Schema) {
	t.Helper()
	acc := active.NewAccessor()
	registry := map[string]*v1.Schema{
		"max-scope":     {},
		"default":       {},
		"capa-firewall": {},
	}
	acc.SetRegistry(registry)
	if err := acc.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	return acc, registry
}

func newSeededResolver(t *testing.T, userDefault string) *DoctrineResolver {
	t.Helper()
	acc, registry := builtinAccessor(t)
	if userDefault != "" {
		if err := acc.SetUserDefault(userDefault); err != nil {
			t.Fatalf("SetUserDefault(%q): %v", userDefault, err)
		}
	}
	r := NewDoctrineResolver(acc)
	r.SetRegistry(registry)
	return r
}

func TestDoctrineResolverResolveMaxScope(t *testing.T) {
	r := newSeededResolver(t, "max-scope")
	prof, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.Name != "max-scope" {
		t.Errorf("Name = %q; want %q", prof.Name, "max-scope")
	}
	if prof.MaxResults != 10 {
		t.Errorf("MaxResults = %d; want 10 (max-scope per spec §2.7)", prof.MaxResults)
	}
	if !prof.LLMJudgeEnabled {
		t.Errorf("LLMJudgeEnabled = false; want true (max-scope per spec §2.7)")
	}
	if prof.RefuseOnUnverified {
		t.Errorf("RefuseOnUnverified = true; want false (max-scope per spec §2.7)")
	}
	if prof.AuditEmissionLevel != AuditAll8Events {
		t.Errorf("AuditEmissionLevel = %v; want AuditAll8Events", prof.AuditEmissionLevel)
	}

	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.3, EcoPython: 0.5, EcoTypeScript: 0.8, EcoRust: 0.4,
	}
	for eco, want := range wantThresh {
		if got := prof.AbstentionThresholds[eco]; got != want {
			t.Errorf("max-scope λ[%s] = %v; want %v", eco, got, want)
		}
	}
}

func TestDoctrineResolverResolveDefault(t *testing.T) {
	r := newSeededResolver(t, "")
	prof, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.Name != "default" {
		t.Errorf("Name = %q; want %q", prof.Name, "default")
	}
	if prof.MaxResults != 5 {
		t.Errorf("MaxResults = %d; want 5 (default per spec §2.7)", prof.MaxResults)
	}
	if prof.LLMJudgeEnabled {
		t.Errorf("LLMJudgeEnabled = true; want false (default per spec §2.7)")
	}
	if prof.RefuseOnUnverified {
		t.Errorf("RefuseOnUnverified = true; want false (default per spec §2.7)")
	}
	if prof.AuditEmissionLevel != AuditQueryAbstainVerifyFailureAnswer {
		t.Errorf("AuditEmissionLevel = %v; want AuditQueryAbstainVerifyFailureAnswer",
			prof.AuditEmissionLevel)
	}

	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.45, EcoPython: 0.75, EcoTypeScript: 1.2, EcoRust: 0.6,
	}
	for eco, want := range wantThresh {
		got := prof.AbstentionThresholds[eco]
		if absf(got-want) > 1e-9 {
			t.Errorf("default λ[%s] = %v; want %v (mid-λ ×1.5)", eco, got, want)
		}
	}
}

func TestDoctrineResolverResolveCapaFirewall(t *testing.T) {
	r := newSeededResolver(t, "capa-firewall")
	prof, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.Name != "capa-firewall" {
		t.Errorf("Name = %q; want %q", prof.Name, "capa-firewall")
	}
	if !prof.LLMJudgeEnabled {
		t.Errorf("LLMJudgeEnabled = false; want true (capa-firewall per spec §2.7)")
	}
	if !prof.RefuseOnUnverified {
		t.Errorf("RefuseOnUnverified = false; want true (capa-firewall per spec §2.7)")
	}

	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.6, EcoPython: 1.0, EcoTypeScript: 1.6, EcoRust: 0.8,
	}
	for eco, want := range wantThresh {
		got := prof.AbstentionThresholds[eco]
		if absf(got-want) > 1e-9 {
			t.Errorf("capa-firewall λ[%s] = %v; want %v (high-λ ×2.0)", eco, got, want)
		}
	}
}

func TestDoctrineResolverResolveCustomFallsBackToDefault(t *testing.T) {

	acc := active.NewAccessor()
	registry := map[string]*v1.Schema{
		"my-custom-doctrine": {},
		"max-scope":          {},
	}
	acc.SetRegistry(registry)
	if err := acc.SetUserDefault("my-custom-doctrine"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	r := NewDoctrineResolver(acc)
	r.SetRegistry(registry)
	prof, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.Name != "my-custom-doctrine" {
		t.Errorf("Name = %q; want %q (preserve operator choice)", prof.Name, "my-custom-doctrine")
	}

	if prof.MaxResults != 5 {
		t.Errorf("custom MaxResults = %d; want 5 (default fallback)", prof.MaxResults)
	}
	if prof.LLMJudgeEnabled {
		t.Errorf("custom LLMJudgeEnabled = true; want false (default fallback)")
	}
}

func TestDoctrineResolverResolveProjectScoped(t *testing.T) {
	acc, registry := builtinAccessor(t)

	acc.SetForProject("p1", registry["capa-firewall"])
	r := NewDoctrineResolver(acc)
	r.SetRegistry(registry)

	prof, err := r.Resolve(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Resolve p1: %v", err)
	}
	if prof.Name != "capa-firewall" {
		t.Errorf("p1 Name = %q; want %q", prof.Name, "capa-firewall")
	}

	prof2, err := r.Resolve(context.Background(), "p2-not-set")
	if err != nil {
		t.Fatalf("Resolve p2: %v", err)
	}
	if prof2.Name != "default" {
		t.Errorf("p2 Name = %q; want %q (fallback to userDefault)", prof2.Name, "default")
	}
}

func TestDoctrineResolverResolveDefensiveCopy(t *testing.T) {

	r := newSeededResolver(t, "max-scope")

	first, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}

	first.AbstentionThresholds[EcoGo] = 999.0

	second, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if got := second.AbstentionThresholds[EcoGo]; got != 0.3 {
		t.Errorf("second resolve λ[Go] = %v; want 0.3 (defensive copy violated)", got)
	}
}

func TestDoctrineResolverSetRegistryDefensiveCopy(t *testing.T) {

	acc := active.NewAccessor()
	registry := map[string]*v1.Schema{
		"max-scope": {},
		"default":   {},
	}
	acc.SetRegistry(registry)
	if err := acc.SetUserDefault("max-scope"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	r := NewDoctrineResolver(acc)
	r.SetRegistry(registry)

	delete(registry, "max-scope")
	registry["tampered"] = &v1.Schema{}

	prof, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.Name != "max-scope" {
		t.Errorf("Name = %q; want %q (SetRegistry defensive copy violated)",
			prof.Name, "max-scope")
	}
}

func TestDoctrineResolverContextCancel(t *testing.T) {
	r := newSeededResolver(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Resolve(ctx, ""); err == nil {
		t.Errorf("Resolve(cancelled): want error; got nil")
	}
}

func TestDoctrineResolverNilAccessorPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewDoctrineResolver(nil): want panic; got none")
		}
	}()
	_ = NewDoctrineResolver(nil)
}

func TestDoctrineResolverNameForSchemaNil(t *testing.T) {

	r := newSeededResolver(t, "")
	if got := r.nameForSchema(nil); got != "" {
		t.Errorf("nameForSchema(nil) = %q; want \"\"", got)
	}
}

func TestDoctrineResolverNameForSchemaUnknownPointer(t *testing.T) {

	r := newSeededResolver(t, "")
	stranger := &v1.Schema{}
	if got := r.nameForSchema(stranger); got != "" {
		t.Errorf("nameForSchema(unknown-ptr) = %q; want \"\"", got)
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
