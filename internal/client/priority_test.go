package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPriorityBoostSendsBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	expires := time.Date(2026, 5, 1, 16, 0, 0, 0, time.UTC)
	if err := c.PriorityBoost(context.Background(), "internal-platform-x", 3.0, expires, "urgent"); err != nil {
		t.Fatalf("PriorityBoost: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/priority/boost" {
		t.Errorf("path=%q want /v1/priority/boost", gotPath)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body.alias=%v want internal-platform-x", gotBody["alias"])
	}
	if m, _ := gotBody["multiplier"].(float64); m != 3.0 {
		t.Errorf("body.multiplier=%v want 3.0", gotBody["multiplier"])
	}
	if gotBody["reason"] != "urgent" {
		t.Errorf("body.reason=%v want urgent", gotBody["reason"])
	}
	if s, _ := gotBody["expires_at"].(string); s == "" {
		t.Errorf("body.expires_at missing/empty")
	}
}

func TestPriorityResetSendsBody(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if err := c.PriorityReset(context.Background(), "internal-platform-x"); err != nil {
		t.Fatalf("PriorityReset: %v", err)
	}
	if gotPath != "/v1/priority/reset" {
		t.Errorf("path=%q want /v1/priority/reset", gotPath)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body.alias=%v want internal-platform-x", gotBody["alias"])
	}
}

func TestPriorityListDecodesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%q want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"overrides": [
				{
					"alias": "internal-platform-x",
					"multiplier": 3.0,
					"expires_at": "2026-05-01T16:00:00Z",
					"reason": "urgent",
					"created_at": "2026-05-01T12:00:00Z"
				}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.PriorityList(context.Background())
	if err != nil {
		t.Fatalf("PriorityList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d want 1", len(rows))
	}
	r := rows[0]
	if r.Alias != "internal-platform-x" {
		t.Errorf("alias=%q want internal-platform-x", r.Alias)
	}
	if r.Multiplier != 3.0 {
		t.Errorf("multiplier=%v want 3.0", r.Multiplier)
	}
	if r.Reason != "urgent" {
		t.Errorf("reason=%q want urgent", r.Reason)
	}
	if r.ExpiresAt.IsZero() {
		t.Error("expires_at zero")
	}
	if r.CreatedAt.IsZero() {
		t.Error("created_at zero")
	}
}

// TestPriorityListNilFieldNormalisedToEmpty — when the daemon body
// omits the "overrides" field entirely (or sends null), the client MUST
// normalise to an empty slice so callers can range over it without a
// nil check. Defence against any future daemon-side change that might
// elide the field on empty results.
func TestPriorityListNilFieldNormalisedToEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.PriorityList(context.Background())
	if err != nil {
		t.Fatalf("PriorityList: %v", err)
	}
	if rows == nil {
		t.Error("rows=nil; client must normalise to empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("len(rows)=%d want 0", len(rows))
	}
}

func TestPriorityListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"overrides":[]}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.PriorityList(context.Background())
	if err != nil {
		t.Fatalf("PriorityList: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows)=%d want 0", len(rows))
	}
}

func TestPriorityBoost404PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.PriorityBoost(context.Background(), "missing", 3.0, time.Now().Add(time.Hour), "u")
	if err == nil {
		t.Fatal("expected error on 404; got nil")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err is not HTTPError: %v", err)
	}
	if he.Status != http.StatusNotFound {
		t.Errorf("status=%d want 404", he.Status)
	}
}

func TestPriorityReset404PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.PriorityReset(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error on 404; got nil")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err not 404 HTTPError: %v", err)
	}
}

func TestPriorityList500PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.PriorityList(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("err not 500 HTTPError: %v", err)
	}
}
