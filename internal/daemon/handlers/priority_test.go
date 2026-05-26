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

	"github.com/cbip-solutions/hades-system/internal/quota"
)

type fakeOverrideStore struct {
	mu   sync.Mutex
	rows map[string]quota.Override

	failGet   bool
	failSet   bool
	failReset bool
	failList  bool
}

func newFakeOverrideStore() *fakeOverrideStore {
	return &fakeOverrideStore{rows: map[string]quota.Override{}}
}

func (f *fakeOverrideStore) Get(_ context.Context, alias string) (*quota.Override, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failGet {
		return nil, errFakeOverride
	}
	if r, ok := f.rows[alias]; ok {
		c := r
		return &c, nil
	}
	return nil, nil
}

func (f *fakeOverrideStore) Set(_ context.Context, alias string, multiplier float64, expiresAt time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSet {
		return errFakeOverride
	}

	if multiplier <= 0 || multiplier > 100 {
		return errors.Join(quota.ErrInvalidOverride, errors.New("multiplier out of range"))
	}
	if !expiresAt.After(time.Now()) {
		return errors.Join(quota.ErrInvalidOverride, errors.New("expiresAt must be in future"))
	}
	if strings.TrimSpace(reason) == "" {
		return errors.Join(quota.ErrInvalidOverride, errors.New("reason is empty"))
	}
	f.rows[alias] = quota.Override{
		Alias:      alias,
		Multiplier: multiplier,
		ExpiresAt:  expiresAt,
		Reason:     reason,
		CreatedAt:  time.Now().UTC(),
	}
	return nil
}

func (f *fakeOverrideStore) Reset(_ context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReset {
		return errFakeOverride
	}
	delete(f.rows, alias)
	return nil
}

func (f *fakeOverrideStore) List(_ context.Context) ([]quota.Override, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList {
		return nil, errFakeOverride
	}
	out := make([]quota.Override, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

var errFakeOverride = errors.New("fake override store error")

type fakeOverrideAccessor struct {
	store quota.OverrideStore
}

func (f *fakeOverrideAccessor) OverrideStore() quota.OverrideStore { return f.store }

type nilOverrideAccessor struct{}

func (nilOverrideAccessor) OverrideStore() quota.OverrideStore { return nil }

// TestPriority503WhenAccessorNotImplemented — when the caller passes
// any value that does NOT satisfy overrideStoreAccessor (e.g.,
// daemon-bootstrap typo, programmer error in cmd/zen-swarm-ctld), the
// handler MUST surface the canonical 503 rather than panicking on the
// type assertion.
func TestPriority503WhenAccessorNotImplemented(t *testing.T) {

	bogus := 42
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/priority/list", nil)
	PriorityList(bogus).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503 (non-accessor input)", rec.Code)
	}
}

func TestPriorityBoost503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost",
		bytes.NewBufferString(`{"alias":"x","multiplier":3,"expires_at":"2030-01-01T00:00:00Z","reason":"u"}`))
	PriorityBoost(nilOverrideAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rec.Code)
	}
}

func TestPriorityReset503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset",
		bytes.NewBufferString(`{"alias":"x"}`))
	PriorityReset(nilOverrideAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rec.Code)
	}
}

func TestPriorityList503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/priority/list", nil)
	PriorityList(nilOverrideAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rec.Code)
	}
}

func TestPriorityBoostBadJSON(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(`not-json`))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

func TestPriorityBoostAliasRequired(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	body := `{"alias":"","multiplier":3,"expires_at":"2030-01-01T00:00:00Z","reason":"u"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(body))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityResetBadJSON(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset", bytes.NewBufferString(`not-json`))
	PriorityReset(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

func TestPriorityResetAliasRequired(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset", bytes.NewBufferString(`{"alias":""}`))
	PriorityReset(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

func TestPriorityBoost200Persists(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	expires := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"alias":"internal-platform-x","multiplier":3,"expires_at":"` + expires + `","reason":"investigation"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(body))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	row, ok := st.rows["internal-platform-x"]
	if !ok {
		t.Fatal("row not persisted")
	}
	if row.Multiplier != 3.0 {
		t.Errorf("multiplier=%v want 3.0", row.Multiplier)
	}
	if row.Reason != "investigation" {
		t.Errorf("reason=%q want investigation", row.Reason)
	}
}

func TestPriorityReset200(t *testing.T) {
	st := newFakeOverrideStore()
	st.rows["internal-platform-x"] = quota.Override{
		Alias: "internal-platform-x", Multiplier: 3.0,
		ExpiresAt: time.Now().Add(time.Hour),
		Reason:    "u",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset",
		bytes.NewBufferString(`{"alias":"internal-platform-x"}`))
	PriorityReset(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := st.rows["internal-platform-x"]; ok {
		t.Error("row not deleted")
	}
}

func TestPriorityResetIdempotent(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset",
		bytes.NewBufferString(`{"alias":"never-existed"}`))
	PriorityReset(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d want 200 (idempotent); body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityList200ReturnsRows(t *testing.T) {
	st := newFakeOverrideStore()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(4 * time.Hour)
	st.rows["internal-platform-x"] = quota.Override{
		Alias: "internal-platform-x", Multiplier: 3.0,
		ExpiresAt: expires, Reason: "urgent", CreatedAt: now,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/priority/list", nil)
	PriorityList(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Overrides []map[string]any `json:"overrides"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Overrides) != 1 {
		t.Fatalf("len(overrides)=%d want 1", len(resp.Overrides))
	}
	row := resp.Overrides[0]
	if row["alias"] != "internal-platform-x" {
		t.Errorf("alias=%v want internal-platform-x", row["alias"])
	}
	if m, _ := row["multiplier"].(float64); m != 3.0 {
		t.Errorf("multiplier=%v want 3.0", row["multiplier"])
	}
	if s, _ := row["reason"].(string); s != "urgent" {
		t.Errorf("reason=%v want urgent", row["reason"])
	}
}

func TestPriorityList200Empty(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/priority/list", nil)
	PriorityList(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var resp struct {
		Overrides []map[string]any `json:"overrides"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// MUST be empty array (never null) — predictable shape for clients.
	if resp.Overrides == nil {
		t.Error("overrides=nil want []")
	}
	if len(resp.Overrides) != 0 {
		t.Errorf("len(overrides)=%d want 0", len(resp.Overrides))
	}
}

func TestPriorityBoost422OnInvalidMultiplier(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := `{"alias":"a","multiplier":-1,"expires_at":"` + expires + `","reason":"u"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(body))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status=%d want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityBoost422OnPastExpiry(t *testing.T) {
	st := newFakeOverrideStore()
	rec := httptest.NewRecorder()
	body := `{"alias":"a","multiplier":3,"expires_at":"2000-01-01T00:00:00Z","reason":"u"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(body))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status=%d want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityBoost500OnStoreFailure(t *testing.T) {
	st := newFakeOverrideStore()
	st.failSet = true
	rec := httptest.NewRecorder()
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := `{"alias":"a","multiplier":3,"expires_at":"` + expires + `","reason":"u"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/boost", bytes.NewBufferString(body))
	PriorityBoost(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityReset500OnStoreFailure(t *testing.T) {
	st := newFakeOverrideStore()
	st.failReset = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/priority/reset",
		bytes.NewBufferString(`{"alias":"a"}`))
	PriorityReset(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPriorityList500OnStoreFailure(t *testing.T) {
	st := newFakeOverrideStore()
	st.failList = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/priority/list", nil)
	PriorityList(&fakeOverrideAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want 500; body=%s", rec.Code, rec.Body.String())
	}
}
