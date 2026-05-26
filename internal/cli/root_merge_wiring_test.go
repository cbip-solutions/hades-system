package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestRoot_MergeNamespaceRegistered(t *testing.T) {
	root := NewRootCmd()
	c, _, err := root.Find([]string{"merge"})
	if err != nil || c == nil {
		t.Fatalf("namespace %q not registered: err=%v", "merge", err)
	}
	if c.Name() != "merge" {
		t.Errorf("registered command name = %q, want %q", c.Name(), "merge")
	}
}

func TestRoot_MergeSubcommandsRegistered(t *testing.T) {
	root := NewRootCmd()
	merge, _, err := root.Find([]string{"merge"})
	if err != nil || merge == nil {
		t.Fatalf("zen merge not registered: %v", err)
	}
	want := map[string]bool{
		"inspect":       false,
		"replay":        false,
		"score-explain": false,
		"baseline":      false,
		"cache":         false,
		"config":        false,
		"anomaly":       false,
	}
	for _, sub := range merge.Commands() {

		name := strings.SplitN(sub.Use, " ", 2)[0]
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand: zen merge %q", name)
		}
	}
}

func TestRoot_MergeHelpResponds(t *testing.T) {
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"merge", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("zen merge --help: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"inspect", "replay", "score-explain", "baseline", "cache", "config", "anomaly"} {
		if !strings.Contains(out, want) {
			t.Errorf("zen merge --help output missing %q:\n%s", want, out)
		}
	}
}

func TestRoot_MergeCacheStatus_RoutesToDaemonHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/merge/cache/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"size":42,"hit_rate_pct":75.5,"last_rebuilt":"2026-05-05T00:00:00Z"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SilenceUsage = true
	root.SilenceErrors = true
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"merge", "cache", "status"})

	if err := root.Execute(); err != nil {
		t.Fatalf("zen merge cache status: %v\noutput:\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{"size: 42", "hit_rate: 75.50%", "2026-05-05"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
