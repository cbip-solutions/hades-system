package onboard

import (
	"reflect"
	"testing"
	"testing/quick"
)

func TestGetDefaultsGlobal(t *testing.T) {
	d := GetDefaults(WizardKindGlobal)
	if d.LLMProvider == "" {
		t.Error("GetDefaults(Global): LLMProvider empty")
	}
	if d.Doctrine == "" {
		t.Error("GetDefaults(Global): Doctrine empty")
	}
	if len(d.MCPSelections) == 0 {
		t.Error("GetDefaults(Global): MCPSelections empty (expected tier 1+2 set)")
	}
}

func TestGetDefaultsGreenfield(t *testing.T) {
	d := GetDefaults(WizardKindGreenfield)
	if d.TemplateName == "" {
		t.Error("GetDefaults(Greenfield): TemplateName empty")
	}
	if d.Doctrine == "" {
		t.Error("GetDefaults(Greenfield): Doctrine empty")
	}
}

func TestGetDefaultsBrownfield(t *testing.T) {
	d := GetDefaults(WizardKindBrownfield)
	if d.Doctrine == "" {
		t.Error("GetDefaults(Brownfield): Doctrine empty")
	}
}

func TestGetDefaultsUnknownReturnsZero(t *testing.T) {
	d := GetDefaults(WizardKindUnknown)
	zero := WizardDefaults{}
	if !reflect.DeepEqual(d, zero) {
		t.Errorf("GetDefaults(Unknown): expected zero WizardDefaults, got %+v", d)
	}
	d2 := GetDefaults(WizardKind(99))
	if !reflect.DeepEqual(d2, zero) {
		t.Errorf("GetDefaults(99): expected zero WizardDefaults, got %+v", d2)
	}
}

func TestGetDefaultsDeterminism(t *testing.T) {
	determ := func(raw int) bool {
		k := WizardKind(raw % 4)
		a := GetDefaults(k)
		b := GetDefaults(k)
		return reflect.DeepEqual(a, b)
	}
	if err := quick.Check(determ, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("GetDefaults not deterministic: %v", err)
	}
}

func TestGetDefaultsReturnsCopy(t *testing.T) {
	a := GetDefaults(WizardKindGlobal)
	a.LLMProvider = "mutated"
	a.MCPSelections = append(a.MCPSelections, "extra-poison")
	b := GetDefaults(WizardKindGlobal)
	if b.LLMProvider == "mutated" {
		t.Errorf("GetDefaults LLMProvider not isolated: caller mutation leaked: %q", b.LLMProvider)
	}
	for _, m := range b.MCPSelections {
		if m == "extra-poison" {
			t.Errorf("GetDefaults MCPSelections not deep-copied: caller append leaked")
		}
	}
}
