// SPDX-License-Identifier: MIT
// Package handlers — notifications_post_test.go.
//
// TDD-first test set for the NotificationsPost endpoint
// (POST /v1/notifications/post). Covers the full status-code matrix
// dictated by the spec: 200 happy path, 400 bad-shape rejections
// (bad JSON, missing/invalid severity, missing source, missing or bad
// timestamp), 503 store-not-wired (boot race window), 500 insert error.
//
// The endpoint is the daemon-side companion to the cross-repo
// http_notify HTTP client living in `cbip-solutions/zen-bypass-tier1`.
// The sidecar maintains NO SQLite state (inv-zen-282); notifications
// flow exclusively through this endpoint.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type fakeNotificationsInserter struct {
	calls []store.Notification
	err   error
	id    int64
}

func (f *fakeNotificationsInserter) InsertBypassNotification(_ context.Context, n store.Notification) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.calls = append(f.calls, n)
	if f.id == 0 {
		return 42, nil
	}
	return f.id, nil
}

type fakeNotificationsPostCtx struct {
	inserter NotificationsInserter
}

func (f *fakeNotificationsPostCtx) NotificationsInserter() NotificationsInserter {
	return f.inserter
}

func validNotificationBody() map[string]any {
	return map[string]any{
		"severity": "warn",
		"source":   "bypass-sidecar",
		"ts":       time.Now().UTC().Format(time.RFC3339),
		"payload": map[string]any{
			"title": "test title",
			"body":  "test body",
		},
	}
}

func dispatchPost(t *testing.T, ctx NotificationsPostCtx, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications/post", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NotificationsPost(ctx)(w, req)
	return w
}

func TestNotificationsPost_OK(t *testing.T) {
	inserter := &fakeNotificationsInserter{id: 99}
	ctx := &fakeNotificationsPostCtx{inserter: inserter}
	w := dispatchPost(t, ctx, validNotificationBody())

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(inserter.calls) != 1 {
		t.Fatalf("inserter calls = %d, want 1", len(inserter.calls))
	}
	got := inserter.calls[0]
	if got.Severity != "WARN" {
		t.Errorf("severity = %q, want WARN (canonical upper)", got.Severity)
	}
	if got.Source != "bypass-sidecar" {
		t.Errorf("source = %q", got.Source)
	}
	if got.TS.IsZero() {
		t.Error("ts is zero")
	}

	if got.Title != "test title" {
		t.Errorf("title = %q, want test title", got.Title)
	}
	if got.Body != "test body" {
		t.Errorf("body = %q, want test body", got.Body)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["inserted"] != true {
		t.Errorf("response inserted = %v, want true", resp["inserted"])
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing")
	}
}

func TestNotificationsPost_SeverityNormalization(t *testing.T) {
	cases := []struct {
		wire string
		want string
	}{
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"WARN", "WARN"},
		{"error", "CRITICAL"},
		{"ERROR", "CRITICAL"},
		{"critical", "CRITICAL"},
		{"CRITICAL", "CRITICAL"},
	}
	for _, tc := range cases {
		t.Run(tc.wire, func(t *testing.T) {
			inserter := &fakeNotificationsInserter{}
			body := validNotificationBody()
			body["severity"] = tc.wire
			w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: inserter}, body)
			if w.Code != http.StatusOK {
				t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
			}
			if len(inserter.calls) != 1 {
				t.Fatalf("inserter calls = %d", len(inserter.calls))
			}
			if got := inserter.calls[0].Severity; got != tc.want {
				t.Errorf("severity = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNotificationsPost_PayloadFallback(t *testing.T) {
	inserter := &fakeNotificationsInserter{}
	body := validNotificationBody()
	body["payload"] = map[string]any{
		"field": "anomaly-unknown",
		"pct":   0.51,
	}
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: inserter}, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(inserter.calls) != 1 {
		t.Fatalf("inserter calls = %d", len(inserter.calls))
	}

	if inserter.calls[0].Title == "" {
		t.Error("title fell back to empty; expected source-derived default")
	}

	if !strings.Contains(inserter.calls[0].Body, "anomaly-unknown") {
		t.Errorf("body = %q; expected serialized payload", inserter.calls[0].Body)
	}
}

func TestNotificationsPost_BadJSON(t *testing.T) {
	ctx := &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications/post", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NotificationsPost(ctx)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "bad_json" {
		t.Errorf("code = %q, want bad_json", resp.Code)
	}
	if resp.RequestID == "" {
		t.Error("request_id missing in error body")
	}
}

func TestNotificationsPost_BadSeverity(t *testing.T) {
	cases := []string{"", "fatal", "verbose", "DEBUG", "high"}
	for _, sev := range cases {
		t.Run(sev, func(t *testing.T) {
			body := validNotificationBody()
			body["severity"] = sev
			w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d, want 400", w.Code)
			}
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != "bad_severity" {
				t.Errorf("code = %q, want bad_severity", resp.Code)
			}
		})
	}
}

func TestNotificationsPost_MissingSource(t *testing.T) {
	body := validNotificationBody()
	body["source"] = ""
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "bad_source" {
		t.Errorf("code = %q, want bad_source", resp.Code)
	}
}

func TestNotificationsPost_SourceTooLong(t *testing.T) {
	body := validNotificationBody()
	body["source"] = strings.Repeat("s", maxNotificationSource+1)
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "bad_source" {
		t.Errorf("code = %q, want bad_source", resp.Code)
	}
}

func TestNotificationsPost_BadTimestamp(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"empty_string", ""},
		{"not_rfc3339", "yesterday"},
		{"zero", "0001-01-01T00:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validNotificationBody()
			body["ts"] = tc.val
			w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d, want 400", w.Code)
			}
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != "bad_timestamp" {
				t.Errorf("code = %q, want bad_timestamp", resp.Code)
			}
		})
	}
}

func TestNotificationsPost_PayloadTooLarge(t *testing.T) {
	body := validNotificationBody()

	big := strings.Repeat("x", maxNotificationPayload+1)
	body["payload"] = map[string]any{"body": big}
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: &fakeNotificationsInserter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "payload_too_large" {
		t.Errorf("code = %q, want payload_too_large", resp.Code)
	}
}

func TestNotificationsPost_InserterUnwired(t *testing.T) {
	ctx := &fakeNotificationsPostCtx{inserter: nil}
	w := dispatchPost(t, ctx, validNotificationBody())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d, want 503", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "store_unavailable" {
		t.Errorf("code = %q, want store_unavailable", resp.Code)
	}
}

func TestNotificationsPost_DBError(t *testing.T) {
	inserter := &fakeNotificationsInserter{err: errors.New("sqlite: disk i/o error /tmp/zen.db")}
	ctx := &fakeNotificationsPostCtx{inserter: inserter}
	w := dispatchPost(t, ctx, validNotificationBody())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d, want 500", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "insert_failed" {
		t.Errorf("code = %q, want insert_failed", resp.Code)
	}
	// Defense-in-depth: upstream SQL error text MUST NOT leak (paths,
	// errno strings). Only opaque stable code goes out.
	if strings.Contains(resp.Error, "disk i/o") || strings.Contains(resp.Error, "/tmp/zen.db") {
		t.Errorf("response leaked upstream error text: %q", resp.Error)
	}
}

func TestNotificationsPost_PayloadOnlyTitleAndBody(t *testing.T) {
	inserter := &fakeNotificationsInserter{}
	body := validNotificationBody()
	body["payload"] = map[string]any{
		"title": "explicit title",
		"body":  "explicit body",
	}
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: inserter}, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(inserter.calls) != 1 {
		t.Fatalf("inserter calls=%d", len(inserter.calls))
	}
	if inserter.calls[0].Title != "explicit title" {
		t.Errorf("title = %q", inserter.calls[0].Title)
	}
	if inserter.calls[0].Body != "explicit body" {
		t.Errorf("body = %q", inserter.calls[0].Body)
	}
}

func TestNotificationsPost_PayloadTitleOnly(t *testing.T) {
	inserter := &fakeNotificationsInserter{}
	body := validNotificationBody()
	body["payload"] = map[string]any{
		"title": "title only",
	}
	w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: inserter}, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if inserter.calls[0].Title != "title only" {
		t.Errorf("title = %q", inserter.calls[0].Title)
	}
	if inserter.calls[0].Body != "" {
		t.Errorf("body = %q, want empty (no residual)", inserter.calls[0].Body)
	}
}

func TestNotificationsPost_NilPayloadAccepted(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{
			"omitted",
			map[string]any{
				"severity": "info",
				"source":   "bypass-sidecar",
				"ts":       time.Now().UTC().Format(time.RFC3339),
			},
		},
		{
			"null",
			map[string]any{
				"severity": "info",
				"source":   "bypass-sidecar",
				"ts":       time.Now().UTC().Format(time.RFC3339),
				"payload":  nil,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inserter := &fakeNotificationsInserter{}
			w := dispatchPost(t, &fakeNotificationsPostCtx{inserter: inserter}, tc.body)
			if w.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
			if len(inserter.calls) != 1 {
				t.Fatalf("inserter calls=%d", len(inserter.calls))
			}
			if inserter.calls[0].Title == "" {
				t.Error("title is empty; expected synthesized title")
			}
		})
	}
}
