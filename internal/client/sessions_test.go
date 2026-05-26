package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAttachSessionSendsBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tmux_cmd":"tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch"}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	tmuxCmd, err := c.AttachSession(context.Background(), "internal-platform-x", "orch")
	if err != nil {
		t.Fatalf("AttachSession: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/internal-platform-x/attach" {
		t.Errorf("path=%q want /v1/sessions/internal-platform-x/attach", gotPath)
	}
	if gotBody["window"] != "orch" {
		t.Errorf("body.window=%v want orch", gotBody["window"])
	}
	if !strings.HasPrefix(tmuxCmd, "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch") {
		t.Errorf("tmuxCmd=%q want prefix tmux ... :orch", tmuxCmd)
	}
}

func TestAttachSessionEscapesAliasInURL(t *testing.T) {
	var gotEscaped string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		gotEscaped = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tmux_cmd":"tmux"}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	// alias with a slash would corrupt the URL path; PathEscape MUST
	// emit %2F so the wire form preserves a single path segment.
	_, err := c.AttachSession(context.Background(), "weird/alias", "orch")
	if err != nil {
		t.Fatalf("AttachSession: %v", err)
	}
	if !strings.Contains(gotEscaped, "weird%2Falias") {
		t.Errorf("escaped path=%q missing weird%%2Falias", gotEscaped)
	}
}

func TestAttachSession404IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.AttachSession(context.Background(), "missing", "orch")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected HTTPError chain; got %T", err)
	}
	if he.Status != http.StatusNotFound {
		t.Errorf("status=%d want 404", he.Status)
	}
	if !IsHTTPStatus(err, 404) {
		t.Error("IsHTTPStatus(err, 404) = false")
	}
}

func TestAttachSession503IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("phase I pending"))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.AttachSession(context.Background(), "x", "orch")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, 503) {
		t.Errorf("IsHTTPStatus(err, 503) = false; got %v", err)
	}
}

func TestListSessionsDecodesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%q want GET", r.Method)
		}
		if r.URL.Path != "/v1/sessions" {
			t.Errorf("path=%q want /v1/sessions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sessions": [
				{"alias":"internal-platform-x","sha8":"deadbeef","status":"active","last_attach":"2026-05-01T14:00:00Z","pane_count":5},
				{"alias":"nexus","sha8":"12345678","status":"idle","last_attach":"2026-04-30T09:00:00Z","pane_count":3}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d want 2", len(rows))
	}
	if rows[0].Alias != "internal-platform-x" || rows[0].Sha8 != "deadbeef" {
		t.Errorf("rows[0] = %+v", rows[0])
	}
	if rows[0].Status != "active" || rows[0].PaneCount != 5 {
		t.Errorf("rows[0] = %+v", rows[0])
	}
}

func TestListSessionsEmptyArrayReturnsNonNilSlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil; expected non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("len(rows)=%d want 0", len(rows))
	}
}

func TestListSessionsNullArrayReturnsNonNilSlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sessions":null}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	rows, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil; expected non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("len(rows)=%d want 0", len(rows))
	}
}

func TestListSessions503IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, 503) {
		t.Errorf("IsHTTPStatus(err, 503) = false; got %v", err)
	}
}

func TestListSessions500IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, 500) {
		t.Errorf("IsHTTPStatus(err, 500) = false; got %v", err)
	}
}

func TestRepaintLayoutHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"windows_repainted":["orch","leads","workers","hra","logs"],"scratch_preserved":true,"duration_ms":42}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if err := c.RepaintLayout(context.Background(), "internal-platform-x"); err != nil {
		t.Fatalf("RepaintLayout: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/internal-platform-x/layout/repaint" {
		t.Errorf("path=%q want /v1/sessions/internal-platform-x/layout/repaint", gotPath)
	}
}

func TestRepaintLayout404IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.RepaintLayout(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, 404) {
		t.Errorf("IsHTTPStatus(err, 404) = false; got %v", err)
	}
}

func TestRepaintLayout503IsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	err := c.RepaintLayout(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, 503) {
		t.Errorf("IsHTTPStatus(err, 503) = false; got %v", err)
	}
}

func TestSessionRowJSONRoundTrip(t *testing.T) {
	in := SessionRow{Alias: "x", Sha8: "abcdef01", Status: "active", PaneCount: 5}
	buf, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out SessionRow
	if err := json.Unmarshal(buf, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("roundtrip mismatch: %+v != %+v", out, in)
	}
	for _, want := range []string{`"alias"`, `"sha8"`, `"status"`, `"last_attach"`, `"pane_count"`} {
		if !strings.Contains(string(buf), want) {
			t.Errorf("encoded missing %s: %s", want, string(buf))
		}
	}
}
