package audit

import (
	"sort"
	"strings"
	"testing"
)

func TestDefaultCriteriaTemplateNames(t *testing.T) {
	got := DefaultCriteriaTemplateNames()
	want := []string{"default", "doctrine-violation", "performance", "security"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("DefaultCriteriaTemplateNames must be sorted; got %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] got %q, want %q", i, got[i], w)
		}
	}
	// Every name returned MUST also be resolvable via the registry.
	reg := NewCriteriaRegistry(nil)
	for _, n := range got {
		if _, ok := reg.Get(n); !ok {
			t.Errorf("DefaultCriteriaTemplateNames advertises %q but registry rejects it", n)
		}
	}
}

// TestBuiltinTemplatesExist verifies that all four built-in criteria names
// resolve to non-empty prompt strings. Per spec §2.2 Capa 3:
// built-in: default, security, performance, doctrine-violation.
func TestBuiltinTemplatesExist(t *testing.T) {
	reg := NewCriteriaRegistry(nil)
	for _, name := range []string{"default", "security", "performance", "doctrine-violation"} {
		tmpl, ok := reg.Get(name)
		if !ok {
			t.Errorf("built-in criteria %q not found in registry", name)
		}
		if strings.TrimSpace(tmpl) == "" {
			t.Errorf("built-in criteria %q has empty template", name)
		}
	}
}

func TestBuiltinTemplatesContainStructuredOutputInstruction(t *testing.T) {
	reg := NewCriteriaRegistry(nil)
	for _, name := range []string{"default", "security", "performance", "doctrine-violation"} {
		tmpl, _ := reg.Get(name)
		lower := strings.ToLower(tmpl)
		if !strings.Contains(lower, "classification") {
			t.Errorf("criteria %q template missing 'classification' instruction", name)
		}
		if !strings.Contains(lower, "concerns") {
			t.Errorf("criteria %q template missing 'concerns' instruction", name)
		}
		if !strings.Contains(lower, "suggestions") {
			t.Errorf("criteria %q template missing 'suggestions' instruction", name)
		}

		for _, cls := range []string{"clean", "minor", "major", "reject"} {
			if !strings.Contains(lower, cls) {
				t.Errorf("criteria %q template missing classification value %q", name, cls)
			}
		}
	}
}

func TestCustomCriteriaOverridesBuiltin(t *testing.T) {
	custom := map[string]string{
		"default": "custom default prompt text for testing",
	}
	reg := NewCriteriaRegistry(custom)
	tmpl, ok := reg.Get("default")
	if !ok {
		t.Fatal("default criteria not found after custom override")
	}
	if tmpl != "custom default prompt text for testing" {
		t.Errorf("custom override not applied: got %q", tmpl)
	}
}

func TestCustomCriteriaNewName(t *testing.T) {
	custom := map[string]string{
		"internal-platform-x-specific": "check internal-platform-x invariants per internal-platform-x docs",
	}
	reg := NewCriteriaRegistry(custom)
	tmpl, ok := reg.Get("internal-platform-x-specific")
	if !ok {
		t.Fatal("custom criteria internal-platform-x-specific not found")
	}
	if tmpl == "" {
		t.Error("custom criteria template is empty")
	}
}

func TestUnknownCriteriaFallsBackToDefault(t *testing.T) {
	reg := NewCriteriaRegistry(nil)
	tmpl, ok := reg.Get("nonexistent-criteria-xyz")
	if ok {
		t.Error("Get(nonexistent) should return ok=false")
	}
	if strings.TrimSpace(tmpl) == "" {
		t.Error("Get(nonexistent) should return non-empty default fallback template")
	}
}

func TestCriteriaRegistryListNames(t *testing.T) {
	custom := map[string]string{"custom-one": "prompt one"}
	reg := NewCriteriaRegistry(custom)
	names := reg.Names()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for _, required := range []string{"default", "security", "performance", "doctrine-violation", "custom-one"} {
		if !nameSet[required] {
			t.Errorf("Names() missing %q", required)
		}
	}
}
