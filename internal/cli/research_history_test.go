package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func mockResearchP9Server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ResearchHistoryEntry{
				{Query: "audit chain integrity", DispatchedAt: 1762000000, FindingsCount: 5, Source: "cache_hit_exact"},
				{Query: "test coverage patterns", DispatchedAt: 1762000100, FindingsCount: 3, Source: "fresh_dispatch"},
			},
			"count": 2,
		})
	})
	mux.HandleFunc("/v1/research/cache/invalidate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"invalidated": 1})
	})
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ResearchCacheEntryP9{
				{Hash: "sha256abc123", BytesSize: 4096, CreatedAt: 1762000000, TTLUnix: 1762086400, SourceURL: "https://arxiv.org/abs/2503.12345", ContentHash: "def456"},
			},
			"count": 1,
		})
	})
	return httptest.NewServer(mux)
}

func invokeResearchP9Cmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })
	cmd := NewResearchCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestResearchHistory_Render(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	out, stderr, err := invokeResearchP9Cmd(t, []string{"history"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"audit chain integrity", "cache_hit_exact", "fresh_dispatch"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestResearchHistory_TypeFlag(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	_, stderr, err := invokeResearchP9Cmd(t, []string{"history", "--type", "cache_hit"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v\nstderr=%s", err, stderr)
	}
}

func TestResearchCacheInvalidate_HappyPath(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	out, stderr, err := invokeResearchP9Cmd(t, []string{"cache", "invalidate", "audit chain integrity"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(out, "audit chain integrity") {
		t.Errorf("output missing query: %s", out)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("output missing invalidated count: %s", out)
	}
}

func TestResearchCacheInvalidate_RequiresQueryArg(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	_, _, err := invokeResearchP9Cmd(t, []string{"cache", "invalidate"}, srv.URL)
	if err == nil {
		t.Fatal("expected error when query arg missing, got nil")
	}
}

func TestResearchCacheLs_HappyPath(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	out, stderr, err := invokeResearchP9Cmd(t, []string{"cache", "ls"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(out, "arxiv.org") {
		t.Errorf("output missing source URL: %s", out)
	}
}

func TestResearchHistory_InvalidSince(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	_, _, err := invokeResearchP9Cmd(t, []string{"history", "--since", "notaduration"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid --since value, got nil")
	}
}

func TestResearchCacheLs_SourcePrefix(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()
	out, stderr, err := invokeResearchP9Cmd(t, []string{"cache", "ls", "--source", "https://arxiv.org/"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(out, "arxiv.org") {
		t.Errorf("output missing source URL: %s", out)
	}
}
