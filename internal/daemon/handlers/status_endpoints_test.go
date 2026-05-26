package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeStatusAccessor struct {
	costCounters *orchestrator.CostCounters
	tiers        []providers.TierBackend
	udsPath      string
	activeModel  string
}

func (f *fakeStatusAccessor) CostCounters() *orchestrator.CostCounters { return f.costCounters }
func (f *fakeStatusAccessor) Tiers() []providers.TierBackend           { return f.tiers }
func (f *fakeStatusAccessor) UDSPath() string                          { return f.udsPath }
func (f *fakeStatusAccessor) ActiveModel() string                      { return f.activeModel }

func TestCascadeState_NoAccessor_Returns503(t *testing.T) {

	handler := CascadeState("not-a-server")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cascade/state", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Error("body missing 'error' key")
	}
}

func TestCascadeState_NoTiers_ReturnsZeroState(t *testing.T) {

	acc := &fakeStatusAccessor{tiers: nil}
	handler := CascadeState(acc)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cascade/state", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cascadeStateResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ActiveTier != 0 {
		t.Errorf("want active_tier=0, got %d", resp.ActiveTier)
	}
	if resp.TierName != "none" {
		t.Errorf("want tier_name='none', got %q", resp.TierName)
	}
	if resp.ProviderCount != 0 {
		t.Errorf("want provider_count=0, got %d", resp.ProviderCount)
	}
}

func TestCascadeState_WithTiers_ReturnsFirstTierAsActive(t *testing.T) {

	tiers := []providers.TierBackend{
		&fakeTierBackend{name: "anthropic-bypass", tier: providers.TierInHouse},
		&fakeTierBackend{name: "anthropic-paygo", tier: providers.TierGenericOpenAICompat},
	}
	acc := &fakeStatusAccessor{tiers: tiers}
	handler := CascadeState(acc)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cascade/state", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cascadeStateResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ActiveTier != 1 {
		t.Errorf("want active_tier=1, got %d", resp.ActiveTier)
	}
	if resp.TierName != "anthropic-bypass" {
		t.Errorf("want tier_name='anthropic-bypass', got %q", resp.TierName)
	}
	if resp.ProviderCount != 2 {
		t.Errorf("want provider_count=2, got %d", resp.ProviderCount)
	}
}

func TestCost24h_NoAccessor_ReturnsZeros(t *testing.T) {

	handler := Cost24h("not-a-server")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cost/24h", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cost24hResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Spend24hUSD != 0 {
		t.Errorf("want spend_24h_usd=0, got %f", resp.Spend24hUSD)
	}
}

func TestCost24h_NilCostCounters_ReturnsZeros(t *testing.T) {

	acc := &fakeStatusAccessor{costCounters: nil}
	handler := Cost24h(acc)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cost/24h", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cost24hResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Spend24hUSD != 0 {
		t.Errorf("want spend_24h_usd=0, got %f", resp.Spend24hUSD)
	}
}

func TestCost24h_WithCounters_ReturnsSummedSpend(t *testing.T) {

	counters := orchestrator.NewCostCounters(newBudgetFakeStore())
	acc := &fakeStatusAccessor{costCounters: counters}
	handler := Cost24h(acc)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cost/24h", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cost24hResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if resp.Spend24hUSD != 0 {
		t.Errorf("want spend_24h_usd=0 for empty counters, got %f", resp.Spend24hUSD)
	}

	if resp.SpendSessionUSD != 0 {
		t.Errorf("want spend_session_usd=0 (Phase C stub), got %f", resp.SpendSessionUSD)
	}
}

func TestContextUsed_ReturnsZeroTokens(t *testing.T) {

	handler := ContextUsed(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/context/used", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp contextUsedResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.UsedTokens != 0 {
		t.Errorf("want used_tokens=0 (Phase C stub), got %d", resp.UsedTokens)
	}
	if resp.MaxTokens != 0 {
		t.Errorf("want max_tokens=0 (Phase C stub), got %d", resp.MaxTokens)
	}
}

func TestContextUsed_BodyIsValidJSON(t *testing.T) {

	handler := ContextUsed(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/context/used", nil))
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if _, ok := body["used_tokens"]; !ok {
		t.Error("missing 'used_tokens' key")
	}
	if _, ok := body["max_tokens"]; !ok {
		t.Error("missing 'max_tokens' key")
	}
}

func TestProfileActive_DefaultProfile_WhenEnvUnset(t *testing.T) {

	t.Setenv("ZEN_PROFILE", "")
	handler := ProfileActive(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/profile/active", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp profileActiveResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ProfileName != "default" {
		t.Errorf("want profile_name='default', got %q", resp.ProfileName)
	}
	if resp.Kind != "builtin" {
		t.Errorf("want kind='builtin', got %q", resp.Kind)
	}
}

func TestProfileActive_EnvProfile_WhenZenProfileSet(t *testing.T) {

	t.Setenv("ZEN_PROFILE", "max-scope")
	handler := ProfileActive(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/profile/active", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp profileActiveResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ProfileName != "max-scope" {
		t.Errorf("want profile_name='max-scope', got %q", resp.ProfileName)
	}
	if resp.Kind != "env" {
		t.Errorf("want kind='env', got %q", resp.Kind)
	}
}

func TestCWD_ReturnsDaemonWorkingDirectory(t *testing.T) {

	expectedCwd, err := os.Getwd()
	if err != nil {
		t.Skip("os.Getwd() failed; skipping test")
	}
	handler := CWD(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cwd", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp cwdResp
	if err2 := json.NewDecoder(w.Body).Decode(&resp); err2 != nil {
		t.Fatalf("decode body: %v", err2)
	}
	if resp.Cwd != expectedCwd {
		t.Errorf("want cwd=%q, got %q", expectedCwd, resp.Cwd)
	}
}

func TestCWD_ResponseHasCwdKey(t *testing.T) {

	handler := CWD(nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cwd", nil))
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if _, ok := body["cwd"]; !ok {
		t.Error("missing 'cwd' key in response")
	}
}

type fakeHealthCtxLegacy struct {
	startedAt time.Time
}

func (f *fakeHealthCtxLegacy) StartedAt() time.Time { return f.startedAt }

type fakeHealthCtxExtended struct {
	startedAt   time.Time
	udsPath     string
	activeModel string
}

func (f *fakeHealthCtxExtended) StartedAt() time.Time { return f.startedAt }
func (f *fakeHealthCtxExtended) UDSPath() string      { return f.udsPath }
func (f *fakeHealthCtxExtended) ActiveModel() string  { return f.activeModel }

func TestHealth_LegacyCtx_DoesNotIncludeExtendedFields(t *testing.T) {

	s := &fakeHealthCtxLegacy{startedAt: time.Now()}
	handler := Health(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	for _, required := range []string{"status", "version", "uptime_seconds", "pid"} {
		if _, ok := body[required]; !ok {
			t.Errorf("missing required key %q in legacy Health response", required)
		}
	}

	for _, extended := range []string{"uds_path", "active_model"} {
		if _, ok := body[extended]; ok {
			t.Errorf("legacy HealthCtx should not include extended key %q", extended)
		}
	}
}

func TestHealth_ExtendedCtx_IncludesPidUDSActiveModel(t *testing.T) {

	s := &fakeHealthCtxExtended{
		startedAt:   time.Now(),
		udsPath:     "/tmp/zen-swarm.sock",
		activeModel: "claude-opus-4-7",
	}
	handler := Health(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["uds_path"] != "/tmp/zen-swarm.sock" {
		t.Errorf("want uds_path='/tmp/zen-swarm.sock', got %v", body["uds_path"])
	}
	if body["active_model"] != "claude-opus-4-7" {
		t.Errorf("want active_model='claude-opus-4-7', got %v", body["active_model"])
	}

	if _, ok := body["pid"]; !ok {
		t.Error("missing 'pid' key")
	}
}
