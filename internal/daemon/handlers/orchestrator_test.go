package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakePinStore struct {
	mu     sync.Mutex
	rows   map[string]orchestrator.PinRow
	nextID int64
}

func newFakePinStore() *fakePinStore {
	return &fakePinStore{rows: map[string]orchestrator.PinRow{}}
}

func (f *fakePinStore) key(scope, scopeID string) string { return scope + "/" + scopeID }

func (f *fakePinStore) Insert(p orchestrator.PinRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	p.ID = f.nextID
	f.rows[f.key(p.Scope, p.ScopeID)] = p
	return nil
}

func (f *fakePinStore) Delete(scope, scopeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, f.key(scope, scopeID))
	return nil
}

func (f *fakePinStore) Query(scope, scopeID string) (*orchestrator.PinRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[f.key(scope, scopeID)]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakePinStore) ListAll() ([]orchestrator.PinRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]orchestrator.PinRow, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakePinStore) PurgeExpired(now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	purged := 0
	for k, r := range f.rows {
		if r.ExpiresAt != nil && r.ExpiresAt.Before(now) {
			delete(f.rows, k)
			purged++
		}
	}
	return purged, nil
}

type fakeTierBackend struct {
	tier     providers.Tier
	name     string
	probeErr error
}

func (f *fakeTierBackend) Tier() providers.Tier { return f.tier }
func (f *fakeTierBackend) Forward(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
	return nil, errors.New("not used in K-3 tests")
}
func (f *fakeTierBackend) Probe(_ context.Context) error { return f.probeErr }
func (f *fakeTierBackend) Close() error                  { return nil }
func (f *fakeTierBackend) Name() string                  { return f.name }
func (f *fakeTierBackend) Capabilities() providers.TierCapabilities {
	return providers.TierCapabilities{}
}

type fakeOrchestratorServer struct {
	cb       *orchestrator.CircuitBreaker
	pins     *orchestrator.PinOverrides
	counters *orchestrator.CostCounters
	tiers    []providers.TierBackend
}

func (f *fakeOrchestratorServer) CircuitBreaker() *orchestrator.CircuitBreaker {
	return f.cb
}
func (f *fakeOrchestratorServer) PinOverrides() *orchestrator.PinOverrides {
	return f.pins
}
func (f *fakeOrchestratorServer) CostCounters() *orchestrator.CostCounters {
	return f.counters
}
func (f *fakeOrchestratorServer) Tiers() []providers.TierBackend {
	return f.tiers
}

func newFakeServer() (*fakeOrchestratorServer, *fakeTierBackend, *fakeTierBackend) {
	tier1 := &fakeTierBackend{tier: providers.TierInHouse, name: "in-house"}
	tier2 := &fakeTierBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	return &fakeOrchestratorServer{
		cb:    orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{}),
		pins:  orchestrator.NewPinOverrides(newFakePinStore()),
		tiers: []providers.TierBackend{tier1, tier2},
	}, tier1, tier2
}

// TestOrchestratorStatus_AccessorWithNilPinOverridesReturnsEmptyPins pins the
// defence-in-depth branch where the accessor IS configured (acc != nil) but
// PinOverrides() returns nil — e.g. daemon started before I-2 wiring
// completes.  Distinct from TestOrchestratorStatus_NilServerReturnsEmptyShape
// which exercises the acc==nil early-return path.  The pins field in the JSON
// response MUST be an empty array (not JSON null).
func TestOrchestratorStatus_AccessorWithNilPinOverridesReturnsEmptyPins(t *testing.T) {
	tier1 := &fakeTierBackend{tier: providers.TierInHouse, name: "in-house"}
	srv := &fakeOrchestratorServer{
		cb:       orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{}),
		pins:     nil,
		counters: nil,
		tiers:    []providers.TierBackend{tier1},
	}
	rr := httptest.NewRecorder()
	OrchestratorStatus(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var body orchestratorStatusResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// pins MUST be a non-nil empty slice (JSON []), not null.
	if body.Pins == nil {
		t.Errorf("pins field must be non-nil empty slice when PinOverrides is nil; got nil")
	}
	if len(body.Pins) != 0 {
		t.Errorf("pins len = %d, want 0 when PinOverrides is nil", len(body.Pins))
	}

	if len(body.Tiers) != 1 || body.Tiers[0].Tier != "in-house" {
		t.Errorf("tiers shape unexpected: %+v", body.Tiers)
	}
}

func TestOrchestratorStatus_NilServerReturnsEmptyShape(t *testing.T) {
	rr := httptest.NewRecorder()
	OrchestratorStatus(nil)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var body orchestratorStatusResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Tiers == nil || body.Pins == nil || body.Costs == nil {
		t.Errorf("nil slices in response shape: %+v", body)
	}
	if len(body.Tiers) != 0 || len(body.Pins) != 0 || len(body.Costs) != 0 {
		t.Errorf("expected empty arrays, got: %+v", body)
	}
}

func TestOrchestratorStatus_TiersReportClosedDefault(t *testing.T) {
	srv, _, _ := newFakeServer()
	rr := httptest.NewRecorder()
	OrchestratorStatus(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body orchestratorStatusResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tiers) != 2 {
		t.Fatalf("tiers len = %d, want 2", len(body.Tiers))
	}
	for _, ts := range body.Tiers {
		if ts.State != "closed" {
			t.Errorf("tier %s state = %q, want closed", ts.Tier, ts.State)
		}
	}
}

func TestOrchestratorStatus_TiersReportSuspectAfterFailures(t *testing.T) {
	srv, _, _ := newFakeServer()
	for i := 0; i < 3; i++ {

		srv.cb.RecordFailure("in-house")
	}
	rr := httptest.NewRecorder()
	OrchestratorStatus(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	var body orchestratorStatusResp
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	gotInHouse := stateOf(body.Tiers, "in-house")
	if gotInHouse != "suspect" {
		t.Errorf("in-house state = %q, want suspect after 3 failures", gotInHouse)
	}
	if stateOf(body.Tiers, "openclaude") != "closed" {
		t.Errorf("openclaude state must remain closed (independent tier)")
	}
}

// TestCollectTierStates_PerProviderVisibility — Plan 16 Phase B Task 20.
// When two backends share one Tier enum (e.g. deepseek-direct +
// siliconflow-deepseek both providers.TierGenericOpenAICompat), the
// operator's `zen orchestrator status` output MUST distinguish them via
// the Provider field on tierStateRow. Without Provider, the two rows are
// indistinguishable from each other and breaker state observed at
// Backend.Name() granularity (T17) is invisible.
//
// Pins
//   - tierStateRow has a Provider field populated from t.Name()
//   - same Tier on both rows still renders both rows (one per backend)
//   - per-provider breaker state surfaces independently: the failing
//     provider goes suspect, the sibling stays closed
//
// inv-zen-213 (per-provider observability on the status surface).
func TestCollectTierStates_PerProviderVisibility(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{FailureThreshold: 1})
	cb.RecordFailure("deepseek-direct")

	tiers := []providers.TierBackend{
		&fakeTierBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat},
		&fakeTierBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat},
	}
	rows := collectTierStates(cb, tiers)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	if rows[0].Provider != "deepseek-direct" {
		t.Errorf("rows[0].Provider = %q, want deepseek-direct", rows[0].Provider)
	}
	if rows[1].Provider != "siliconflow-deepseek" {
		t.Errorf("rows[1].Provider = %q, want siliconflow-deepseek", rows[1].Provider)
	}

	if rows[0].Tier != rows[1].Tier {
		t.Errorf("expected same tier on both rows, got %q + %q", rows[0].Tier, rows[1].Tier)
	}
	if rows[0].Tier != "openai-compat" {
		t.Errorf("rows[0].Tier = %q, want openai-compat", rows[0].Tier)
	}

	if rows[0].State != "suspect" {
		t.Errorf("rows[0].State = %q, want suspect", rows[0].State)
	}
	if rows[1].State != "closed" {
		t.Errorf("rows[1].State = %q, want closed", rows[1].State)
	}
}

func stateOf(rows []tierStateRow, tier string) string {
	for _, r := range rows {
		if r.Tier == tier {
			return r.State
		}
	}
	return ""
}

func TestOrchestratorStatus_PinsListedAfterSet(t *testing.T) {
	srv, _, _ := newFakeServer()
	if err := srv.pins.Set("global", "", "openclaude", "", 0, "operator"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	rr := httptest.NewRecorder()
	OrchestratorStatus(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	var body orchestratorStatusResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Pins) != 1 {
		t.Fatalf("pins len = %d, want 1", len(body.Pins))
	}
	if body.Pins[0].Scope != "global" || body.Pins[0].Tier != "openclaude" {
		t.Errorf("pin shape: %+v", body.Pins[0])
	}
	if body.Pins[0].Reason != "operator" {
		t.Errorf("reason: got %q, want operator", body.Pins[0].Reason)
	}
}

// TestOrchestratorStatus_CostsBackfilledFromCounters — K-4 backfill of
// the K-3 collect30dCosts placeholder. Seeded CostCounters MUST surface
// per-tier 30d totals summed across (project, profile) tuples. Tiers
// with no recorded spend stay at 0.0 (shape-correct rendering).
//
// Pre-K-4: this test would fail because collect30dCosts returned 0.0
// for every tier regardless of seed. K-4 wires AllKeys + per-tier
// rollup.
func TestOrchestratorStatus_CostsBackfilledFromCounters(t *testing.T) {
	srv, _, _ := newFakeServer()
	cc := orchestrator.NewCostCounters(newBudgetFakeStore())

	rows := []orchestrator.CostLedgerRow{
		mkBudgetRow("idem-1", "internal-platform-x", "orchestrator", "openclaude", 0.10),
		mkBudgetRow("idem-2", "nexus", "orchestrator", "openclaude", 0.20),
	}
	for _, r := range rows {
		if err := cc.Record(r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	srv.counters = cc

	rr := httptest.NewRecorder()
	OrchestratorStatus(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body orchestratorStatusResp
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Costs) != 2 {
		t.Fatalf("costs len = %d, want 2 (one row per tier)", len(body.Costs))
	}
	gotTotals := map[string]float64{}
	for _, c := range body.Costs {
		gotTotals[c.Tier] = c.Total
		if c.Window != "30d" {
			t.Errorf("window: got %q, want 30d", c.Window)
		}
	}
	if got := gotTotals["openclaude"]; got < 0.299 || got > 0.301 {
		t.Errorf("openclaude 30d total: got %f, want 0.30 (sum of seeded rows)", got)
	}
	if got := gotTotals["in-house"]; got != 0 {
		t.Errorf("in-house 30d total: got %f, want 0 (no spend seeded)", got)
	}
}

func TestOrchestratorPin_Global(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "global", Tier: "openclaude", Reason: "operator"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("pin: got %d, want 204; body=%q", rr.Code, rr.Body.String())
	}
	rows, err := srv.pins.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 || rows[0].Scope != "global" || rows[0].Tier != "openclaude" {
		t.Errorf("pin not persisted: %+v", rows)
	}
}

func TestOrchestratorPin_Project(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "project", Project: "p1", Tier: "in-house"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("pin: got %d", rr.Code)
	}
}

func TestOrchestratorPin_Session(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "session", Session: "s1", Tier: "in-house"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("pin: got %d", rr.Code)
	}
}

func TestOrchestratorPin_TTL(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "global", Tier: "openclaude", TTL: "1h"}
	before := time.Now()
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("pin: got %d body=%q", rr.Code, rr.Body.String())
	}
	rows, _ := srv.pins.ListAll()
	if len(rows) != 1 || rows[0].ExpiresAt == nil {
		t.Fatalf("expected one row with ExpiresAt set: %+v", rows)
	}
	delta := rows[0].ExpiresAt.Sub(before)
	if delta < 59*time.Minute || delta > 61*time.Minute {
		t.Errorf("ExpiresAt delta = %v, want ~1h", delta)
	}
}

func TestOrchestratorPin_BadTier(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "global", Tier: "frobnicate"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestOrchestratorPin_BadScope(t *testing.T) {
	srv, _, _ := newFakeServer()
	cases := []pinReq{
		{Scope: "project", Tier: "in-house"},
		{Scope: "session", Tier: "in-house"},
		{Scope: "global", Project: "x", Tier: "in-house"},
		{Scope: "unknown", Tier: "in-house"},
		{Scope: "session", Session: "s", Project: "p", Tier: "x"},
	}
	for i, body := range cases {
		rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("case %d (%+v): got %d, want 400", i, body, rr.Code)
		}
	}
}

func TestOrchestratorPin_BadTTL(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := pinReq{Scope: "global", Tier: "openclaude", TTL: "forever"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestOrchestratorPin_NotConfigured(t *testing.T) {
	srv := &fakeOrchestratorServer{}
	body := pinReq{Scope: "global", Tier: "openclaude"}
	rr := postJSON(t, OrchestratorPin(srv), "/v1/orchestrator/pin", body)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

func TestOrchestratorPin_BadJSON(t *testing.T) {
	srv, _, _ := newFakeServer()
	req := httptest.NewRequest("POST", "/v1/orchestrator/pin", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	OrchestratorPin(srv)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestOrchestratorUnpin_Single(t *testing.T) {
	srv, _, _ := newFakeServer()
	if err := srv.pins.Set("project", "p1", "openclaude", "", 0, ""); err != nil {
		t.Fatalf("seed Set: %v", err)
	}
	body := unpinReq{Scope: "project", Project: "p1"}
	rr := postJSON(t, OrchestratorUnpin(srv), "/v1/orchestrator/unpin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("unpin: got %d", rr.Code)
	}
	rows, _ := srv.pins.ListAll()
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestOrchestratorUnpin_All(t *testing.T) {
	srv, _, _ := newFakeServer()
	_ = srv.pins.Set("project", "p1", "openclaude", "", 0, "")
	_ = srv.pins.Set("project", "p2", "openclaude", "", 0, "")
	_ = srv.pins.Set("global", "", "in-house", "", 0, "")
	body := unpinReq{All: true}
	rr := postJSON(t, OrchestratorUnpin(srv), "/v1/orchestrator/unpin", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("unpin --all: got %d", rr.Code)
	}
	rows, _ := srv.pins.ListAll()
	if len(rows) != 0 {
		t.Errorf("expected 0 rows after UnpinAll, got %d", len(rows))
	}
}

func TestOrchestratorUnpin_AllAndScopeMutuallyExclusive(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := unpinReq{All: true, Scope: "global"}
	rr := postJSON(t, OrchestratorUnpin(srv), "/v1/orchestrator/unpin", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestOrchestratorUnpin_BadScope(t *testing.T) {
	srv, _, _ := newFakeServer()
	body := unpinReq{Scope: "project"}
	rr := postJSON(t, OrchestratorUnpin(srv), "/v1/orchestrator/unpin", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestOrchestratorUnpin_NotConfigured(t *testing.T) {
	srv := &fakeOrchestratorServer{}
	body := unpinReq{All: true}
	rr := postJSON(t, OrchestratorUnpin(srv), "/v1/orchestrator/unpin", body)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

func TestOrchestratorPins_EmptyAndPopulated(t *testing.T) {
	srv, _, _ := newFakeServer()

	rr1 := httptest.NewRecorder()
	OrchestratorPins(srv)(rr1, httptest.NewRequest("GET", "/v1/orchestrator/pins", nil))
	if rr1.Code != http.StatusOK {
		t.Fatalf("pins empty: got %d", rr1.Code)
	}
	var bodyEmpty struct {
		Pins []pinSummary `json:"pins"`
	}
	if err := json.Unmarshal(rr1.Body.Bytes(), &bodyEmpty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bodyEmpty.Pins == nil || len(bodyEmpty.Pins) != 0 {
		t.Errorf("expected empty pins slice (non-nil), got: %+v", bodyEmpty.Pins)
	}

	_ = srv.pins.Set("global", "", "openclaude", "", 0, "test")
	rr2 := httptest.NewRecorder()
	OrchestratorPins(srv)(rr2, httptest.NewRequest("GET", "/v1/orchestrator/pins", nil))
	var bodyPop struct {
		Pins []pinSummary `json:"pins"`
	}
	_ = json.Unmarshal(rr2.Body.Bytes(), &bodyPop)
	if len(bodyPop.Pins) != 1 {
		t.Errorf("expected 1 pin, got %d", len(bodyPop.Pins))
	}
}

func TestOrchestratorProbe_RecoversSuspectTier(t *testing.T) {
	srv, t1, _ := newFakeServer()
	_ = t1

	for i := 0; i < 3; i++ {
		srv.cb.RecordFailure("in-house")
	}
	if srv.cb.State("in-house") != orchestrator.StateSuspect {
		t.Fatalf("precondition: want suspect; got %v", srv.cb.State("in-house"))
	}
	rr := httptest.NewRecorder()
	OrchestratorProbe(srv)(rr, httptest.NewRequest("POST", "/v1/orchestrator/probe", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("probe: got %d", rr.Code)
	}
	var body struct {
		Tiers []tierStateRow `json:"tiers"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if stateOf(body.Tiers, "in-house") != "closed" {
		t.Errorf("expected in-house to recover to closed; got %q", stateOf(body.Tiers, "in-house"))
	}
}

func TestOrchestratorProbe_SuspectFailsToOpen(t *testing.T) {
	srv, t1, _ := newFakeServer()
	t1.probeErr = errors.New("synthetic probe failure")
	for i := 0; i < 3; i++ {
		srv.cb.RecordFailure("in-house")
	}
	rr := httptest.NewRecorder()
	OrchestratorProbe(srv)(rr, httptest.NewRequest("POST", "/v1/orchestrator/probe", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("probe: got %d", rr.Code)
	}
	var body struct {
		Tiers []tierStateRow `json:"tiers"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if stateOf(body.Tiers, "in-house") != "open" {
		t.Errorf("expected in-house to escalate to open on probe failure; got %q", stateOf(body.Tiers, "in-house"))
	}
}

func TestOrchestratorProbe_NoBreakerReturnsEmptyShape(t *testing.T) {
	srv := &fakeOrchestratorServer{}
	rr := httptest.NewRecorder()
	OrchestratorProbe(srv)(rr, httptest.NewRequest("POST", "/v1/orchestrator/probe", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var body struct {
		Tiers []tierStateRow `json:"tiers"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if len(body.Tiers) != 0 {
		t.Errorf("expected 0 tiers, got %d", len(body.Tiers))
	}
}

func TestOrchestratorHistory_RendersCurrentStatePerTier(t *testing.T) {
	srv, _, _ := newFakeServer()
	rr := httptest.NewRecorder()
	OrchestratorHistory(srv)(rr, httptest.NewRequest("GET", "/v1/orchestrator/history", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	var body struct {
		Tiers []tierStateRow `json:"tiers"`
		Note  string         `json:"note"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tiers) != 2 {
		t.Errorf("expected 2 tiers, got %d", len(body.Tiers))
	}
	if !strings.Contains(body.Note, "rescope") {
		t.Errorf("note should mention rescope: %q", body.Note)
	}
}

func TestScopeIDFor(t *testing.T) {
	cases := []struct {
		name        string
		scope, p, s string
		wantS       string
		wantID      string
		wantOK      bool
	}{
		{"global ok", "global", "", "", "global", "", true},
		{"global with project rejected", "global", "p", "", "", "", false},
		{"global with session rejected", "global", "", "s", "", "", false},
		{"project ok", "project", "p1", "", "project", "p1", true},
		{"project missing rejected", "project", "", "", "", "", false},
		{"session ok", "session", "", "s1", "session", "s1", true},
		{"session missing rejected", "session", "", "", "", "", false},
		{"unknown scope", "weird", "", "", "", "", false},
		{"empty scope", "", "", "", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, id, ok := scopeIDFor(c.scope, c.p, c.s)
			if ok != c.wantOK || s != c.wantS || id != c.wantID {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", s, id, ok, c.wantS, c.wantID, c.wantOK)
			}
		})
	}
}

func postJSON(t *testing.T, h http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}
