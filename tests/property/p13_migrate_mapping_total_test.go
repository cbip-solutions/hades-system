// go:build property
//go:build property
// +build property

package property

import (
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func TestProperty_MigrateMapping_TotalForEmptyInventory(t *testing.T) {
	cfg := &quick.Config{MaxCount: 30}
	err := quick.Check(func(presetSelector bool) bool {
		preset := mapping.PresetLenient
		if presetSelector {
			preset = mapping.PresetStrict
		}
		inv := &source.Inventory{}
		plan, err := mapping.Map(inv, preset)
		if err != nil {
			t.Errorf("Map(empty, %s): unexpected err %v", preset, err)
			return false
		}
		if plan == nil {
			t.Errorf("Map(empty, %s): plan nil", preset)
			return false
		}
		if plan.Preset != preset {
			t.Errorf("Plan.Preset = %s; want %s", plan.Preset, preset)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("empty-inventory totality property failed: %v", err)
	}
}

func TestProperty_MigrateMapping_NilInventoryReturnsEmptyPlan(t *testing.T) {
	plan, err := mapping.Map(nil, mapping.PresetStrict)
	if err != nil {
		t.Fatalf("Map(nil, strict): %v", err)
	}
	if plan == nil {
		t.Fatal("Map(nil, strict): plan nil")
	}
	if len(plan.Entries) != 0 {
		t.Errorf("entries = %d; want 0 for nil inventory", len(plan.Entries))
	}
}

func TestProperty_MigrateMapping_InvalidPresetAlwaysErrors(t *testing.T) {
	cfg := &quick.Config{MaxCount: 30}
	err := quick.Check(func(s string) bool {
		preset := mapping.Preset(s)
		if preset == mapping.PresetStrict || preset == mapping.PresetLenient {
			return true
		}
		_, err := mapping.Map(&source.Inventory{}, preset)
		if err == nil {
			t.Errorf("Map with invalid preset %q: err = nil; want ErrInvalidPreset", s)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("invalid-preset totality property failed: %v", err)
	}
}
