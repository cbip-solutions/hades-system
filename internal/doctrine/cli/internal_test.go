package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestExtractSchemaVersion_HappyAndEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", `schema_version = "1.0"`, "1.0"},
		{"with-spaces", `   schema_version = "2.5"`, "2.5"},
		{"missing-key", `name = "x"`, ""},
		{"missing-quote", `schema_version = 1.0`, ""},
		{"unterminated-quote", `schema_version = "1.0`, ""},
		{"second-line-wins-not", "name = \"x\"\nschema_version = \"1.0\"", "1.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSchemaVersion([]byte(tc.in))
			if got != tc.want {
				t.Errorf("extractSchemaVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSemverMajor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.0", "1"},
		{"2.5.7", "2"},
		{"1", "1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := semverMajor(tc.in); got != tc.want {
			t.Errorf("semverMajor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "2.0", -1},
		{"2.0", "1.0", 1},
		{"1.0", "1.0.1", -1},
		{"1.10", "1.9", 1},
		{"1.0-beta", "1.0", 1},
		{"abc", "xyz", -1},
		{"abc", "abc", 0},
		{"abc", "abd", -1},
	}
	for _, tc := range cases {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAtoiSafe(t *testing.T) {
	if n, err := atoiSafe("42"); err != nil || n != 42 {
		t.Errorf("atoiSafe(\"42\") = (%d,%v), want (42,nil)", n, err)
	}
	if _, err := atoiSafe(""); err == nil {
		t.Error("atoiSafe(\"\") should error")
	}
	if _, err := atoiSafe("a1"); err == nil {
		t.Error("atoiSafe(\"a1\") should error")
	}
}

func TestOr(t *testing.T) {
	if got := or("", "fallback"); got != "fallback" {
		t.Errorf("or empty: got %q", got)
	}
	if got := or("value", "fallback"); got != "value" {
		t.Errorf("or non-empty: got %q", got)
	}
	if got := or("   ", "fallback"); got != "fallback" {
		t.Errorf("or whitespace: got %q (want fallback)", got)
	}
}

func TestSummarizePayload(t *testing.T) {
	if got := summarizePayload(nil); got != "" {
		t.Errorf("nil payload: got %q", got)
	}
	if got := summarizePayload(map[string]any{}); got != "" {
		t.Errorf("empty payload: got %q", got)
	}
	got := summarizePayload(map[string]any{
		"name":   "max-scope",
		"source": "embed",
		"unused": "ignored",
	})
	if !strings.Contains(got, "name=max-scope") || !strings.Contains(got, "source=embed") {
		t.Errorf("populated payload: got %q", got)
	}
	if strings.Contains(got, "unused=ignored") {
		t.Errorf("unknown key leaked: %q", got)
	}
}

func TestDefaultUserDoctrinePath(t *testing.T) {

	t.Setenv("XDG_CONFIG_HOME", "/custom/cfg")
	if got := defaultUserDoctrinePath("foo"); got != "/custom/cfg/zen-swarm/doctrines/foo.toml" {
		t.Errorf("XDG branch: got %q", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/fake/home")
	got := defaultUserDoctrinePath("bar")

	if !strings.HasSuffix(got, ".config/zen-swarm/doctrines/bar.toml") {
		t.Errorf("default branch: got %q", got)
	}
}

func TestClient_WithUDS_ConstructsTransport(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "doctrine.sock")
	listener, err := newUDSListener(sock)
	if err != nil {
		t.Skipf("UDS not available on this platform: %v", err)
	}
	defer listener.Close()
	srv := &httptest.Server{
		Listener: listener,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"name":"max-scope","schema_version":"1.0","doctrine_version":"1.0.0","source":"embed"}`)
		})},
	}
	srv.Start()
	defer srv.Close()

	c := NewClient("http://unix").withUDS(sock)
	resp, err := c.Active(context.Background())
	if err != nil {
		t.Fatalf("Active over UDS: %v", err)
	}
	if resp.Name != "max-scope" {
		t.Errorf("response over UDS: %+v", resp)
	}
}

func TestClientFromCmd_ProductionPath(t *testing.T) {
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = nil
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := &cobra.Command{Use: "zen"}
	root.PersistentFlags().String("uds", "/tmp/zen.sock", "Daemon UDS path")
	leaf := NewRoot()
	root.AddCommand(leaf)

	c := clientFromCmd(leaf)
	if c == nil {
		t.Fatal("clientFromCmd returned nil")
	}

	if c.baseURL != "http://unix" {
		t.Errorf("expected baseURL http://unix, got %q", c.baseURL)
	}
}

func TestClient_Do_BodyEncodeError(t *testing.T) {
	c := NewClient("http://unused")
	err := c.do(context.Background(), http.MethodPost, "/v1/test", nil, make(chan int), nil)
	if err == nil {
		t.Fatal("expected encode error")
	}
	if !strings.Contains(err.Error(), "codificación JSON") {
		t.Errorf("error should mention encode failure: %v", err)
	}
}

func TestClient_Do_NewRequestError(t *testing.T) {
	c := NewClient("://broken")
	err := c.do(context.Background(), "BAD METHOD", "/v1/test", nil, nil, nil)
	if err == nil {
		t.Fatal("expected request build error")
	}
	if !strings.Contains(err.Error(), "construcción") && !strings.Contains(err.Error(), "petición") {
		t.Errorf("error should mention construction failure: %v", err)
	}
}

func TestClient_Do_HTTPError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Active(ctx)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestClient_Do_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	_, err := c.Active(context.Background())
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decodificación") {
		t.Errorf("error should mention decode failure: %v", err)
	}
}

func TestSmoke_BytesCompareLoadBearing(t *testing.T) {
	a := []byte("xy")
	b := []byte("xy")
	if !bytes.Equal(a, b) {
		t.Fatalf("bytes.Equal sanity")
	}
	_ = os.Args
}
