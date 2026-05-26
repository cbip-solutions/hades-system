package config_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func TestBuiltinProfileDefaults(t *testing.T) {
	defs := config.BuiltinProfileDefaults()

	for _, role := range []string{"orchestrator", "worker-code", "worker-reasoning", "tactical", "local-code"} {
		c, ok := defs[role]
		if !ok {
			t.Errorf("built-in defaults missing role %q", role)
			continue
		}
		if len(c.Cascade) == 0 {
			t.Errorf("role %q has an empty default cascade", role)
		}
		if c.Name != role {
			t.Errorf("role %q ProfileConfig.Name = %q", role, c.Name)
		}
	}
}

func TestBuiltinProfileDefaultsIsolated(t *testing.T) {
	a := config.BuiltinProfileDefaults()
	delete(a, "orchestrator")
	b := config.BuiltinProfileDefaults()
	if _, ok := b["orchestrator"]; !ok {
		t.Error("BuiltinProfileDefaults shares state across calls")
	}
}

func TestProfileResolverBaseLayer(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{})
	got, err := r.Resolve("worker-code", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"deepseek-direct", "siliconflow-deepseek", "openrouter-deepseek"}
	if !equalStrings(got, want) {
		t.Errorf("Resolve = %v, want %v", got, want)
	}
}

func TestProfileResolverProfilesTOMLOverrides(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"worker-code": {Name: "worker-code", Cascade: []string{"only-deepseek"}},
		},
	})
	got, err := r.Resolve("worker-code", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !equalStrings(got, []string{"only-deepseek"}) {
		t.Errorf("Resolve = %v, want [only-deepseek] (profiles.toml replaces base)", got)
	}
}

func TestProfileResolverProjectOverridesProfiles(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"worker-code": {Name: "worker-code", Cascade: []string{"from-profiles"}},
		},
		Orchestrators: map[string]config.OrchestratorConfig{
			"alpha": {FallbackChain: []string{"from-project-a", "from-project-b"}},
		},
	})
	got, err := r.Resolve("worker-code", "alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !equalStrings(got, []string{"from-project-a", "from-project-b"}) {
		t.Errorf("Resolve = %v, want the project FallbackChain", got)
	}
}

func TestProfileResolverDeterministic(t *testing.T) {
	layers := config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"worker-code":      {Name: "worker-code", Cascade: []string{"p1", "p2"}},
			"orchestrator":     {Name: "orchestrator", Cascade: []string{"o1"}},
			"tactical":         {Name: "tactical", Cascade: []string{"t1", "t2", "t3"}},
			"worker-reasoning": {Name: "worker-reasoning", Cascade: []string{"r1"}},
		},
		Orchestrators: map[string]config.OrchestratorConfig{
			"alpha": {FallbackChain: []string{"a1", "a2"}},
			"beta":  {Default: "tactical"},
		},
	}
	r := config.NewProfileResolver(layers)
	first, err := r.Resolve("worker-code", "alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, err := r.Resolve("worker-code", "alpha")
		if err != nil {
			t.Fatalf("Resolve iteration %d: %v", i, err)
		}
		if !equalStrings(got, first) {
			t.Fatalf("inv-zen-066 violated: iteration %d = %v, first = %v", i, got, first)
		}
	}
}

func TestProfileResolverUnknownProfile(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{})
	_, err := r.Resolve("nonexistent-profile", "")
	if err == nil {
		t.Fatal("Resolve returned nil error for an unknown profile")
	}
}

func TestProfileResolverProjectDefaultProfile(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		Orchestrators: map[string]config.OrchestratorConfig{
			"beta": {Default: "tactical"},
		},
	})
	got, err := r.Resolve("", "beta")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !equalStrings(got, []string{"gemini-flash", "zhipu-glm-flash", "openrouter-glm"}) {
		t.Errorf("Resolve = %v, want the tactical built-in cascade", got)
	}
}

func TestProfileResolverCheckoutCascade(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"worker-code": {Name: "worker-code", Cascade: []string{"from-profiles"}},
		},
		Orchestrators: map[string]config.OrchestratorConfig{
			"alpha": {FallbackChain: []string{"from-project"}},
		},
		CheckoutCascade: []string{"from-checkout-1", "from-checkout-2"},
	})
	got, err := r.Resolve("worker-code", "alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !equalStrings(got, []string{"from-checkout-1", "from-checkout-2"}) {
		t.Errorf("Resolve = %v, want CheckoutCascade to replace lower layers", got)
	}
}

func TestProfileResolverCheckoutProfileFallback(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		CheckoutProfile: "tactical",
	})
	got, err := r.Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !equalStrings(got, []string{"gemini-flash", "zhipu-glm-flash", "openrouter-glm"}) {
		t.Errorf("Resolve = %v, want tactical built-in via CheckoutProfile fallback", got)
	}
}

func TestProfileResolverNoProfileName(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{})
	_, err := r.Resolve("", "")
	if err == nil {
		t.Fatal("Resolve returned nil error when no profile name was resolvable")
	}
}

func TestProfileResolverProfileNames(t *testing.T) {
	r := config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles: map[string]config.ProfileConfig{
			"custom-profile": {Name: "custom-profile", Cascade: []string{"foo", "bar"}},
		},
	})
	names := r.ProfileNames()

	foundCustom := false
	foundBuiltin := false
	for _, n := range names {
		if n == "custom-profile" {
			foundCustom = true
		}
		if n == "worker-code" {
			foundBuiltin = true
		}
	}
	if !foundCustom {
		t.Errorf("ProfileNames() missing custom-profile; got %v", names)
	}
	if !foundBuiltin {
		t.Errorf("ProfileNames() missing built-in worker-code; got %v", names)
	}

	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("ProfileNames() not sorted: %v", names)
			break
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
