package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthOverUDS(t *testing.T) {
	dir := t.TempDir()
	udsPath := filepath.Join(dir, "client.sock")
	udsLn, err := net.Listen("unix", udsPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer udsLn.Close()

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","version":"test","uptime_seconds":42}`))
		})
		_ = http.Serve(udsLn, mux)
	}()

	c := New(udsPath)
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got.Status != "ok" || got.UptimeSeconds != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestHealthWhenDaemonDown(t *testing.T) {
	c := New("/nonexistent/path.sock")
	_, err := c.Health(context.Background())
	if err == nil {
		t.Error("expected error when socket missing")
	}
}

type captureLogger struct {
	lines []string
}

func (c *captureLogger) Logf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func TestSetDebugLogger_LogsRoundTrips(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/echo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	cap := &captureLogger{}
	c.SetDebugLogger(cap)

	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if err := c.postJSON(context.Background(), "/v1/echo", map[string]string{"k": "v"}, nil); err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(cap.lines) != 2 {
		t.Fatalf("want 2 log lines, got %d: %v", len(cap.lines), cap.lines)
	}
	gotJoined := strings.Join(cap.lines, "\n")
	for _, want := range []string{"GET /v1/health", "POST /v1/echo", "200"} {
		if !strings.Contains(gotJoined, want) {
			t.Errorf("missing %q in log:\n%s", want, gotJoined)
		}
	}
}

func TestSetDebugLogger_LogsErrors(t *testing.T) {
	c := NewWithBaseURL("http://127.0.0.1:1")
	cap := &captureLogger{}
	c.SetDebugLogger(cap)
	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("expected dispatch error")
	}
	if len(cap.lines) != 1 {
		t.Fatalf("want 1 log line, got %d: %v", len(cap.lines), cap.lines)
	}
	if !strings.Contains(cap.lines[0], "ERR=") {
		t.Errorf("log line should mark error: %q", cap.lines[0])
	}
}

func TestSetDebugLogger_NilIsSilent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

}

func TestHTTPError_TypedDiscrimination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/notfound", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"missing"}`))
	})
	mux.HandleFunc("/v1/conflict", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`already-exists`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.getJSON(context.Background(), "/v1/notfound", nil)
	if err == nil {
		t.Fatal("expected 404 error")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("error chain should contain *HTTPError; got %T %v", err, err)
	}
	if he.Status != 404 || he.Method != http.MethodGet || he.Path != "/v1/notfound" {
		t.Errorf("HTTPError: %+v", he)
	}
	if !strings.Contains(string(he.RawBody), "missing") {
		t.Errorf("RawBody: %q", he.RawBody)
	}

	if !IsHTTPStatus(err, 404) {
		t.Error("IsHTTPStatus(404) should be true")
	}
	if IsHTTPStatus(err, 500) {
		t.Error("IsHTTPStatus(500) should be false")
	}

	if !strings.Contains(err.Error(), ": 404 ") {
		t.Errorf("legacy text format broken: %v", err)
	}

	err = c.postJSON(context.Background(), "/v1/conflict", map[string]any{}, nil)
	if !IsHTTPStatus(err, 409) {
		t.Errorf("POST 409 → IsHTTPStatus(409) should be true: %v", err)
	}
}

func TestIsHTTPStatus_NilAndNonHTTPError(t *testing.T) {
	if IsHTTPStatus(nil, 404) {
		t.Error("nil err: should be false")
	}
	if IsHTTPStatus(errors.New("plain"), 404) {
		t.Error("non-HTTPError: should be false")
	}
}

func TestHTTPError_ErrorMessageMatchesPreFix(t *testing.T) {
	he := &HTTPError{
		Method:  http.MethodGet,
		Path:    "/v1/x",
		Status:  500,
		RawBody: []byte("server crashed"),
	}
	got := he.Error()
	want := "GET /v1/x: 500 server crashed"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPostJSONHSendsHeaders asserts the v0.20.0 header-aware
// helper propagates per-call headers to the request. invariant:
// every caronte CLI client method MUST be able to send
// X-Zen-Project-ID via this seam.
func TestPostJSONHSendsHeaders(t *testing.T) {
	t.Parallel()
	var gotHeader, gotXOther string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		gotXOther = r.Header.Get("X-Other-Marker")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	headers := map[string]string{
		"X-Zen-Project-ID": "zen-foo-aa11",
		"X-Other-Marker":   "marker-value",
	}
	if err := c.postJSONH(context.Background(), "/v1/test", headers, struct{}{}, nil); err != nil {
		t.Fatalf("postJSONH: %v", err)
	}
	if gotHeader != "zen-foo-aa11" {
		t.Errorf("X-Zen-Project-ID propagation: got %q, want %q", gotHeader, "zen-foo-aa11")
	}
	if gotXOther != "marker-value" {
		t.Errorf("X-Other-Marker propagation: got %q, want %q", gotXOther, "marker-value")
	}
}

func TestPostJSONHSurfaces404AsHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.postJSONH(context.Background(), "/v1/missing", nil, struct{}{}, nil)
	if err == nil {
		t.Fatal("expected error for 404; got nil")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err is not *HTTPError: %v", err)
	}
	if he.Status != http.StatusNotFound {
		t.Errorf("Status = %d; want 404", he.Status)
	}
}

func TestPostJSONHNilHeadersIsValid(t *testing.T) {
	t.Parallel()
	var saw bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if err := c.postJSONH(context.Background(), "/v1/test", nil, struct{}{}, nil); err != nil {
		t.Fatalf("postJSONH with nil headers: %v", err)
	}
	if !saw {
		t.Error("server handler not invoked")
	}
}

func TestPostJSONStillWorksWithoutHeaders(t *testing.T) {
	t.Parallel()
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if err := c.postJSON(context.Background(), "/v1/test", map[string]string{"k": "v"}, nil); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", contentType)
	}
}

func TestGetJSONHSendsHeaders(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	var out struct{}
	if err := c.getJSONH(context.Background(), "/v1/test", map[string]string{"X-Zen-Project-ID": "alpha"}, &out); err != nil {
		t.Fatalf("getJSONH: %v", err)
	}
	if gotHeader != "alpha" {
		t.Errorf("X-Zen-Project-ID = %q; want alpha", gotHeader)
	}
}

func TestGetJSONHSurfaces404AsHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	var out struct{}
	err := c.getJSONH(context.Background(), "/v1/missing", nil, &out)
	if err == nil {
		t.Fatal("expected error for 404; got nil")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err is not *HTTPError: %v", err)
	}
	if he.Status != http.StatusNotFound {
		t.Errorf("Status = %d; want 404", he.Status)
	}
}
