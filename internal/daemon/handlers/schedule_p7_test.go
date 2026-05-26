package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

type fakeScheduleStore struct {
	mu       sync.Mutex
	byID     map[string]*scheduler.Schedule
	history  map[string][]scheduler.HistoryEntry
	deletes  []string
	failGet  bool
	failList bool
	failIns  bool
	failDel  bool
}

func newFakeScheduleStore() *fakeScheduleStore {
	return &fakeScheduleStore{
		byID:    map[string]*scheduler.Schedule{},
		history: map[string][]scheduler.HistoryEntry{},
	}
}

func (f *fakeScheduleStore) Insert(_ context.Context, s *scheduler.Schedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failIns {
		return errFakeStore
	}
	if _, ok := f.byID[s.ID]; ok {
		return errFakeStore
	}
	clone := *s
	f.byID[s.ID] = &clone
	return nil
}

func (f *fakeScheduleStore) Get(_ context.Context, id string) (*scheduler.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failGet {
		return nil, errFakeStore
	}
	s, ok := f.byID[id]
	if !ok {
		return nil, nil
	}
	clone := *s
	return &clone, nil
}

func (f *fakeScheduleStore) List(_ context.Context, alias string) ([]*scheduler.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList {
		return nil, errFakeStore
	}
	out := make([]*scheduler.Schedule, 0, len(f.byID))
	for _, s := range f.byID {
		if alias != "" && s.ProjectAlias != alias {
			continue
		}
		clone := *s
		out = append(out, &clone)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeScheduleStore) SoftDelete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDel {
		return errFakeStore
	}
	delete(f.byID, id)
	f.deletes = append(f.deletes, id)
	return nil
}

func (f *fakeScheduleStore) QueryHistory(_ context.Context, scheduleID string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.history[scheduleID]
	out := make([]scheduler.HistoryEntry, len(rows))
	copy(out, rows)
	return out, nil
}

func (f *fakeScheduleStore) ListDue(_ context.Context, until time.Time) ([]*scheduler.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*scheduler.Schedule, 0)
	for _, s := range f.byID {
		if s.Status != scheduler.StatusEnabled {
			continue
		}
		if s.NextRunAt.IsZero() {
			continue
		}
		if !s.NextRunAt.After(until) {
			clone := *s
			out = append(out, &clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRunAt.Before(out[j].NextRunAt) })
	return out, nil
}

var errFakeStore = &scheduleHandlerErr{msg: "fake store error"}

type scheduleHandlerErr struct{ msg string }

func (e *scheduleHandlerErr) Error() string { return e.msg }

type fakeScheduleAccessor struct{ store ScheduleStore }

func (f *fakeScheduleAccessor) ScheduleStore() ScheduleStore { return f.store }

type nilScheduleAccessor struct{}

func (nilScheduleAccessor) ScheduleStore() ScheduleStore { return nil }

func TestScheduleCreate503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(`{"project_alias":"x","action":"y"}`))
	ScheduleCreate(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleList503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	ScheduleList(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleDelete503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/delete", nil)
	ScheduleDelete(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleRun503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/run", nil)
	ScheduleRun(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleHistory503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/abc/history?from=2026-05-01T00:00:00Z&to=2026-05-07T23:59:59Z", nil)
	ScheduleHistory(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleQueue503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/queue", nil)
	ScheduleQueue(nilScheduleAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestScheduleCreateBadJSON(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(`not json`))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleCreateMissingProjectAlias(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(`{"action":"x"}`))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleCreateMissingAction(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(`{"project_alias":"x"}`))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleCreateUnknownKind422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"kind":"banana","project_alias":"x","action":"y"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestScheduleCreateRoutineUnknownTrigger422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"x","action":"y","trigger":"banana"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateRoutineUnknownMissPolicy422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"x","action":"y","trigger":"http","miss_policy":"banana"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateRoutineCronMissingExpr422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"x","action":"y","trigger":"cron"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateRoutineGitPollMissingRepo422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"x","action":"y","trigger":"git-poll"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateTaskNonPositiveIn422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"kind":"task","project_alias":"x","action":"y","in_ns":0}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateLoopSubMinuteInterval422(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"kind":"loop","project_alias":"x","action":"y","interval_ns":1000}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCreateRoutineCronOK(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"internal-platform-x","action":"morning-brief","trigger":"cron","cron_expr":"0 8 * * 1-5"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID == "" {
		t.Error("ID empty in response")
	}
	if resp.Tier != "routine" {
		t.Errorf("Tier = %q, want routine", resp.Tier)
	}
	if resp.NextRunAt.IsZero() {
		t.Error("NextRunAt should be non-zero for cron trigger")
	}
	if resp.RawBearerToken != "" {
		t.Errorf("cron trigger should not produce a bearer token; got %q", resp.RawBearerToken)
	}

	if _, ok := st.byID[resp.ID]; !ok {
		t.Error("schedule not persisted to store")
	}
}

func TestScheduleCreateRoutineHTTPRevealsBearer(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"internal-platform-x","action":"webhook-fire","trigger":"http"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RawBearerToken == "" {
		t.Error("http trigger MUST surface RawBearerToken")
	}

	if len(resp.RawBearerToken) != 43 {
		t.Errorf("RawBearerToken len=%d, want 43 (base64.RawURLEncoding of 32 bytes)", len(resp.RawBearerToken))
	}
	persisted := st.byID[resp.ID]
	if persisted == nil {
		t.Fatal("schedule not persisted")
	}
	if persisted.TriggerConfig.BearerTokenHash == "" {
		t.Error("BearerTokenHash not stored on schedule")
	}
}

func TestScheduleCreateRoutineGitPollOK(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"project_alias":"internal-platform-x","action":"git-watcher","trigger":"git-poll","repo_url":"https://github.com/owner/repo","branch":"dev"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateScheduleResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	persisted := st.byID[resp.ID]
	if persisted == nil {
		t.Fatal("schedule not persisted")
	}
	if persisted.TriggerConfig.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q", persisted.TriggerConfig.RepoURL)
	}
	if persisted.TriggerConfig.Branch != "dev" {
		t.Errorf("Branch = %q", persisted.TriggerConfig.Branch)
	}
}

func TestScheduleCreateTaskOK(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"kind":"task","project_alias":"internal-platform-x","action":"send-report","in_ns":1800000000000}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateScheduleResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Tier != "task" {
		t.Errorf("Tier = %q, want task", resp.Tier)
	}
	if resp.NextRunAt.IsZero() {
		t.Error("NextRunAt should be set for task")
	}
}

func TestScheduleCreateLoopOK(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	body := `{"kind":"loop","project_alias":"internal-platform-x","action":"watch","interval_ns":300000000000}`
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	ScheduleCreate(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateScheduleResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Tier != "loop" {
		t.Errorf("Tier = %q, want loop", resp.Tier)
	}
}

func TestScheduleListEmpty(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	ScheduleList(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp ScheduleListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Schedules == nil {
		t.Error("Schedules should be non-nil empty slice")
	}
	if len(resp.Schedules) != 0 {
		t.Errorf("len = %d, want 0", len(resp.Schedules))
	}
}

func TestScheduleListReturnsRows(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["a"] = &scheduler.Schedule{
		ID:           "a",
		Tier:         scheduler.TierRoutine,
		ProjectAlias: "internal-platform-x",
		Action:       "morning-brief",
		Status:       scheduler.StatusEnabled,
		NextRunAt:    time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC),
	}
	st.byID["b"] = &scheduler.Schedule{
		ID:           "b",
		Tier:         scheduler.TierTask,
		ProjectAlias: "nexus",
		Action:       "report",
		Status:       scheduler.StatusEnabled,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	ScheduleList(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp ScheduleListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Schedules) != 2 {
		t.Errorf("len = %d, want 2", len(resp.Schedules))
	}

	gotTiers := map[string]bool{}
	for _, r := range resp.Schedules {
		gotTiers[r.Tier] = true
	}
	if !gotTiers["routine"] || !gotTiers["task"] {
		t.Errorf("tier strings missing: %v", gotTiers)
	}
}

func TestScheduleListByAlias(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["a"] = &scheduler.Schedule{ID: "a", ProjectAlias: "internal-platform-x", Status: scheduler.StatusEnabled}
	st.byID["b"] = &scheduler.Schedule{ID: "b", ProjectAlias: "nexus", Status: scheduler.StatusEnabled}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules?alias=internal-platform-x", nil)
	ScheduleList(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	var resp ScheduleListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Schedules) != 1 || resp.Schedules[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("filter not applied; got %+v", resp.Schedules)
	}
}

func TestScheduleListStoreError500(t *testing.T) {
	st := newFakeScheduleStore()
	st.failList = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	ScheduleList(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestScheduleDeleteOK(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/delete", nil)
	ScheduleDelete(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(st.deletes) != 1 || st.deletes[0] != "abc" {
		t.Errorf("delete trace = %v", st.deletes)
	}
	if _, ok := st.byID["abc"]; ok {
		t.Error("row not removed from store")
	}
}

func TestScheduleDeleteNotFound404(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/missing/delete", nil)
	ScheduleDelete(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestScheduleDeleteEmptyID400(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules//delete", nil)
	ScheduleDelete(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleDeleteStoreError500(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	st.failDel = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/delete", nil)
	ScheduleDelete(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestScheduleDeleteGetError500(t *testing.T) {
	st := newFakeScheduleStore()
	st.failGet = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/delete", nil)
	ScheduleDelete(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestScheduleRunNotFound404(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/missing/run", nil)
	ScheduleRun(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestScheduleRunPhaseIGap503(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/run", nil)
	ScheduleRun(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (Phase I gap)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Phase I") && !strings.Contains(rec.Body.String(), "FireDeps") {
		t.Errorf("503 body should hint Phase I / FireDeps; got %q", rec.Body.String())
	}
}

func TestScheduleRunGetError500(t *testing.T) {
	st := newFakeScheduleStore()
	st.failGet = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules/abc/run", nil)
	ScheduleRun(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestScheduleRunEmptyID400(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules//run", nil)
	ScheduleRun(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleHistoryMissingFrom400(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/abc/history?to=2026-05-07T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleHistoryMissingTo400(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/abc/history?from=2026-05-01T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleHistoryMalformedFrom400(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/abc/history?from=banana&to=2026-05-07T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleHistoryNotFound404(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/missing/history?from=2026-05-01T00:00:00Z&to=2026-05-07T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestScheduleHistoryReturnsRows(t *testing.T) {
	st := newFakeScheduleStore()
	st.byID["abc"] = &scheduler.Schedule{ID: "abc"}
	st.history["abc"] = []scheduler.HistoryEntry{
		{
			ScheduleID: "abc",
			FiredAt:    time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC),
			Outcome:    scheduler.OutcomeSuccess,
			CostUSD:    0.123,
			DurationMs: 4567,
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/abc/history?from=2026-05-01T00:00:00Z&to=2026-05-07T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp ScheduleHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(resp.Rows))
	}
	if resp.Rows[0].Outcome != int(scheduler.OutcomeSuccess) {
		t.Errorf("Outcome = %d, want 0", resp.Rows[0].Outcome)
	}
	if resp.Rows[0].CostUSD != 0.123 {
		t.Errorf("CostUSD = %v", resp.Rows[0].CostUSD)
	}
}

func TestScheduleHistoryEmptyID400(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules//history?from=2026-05-01T00:00:00Z&to=2026-05-07T00:00:00Z", nil)
	ScheduleHistory(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestScheduleQueueOrderedByNextRunAt(t *testing.T) {
	st := newFakeScheduleStore()
	now := time.Now().UTC()
	st.byID["a"] = &scheduler.Schedule{
		ID: "a", ProjectAlias: "internal-platform-x", Action: "morning-brief",
		Status: scheduler.StatusEnabled, NextRunAt: now.Add(8 * time.Hour),
	}
	st.byID["b"] = &scheduler.Schedule{
		ID: "b", ProjectAlias: "nexus", Action: "report",
		Status: scheduler.StatusEnabled, NextRunAt: now.Add(2 * time.Hour),
	}
	st.byID["c-disabled"] = &scheduler.Schedule{
		ID: "c", ProjectAlias: "x", Action: "y",
		Status: scheduler.StatusDisabled, NextRunAt: now.Add(1 * time.Hour),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/queue", nil)
	ScheduleQueue(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp ScheduleQueueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Rows) != 2 {
		t.Fatalf("rows len = %d, want 2 (disabled excluded)", len(resp.Rows))
	}

	if resp.Rows[0].ID != "b" || resp.Rows[1].ID != "a" {
		t.Errorf("order = [%s, %s], want [b, a]", resp.Rows[0].ID, resp.Rows[1].ID)
	}
}

func TestScheduleQueueEmpty(t *testing.T) {
	st := newFakeScheduleStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/schedules/queue", nil)
	ScheduleQueue(&fakeScheduleAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp ScheduleQueueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Rows == nil {
		t.Error("Rows should be non-nil empty")
	}
	if len(resp.Rows) != 0 {
		t.Errorf("len = %d, want 0", len(resp.Rows))
	}
}

func TestScheduleHandlersAllReturnHandler(t *testing.T) {
	st := newFakeScheduleStore()
	acc := &fakeScheduleAccessor{store: st}
	for _, h := range []http.HandlerFunc{
		ScheduleCreate(acc),
		ScheduleList(acc),
		ScheduleDelete(acc),
		ScheduleRun(acc),
		ScheduleHistory(acc),
		ScheduleQueue(acc),
	} {
		if h == nil {
			t.Error("handler factory returned nil")
		}
	}
}

func TestScheduleE2EHTTPRoundTrip(t *testing.T) {
	st := newFakeScheduleStore()
	acc := &fakeScheduleAccessor{store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/schedules", ScheduleCreate(acc))
	mux.HandleFunc("GET /v1/schedules", ScheduleList(acc))
	mux.HandleFunc("POST /v1/schedules/{id}/delete", ScheduleDelete(acc))
	mux.HandleFunc("POST /v1/schedules/{id}/run", ScheduleRun(acc))
	mux.HandleFunc("GET /v1/schedules/{id}/history", ScheduleHistory(acc))
	mux.HandleFunc("GET /v1/schedules/queue", ScheduleQueue(acc))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"project_alias":"internal-platform-x","action":"morning-brief","trigger":"cron","cron_expr":"0 8 * * 1-5"}`
	httpResp, err := http.Post(srv.URL+"/v1/schedules", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", httpResp.StatusCode)
	}
	var created CreateScheduleResponse
	_ = json.NewDecoder(httpResp.Body).Decode(&created)
	httpResp.Body.Close()
	if created.ID == "" {
		t.Fatal("create returned empty ID")
	}

	listResp, err := http.Get(srv.URL + "/v1/schedules")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", listResp.StatusCode)
	}
	listResp.Body.Close()

	delResp, err := http.Post(srv.URL+"/v1/schedules/"+created.ID+"/delete", "", nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", delResp.StatusCode)
	}
	delResp.Body.Close()
}
