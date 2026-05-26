package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScheduleCreate_RoutineHTTPRoundTrip(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody CreateRoutineRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "01H8XK0J1234567890",
			"tier": "routine",
			"next_run_at": "2026-05-08T08:00:00Z",
			"raw_bearer_token": "tok-abc"
		}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.ScheduleCreate(context.Background(), CreateRoutineRequest{
		ProjectAlias:  "internal-platform-x",
		Action:        "morning-brief",
		Trigger:       "http",
		MissPolicyStr: "doctrine",
	})
	if err != nil {
		t.Fatalf("ScheduleCreate: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/schedules" {
		t.Errorf("path = %q, want /v1/schedules", gotPath)
	}
	if gotBody.ProjectAlias != "internal-platform-x" || gotBody.Action != "morning-brief" || gotBody.Trigger != "http" {
		t.Errorf("body = %+v, want internal-platform-x/morning-brief/http", gotBody)
	}
	if resp.ID != "01H8XK0J1234567890" || resp.Tier != "routine" || resp.RawBearerToken != "tok-abc" {
		t.Errorf("resp = %+v, want full decoded shape", resp)
	}
	want := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	if !resp.NextRunAt.Equal(want) {
		t.Errorf("NextRunAt = %v, want %v", resp.NextRunAt, want)
	}
}

func TestScheduleCreate_422Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid cron expression", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.ScheduleCreate(context.Background(), CreateRoutineRequest{
		ProjectAlias: "x", Action: "y", Trigger: "cron", CronExpr: "bogus",
	})
	if err == nil {
		t.Fatal("expected 422 error")
	}
	if !IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		t.Errorf("err not 422 typed: %v", err)
	}
}

func TestScheduleCreateTask_HTTPRoundTrip(t *testing.T) {
	var gotKind string
	var gotIn float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotKind, _ = body["kind"].(string)
		gotIn, _ = body["in_ns"].(float64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "task-01",
			"tier": "task",
			"next_run_at": "2026-05-07T13:30:00Z"
		}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.ScheduleCreateTask(context.Background(), CreateTaskRequest{
		ProjectAlias: "internal-platform-x", Action: "send-report", In: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ScheduleCreateTask: %v", err)
	}
	if gotKind != "task" {
		t.Errorf("body.kind = %q, want task", gotKind)
	}
	if int64(gotIn) != int64(30*time.Minute) {
		t.Errorf("body.in_ns = %v, want 30m", gotIn)
	}
	if resp.ID != "task-01" || resp.Tier != "task" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestScheduleCreateLoop_HTTPRoundTrip(t *testing.T) {
	var gotKind string
	var gotInterval float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotKind, _ = body["kind"].(string)
		gotInterval, _ = body["interval_ns"].(float64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "loop-01",
			"tier": "loop",
			"session_id": "zen-internal-platform-x-deadbeef"
		}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.ScheduleCreateLoop(context.Background(), CreateLoopRequest{
		ProjectAlias: "internal-platform-x", Action: "watch-inbox", Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ScheduleCreateLoop: %v", err)
	}
	if gotKind != "loop" {
		t.Errorf("body.kind = %q, want loop", gotKind)
	}
	if int64(gotInterval) != int64(5*time.Minute) {
		t.Errorf("body.interval_ns = %v, want 5m", gotInterval)
	}
	if resp.SessionID != "zen-internal-platform-x-deadbeef" {
		t.Errorf("resp.SessionID = %q", resp.SessionID)
	}
}

func TestScheduleList_AllProjects(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schedules": [
				{"id":"a","project_alias":"internal-platform-x","action":"morning-brief","tier":"routine","status":"enabled","next_run_at":"2026-05-08T08:00:00Z"},
				{"id":"b","project_alias":"nexus","action":"weekly-review","tier":"routine","status":"enabled","next_run_at":"2026-05-09T18:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleList(context.Background(), "")
	if err != nil {
		t.Fatalf("ScheduleList: %v", err)
	}
	if gotPath != "/v1/schedules" {
		t.Errorf("path = %q, want /v1/schedules (no query)", gotPath)
	}
	if len(rows) != 2 || rows[0].ProjectAlias != "internal-platform-x" || rows[1].ProjectAlias != "nexus" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestScheduleList_ByAlias(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schedules":[]}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleList(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("ScheduleList: %v", err)
	}
	if !strings.Contains(gotPath, "alias=internal-platform-x") {
		t.Errorf("query missing alias filter: %q", gotPath)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("rows should be empty non-nil slice, got %v", rows)
	}
}

func TestScheduleDelete_OK(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if err := c.ScheduleDelete(context.Background(), "abc-123"); err != nil {
		t.Fatalf("ScheduleDelete: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/schedules/abc-123/delete" {
		t.Errorf("path = %q, want /v1/schedules/abc-123/delete", gotPath)
	}
}

func TestScheduleDelete_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "schedule not found", http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.ScheduleDelete(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err not 404: %v", err)
	}
}

func TestScheduleDelete_EmptyIDRefused(t *testing.T) {
	c := NewWithBaseURL("http://unused")
	err := c.ScheduleDelete(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on empty id")
	}
	if !strings.Contains(err.Error(), "id is empty") {
		t.Errorf("err = %v, want substring 'id is empty'", err)
	}
}

func TestScheduleRun_HTTPRoundTrip(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"outcome": "success",
			"cost_usd": 0.0123,
			"duration_ms": 4567
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.ScheduleRun(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/schedules/abc-123/run" {
		t.Errorf("path = %q", gotPath)
	}
	if resp.Outcome != "success" || resp.DurationMs != 4567 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestScheduleRun_EmptyIDRefused(t *testing.T) {
	c := NewWithBaseURL("http://unused")
	if _, err := c.ScheduleRun(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty id")
	}
}

func TestScheduleHistory_HTTPRoundTrip(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"rows": [
				{"schedule_id":"abc","fired_at":"2026-05-07T08:00:00Z","outcome":0,"duration_ms":4000,"cost_usd":0.01}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 7, 23, 59, 59, 0, time.UTC)
	rows, err := c.ScheduleHistory(context.Background(), "abc", from, to)
	if err != nil {
		t.Fatalf("ScheduleHistory: %v", err)
	}
	if !strings.Contains(gotPath, "/v1/schedules/abc/history") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotPath, "from=2026-05-01") || !strings.Contains(gotPath, "to=2026-05-07") {
		t.Errorf("path missing from/to in query: %q", gotPath)
	}
	if len(rows) != 1 || rows[0].ScheduleID != "abc" || rows[0].Outcome != 0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestScheduleHistory_EmptyIDRefused(t *testing.T) {
	c := NewWithBaseURL("http://unused")
	if _, err := c.ScheduleHistory(context.Background(), "", time.Now(), time.Now()); err == nil {
		t.Fatal("expected error on empty id")
	}
}

func TestScheduleQueue_HTTPRoundTrip(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"rows": [
				{"id":"a","project_alias":"internal-platform-x","action":"morning-brief","next_run_at":"2026-05-08T08:00:00Z"},
				{"id":"b","project_alias":"nexus","action":"hourly-poll","next_run_at":"2026-05-07T19:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleQueue(context.Background())
	if err != nil {
		t.Fatalf("ScheduleQueue: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/schedules/queue" {
		t.Errorf("path = %q", gotPath)
	}
	if len(rows) != 2 || rows[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestScheduleQueue_EmptyArrayNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":null}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleQueue(context.Background())
	if err != nil {
		t.Fatalf("ScheduleQueue: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", rows)
	}
}

func TestScheduleHistory_EmptyArrayNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":null}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleHistory(context.Background(), "abc", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("ScheduleHistory: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", rows)
	}
}

func TestScheduleList_EmptyArrayNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schedules":null}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ScheduleList(context.Background(), "")
	if err != nil {
		t.Fatalf("ScheduleList: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", rows)
	}
}
