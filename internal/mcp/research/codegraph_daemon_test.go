package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func makeCodegraphFakeServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	_ = captured
	return srv, captured
}

func makeCaronteClient(t *testing.T, srv *httptest.Server) *client.Client {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c, err := client.New(client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "research",
	})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c
}

func TestCaronteCodeGraphHappyPath(t *testing.T) {
	respBody := `{"hits":[
		{"symbol":"pkg/x.Foo","file":"pkg/x/foo.go","line":10,"kind":"function","confidence":80},
		{"symbol":"pkg/x.Bar","file":"pkg/x/bar.go","line":22,"kind":"method","confidence":50}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/mcpgateway/codegraph" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	adapter := NewCaronteCodeGraph(c)

	result, err := adapter.CodeGraph(context.Background(), "Foo query", "proj-alpha")
	if err != nil {
		t.Fatalf("CodeGraph returned error: %v", err)
	}
	if result.ProjectID != "proj-alpha" {
		t.Errorf("ProjectID = %q; want %q", result.ProjectID, "proj-alpha")
	}
	if len(result.Hits) != 2 {
		t.Fatalf("len(Hits) = %d; want 2", len(result.Hits))
	}

	h0 := result.Hits[0]
	if h0.Node != "pkg/x.Foo" {
		t.Errorf("Hits[0].Node = %q; want %q", h0.Node, "pkg/x.Foo")
	}
	if h0.Score != 0.80 {
		t.Errorf("Hits[0].Score = %f; want 0.80", h0.Score)
	}
	if h0.URL != "caronte://proj-alpha/pkg/x.Foo" {
		t.Errorf("Hits[0].URL = %q; want caronte://proj-alpha/pkg/x.Foo", h0.URL)
	}

	h1 := result.Hits[1]
	if h1.Node != "pkg/x.Bar" {
		t.Errorf("Hits[1].Node = %q; want %q", h1.Node, "pkg/x.Bar")
	}
	if h1.Score != 0.50 {
		t.Errorf("Hits[1].Score = %f; want 0.50", h1.Score)
	}
}

// TestCaronteCodeGraphURLSchemeIsCaronte is the sister-test for the cite
// bypass. The cite verifier's localSchemes default is {"file","caronte"};
// any URL emitted by CaronteCodeGraph MUST use the "caronte" scheme so
// HEAD probing is skipped. Revert CodeGraph's caronteURL to use "file://"
// or "https://" and this test fails — the load-bearing contract is preserved.
func TestCaronteCodeGraphURLSchemeIsCaronte(t *testing.T) {
	respBody := `{"hits":[{"symbol":"pkg/a.Fn","confidence":60}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	result, err := NewCaronteCodeGraph(c).CodeGraph(context.Background(), "q", "my-project")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(result.Hits))
	}
	url := result.Hits[0].URL
	if !strings.HasPrefix(url, "caronte://") {
		t.Errorf("hit URL %q does not use caronte:// scheme — cite verifier HEAD bypass BROKEN", url)
	}
}

func TestCaronteCodeGraphDaemonNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("engine not ready"))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	_, err := NewCaronteCodeGraph(c).CodeGraph(context.Background(), "q", "proj")
	if err == nil {
		t.Fatal("expected error on 503 response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention 503, got: %v", err)
	}
}

func TestCaronteCodeGraphEmptyHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	result, err := NewCaronteCodeGraph(c).CodeGraph(context.Background(), "q", "proj")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(result.Hits))
	}
	if result.ProjectID != "proj" {
		t.Errorf("ProjectID = %q; want %q", result.ProjectID, "proj")
	}
}

func TestCaronteCodeGraphRequestFields(t *testing.T) {
	var gotReq codegraphRESTRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	_, err := NewCaronteCodeGraph(c).CodeGraph(context.Background(), "find Foo usages", "zen-swarm")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if gotReq.Query != "find Foo usages" {
		t.Errorf("request Query = %q; want %q", gotReq.Query, "find Foo usages")
	}
	if gotReq.ProjectAlias != "zen-swarm" {
		t.Errorf("request ProjectAlias = %q; want %q", gotReq.ProjectAlias, "zen-swarm")
	}
}

func TestCaronteCodeGraphCloseNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c := makeCaronteClient(t, srv)
	adapter := NewCaronteCodeGraph(c)
	if err := adapter.Close(); err != nil {
		t.Errorf("Close() = %v; want nil", err)
	}
}

func TestConfidenceToScore(t *testing.T) {
	cases := []struct {
		in   int
		want float64
	}{
		{0, 0.0},
		{1, 0.01},
		{50, 0.50},
		{80, 0.80},
		{100, 1.0},
		{-5, 0.0},
		{110, 1.0},
		{25, 0.25},
		{10, 0.10},
	}
	for _, tc := range cases {
		got := confidenceToScore(tc.in)
		if got != tc.want {
			t.Errorf("confidenceToScore(%d) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestCaronteURL(t *testing.T) {
	cases := []struct {
		project, symbol, want string
	}{
		{"proj-alpha", "pkg/x.Foo", "caronte://proj-alpha/pkg/x.Foo"},
		{"zen-swarm", "internal/daemon.Start", "caronte://zen-swarm/internal/daemon.Start"},
		{"", "x.Y", "caronte:///x.Y"},
	}
	for _, tc := range cases {
		got := caronteURL(tc.project, tc.symbol)
		if got != tc.want {
			t.Errorf("caronteURL(%q, %q) = %q; want %q", tc.project, tc.symbol, got, tc.want)
		}
	}
}

func TestCaronteCodeGraphCompileTimeAssert(t *testing.T) {

	t.Log("compile-time assertion enforced by var _ GitnexusClient = (*CaronteCodeGraph)(nil) in codegraph_daemon.go")
}
