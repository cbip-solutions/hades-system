package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func TestClient_Active(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/active" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":             "max-scope",
			"schema_version":   "1.0",
			"doctrine_version": "1.2.3",
			"source":           "embed",
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := c.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if resp.Name != "max-scope" || resp.SchemaVersion != "1.0" || resp.DoctrineVersion != "1.2.3" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("source"); got != "all" {
			t.Errorf("source query: want \"all\", got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"name": "max-scope", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
				{"name": "default", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
				{"name": "capa-firewall", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
			},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.List(context.Background(), "all")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("want 3 items, got %d", len(resp.Items))
	}
}

func TestClient_Validate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":  true,
			"errors": []string{},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Validate(context.Background(), "max-scope", "doctrine_version=\"1.2.3\"")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !resp.Valid {
		t.Errorf("want valid=true, got %+v", resp)
	}
}

func TestClient_Reinforce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("task_kind"); got != "worker" {
			t.Errorf("task_kind query: want \"worker\", got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rendered": "## You are a worker subprocess in project foo at stage 1...",
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Reinforce(context.Background(), cli.ReinforceReq{TaskKind: "worker", ProjectAlias: "foo", Stage: "1"})
	if err != nil {
		t.Fatalf("Reinforce: %v", err)
	}
	if !strings.Contains(resp.Rendered, "worker") {
		t.Errorf("rendered should reference task_kind: %q", resp.Rendered)
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"daemon overload"}`))
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	_, err := c.Active(context.Background())
	if err == nil {
		t.Fatal("want error from 500 response")
	}
	if !strings.Contains(err.Error(), "500") && !strings.Contains(err.Error(), "overload") {
		t.Errorf("error should mention status or message: %v", err)
	}
}

func TestClient_Show(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "max-scope" {
			t.Errorf("name query: want max-scope, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope", "format": "toml", "section": "", "body": `schema_version = "1.0"`,
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Show(context.Background(), "max-scope", "", "")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if resp.Name != "max-scope" || resp.Format != "toml" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("project"); got != "foo" {
			t.Errorf("project query: want foo, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active":          map[string]any{"name": "max-scope"},
			"last_reload_at":  "2026-05-03T12:00:00Z",
			"last_reload_ok":  true,
			"watcher_healthy": true,
			"pending_changes": []string{},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Status(context.Background(), "foo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.Active.Name != "max-scope" || !resp.LastReloadOk {
		t.Errorf("unexpected status: %+v", resp)
	}
}

func TestClient_History(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("since") != "24h" || q.Get("filter") != "category:cost" || q.Get("limit") != "5" {
			t.Errorf("history query mismatch: %v", q)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{"type": "DoctrineLoaded", "at_unix": 1714737600},
			},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.History(context.Background(), cli.HistoryReq{Since: "24h", Filter: "category:cost", Limit: 5})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Errorf("want 1 event, got %d", len(resp.Events))
	}
}

func TestClient_Diff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("a") != "default" || q.Get("b") != "max-scope" || q.Get("section") != "merge" {
			t.Errorf("diff query mismatch: %v", q)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"from":  "default",
			"to":    "max-scope",
			"diffs": []any{},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Diff(context.Background(), "default", "max-scope", "merge")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if resp.From != "default" || resp.To != "max-scope" {
		t.Errorf("unexpected diff: %+v", resp)
	}
}

func TestClient_Reload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reloaded": true,
			"state":    map[string]any{"name": "max-scope"},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Reload(context.Background(), "/tmp/foo.toml")
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !resp.Reloaded {
		t.Errorf("expected reloaded=true: %+v", resp)
	}
}

func TestClient_Migrate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_schema_version": "2.0",
			"toml_content":      `schema_version = "2.0"`,
			"warnings":          []string{},
		})
	}))
	defer srv.Close()
	c := cli.NewClient(srv.URL)
	resp, err := c.Migrate(context.Background(), cli.MigrateReq{TOMLContent: `schema_version = "1.0"`, FromSchemaVersion: "1.0"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if resp.ToSchemaVersion != "2.0" {
		t.Errorf("unexpected migrate: %+v", resp)
	}
}
