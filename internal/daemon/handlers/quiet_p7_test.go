package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type fakeQuietStore struct {
	mu        sync.Mutex
	cfg       inbox.QuietConfig
	getErr    error
	pauseErr  error
	cancelErr error

	lastPauseUntil time.Time
	cancelInvoked  bool
}

func newFakeQuietStore() *fakeQuietStore {
	return &fakeQuietStore{
		cfg: inbox.QuietConfig{
			Default: inbox.QuietHours{
				Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: true,
			},
			PerProject: map[string]inbox.QuietHours{},
		},
	}
}

func (f *fakeQuietStore) Get(_ context.Context) (inbox.QuietConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return inbox.QuietConfig{}, f.getErr
	}
	return f.cfg, nil
}

func (f *fakeQuietStore) SetUrgentPause(_ context.Context, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPauseUntil = until
	if f.pauseErr != nil {
		return f.pauseErr
	}
	t := until
	f.cfg.UrgentPauseUntil = &t
	return nil
}

func (f *fakeQuietStore) CancelUrgentPause(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelInvoked = true
	if f.cancelErr != nil {
		return f.cancelErr
	}
	f.cfg.UrgentPauseUntil = nil
	return nil
}

type fakeQuietAccessor struct{ store QuietStore }

func (f *fakeQuietAccessor) QuietStore() QuietStore { return f.store }

type nilQuietAccessor struct{}

func (nilQuietAccessor) QuietStore() QuietStore { return nil }

func TestQuietGet503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	QuietGetHandler(nilQuietAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestQuietUrgentPause503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(`{"until":"2026-05-08T08:00:00Z"}`))
	QuietUrgentPauseHandler(nilQuietAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestQuietCancel503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/cancel", nil)
	QuietCancelHandler(nilQuietAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestQuietResolveNonAccessorYields503(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	type notAnAccessor struct{}
	QuietGetHandler(notAnAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestQuietUrgentPauseBadJSON(t *testing.T) {
	st := newFakeQuietStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(`not json`))
	QuietUrgentPauseHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestQuietUrgentPauseMissingUntil422(t *testing.T) {
	st := newFakeQuietStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(`{}`))
	QuietUrgentPauseHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestQuietUrgentPausePastUntil422(t *testing.T) {
	st := newFakeQuietStore()
	rec := httptest.NewRecorder()
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	body := `{"until":"` + past + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(body))
	QuietUrgentPauseHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuietGetHappyPath(t *testing.T) {
	st := newFakeQuietStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	QuietGetHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp QuietGetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Default.StartSec != int64((21 * time.Hour).Seconds()) {
		t.Errorf("StartSec = %d, want %d", resp.Default.StartSec, int64((21 * time.Hour).Seconds()))
	}
	if !resp.Default.UrgentBypass {
		t.Error("UrgentBypass = false, want true")
	}
}

func TestQuietGetWithActivePause(t *testing.T) {
	st := newFakeQuietStore()
	until := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	st.cfg.UrgentPauseUntil = &until
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	QuietGetHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp QuietGetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.UrgentPauseUntil == nil {
		t.Fatal("UrgentPauseUntil is nil; want non-nil")
	}
	if !resp.UrgentPauseUntil.Equal(until) {
		t.Errorf("UrgentPauseUntil = %v, want %v", *resp.UrgentPauseUntil, until)
	}
}

func TestQuietGetWithPerProject(t *testing.T) {
	st := newFakeQuietStore()
	st.cfg.PerProject["project-a"] = inbox.QuietHours{
		Start: 22 * time.Hour, End: 6 * time.Hour, UrgentBypass: true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	QuietGetHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	var resp QuietGetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	override, ok := resp.PerProject["project-a"]
	if !ok {
		t.Fatal("project-a override missing")
	}
	if override.StartSec != int64((22 * time.Hour).Seconds()) {
		t.Errorf("Start = %d, want %d", override.StartSec, int64((22 * time.Hour).Seconds()))
	}
}

func TestQuietUrgentPauseHappyPath(t *testing.T) {
	st := newFakeQuietStore()
	until := time.Now().UTC().Add(30 * time.Minute)
	body, _ := json.Marshal(QuietPauseRequest{Until: until})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(string(body)))
	QuietUrgentPauseHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !st.lastPauseUntil.Equal(until) {
		t.Errorf("lastPauseUntil = %v, want %v", st.lastPauseUntil, until)
	}
}

func TestQuietCancelHappyPath(t *testing.T) {
	st := newFakeQuietStore()
	until := time.Now().UTC().Add(30 * time.Minute)
	st.cfg.UrgentPauseUntil = &until
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/cancel", nil)
	QuietCancelHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !st.cancelInvoked {
		t.Error("Cancel was not invoked")
	}
}

func TestQuietGet500OnBackendError(t *testing.T) {
	st := newFakeQuietStore()
	st.getErr = errFakeQuietStore
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/quiet", nil)
	QuietGetHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestQuietUrgentPause500OnBackendError(t *testing.T) {
	st := newFakeQuietStore()
	st.pauseErr = errFakeQuietStore
	until := time.Now().UTC().Add(time.Hour)
	body, _ := json.Marshal(QuietPauseRequest{Until: until})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/urgent-pause",
		strings.NewReader(string(body)))
	QuietUrgentPauseHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestQuietCancel500OnBackendError(t *testing.T) {
	st := newFakeQuietStore()
	st.cancelErr = errFakeQuietStore
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quiet/cancel", nil)
	QuietCancelHandler(&fakeQuietAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

var errFakeQuietStore = errors.New("fake quiet store error")
