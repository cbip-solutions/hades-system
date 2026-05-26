package amendment_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

func TestCooldownArmThenSuppress(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	reg := amendment.NewCooldownRegistry(clk)
	pat := "max-scope|operator_override|tier_select|p1"
	if reg.Suppressed(pat) {
		t.Fatal("freshly-constructed registry should not suppress")
	}
	reg.Arm(pat, "max-scope")
	if !reg.Suppressed(pat) {
		t.Fatal("after Arm, Suppressed should return true")
	}
	clk.Advance(23*time.Hour + 59*time.Minute)
	if !reg.Suppressed(pat) {
		t.Fatal("still inside max-scope 24h window")
	}
	clk.Advance(2 * time.Minute)
	if reg.Suppressed(pat) {
		t.Fatal("after 24h+, max-scope window expired")
	}
}

func TestCooldownDoctrineSpecificWindows(t *testing.T) {
	cases := []struct {
		doctrine string
		hours    int
	}{
		{"max-scope", 24},
		{"default", 72},
		{"capa-firewall", 168},
	}
	for _, tc := range cases {
		t.Run(tc.doctrine, func(t *testing.T) {
			clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
			reg := amendment.NewCooldownRegistry(clk)
			pat := tc.doctrine + "|x|y|z"
			reg.Arm(pat, tc.doctrine)
			clk.Advance(time.Duration(tc.hours-1) * time.Hour)
			if !reg.Suppressed(pat) {
				t.Errorf("%s: expected still suppressed at %dh", tc.doctrine, tc.hours-1)
			}
			clk.Advance(2 * time.Hour)
			if reg.Suppressed(pat) {
				t.Errorf("%s: expected expired past %dh", tc.doctrine, tc.hours+1)
			}
		})
	}
}

func TestCooldownDistinctPatterns(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	reg := amendment.NewCooldownRegistry(clk)
	reg.Arm("a", "max-scope")
	if reg.Suppressed("b") {
		t.Fatal("distinct pattern should not be suppressed")
	}
	if !reg.Suppressed("a") {
		t.Fatal("armed pattern should be suppressed")
	}
}

func TestCooldownReArmExtendsWindow(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	reg := amendment.NewCooldownRegistry(clk)
	reg.Arm("p", "max-scope")
	clk.Advance(20 * time.Hour)
	reg.Arm("p", "max-scope")
	clk.Advance(20 * time.Hour)
	if !reg.Suppressed("p") {
		t.Fatal("re-arm should extend window past T+40h")
	}
}

func TestCooldownUnknownDoctrineUsesMaxScope(t *testing.T) {
	if got := amendment.CooldownWindowFor("nonexistent"); got != 24*time.Hour {
		t.Errorf("unknown doctrine should fall back to 24h, got %v", got)
	}
	if got := amendment.CooldownWindowFor(""); got != 24*time.Hour {
		t.Errorf("empty doctrine should fall back to 24h, got %v", got)
	}
}

func TestCooldownExpiredEntryGCd(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	reg := amendment.NewCooldownRegistry(clk)
	reg.Arm("p", "max-scope")
	clk.Advance(25 * time.Hour)
	if reg.Suppressed("p") {
		t.Fatal("expected expired")
	}

	if reg.Suppressed("p") {
		t.Fatal("after GC, second call must return false via no-entry branch")
	}
}

func TestNewCooldownRegistryPanicsOnNilClock(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil clock")
		}
	}()
	amendment.NewCooldownRegistry(nil)
}

func TestCooldownConcurrentArmSuppress(t *testing.T) {

	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	reg := amendment.NewCooldownRegistry(clk)
	const N = 64
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			reg.Arm("p", "max-scope")
			_ = reg.Suppressed("p")
			_ = idx
		}(i)
		go func() {
			defer wg.Done()
			_ = reg.Suppressed("p")
		}()
	}
	wg.Wait()
	if !reg.Suppressed("p") {
		t.Fatal("expected suppressed after concurrent Arm")
	}
}
