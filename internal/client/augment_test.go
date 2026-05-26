package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAugmentProbeHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/augment/probe" {
			t.Errorf("path = %s, want /v1/augment/probe", r.URL.Path)
		}
		check := r.URL.Query().Get("check")
		if check != "backend-reachable" {
			t.Errorf("check = %q, want backend-reachable", check)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AugmentProbeResp{Status: "ok", Detail: "backend healthy"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AugmentProbe(context.Background(), "backend-reachable")
	if err != nil {
		t.Fatalf("AugmentProbe: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want ok", resp.Status)
	}
}

func TestAugmentProbeURLEncoding(t *testing.T) {
	t.Parallel()
	var gotCheck string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCheck = r.URL.Query().Get("check")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AugmentProbeResp{Status: "warn"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, _ = c.AugmentProbe(context.Background(), "kg cache-hit")

	if gotCheck != "kg cache-hit" {
		t.Fatalf("check = %q, want 'kg cache-hit'", gotCheck)
	}
}

func TestAugmentSummaryHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/augment/summary" {
			t.Errorf("path = %s, want /v1/augment/summary", r.URL.Path)
		}
		date := r.URL.Query().Get("date")
		if date != "2026-05-01" {
			t.Errorf("date = %q, want 2026-05-01", date)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AugmentSummaryResponse{
			Date:           "2026-05-01",
			TotalCost:      1.23,
			TokensConsumed: 8234,
			TokensCeiling:  10000,
			KGQueriesFired: 47,
			CacheHitRate:   0.72,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AugmentSummary(context.Background(), "2026-05-01")
	if err != nil {
		t.Fatalf("AugmentSummary: %v", err)
	}
	if resp.Date != "2026-05-01" {
		t.Fatalf("Date = %q, want 2026-05-01", resp.Date)
	}
	if resp.TotalCost != 1.23 {
		t.Fatalf("TotalCost = %v, want 1.23", resp.TotalCost)
	}
	if resp.KGQueriesFired != 47 {
		t.Fatalf("KGQueriesFired = %d, want 47", resp.KGQueriesFired)
	}
}

func TestAugmentSummaryNoDate(t *testing.T) {
	t.Parallel()
	var gotDate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDate = r.URL.Query().Get("date")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AugmentSummaryResponse{})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, _ = c.AugmentSummary(context.Background(), "")
	if gotDate != "" {
		t.Fatalf("empty date should produce no ?date query param; got %q", gotDate)
	}
}

// TestAugmentProbeServerError verifies AugmentProbe propagates daemon
// errors (5xx, 4xx) as wrapped errors rather than swallowing them. The
// pre-fix branch was uncovered (coverage 80% — only happy path + URL
// encoding exercised the body); MINOR-2 fix-cycle pins ≥90% by
// asserting the error path returns (nil, non-nil err) when the daemon
// responds 503.
//
// internal/client/augment.go (security-adjacent — doctor consumes
// AugmentProbe to surface backend-reachability health).
func TestAugmentProbeServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"backend offline"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AugmentProbe(context.Background(), "backend-reachable")
	if err == nil {
		t.Fatal("expected non-nil err from AugmentProbe on 503, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got %+v", resp)
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("expected wrapped HTTPError with 503, got: %v", err)
	}
}

func TestAugmentSummaryServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"unhandled"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AugmentSummary(context.Background(), "2026-05-12")
	if err == nil {
		t.Fatal("expected non-nil err from AugmentSummary on 500, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got %+v", resp)
	}
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("expected wrapped HTTPError with 500, got: %v", err)
	}
}
