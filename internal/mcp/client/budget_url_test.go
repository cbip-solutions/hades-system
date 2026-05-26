package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeDaemonError(t *testing.T, path string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			http.Error(w, "fake daemon error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

func fakeDaemonBadJSON(t *testing.T, path string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not valid json`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

func TestBudget_HappyPathsStillWork(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	if _, err := bc.CapStatus(context.Background(), "stage", "design"); err != nil {
		t.Errorf("CapStatus: %v", err)
	}
	if _, err := bc.Axes(context.Background(), "cost-x"); err != nil {
		t.Errorf("Axes: %v", err)
	}
	if _, err := bc.AnomalyCheck(context.Background(), "project", "1h"); err != nil {
		t.Errorf("AnomalyCheck: %v", err)
	}
	if _, err := bc.Events(context.Background(), time.Time{}); err != nil {
		t.Errorf("Events: %v", err)
	}
}

func TestBudget_URLEncodingSafe(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	cases := []struct {
		name  string
		axis  string
		value string
	}{
		{"spaces", "stage with space", "design with space"},
		{"plus", "stage+plus", "value+plus"},
		{"unicode", "axis", "valor-ñoño"},
		{"slash", "axis", "a/b/c"},
		{"ampersand", "axis", "a&b"},
		{"equals", "axis", "a=b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := bc.CapStatus(context.Background(), tc.axis, tc.value)
			if err != nil {
				t.Errorf("CapStatus(%q,%q): %v", tc.axis, tc.value, err)
				return
			}

			if resp.Axis != tc.axis {
				t.Errorf("Axis round-trip: got %q, want %q", resp.Axis, tc.axis)
			}
			if resp.Value != tc.value {
				t.Errorf("Value round-trip: got %q, want %q", resp.Value, tc.value)
			}
		})
	}
}

func TestBudget_ErrorMessagesIncludeMethodName(t *testing.T) {

	srv := fakeBudgetServer(t)
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	resp, err := bc.CapStatus(context.Background(), "x", "y")
	if err != nil && !strings.HasPrefix(err.Error(), "budget.CapStatus: ") {
		t.Errorf("error prefix mismatch: %v", err)
	}
	if err == nil && resp == nil {
		t.Error("happy path: expected non-nil response")
	}
}

func TestBudget_RollupPauseResumeHappyPaths(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	if _, err := bc.Rollup(context.Background(), "stage", "design", time.Time{}); err != nil {
		t.Errorf("Rollup: %v", err)
	}
	if _, err := bc.Pause(context.Background(), "stage", "test reason"); err != nil {
		t.Errorf("Pause: %v", err)
	}
	if _, err := bc.Resume(context.Background(), "stage"); err != nil {
		t.Errorf("Resume: %v", err)
	}
}

func TestBudget_RollupDaemonError(t *testing.T) {
	srv := fakeDaemonError(t, "/v1/budget/rollup")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Rollup(context.Background(), "stage", "design", time.Time{})
	if err == nil {
		t.Fatal("expected error from Rollup on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "budget.Rollup:") {
		t.Errorf("error prefix mismatch: %v", err)
	}
}

func TestBudget_PauseDaemonError(t *testing.T) {
	srv := fakeDaemonError(t, "/v1/budget/pause")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Pause(context.Background(), "stage", "reason")
	if err == nil {
		t.Fatal("expected error from Pause on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "budget.Pause:") {
		t.Errorf("error prefix mismatch: %v", err)
	}
}

func TestBudget_ResumeDaemonError(t *testing.T) {
	srv := fakeDaemonError(t, "/v1/budget/resume")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Resume(context.Background(), "stage")
	if err == nil {
		t.Fatal("expected error from Resume on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "budget.Resume:") {
		t.Errorf("error prefix mismatch: %v", err)
	}
}

func TestBudget_RollupInvalidJSON(t *testing.T) {
	srv := fakeDaemonBadJSON(t, "/v1/budget/rollup")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Rollup(context.Background(), "stage", "design", time.Time{})
	if err == nil {
		t.Fatal("expected JSON decode error from Rollup, got nil")
	}
}

func TestBudget_PauseInvalidJSON(t *testing.T) {
	srv := fakeDaemonBadJSON(t, "/v1/budget/pause")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Pause(context.Background(), "stage", "reason")
	if err == nil {
		t.Fatal("expected JSON decode error from Pause, got nil")
	}
}

func TestBudget_ResumeInvalidJSON(t *testing.T) {
	srv := fakeDaemonBadJSON(t, "/v1/budget/resume")
	defer srv.Close()
	bc := newBudgetClient(t, srv)

	_, err := bc.Resume(context.Background(), "stage")
	if err == nil {
		t.Fatal("expected JSON decode error from Resume, got nil")
	}
}
