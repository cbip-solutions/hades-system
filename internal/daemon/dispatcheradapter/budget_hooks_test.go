package dispatcheradapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/budget"
	"github.com/cbip-solutions/hades-system/internal/store"
)

var errInjected = errors.New("injected test error")

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "adapter.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewBudgetAdapterRejectsNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewBudgetAdapter(nil)
}

func TestBudgetAdapterPostCallWritesAxisTags(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	costID := int64(101)

	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-1",
	}
	if err := a.PostCall(context.Background(), costID, axisTags); err != nil {
		t.Fatalf("PostCall: %v", err)
	}
	tags, err := store.QueryCostAxisTags(s.DB(), costID)
	if err != nil {
		t.Fatalf("QueryCostAxisTags: %v", err)
	}
	if len(tags) != 4 {
		t.Errorf("len(tags) = %d, want 4", len(tags))
	}
}

func TestBudgetAdapterPostCallEmitsLossOnIncomplete(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	costID := int64(202)

	axisTags := map[string]string{
		"project": "internal-platform-x",

		"stage": "design",
		"task":  "T-1",
	}
	err := a.PostCall(context.Background(), costID, axisTags)
	if err == nil {
		t.Error("err = nil, want ErrAxisIncomplete")
	}
	losses, _ := store.QueryAxisTagLosses(s.DB(), costID)
	if len(losses) != 1 {
		t.Errorf("len(losses) = %d, want 1", len(losses))
	}
}

func TestBudgetAdapterPreCallReturnsAllowedWhenUnderCap(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	scopes := budget.Scopes{Project: "internal-platform-x", Doctrine: "max-scope", Stage: "design", Worker: "w-42"}
	caps := budget.Caps{Project: 100, Doctrine: 100, Stage: 100, Worker: 100}
	d, err := a.PreCall(context.Background(), scopes, caps, 0.50)
	if err != nil {
		t.Fatalf("PreCall: %v", err)
	}
	if !d.Allowed {
		t.Errorf("Allowed = false, want true; BlockedScopes = %v", d.BlockedScopes)
	}
}

func TestBudgetAdapterPreCallBlocksWhenPaused(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	if err := a.Pauser().Trigger(context.Background(), "stage", "design", "manual", 0); err != nil {
		t.Fatalf("Pauser.Trigger: %v", err)
	}
	scopes := budget.Scopes{Project: "internal-platform-x", Doctrine: "max-scope", Stage: "design", Worker: "w-42"}
	caps := budget.Caps{Project: 100, Doctrine: 100, Stage: 100, Worker: 100}
	d, _ := a.PreCall(context.Background(), scopes, caps, 0.50)
	if d.Allowed {
		t.Errorf("Allowed = true, want false (stage paused)")
	}
	if len(d.BlockedScopes) != 1 || d.BlockedScopes[0] != "stage" {
		t.Errorf("BlockedScopes = %v, want [stage]", d.BlockedScopes)
	}
}

func TestBudgetAdapterRolledUSDByAxisReturnsZeroPreCostLedger(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	rolled, err := a.RolledUSDByAxis(context.Background(), "stage", "design", 0)
	if err != nil {
		t.Fatalf("RolledUSDByAxis: %v", err)
	}
	if rolled != 0 {
		t.Errorf("rolled = %f, want 0 (Option A: cost_ledger not yet merged)", rolled)
	}
}

func TestBudgetAdapterAnomalyAppendAndWindow(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.05, 0.99} {
		if err := a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i)); err != nil {
			t.Fatalf("AppendAnomalySample: %v", err)
		}
	}
	window, err := a.AnomalyWindow(context.Background(), "stage", "design", 100)
	if err != nil {
		t.Fatalf("AnomalyWindow: %v", err)
	}
	if len(window) != 3 {
		t.Errorf("len(window) = %d, want 3", len(window))
	}
}

func TestBudgetAdapterAnomalyAppendRowSurvivesRoundtrip(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	row := budget.AnomalyRow{
		Scope:      "stage",
		ScopeValue: "design",
		ZScore:     5.5,
		Mean:       1.0,
		Std:        0.1,
		WindowSize: 60,
		DetectedAt: time.Now(),
	}
	if err := a.AnomalyAppend(context.Background(), row); err != nil {
		t.Fatalf("AnomalyAppend: %v", err)
	}
	rows, err := store.ListBudgetAnomalies(s.DB(), 100)
	if err != nil {
		t.Fatalf("ListBudgetAnomalies: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Scope != "stage" || rows[0].ScopeValue != "design" || rows[0].ZScore != 5.5 {
		t.Errorf("row = %+v, want stage:design z=5.5", rows[0])
	}
}

func TestBudgetAdapterPauseRoundtrip(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	if err := a.PauseSet(context.Background(), "stage", "design", "manual", 0, 0); err != nil {
		t.Fatalf("PauseSet: %v", err)
	}

	active, autoMs, err := a.PauseGet(context.Background(), "stage", "design")
	if err != nil {
		t.Fatalf("PauseGet: %v", err)
	}
	if !active {
		t.Errorf("active = false, want true")
	}
	if autoMs != 0 {
		t.Errorf("autoMs = %d, want 0 (indefinite)", autoMs)
	}

	rows, _ := a.PauseListActive(context.Background())
	if len(rows) != 1 {
		t.Errorf("len = %d, want 1", len(rows))
	}

	if err := a.PauseClear(context.Background(), "stage", "design"); err != nil {
		t.Fatalf("PauseClear: %v", err)
	}
	active, _, _ = a.PauseGet(context.Background(), "stage", "design")
	if active {
		t.Error("active = true after Clear, want false")
	}
}

func TestBudgetAdapterPauseListActiveWithAutoResume(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	future := time.Now().Add(time.Hour).UnixMilli()
	_ = a.PauseSet(context.Background(), "worker_id", "w-1", "anomaly", 0, future)
	rows, _ := a.PauseListActive(context.Background())
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].AutoResumeAtMs != future {
		t.Errorf("AutoResumeAtMs = %d, want %d", rows[0].AutoResumeAtMs, future)
	}
}

func TestBudgetAdapterAxisQueryRoundtrip(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	if err := a.InsertCostAxisTag(context.Background(), 7, "stage", "design"); err != nil {
		t.Fatalf("InsertCostAxisTag: %v", err)
	}
	if err := a.InsertCostAxisTag(context.Background(), 7, "task", "T-1"); err != nil {
		t.Fatalf("InsertCostAxisTag: %v", err)
	}
	tags, _ := a.QueryAxisTags(context.Background(), 7)
	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2", len(tags))
	}
	if tags["stage"] != "design" || tags["task"] != "T-1" {
		t.Errorf("tags = %v, want stage:design task:T-1", tags)
	}

	ids, _ := a.QueryCostIDsByAxis(context.Background(), "stage", "design")
	if len(ids) != 1 || ids[0] != 7 {
		t.Errorf("ids = %v, want [7]", ids)
	}

	if err := a.EmitAxisTagLoss(context.Background(), 7, "doctrine"); err != nil {
		t.Fatalf("EmitAxisTagLoss: %v", err)
	}
	losses, _ := a.QueryAxisTagLosses(context.Background(), 7)
	if len(losses) != 1 || losses[0] != "doctrine" {
		t.Errorf("losses = %v, want [doctrine]", losses)
	}
}

func TestBudgetAdapterImplementsBudgetStore(t *testing.T) {

	var _ budget.BudgetStore = (*BudgetAdapter)(nil)
}

func TestBudgetAdapterDetectorAndPauserAccessors(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	if a.Pauser() == nil {
		t.Error("Pauser() returned nil")
	}
	if a.Detector() == nil {
		t.Error("Detector() returned nil")
	}
	if a.Tagger() == nil {
		t.Error("Tagger() returned nil")
	}
	if a.Gate() == nil {
		t.Error("Gate() returned nil")
	}
}

func TestBudgetAdapterSetAnomalyConfig(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	a.SetAnomalyConfig(3.0, 30)
	det := a.Detector()
	if det.Threshold() != 3.0 {
		t.Errorf("Threshold = %f, want 3.0", det.Threshold())
	}
	if det.WindowMax() != 30 {
		t.Errorf("WindowMax = %d, want 30", det.WindowMax())
	}
}

func TestBudgetAdapterPostCallWithCostTriggersAnomalyPause(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	a.SetAnomalyConfig(2.0, 60)
	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0, 0.98, 1.02} {
		_ = a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i))
	}
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}
	if err := a.PostCallWithCost(context.Background(), 999, axisTags, 100.0); err != nil {
		t.Fatalf("PostCallWithCost: %v", err)
	}

	paused, _ := a.Pauser().IsPaused(context.Background(), "stage", "design")
	if !paused {
		t.Errorf("paused = false, want true (anomaly triggered auto-pause)")
	}

	rows, _ := store.ListBudgetAnomalies(s.DB(), 100)
	if len(rows) < 1 {
		t.Errorf("budget_anomalies count = %d, want >= 1", len(rows))
	}
}

func TestBudgetAdapterPostCallWithCostNoTriggerNoPause(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	a.SetAnomalyConfig(4.0, 60)
	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0, 0.98, 1.02} {
		_ = a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i))
	}
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}
	if err := a.PostCallWithCost(context.Background(), 1000, axisTags, 1.0); err != nil {
		t.Fatalf("PostCallWithCost: %v", err)
	}
	paused, _ := a.Pauser().IsPaused(context.Background(), "stage", "design")
	if paused {
		t.Errorf("paused = true, want false (in-distribution sample)")
	}
}

func TestBudgetAdapterPostCallWithCostPropagatesTaggerError(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	axisTags := map[string]string{
		"project": "internal-platform-x",
	}
	err := a.PostCallWithCost(context.Background(), 1001, axisTags, 1.0)
	if err == nil {
		t.Error("err = nil, want ErrAxisIncomplete")
	}
}

func TestBudgetAdapterRunBudgetSchedulersStopsOnContextCancel(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = a.RunBudgetSchedulers(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("scheduler did not exit within 2s after ctx cancel")
	}
}

func TestBudgetAdapterPostCallWithCostAppendErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}

	orig := appendAnomalySampleByCostIDFn
	t.Cleanup(func() { appendAnomalySampleByCostIDFn = orig })
	appendAnomalySampleByCostIDFn = func(*store.Store, string, string, int64, float64, int64) error {
		return errInjected
	}

	err := a.PostCallWithCost(context.Background(), 7777, axisTags, 1.0)
	if err == nil {
		t.Error("err = nil, want injected append error")
	}
}

func TestBudgetAdapterPostCallWithCostAnomalyWindowErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}

	orig := queryAnomalyWindowFn
	t.Cleanup(func() { queryAnomalyWindowFn = orig })
	queryAnomalyWindowFn = func(*store.Store, string, string, int) ([]float64, error) {
		return nil, errInjected
	}

	err := a.PostCallWithCost(context.Background(), 7778, axisTags, 1.0)
	if err == nil {
		t.Error("err = nil, want injected anomaly-window error")
	}
}

func TestBudgetAdapterPostCallWithCostAutoPauseErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	a.SetAnomalyConfig(0.001, 60)
	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0} {
		_ = a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i))
	}
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}

	orig := upsertBudgetPauseFn
	t.Cleanup(func() { upsertBudgetPauseFn = orig })
	upsertBudgetPauseFn = func(*store.Store, string, string, string, int64, int64) error {
		return errInjected
	}

	err := a.PostCallWithCost(context.Background(), 7779, axisTags, 50.0)
	if err == nil {
		t.Error("err = nil, want injected auto-pause Trigger error")
	}
}

func TestBudgetAdapterPostCallWithCostAnomalyUpdateErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}

	a.SetAnomalyConfig(0.001, 60)
	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0} {
		_ = a.AppendAnomalySample(context.Background(), "stage", "design", sample, now+int64(i))
	}

	if err := a.PostCallWithCost(context.Background(), 8001, axisTags, 50.0); err != nil {
		t.Fatalf("PostCallWithCost: %v", err)
	}
	paused, _ := a.Pauser().IsPaused(context.Background(), "stage", "design")
	if !paused {
		t.Error("paused = false, want true (auto-pause)")
	}
}

func TestBudgetAdapterQueryAxisTagsErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	_ = s.Close()
	if _, err := a.QueryAxisTags(context.Background(), 1); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestBudgetAdapterQueryAxisTagLossesErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	_ = s.Close()
	if _, err := a.QueryAxisTagLosses(context.Background(), 1); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestBudgetAdapterPauseListActiveErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	_ = s.Close()
	if _, err := a.PauseListActive(context.Background()); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestBudgetAdapterPauseClearIfExpiredCAS(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	if err := a.PauseSet(context.Background(), "stage", "design", "expired", 100, 200); err != nil {
		t.Fatalf("PauseSet expired: %v", err)
	}
	if err := a.PauseSet(context.Background(), "worker_id", "w-1", "extended", 100, 1000); err != nil {
		t.Fatalf("PauseSet extended: %v", err)
	}

	if err := a.PauseClearIfExpired(context.Background(), "stage", "design", 500); err != nil {
		t.Fatalf("PauseClearIfExpired expired: %v", err)
	}
	if err := a.PauseClearIfExpired(context.Background(), "worker_id", "w-1", 500); err != nil {
		t.Fatalf("PauseClearIfExpired extended: %v", err)
	}

	expiredActive, _, _ := a.PauseGet(context.Background(), "stage", "design")
	if expiredActive {
		t.Error("expired row not deleted")
	}
	extendedActive, _, _ := a.PauseGet(context.Background(), "worker_id", "w-1")
	if !extendedActive {
		t.Error("extended row deleted; CAS failed to preserve")
	}
}

func TestBudgetAdapterPauseClearIfExpiredErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	orig := deleteBudgetPauseIfExpiredFn
	t.Cleanup(func() { deleteBudgetPauseIfExpiredFn = orig })
	deleteBudgetPauseIfExpiredFn = func(*store.Store, string, string, int64) error {
		return errInjected
	}

	if err := a.PauseClearIfExpired(context.Background(), "stage", "design", 0); err == nil {
		t.Error("err = nil, want injected error")
	}
}

// TestPostCallWithCostIdempotentUnderRetry is the C-2 regression test
// (post-review fix): PostCallWithCost retries with the same cost_id
// MUST NOT inflate budget_anomaly_samples (the rolling-window
// denominator) or budget_anomalies (the audit trail). Pre-fix:
// AppendAnomalySample inserted a fresh row per call; partial-failure
// retry of the orchestration double-counted samples.
//
// Post-fix contract: AppendAnomalySampleByCostID uses INSERT OR IGNORE
// on (scope, scope_value, cost_id); same cost_id retries collapse to
// the original row. Anomaly engine's same-sample dedupe (post-review
// C-3 fix) collapses budget_anomalies to 1 row per distinct sample.
//
// Test invoke PostCallWithCost N times with the SAME cost_id +
// axis_tags + cost_usd. Assert sample count and anomaly count are
// EXACTLY 1 per scope, not N.
func TestPostCallWithCostIdempotentUnderRetry(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)
	a.SetAnomalyConfig(2.0, 60)

	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03} {
		if err := a.AppendAnomalySampleByCostID(
			context.Background(), "stage", "design", int64(1000+i), sample, now+int64(i),
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
		"worker_id": "w-1",
	}

	const costID = int64(99)
	const N = 5
	for i := 0; i < N; i++ {
		if err := a.PostCallWithCost(context.Background(), costID, axisTags, 50.0); err != nil {
			t.Fatalf("PostCallWithCost iter %d: %v", i, err)
		}
	}

	rows, err := s.DB().Query(
		`SELECT scope, scope_value, COUNT(*) FROM budget_anomaly_samples
		 WHERE cost_id = ? GROUP BY scope, scope_value`,
		costID,
	)
	if err != nil {
		t.Fatalf("count samples: %v", err)
	}
	defer rows.Close()
	scopeRows := 0
	for rows.Next() {
		var scope, value string
		var count int
		if err := rows.Scan(&scope, &value, &count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		scopeRows++
		if count != 1 {
			t.Errorf("scope=%s:%s sample count = %d, want 1 (idempotent under N=%d retries)", scope, value, count, N)
		}
	}

	if scopeRows != 4 {
		t.Errorf("scope groups = %d, want 4 (one row per anomaly-tracked scope)", scopeRows)
	}

	anomalies, err := store.ListBudgetAnomalies(s.DB(), 1000)
	if err != nil {
		t.Fatalf("ListBudgetAnomalies: %v", err)
	}
	scopeAnoms := map[string]int{}
	for _, r := range anomalies {
		scopeAnoms[r.Scope+":"+r.ScopeValue]++
	}
	for k, n := range scopeAnoms {
		if n != 1 {
			t.Errorf("budget_anomalies count for %s = %d, want 1 (same-sample dedupe under N=%d retries)", k, n, N)
		}
	}
}

func TestBudgetAdapterAppendAnomalySampleByCostIDErrorPropagated(t *testing.T) {
	s := openTestStore(t)
	a := NewBudgetAdapter(s)

	orig := appendAnomalySampleByCostIDFn
	t.Cleanup(func() { appendAnomalySampleByCostIDFn = orig })
	appendAnomalySampleByCostIDFn = func(*store.Store, string, string, int64, float64, int64) error {
		return errInjected
	}

	axisTags := map[string]string{
		"project": "internal-platform-x", "doctrine": "max-scope", "stage": "design", "task": "T-1",
	}
	if err := a.PostCallWithCost(context.Background(), 100, axisTags, 1.0); err == nil {
		t.Error("err = nil, want injected error from AppendAnomalySampleByCostID")
	}
}
