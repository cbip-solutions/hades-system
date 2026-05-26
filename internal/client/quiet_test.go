package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuietGetHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/quiet" {
			t.Errorf("path = %s, want /v1/quiet", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(QuietGetResponse{
			Default: QuietHoursWire{
				StartSec:        int64((21 * time.Hour).Seconds()),
				EndSec:          int64((9 * time.Hour).Seconds()),
				WeekendExtended: true,
				UrgentBypass:    true,
			},
			PerProject: map[string]QuietHoursWire{},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.QuietGet(context.Background())
	if err != nil {
		t.Fatalf("QuietGet: %v", err)
	}
	if resp.Default.StartSec != int64((21 * time.Hour).Seconds()) {
		t.Errorf("StartSec = %d, want %d", resp.Default.StartSec, int64((21 * time.Hour).Seconds()))
	}
	if !resp.Default.UrgentBypass {
		t.Error("UrgentBypass = false, want true")
	}
}

func TestQuietGetNilPerProjectTreatedAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default":{"start_sec":75600,"end_sec":32400,"weekend_extended":false,"urgent_bypass":true},"per_project":null}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.QuietGet(context.Background())
	if err != nil {
		t.Fatalf("QuietGet: %v", err)
	}
	if resp.PerProject == nil {
		t.Error("PerProject is nil; client should return non-nil empty map")
	}
}

func TestQuietGet503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quiet store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.QuietGet(context.Background())
	if err == nil {
		t.Fatal("expected 503 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestQuietGetActivePauseDecoded(t *testing.T) {
	until := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(QuietGetResponse{
			Default: QuietHoursWire{
				StartSec:     int64((21 * time.Hour).Seconds()),
				EndSec:       int64((9 * time.Hour).Seconds()),
				UrgentBypass: true,
			},
			PerProject:       map[string]QuietHoursWire{},
			UrgentPauseUntil: &until,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.QuietGet(context.Background())
	if err != nil {
		t.Fatalf("QuietGet: %v", err)
	}
	if resp.UrgentPauseUntil == nil {
		t.Fatal("UrgentPauseUntil is nil; want non-nil")
	}
	if !resp.UrgentPauseUntil.Equal(until) {
		t.Errorf("UrgentPauseUntil = %v, want %v", *resp.UrgentPauseUntil, until)
	}
}

func TestQuietGetWithPerProjectOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(QuietGetResponse{
			Default: QuietHoursWire{
				StartSec:     int64((21 * time.Hour).Seconds()),
				EndSec:       int64((9 * time.Hour).Seconds()),
				UrgentBypass: true,
			},
			PerProject: map[string]QuietHoursWire{
				"project-a": {
					StartSec:     int64((22 * time.Hour).Seconds()),
					EndSec:       int64((6 * time.Hour).Seconds()),
					UrgentBypass: true,
				},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.QuietGet(context.Background())
	if err != nil {
		t.Fatalf("QuietGet: %v", err)
	}
	override, ok := resp.PerProject["project-a"]
	if !ok {
		t.Fatal("project-a override missing")
	}
	if override.StartSec != int64((22 * time.Hour).Seconds()) {
		t.Errorf("override Start = %d, want %d", override.StartSec, int64((22 * time.Hour).Seconds()))
	}
}

func TestQuietUrgentPauseHappyPath(t *testing.T) {
	until := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/quiet/urgent-pause" {
			t.Errorf("path = %s, want /v1/quiet/urgent-pause", r.URL.Path)
		}
		var req QuietPauseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !req.Until.Equal(until) {
			t.Errorf("Until = %v, want %v", req.Until, until)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "until": until.Format(time.RFC3339)})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.QuietUrgentPause(context.Background(), until); err != nil {
		t.Fatalf("QuietUrgentPause: %v", err)
	}
}

func TestQuietUrgentPauseRejectsZeroUntil(t *testing.T) {
	c := NewWithBaseURL("http://invalid")
	err := c.QuietUrgentPause(context.Background(), time.Time{})
	if err == nil {
		t.Fatal("expected error on zero until")
	}
}

func TestQuietUrgentPause422Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "until must be in the future", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.QuietUrgentPause(context.Background(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected 422 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		t.Errorf("err = %v, want HTTPError 422", err)
	}
}

func TestQuietUrgentPause503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quiet store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.QuietUrgentPause(context.Background(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestQuietCancelHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/quiet/cancel" {
			t.Errorf("path = %s, want /v1/quiet/cancel", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.QuietCancel(context.Background()); err != nil {
		t.Fatalf("QuietCancel: %v", err)
	}
}

func TestQuietCancel503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quiet store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.QuietCancel(context.Background())
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}
