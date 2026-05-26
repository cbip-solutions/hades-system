package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func invokeResearchCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
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

func mockResearchServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/get", func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		if hash == "miss" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"hit":false}`)
			return
		}
		_ = json.NewEncoder(w).Encode(client.ResearchCacheGetResp{
			Hit: true, ResponseJSON: `{"answer":"42"}`, TTLUnix: 9999999999,
		})
	})
	mux.HandleFunc("/v1/research/cache/set", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheSetResp{Stored: true, TTLUnix: 1234})
	})
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ResearchCacheEntry{
				{Hash: "abc1234567890def", BytesSize: 100, CreatedAt: 1759320000, TTLUnix: 1759920000},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/v1/research/cache/clear", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted": 7})
	})
	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheStats{
			TotalEntries: 50, TotalBytes: 100000, ExpiredCount: 3,
			OldestUnix: 1759000000, NewestUnix: 1759320000,
		})
	})
	mux.HandleFunc("/v1/research/cache/show", func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		if hash == "miss" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(client.ResearchCacheShow{
			Hash: hash, ResponseJSON: `{"answer":"42"}`, BytesSize: 13,
			CreatedAt: 1759000000, TTLUnix: 1759920000,
		})
	})
	return httptest.NewServer(mux)
}

func TestResearchCacheGet_Hit(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "get", "abc"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "Hit:") || !strings.Contains(stdout, "true") || !strings.Contains(stdout, "42") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchCacheGet_Miss(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "get", "miss"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "false") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchCacheSet_RequiresFlags(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "set"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for missing flags")
	}
}

func TestResearchCacheSet_HappyPath(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "set", "--hash=h1", "--body={}", "--ttl=24h"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "stored=true") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchCacheSet_BadTTL(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "set", "--hash=h1", "--body={}", "--ttl=xyz"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for bad ttl")
	}
}

func TestResearchCacheList(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "list"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "abc123456789") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchCacheClear_RequiresOlderThan(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "clear"}, srv.URL)
	if err == nil {
		t.Fatal("expected --older-than error")
	}
}

func TestResearchCacheClear_RequiresYes(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "clear", "--older-than=24h"}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestResearchCacheClear_HappyPath(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "clear", "--older-than=24h", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "deleted 7") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchCacheStats(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "stats"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "TotalEntries") || !strings.Contains(stdout, "50") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchShow_Hit(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"show", "abc"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "Hash:") || !strings.Contains(stdout, "abc") {
		t.Errorf("got %s", stdout)
	}
}

func TestResearchShow_Miss(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"show", "miss"}, srv.URL)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestResearchSources_MaxScopeDoctrine(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope",
			"research": map[string]any{
				"sources": []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"sources"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in max-scope output: %s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "max-scope") {
		t.Errorf("expected active doctrine label 'max-scope': %s", stdout)
	}
}

func TestResearchSources_DefaultDoctrineExcludesCodeGraph(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "default",
			"research": map[string]any{
				"sources": []string{"web_search", "arxiv", "github_search"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"sources"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"web_search", "arxiv", "github_search"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in default-doctrine output: %s", want, stdout)
		}
	}
	for _, mustNot := range []string{"code_graph", "ecosystem_docs"} {
		if strings.Contains(stdout, mustNot) {
			t.Errorf("default doctrine must NOT include %q: %s", mustNot, stdout)
		}
	}
}

func TestResearchSources_DaemonDownFallsBackToDefault(t *testing.T) {
	stdout, _, err := invokeResearchCmd(t, []string{"sources"}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("daemon-down fallback: %v", err)
	}
	for _, want := range []string{"web_search", "arxiv", "github_search"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("daemon-down fallback missing %q: %s", want, stdout)
		}
	}
}

func TestResearchSources_LocalBuiltinFromName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {

		_ = json.NewEncoder(w).Encode(map[string]any{"name": "max-scope"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"sources"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}

	for _, want := range []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("local-builtin lookup missing %q: %s", want, stdout)
		}
	}
}

func TestResearchSources_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope",
			"research": map[string]any{
				"sources": []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"sources", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(arr) != 5 {
		t.Errorf("want 5 sources (max-scope doctrine), got %d", len(arr))
	}
}

func TestResearchCacheGet_JSON(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "get", "abc", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestResearchCacheList_ExclusiveFlags(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "list", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheClear_BadDuration(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "clear", "--older-than=xyz", "--yes"}, srv.URL)
	if err == nil {
		t.Fatal("expected duration parse error")
	}
}

func TestResearchCacheStats_ExclusiveFlags(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"cache", "stats", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheStats_JSON(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"cache", "stats", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestResearchShow_JSON(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"show", "abc", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestResearchSources_ExclusiveFlags(t *testing.T) {
	srv := mockResearchServer(t)
	defer srv.Close()
	_, _, err := invokeResearchCmd(t, []string{"sources", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShortHash_AllBranches(t *testing.T) {
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("short: %q", got)
	}
	if got := shortHash("0123456789abcdef"); got != "0123456789ab" {
		t.Errorf("truncate: %q", got)
	}
}

func TestResearchShow_BadJSONBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/show", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheShow{
			Hash: "abc", ResponseJSON: "not-valid-json{{{",
			BytesSize: 16, CreatedAt: 1759000000, TTLUnix: 1759920000,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeResearchCmd(t, []string{"show", "abc"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "not-valid-json") {
		t.Errorf("raw fallback should include body: %s", stdout)
	}
}

func TestResearchSubcommandsRegistered(t *testing.T) {
	root := NewResearchCmd()
	want := []string{"cache", "show", "sources"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: research %s", w)
		}
	}
}

func TestResearchCacheSubcommandsRegistered(t *testing.T) {
	root := NewResearchCmd()
	for _, c := range root.Commands() {
		if c.Name() == "cache" {
			want := []string{"get", "set", "list", "clear", "stats"}
			have := map[string]bool{}
			for _, sub := range c.Commands() {
				have[sub.Name()] = true
			}
			for _, w := range want {
				if !have[w] {
					t.Errorf("missing subcommand: research cache %s", w)
				}
			}
			return
		}
	}
	t.Fatal("cache subcommand not found")
}
