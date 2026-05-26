//go:build property && cgo

package ecosystem_property_test

import (
	"math"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestBudget_Property_ClassifyDeterministic(t *testing.T) {
	prop := func(uRaw, tRaw, cRaw uint32) bool {

		target := 10.0 + float64(tRaw%490)
		ceiling := target + 1.0 + float64(cRaw%500)
		usage := float64(uRaw%2000) / 2.0

		s1 := ecosystem.ClassifyBudgetState(usage, target, ceiling)
		s2 := ecosystem.ClassifyBudgetState(usage, target, ceiling)
		return s1 == s2
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-199: ClassifyBudgetState non-deterministic: %v", err)
	}
}

func TestBudget_Property_MonotonicWithUsage(t *testing.T) {
	prop := func(loRaw, deltaRaw, tRaw, cRaw uint32) bool {
		target := 10.0 + float64(tRaw%490)
		ceiling := target + 1.0 + float64(cRaw%500)
		lo := float64(loRaw%1000) / 2.0
		delta := float64(deltaRaw%500) / 2.0
		hi := lo + delta
		if math.IsInf(hi, 0) || math.IsNaN(hi) {
			return true
		}
		s1 := ecosystem.ClassifyBudgetState(lo, target, ceiling)
		s2 := ecosystem.ClassifyBudgetState(hi, target, ceiling)
		if s2 < s1 {
			t.Logf("inv-zen-199: usage ↑ but state ↓: lo=%.1f s1=%v hi=%.1f s2=%v target=%.1f ceiling=%.1f",
				lo, s1, hi, s2, target, ceiling)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 2000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-199: state not monotonic with usage: %v", err)
	}
}

func TestBudget_Property_SpecTableAtDefaultThresholds(t *testing.T) {
	const target = 40.0
	const ceiling = 60.0
	table := []struct {
		usage float64
		want  ecosystem.BudgetState
		desc  string
	}{
		{0.0, ecosystem.BudgetGreen, "0 GB"},
		{16.0, ecosystem.BudgetGreen, "16 GB"},
		{31.9, ecosystem.BudgetGreen, "31.9 GB"},
		{32.0, ecosystem.BudgetYellow, "32 GB (target × 80%)"},
		{35.0, ecosystem.BudgetYellow, "35 GB"},
		{39.999, ecosystem.BudgetYellow, "39.999 GB"},
		{40.0, ecosystem.BudgetRed, "40 GB (target)"},
		{50.0, ecosystem.BudgetRed, "50 GB"},
		{59.999, ecosystem.BudgetRed, "59.999 GB"},
		{60.0, ecosystem.BudgetOverflow, "60 GB (ceiling)"},
		{100.0, ecosystem.BudgetOverflow, "100 GB"},
	}
	for _, tc := range table {
		got := ecosystem.ClassifyBudgetState(tc.usage, target, ceiling)
		if got != tc.want {
			t.Errorf("inv-zen-199: ClassifyBudgetState(%v) = %v; want %v (%s)",
				tc.usage, got, tc.want, tc.desc)
		}
	}
}
