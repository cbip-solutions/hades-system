package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

func TestFetchLastN_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "test-token-xyz")

	commitsCalled := 0
	checkRunsCalled := 0
	tokenSeen := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		commitsCalled++
		tokenSeen = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "abc1234567890abc1234567890abc1234567890a", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/abc1234567890abc1234567890abc1234567890a/check-runs", func(w http.ResponseWriter, r *http.Request) {
		checkRunsCalled++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "completed", "conclusion": "success", "name": "build", "html_url": "https://example/run/1"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	commits, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("want 1 commit; got %d", len(commits))
	}
	if commits[0].SHA != "abc1234567890abc1234567890abc1234567890a" {
		t.Errorf("SHA: got %s", commits[0].SHA)
	}
	if commits[0].Status != "success" {
		t.Errorf("Status: got %s", commits[0].Status)
	}
	if commits[0].URL != "https://example/run/1" {
		t.Errorf("URL: got %s", commits[0].URL)
	}
	if tokenSeen != "Bearer test-token-xyz" {
		t.Errorf("Authorization: got %q; want Bearer test-token-xyz", tokenSeen)
	}
	if commitsCalled != 1 {
		t.Errorf("commits API calls: got %d; want 1", commitsCalled)
	}
	if checkRunsCalled != 1 {
		t.Errorf("check-runs API calls: got %d; want 1", checkRunsCalled)
	}
}

func TestFetchLastN_GHTokenFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-token-fallback")

	tokenSeen := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		tokenSeen = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "abc1234567890abc1234567890abc1234567890a", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/abc1234567890abc1234567890abc1234567890a/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "completed", "conclusion": "success", "name": "build", "html_url": "https://example/run/1"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	if _, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1); err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if tokenSeen != "Bearer gh-token-fallback" {
		t.Errorf("Authorization: got %q; want Bearer gh-token-fallback", tokenSeen)
	}
}

func TestFetchLastN_CachedSkipsAPI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")

	commitsCalled := 0
	checkRunsCalled := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		commitsCalled++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "abc1234567890abc1234567890abc1234567890a", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/abc1234567890abc1234567890abc1234567890a/check-runs", func(w http.ResponseWriter, r *http.Request) {
		checkRunsCalled++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "completed", "conclusion": "success", "name": "build", "html_url": "https://example/run/1"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if checkRunsCalled != 1 {
		t.Errorf("first call check-runs: got %d; want 1", checkRunsCalled)
	}

	_, err = ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if checkRunsCalled != 1 {
		t.Errorf("second call check-runs: got %d; want still 1 (cached)", checkRunsCalled)
	}
}

func TestFetchLastN_GHAPIError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected error on 429; got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429; got: %v", err)
	}
}

func TestFetchLastN_NetworkError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	srv := httptest.NewServer(http.NewServeMux())
	closedURL := srv.URL
	srv.Close()

	t.Setenv("GH_API_BASE_URL", closedURL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected error on closed server; got nil")
	}
}

func TestFetchLastN_CheckRunsFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "fff1234567890fff1234567890fff1234567890b", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/fff1234567890fff1234567890fff1234567890b/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "completed", "conclusion": "failure", "name": "tests", "html_url": "https://example/run/2", "output": map[string]any{"summary": "build error"}},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	commits, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if commits[0].Status != "failure" {
		t.Errorf("Status: got %s; want failure", commits[0].Status)
	}
	if !strings.Contains(commits[0].Reason, "tests") {
		t.Errorf("Reason should contain check name; got %s", commits[0].Reason)
	}
	if !strings.Contains(commits[0].Reason, "build error") {
		t.Errorf("Reason should contain summary; got %s", commits[0].Reason)
	}
}

func TestFetchLastN_CheckRunAnnotationsFeedInfraClassification(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "bbb1234567890bbb1234567890bbb1234567890b", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/bbb1234567890bbb1234567890bbb1234567890b/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{
					"id":         77763552038,
					"status":     "completed",
					"conclusion": "failure",
					"name":       "lint-and-test",
					"html_url":   "https://example/run/billing",
					"output":     map[string]any{"annotations_count": 1},
				},
			},
		})
	})
	mux.HandleFunc("/repos/test/repo/check-runs/77763552038/annotations", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"message": "The job was not started because recent account payments have failed or your spending limit needs to be increased.",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	commits, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if !strings.Contains(commits[0].Reason, "recent account payments have failed") {
		t.Fatalf("Reason should include annotation message; got %q", commits[0].Reason)
	}
	classified := ci.Classify(commits[0], nil)
	if classified.Bucket != "infra" {
		t.Fatalf("Bucket=%q, want infra for billing annotation; reason=%q", classified.Bucket, classified.Reason)
	}
}

func TestFetchLastN_CheckRunsPending(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "111000000000000000000000000000000000000c", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/111000000000000000000000000000000000000c/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "in_progress", "conclusion": "", "name": "build", "html_url": "https://example/run/3"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	commits, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if commits[0].Status != "pending" {
		t.Errorf("Status: got %s; want pending", commits[0].Status)
	}
}

func TestFetchLastN_NoCheckRunsYet(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "222000000000000000000000000000000000000d", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/222000000000000000000000000000000000000d/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	commits, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if commits[0].Status != "pending" {
		t.Errorf("Status on empty check_runs: got %s; want pending", commits[0].Status)
	}
}

func TestFetchLastN_CheckRunsCheckRunsBadJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "333000000000000000000000000000000000000e", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/333000000000000000000000000000000000000e/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected JSON decode error; got nil")
	}
}

func TestFetchLastN_CheckRunsHTTPError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "444000000000000000000000000000000000000f", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/444000000000000000000000000000000000000f/check-runs", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected error on 403; got nil")
	}
}

func TestFetchLastN_CommitsBadJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid{{{"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected JSON decode error on commits; got nil")
	}
}

func TestCommitStatus_StructSerialization(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{
		SHA:    "abc",
		Status: "success",
		Bucket: "success",
		Date:   time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"SHA":"abc"`) {
		t.Errorf("marshalled commit missing SHA: %s", data)
	}
	var back ci.CommitStatus
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.SHA != "abc" || !back.Date.Equal(c.Date) {
		t.Errorf("round-trip failed: %+v", back)
	}
}

func TestFetchLastN_ContextCancelled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {

		time.Sleep(5 * time.Second)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ci.FetchLastN(ctx, "test", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected error on cancelled ctx; got nil")
	}
}

func TestBaseURL_FallsBackToCanonical(t *testing.T) {
	t.Setenv("GH_API_BASE_URL", "")
	ci.ResetHTTPClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ci.FetchLastN(ctx, "owner", "repo", "main", 1)
	if err == nil {
		t.Fatal("expected error on cancelled ctx; got nil")
	}
	if !strings.Contains(err.Error(), "api.github.com") {
		t.Errorf("error should reference canonical api.github.com host; got: %v", err)
	}
}

func TestFetchLastN_BranchWithSlash(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	branchSeen := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		branchSeen = r.URL.Query().Get("sha")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"sha": "555000000000000000000000000000000000000g", "commit": map[string]any{"committer": map[string]any{"date": "2026-05-15T00:00:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/test/repo/commits/555000000000000000000000000000000000000g/check-runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"check_runs": []map[string]any{
				{"status": "completed", "conclusion": "success", "name": "build", "html_url": "https://example/run/4"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GH_API_BASE_URL", srv.URL)
	ci.ResetHTTPClient()

	_, err := ci.FetchLastN(context.Background(), "test", "repo", "feature/foo", 1)
	if err != nil {
		t.Fatalf("FetchLastN: %v", err)
	}
	if branchSeen != "feature/foo" {
		t.Errorf("branch param: got %q; want feature/foo", branchSeen)
	}
	_ = fmt.Sprintf
}
