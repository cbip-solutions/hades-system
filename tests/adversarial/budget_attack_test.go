//go:build adversarial

package adversarial

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/budget"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func openAdversarialStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "advb.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAttack_RaceConditionDoubleTag(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	const G = 100
	const R = 10
	const C = 6
	costIDs := make([]int64, C)
	for i := 0; i < C; i++ {
		costIDs[i] = int64(1000 + i)
	}

	axisTags := func() map[string]string {
		return map[string]string{
			"project":   "internal-platform-x",
			"doctrine":  "max-scope",
			"stage":     "design",
			"task":      "T-X",
			"operation": "concurrent-retry",
			"worker_id": "w-race",
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for r := 0; r < R; r++ {
				for _, cid := range costIDs {
					_ = a.PostCall(context.Background(), cid, axisTags())
				}
			}
		}(g)
	}
	wg.Wait()

	for _, cid := range costIDs {
		tags, err := store.QueryCostAxisTags(s.DB(), cid)
		if err != nil {
			t.Fatalf("QueryCostAxisTags(%d): %v", cid, err)
		}
		if len(tags) != 6 {
			t.Errorf("cost_id=%d len(tags) = %d, want 6", cid, len(tags))
		}
	}
}

func TestAttack_LowAndSlowAnomalyEvasion(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	a.SetAnomalyConfig(4.0, 60)
	scopes := budget.Scopes{
		Project: "internal-platform-x", Doctrine: "max-scope", Stage: "design", Worker: "w-evasive",
	}
	caps := budget.Caps{Project: 1000, Doctrine: 5000, Stage: 5000, Worker: 5000}
	const N = 1440
	now := time.Now().UnixMilli()
	zTriggered := 0
	blockedAt := -1

	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design",
		"task": "T-evasive", "worker_id": "w-evasive",
	}

	for i := 0; i < N; i++ {

		usd := 1.00 + (1.50 * float64(i) / float64(N))

		d, err := a.PreCall(context.Background(), scopes, caps, usd)
		if err != nil {
			t.Fatalf("sample %d PreCall: %v", i, err)
		}
		if !d.Allowed {
			blockedAt = i
			break
		}

		if err := a.AppendAnomalySample(context.Background(), "stage", "design", usd, now+int64(i)); err != nil {
			t.Fatalf("sample %d Append: %v", i, err)
		}
		res, _ := a.Detector().Update(context.Background(), "stage", "design", usd)
		if res.Triggered {
			zTriggered++
		}
		if err := a.PostCall(context.Background(), int64(i+1), axisTags); err != nil {
			t.Fatalf("sample %d PostCall: %v", i, err)
		}
	}

	t.Logf("low-and-slow: zTriggered=%d / 1440 (defense expected via cap, not z)", zTriggered)
	t.Logf("blockedAt=%d (negative = no cap block; expected pre-cost_ledger)", blockedAt)

	if blockedAt != -1 {
		t.Logf("UNEXPECTED: cap tripped pre-cost_ledger merge (sample %d)", blockedAt)
	}
}

func TestAttack_ConcurrentPauseFlapping(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	pauser := a.Pauser()
	const G = 50
	const R = 100
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for r := 0; r < R; r++ {
				if (gID+r)%2 == 0 {
					_ = pauser.Trigger(context.Background(), "stage", "design", "flap", time.Hour)
				} else {
					_ = pauser.Resume(context.Background(), "stage", "design")
				}
				_, _ = pauser.IsPaused(context.Background(), "stage", "design")
			}
		}(g)
	}
	wg.Wait()

	if _, err := pauser.IsPaused(context.Background(), "stage", "design"); err != nil {
		t.Errorf("post-flap IsPaused: %v", err)
	}
}

func TestAttack_TagInjectionMaliciousAxisName(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	cid := int64(50001)

	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "",
		"stage":    "design",
		"task":     "T-1",
	}
	err := a.PostCall(context.Background(), cid, axisTags)
	if err == nil {
		t.Errorf("err = nil, want ErrAxisIncomplete on empty doctrine")
	}
	losses, _ := store.QueryAxisTagLosses(s.DB(), cid)
	if len(losses) != 1 || losses[0].MissingAxis != "doctrine" {
		t.Errorf("losses = %v, want [doctrine]", losses)
	}

	axisTags2 := map[string]string{
		"project":              "internal-platform-x",
		"doctrine":             "max-scope",
		"stage":                "design",
		"task":                 "T-1",
		"unauthorised_axis":    "exfil",
		"--injected; DROP TBL": "x",
	}
	cid2 := int64(50002)
	if err := a.PostCall(context.Background(), cid2, axisTags2); err != nil {
		t.Errorf("PostCall with extra axes: %v (engine should ignore unknown)", err)
	}
	tags, _ := store.QueryCostAxisTags(s.DB(), cid2)
	for _, tag := range tags {
		if tag.AxisName == "unauthorised_axis" || tag.AxisName == "--injected; DROP TBL" {
			t.Errorf("engine wrote unauthorised axis %q", tag.AxisName)
		}
	}
	if len(tags) != 4 {
		t.Errorf("len(tags) = %d, want 4 (4 canonical required, no unknown)", len(tags))
	}
}

func TestAttack_ConcurrentAnomalyTrigger(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	a.SetAnomalyConfig(2.0, 60)

	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03, 0.98, 1.02} {
		if err := a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i)); err != nil {
			t.Fatalf("seed AppendAnomalySample: %v", err)
		}
	}

	const G = 100
	const sample = 50.0
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.Detector().Update(context.Background(), "stage", "design", sample)
		}()
	}
	wg.Wait()

	rows, err := store.ListBudgetAnomalies(s.DB(), 1000)
	if err != nil {
		t.Fatalf("ListBudgetAnomalies: %v", err)
	}
	scopeRows := 0
	for _, r := range rows {
		if r.Scope == "stage" && r.ScopeValue == "design" {
			scopeRows++
		}
	}
	if scopeRows != 1 {
		t.Errorf("budget_anomalies count for stage:design = %d, want exactly 1 (per-scope mutex + same-sample dedupe)", scopeRows)
	}
}

func TestAttack_ConcurrentDifferentCostIDs(t *testing.T) {
	s := openAdversarialStore(t)
	a := dispatcheradapter.NewBudgetAdapter(s)
	const G = 200
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-X",
	}
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			cid := int64(60000 + gID)
			_ = a.PostCall(context.Background(), cid, axisTags)
		}(g)
	}
	wg.Wait()

	for g := 0; g < G; g++ {
		cid := int64(60000 + g)
		tags, _ := store.QueryCostAxisTags(s.DB(), cid)
		if len(tags) != 4 {
			t.Errorf("cost_id=%d len = %d, want 4", cid, len(tags))
		}
	}
}
