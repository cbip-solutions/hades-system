package amendment_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
)

func TestRevertCooldownLastRevertedAtZeroIfNeverArmed(t *testing.T) {
	c := amendment.NewRevertCooldown()
	if _, _, ok := c.LastRevertedAt("autonomy.cost_degradation.soft_check_usd"); ok {
		t.Errorf("LastRevertedAt returned ok=true for never-armed rule")
	}
}

func TestRevertCooldownMarkAndReadBack(t *testing.T) {
	c := amendment.NewRevertCooldown()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	c.MarkReverted("autonomy.cost_degradation.soft_check_usd", now, 24*time.Hour)

	when, cooldown, ok := c.LastRevertedAt("autonomy.cost_degradation.soft_check_usd")
	if !ok {
		t.Fatalf("LastRevertedAt ok=false after MarkReverted")
	}
	if !when.Equal(now) {
		t.Errorf("when=%v, want %v", when, now)
	}
	if cooldown != 24*time.Hour {
		t.Errorf("cooldown=%v, want 24h", cooldown)
	}
}

func TestRevertCooldownReplaceOnRepeatedMark(t *testing.T) {
	c := amendment.NewRevertCooldown()
	now1 := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	now2 := now1.Add(time.Hour)

	c.MarkReverted("rule", now1, 24*time.Hour)
	c.MarkReverted("rule", now2, 48*time.Hour)

	when, cooldown, ok := c.LastRevertedAt("rule")
	if !ok || !when.Equal(now2) || cooldown != 48*time.Hour {
		t.Errorf("LastRevertedAt = (%v, %v, %v), want (%v, 48h, true)", when, cooldown, ok, now2)
	}
}

func TestRevertCooldownSnapshotCopiesAllRecords(t *testing.T) {
	c := amendment.NewRevertCooldown()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 4; i++ {
		c.MarkReverted(fmt.Sprintf("rule-%d", i), now.Add(time.Duration(i)*time.Hour), 24*time.Hour)
	}

	snap := c.Snapshot()
	if len(snap) != 4 {
		t.Errorf("snap len=%d, want 4", len(snap))
	}

	snap = append(snap, amendment.SnapshotEntry{RulePath: "mutated"})
	_ = snap

	when, _, ok := c.LastRevertedAt("rule-0")
	if !ok || !when.Equal(now) {
		t.Errorf("internal state mutated: rule-0 when=%v, want %v", when, now)
	}
}

func TestRevertCooldownConcurrentMarkAndRead(t *testing.T) {
	c := amendment.NewRevertCooldown()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			c.MarkReverted(fmt.Sprintf("rule-%d", i%4), now.Add(time.Duration(i)*time.Second), 24*time.Hour)
		}
	}()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _, _ = c.LastRevertedAt(fmt.Sprintf("rule-%d", j%4))
				_ = c.Snapshot()
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
