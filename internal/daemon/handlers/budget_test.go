// Package handlers — budget_test.go.
//
// Coverage targets:
// - parseRange: "24h" / "30d" / "Xd" / "Xm" / Go duration / errors
// - supportedBudgetWindow: 24h + 30d allowed; others rejected
// - BudgetSummary: nil server → empty shape; nil counters → empty
// shape; seeded counters → correct totals + sort order; unsupported
// range → 400; bad range syntax → 400
// - shape stability: ByTier MUST be a non-nil empty slice on no data
// (never JSON null)
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type budgetFakeServer struct {
	cb       *orchestrator.CircuitBreaker
	pins     *orchestrator.PinOverrides
	counters *orchestrator.CostCounters
	tiers    []providers.TierBackend
}

func (b *budgetFakeServer) CircuitBreaker() *orchestrator.CircuitBreaker { return b.cb }
func (b *budgetFakeServer) PinOverrides() *orchestrator.PinOverrides     { return b.pins }
func (b *budgetFakeServer) CostCounters() *orchestrator.CostCounters     { return b.counters }
func (b *budgetFakeServer) Tiers() []providers.TierBackend               { return b.tiers }

type budgetFakeStore struct {
	rows map[string]orchestrator.CostLedgerRow
	id   int64
}

func newBudgetFakeStore() *budgetFakeStore {
	return &budgetFakeStore{rows: map[string]orchestrator.CostLedgerRow{}}
}

func (f *budgetFakeStore) InsertCostLedger(row orchestrator.CostLedgerRow) (int64, error) {
	if _, ok := f.rows[row.IdempotencyKey]; ok {
		return 0, orchestrator.ErrDuplicateIdempotency
	}
	f.id++
	row.ID = f.id
	f.rows[row.IdempotencyKey] = row
	return row.ID, nil
}

func (f *budgetFakeStore) QueryAllRecentCosts(since time.Time) ([]orchestrator.CostLedgerRow, error) {
	out := []orchestrator.CostLedgerRow{}
	for _, r := range f.rows {
		if r.TS.After(since) || r.TS.Equal(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func mkBudgetRow(idem string, project, profile, tier string, usd float64) orchestrator.CostLedgerRow {
	return orchestrator.CostLedgerRow{
		IdempotencyKey: idem,
		TS:             time.Now(),
		Project:        project,
		Profile:        profile,
		Tier:           tier,
		Model:          "claude-opus-4-6",
		InputTokens:    100,
		OutputTokens:   50,
		CostUSD:        usd,
		SessionID:      "sess-budget-test",
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr string
	}{
		{"24h literal", "24h", 24 * time.Hour, ""},
		{"30d days", "30d", 30 * 24 * time.Hour, ""},
		{"1d days", "1d", 24 * time.Hour, ""},
		{"7d days (parses but unsupported by handler)", "7d", 7 * 24 * time.Hour, ""},
		{"90m duration", "90m", 90 * time.Minute, ""},
		{"empty", "", 0, "range is required"},
		{"negative duration", "-1h", 0, "must be positive"},
		{"zero duration", "0h", 0, "must be positive"},
		{"zero days", "0d", 0, "must be positive"},
		{"negative days", "-1d", 0, "invalid range"},
		{"bad days", "abcd", 0, "invalid range"},
		{"bad duration", "frobnicate", 0, "invalid range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRange(c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("parseRange(%q): got err=%v, want substring %q", c.in, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("parseRange(%q): unexpected error %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("parseRange(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestSupportedBudgetWindow(t *testing.T) {
	if err := supportedBudgetWindow(24 * time.Hour); err != nil {
		t.Errorf("24h must be supported: %v", err)
	}
	if err := supportedBudgetWindow(30 * 24 * time.Hour); err != nil {
		t.Errorf("30d must be supported: %v", err)
	}
	if err := supportedBudgetWindow(7 * 24 * time.Hour); err == nil {
		t.Error("7d must be rejected (Plan 3 v0.3.0 only registers 24h + 30d)")
	}
	if err := supportedBudgetWindow(time.Hour); err == nil {
		t.Error("1h must be rejected")
	}
}

func TestBudgetSummary_NilServerEmptyShape(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetSummary(nil)(rr, httptest.NewRequest("GET", "/v1/budget", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var body budgetSummaryResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Range != "24h" {
		t.Errorf("default range: got %q, want %q", body.Range, "24h")
	}
	if body.TotalUSD != 0 {
		t.Errorf("total: got %f, want 0", body.TotalUSD)
	}
	if body.ByTier == nil {
		t.Error("by_tier MUST be non-nil empty slice; got nil")
	}
}

func TestBudgetSummary_NilCountersEmptyShape(t *testing.T) {
	srv := &budgetFakeServer{counters: nil}
	rr := httptest.NewRecorder()
	BudgetSummary(srv)(rr, httptest.NewRequest("GET", "/v1/budget?range=30d", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body budgetSummaryResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Range != "30d" {
		t.Errorf("range echo: got %q, want %q", body.Range, "30d")
	}
	if body.ByTier == nil || len(body.ByTier) != 0 {
		t.Errorf("by_tier: got %+v, want non-nil empty", body.ByTier)
	}
}

func TestBudgetSummary_BadRange400(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetSummary(nil)(rr, httptest.NewRequest("GET", "/v1/budget?range=frobnicate", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid range") {
		t.Errorf("error body: got %q, want substring 'invalid range'", rr.Body.String())
	}
}

func TestBudgetSummary_UnsupportedRange400(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetSummary(nil)(rr, httptest.NewRequest("GET", "/v1/budget?range=7d", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (7d not registered)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unsupported") {
		t.Errorf("error body: got %q, want substring 'unsupported'", rr.Body.String())
	}
}

func TestBudgetSummary_SeededCountersTotalAndSort(t *testing.T) {
	cc := orchestrator.NewCostCounters(newBudgetFakeStore())
	rows := []orchestrator.CostLedgerRow{
		mkBudgetRow("idem-1", "internal-platform-x", "orchestrator", "openclaude", 0.10),
		mkBudgetRow("idem-2", "internal-platform-x", "orchestrator", "in-house", 0.05),
		mkBudgetRow("idem-3", "nexus", "orchestrator", "openclaude", 0.20),
		mkBudgetRow("idem-4", "internal-platform-x", "orchestrator", "openclaude", 0.30),
	}
	for _, r := range rows {
		if err := cc.Record(r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	srv := &budgetFakeServer{counters: cc}
	rr := httptest.NewRecorder()
	BudgetSummary(srv)(rr, httptest.NewRequest("GET", "/v1/budget?range=24h", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%q", rr.Code, rr.Body.String())
	}
	var body budgetSummaryResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.TotalUSD < 0.649 || body.TotalUSD > 0.651 {
		t.Errorf("total: got %f, want ~0.65", body.TotalUSD)
	}

	if len(body.ByTier) != 3 {
		t.Fatalf("by_tier len = %d, want 3; got=%+v", len(body.ByTier), body.ByTier)
	}

	wantOrder := []struct{ Tier, Project string }{
		{"in-house", "internal-platform-x"},
		{"openclaude", "internal-platform-x"},
		{"openclaude", "nexus"},
	}
	for i, w := range wantOrder {
		if body.ByTier[i].Tier != w.Tier || body.ByTier[i].Project != w.Project {
			t.Errorf("by_tier[%d] = (%s, %s), want (%s, %s)",
				i, body.ByTier[i].Tier, body.ByTier[i].Project, w.Tier, w.Project)
		}
	}

	for _, row := range body.ByTier {
		if row.Tier == "openclaude" && row.Project == "internal-platform-x" {
			if row.SpendUSD < 0.399 || row.SpendUSD > 0.401 {
				t.Errorf("internal-platform-x/openclaude spend: got %f, want 0.40", row.SpendUSD)
			}
		}
	}
}

func TestBudgetSummary_30dWindow(t *testing.T) {
	cc := orchestrator.NewCostCounters(newBudgetFakeStore())
	if err := cc.Record(mkBudgetRow("idem-1", "internal-platform-x", "orchestrator", "openclaude", 0.42)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	srv := &budgetFakeServer{counters: cc}
	rr := httptest.NewRecorder()
	BudgetSummary(srv)(rr, httptest.NewRequest("GET", "/v1/budget?range=30d", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%q", rr.Code, rr.Body.String())
	}
	var body budgetSummaryResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Range != "30d" {
		t.Errorf("range echo: got %q, want 30d", body.Range)
	}
	if body.TotalUSD < 0.419 || body.TotalUSD > 0.421 {
		t.Errorf("30d total: got %f, want 0.42", body.TotalUSD)
	}
}

func TestBudgetSummary_DefaultIs24h(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetSummary(nil)(rr, httptest.NewRequest("GET", "/v1/budget", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body budgetSummaryResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Range != "24h" {
		t.Errorf("default range: got %q, want 24h", body.Range)
	}
}

func TestBudgetProjectStillStubbed(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetProject(nil)(rr, httptest.NewRequest("GET", "/v1/budget/foo", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("status: got %d, want 501", rr.Code)
	}
	if got := rr.Header().Get("X-Zen-Plan"); got != "4" {
		t.Errorf("X-Zen-Plan: got %q, want 4", got)
	}
}

func TestBudgetRaiseStillStubbed(t *testing.T) {
	rr := httptest.NewRecorder()
	BudgetRaise(nil)(rr, httptest.NewRequest("POST", "/v1/budget/foo/raise", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("status: got %d, want 501", rr.Code)
	}
}
