package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeAuditQueryCtx struct {
	events    []handlers.AuditEventRow
	queryErr  error
	gotPrefix string
	gotSince  int64
}

func (f *fakeAuditQueryCtx) AuditEvents(typePrefix, _ string, sinceUnix int64, _ int) ([]handlers.AuditEventRow, error) {
	f.gotPrefix = typePrefix
	f.gotSince = sinceUnix
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.events, nil
}
func (f *fakeAuditQueryCtx) AuditTypes() ([]handlers.AuditTypeRow, error) { return nil, nil }
func (f *fakeAuditQueryCtx) AuditEventByID(_ string) (handlers.AuditEventRow, error) {
	return handlers.AuditEventRow{}, nil
}

type fakeAugmentProbeCtx struct{ h http.Handler }

func (f *fakeAugmentProbeCtx) AugmentHandler() http.Handler { return f.h }

func completed(emittedAt int64, tokens, ceiling, queries int, cacheHit bool, cost float64, lastIdx string) handlers.AuditEventRow {
	payload := map[string]any{
		"tokens_consumed":  tokens,
		"tokens_ceiling":   ceiling,
		"kg_queries_fired": queries,
		"cache_hit":        cacheHit,
		"total_cost_usd":   cost,
		"last_indexed":     lastIdx,
	}
	raw, _ := json.Marshal(payload)
	return handlers.AuditEventRow{
		ID: "evt-c", Type: "AugmentationCompleted",
		PayloadRaw: string(raw), EmittedAt: emittedAt,
	}
}

func started(emittedAt int64) handlers.AuditEventRow {
	return handlers.AuditEventRow{
		ID: "evt-s", Type: "AugmentationStarted",
		PayloadRaw: `{}`, EmittedAt: emittedAt,
	}
}

func TestAugmentSummaryHappyPath(t *testing.T) {
	t.Parallel()

	dayStart, _ := time.Parse("2006-01-02", "2026-05-10")
	ts := dayStart.UTC().Unix()
	ctx := &fakeAuditQueryCtx{events: []handlers.AuditEventRow{
		started(ts + 100),
		started(ts + 200),
		completed(ts+300, 800, 4096, 0, true, 0.012, "2026-05-10T03:14:00Z"),
		completed(ts+400, 1200, 4096, 0, false, 0.024, "2026-05-10T04:55:30Z"),
	}}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date=2026-05-10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp handlers.AugmentSummaryResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Date != "2026-05-10" {
		t.Errorf("Date = %q", resp.Date)
	}
	if resp.TokensConsumed != 2000 {
		t.Errorf("TokensConsumed = %d, want 2000", resp.TokensConsumed)
	}
	if resp.TokensCeiling != 4096 {
		t.Errorf("TokensCeiling = %d, want 4096 (top across events)", resp.TokensCeiling)
	}
	if resp.KGQueriesFired != 2 {
		t.Errorf("KGQueriesFired = %d, want 2 (Started events count)", resp.KGQueriesFired)
	}
	if resp.CacheHitRate < 0.49 || resp.CacheHitRate > 0.51 {
		t.Errorf("CacheHitRate = %f, want ~0.5", resp.CacheHitRate)
	}
	if resp.TotalCost < 0.035 || resp.TotalCost > 0.037 {
		t.Errorf("TotalCost = %f, want ~0.036", resp.TotalCost)
	}
	if resp.LastIndexedRFC3339 != "2026-05-10T04:55:30Z" {
		t.Errorf("LastIndexedRFC3339 = %q", resp.LastIndexedRFC3339)
	}
	if ctx.gotPrefix != "Augmentation" {
		t.Errorf("query prefix = %q, want 'Augmentation'", ctx.gotPrefix)
	}
}

func TestAugmentSummaryEmptyDayReturnsZeros(t *testing.T) {
	t.Parallel()
	ctx := &fakeAuditQueryCtx{events: nil}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date=2026-05-10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentSummaryResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TokensConsumed != 0 || resp.KGQueriesFired != 0 || resp.CacheHitRate != 0 {
		t.Errorf("expected zeros; got %+v", resp)
	}
	if resp.TokensCeiling != 10000 {
		t.Errorf("default TokensCeiling = %d, want 10000", resp.TokensCeiling)
	}
	if resp.Date != "2026-05-10" {
		t.Errorf("Date echo = %q", resp.Date)
	}
}

func TestAugmentSummaryDefaultsToToday(t *testing.T) {
	t.Parallel()
	ctx := &fakeAuditQueryCtx{events: nil}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentSummaryResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	today := time.Now().UTC().Format("2006-01-02")
	if resp.Date != today {
		t.Errorf("Date = %q, want %q (today UTC)", resp.Date, today)
	}
}

func TestAugmentSummaryInvalidDateFormat(t *testing.T) {
	t.Parallel()
	cases := []string{"2026/05/10", "2026-5-10", "abc", "2026-13-01"}
	for _, bad := range cases {
		ctx := &fakeAuditQueryCtx{}
		h := handlers.AugmentSummaryHandler(ctx)
		r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date="+bad, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("date=%q: status = %d, want 400", bad, w.Code)
		}
	}
}

func TestAugmentSummaryMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentSummaryHandler(&fakeAuditQueryCtx{})
	r := httptest.NewRequest(http.MethodPost, "/v1/augment/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestAugmentSummaryMalformedPayloadSkipped(t *testing.T) {
	t.Parallel()
	dayStart, _ := time.Parse("2006-01-02", "2026-05-10")
	ts := dayStart.UTC().Unix()
	ctx := &fakeAuditQueryCtx{events: []handlers.AuditEventRow{
		{ID: "bad", Type: "AugmentationCompleted", PayloadRaw: "not json", EmittedAt: ts + 100},
		completed(ts+200, 500, 4096, 0, true, 0.005, ""),
	}}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date=2026-05-10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentSummaryResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TokensConsumed != 500 {
		t.Errorf("malformed payload should skip; got TokensConsumed = %d", resp.TokensConsumed)
	}
}

func TestAugmentSummaryAuditQueryError(t *testing.T) {
	t.Parallel()
	ctx := &fakeAuditQueryCtx{queryErr: invalidParamErr{"db dead"}}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date=2026-05-10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestAugmentSummaryExcludesNextDayEvents(t *testing.T) {
	t.Parallel()
	dayStart, _ := time.Parse("2006-01-02", "2026-05-10")
	ts := dayStart.UTC().Unix()
	nextDay := ts + int64(25*3600)
	ctx := &fakeAuditQueryCtx{events: []handlers.AuditEventRow{
		completed(ts+100, 500, 4096, 0, true, 0.005, ""),
		completed(nextDay, 9999, 4096, 0, false, 99.99, ""),
	}}
	h := handlers.AugmentSummaryHandler(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/summary?date=2026-05-10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.AugmentSummaryResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TokensConsumed != 500 {
		t.Errorf("next-day event leaked; got TokensConsumed = %d", resp.TokensConsumed)
	}
}

func TestAugmentProbeConfiguredWired(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentProbeHandler(&fakeAugmentProbeCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/probe?check=configured", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
}

func TestAugmentProbeConfiguredNotWired(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentProbeHandler(&fakeAugmentProbeCtx{h: nil})
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/probe?check=configured", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "warn" {
		t.Errorf("Status = %q, want warn", resp.Status)
	}
	if !strings.Contains(resp.Detail, "not wired") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestAugmentProbeUnknownCheck(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentProbeHandler(&fakeAugmentProbeCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/probe?check=mystery", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.AugmentProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "unknown") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestAugmentProbeMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentProbeHandler(&fakeAugmentProbeCtx{})
	r := httptest.NewRequest(http.MethodPost, "/v1/augment/probe", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestAugmentProbePipelineReachableNotWired(t *testing.T) {
	t.Parallel()
	h := handlers.AugmentProbeHandler(&fakeAugmentProbeCtx{h: nil})
	r := httptest.NewRequest(http.MethodGet, "/v1/augment/probe?check=pipeline_reachable", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.AugmentProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "warn" {
		t.Errorf("Status = %q, want warn", resp.Status)
	}
}

type invalidParamErr struct{ s string }

func (e invalidParamErr) Error() string { return e.s }
