package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func invokeRead(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := cli.TestOnlyClientFactory
	cli.TestOnlyClientFactory = func() *cli.Client { return cli.NewClient(baseURL) }
	t.Cleanup(func() { cli.TestOnlyClientFactory = prev })

	root := cli.NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func mockDaemonForList(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"name": "max-scope", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
				{"name": "default", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
				{"name": "capa-firewall", "source": "embed", "schema_version": "1.0", "doctrine_version": "1.0.0"},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestList_Default_RendersThreeBuiltins(t *testing.T) {
	srv := mockDaemonForList(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"list"}, srv.URL)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestList_JSONFormat(t *testing.T) {
	srv := mockDaemonForList(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"list", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var got any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestList_SourceFlagFiltersServerQuery(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/list", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("source")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"list", "--source=user"}, srv.URL)
	if err != nil {
		t.Fatalf("list --source: %v", err)
	}
	if captured != "user" {
		t.Errorf("server received source=%q, want %q", captured, "user")
	}
}

func mockDaemonForShow(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/show", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		fmt := r.URL.Query().Get("format")
		if fmt == "" {
			fmt = "toml"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":   name,
			"format": fmt,
			"body":   "doctrine_version = \"1.0.0\"\nschema_version = \"1.0\"\nname = \"" + name + "\"",
		})
	})
	return httptest.NewServer(mux)
}

func TestShow_Default_RendersTOML(t *testing.T) {
	srv := mockDaemonForShow(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"show", "max-scope"}, srv.URL)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout, "max-scope") || !strings.Contains(stdout, "doctrine_version") {
		t.Errorf("rendered body missing fields: %s", stdout)
	}
}

func TestShow_DoctrineFormatFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/show", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("format")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope", "format": captured, "body": `{"name":"max-scope"}`,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"show", "max-scope", "--doctrine-format=json"}, srv.URL)
	if err != nil {
		t.Fatalf("show --doctrine-format: %v", err)
	}
	if captured != "json" {
		t.Errorf("server received format=%q, want %q", captured, "json")
	}
}

func TestShow_SectionFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/show", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("section")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope", "format": "toml", "body": "merge.weights.score = 1.0",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"show", "max-scope", "--section=merge.weights"}, srv.URL)
	if err != nil {
		t.Fatalf("show --section: %v", err)
	}
	if captured != "merge.weights" {
		t.Errorf("server received section=%q, want merge.weights", captured)
	}
}

func TestShow_RequiresName(t *testing.T) {
	srv := mockDaemonForShow(t)
	defer srv.Close()
	_, stderr, err := invokeRead(t, []string{"show"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: missing positional <name>")
	}
	if !strings.Contains(err.Error()+stderr, "doctrina") && !strings.Contains(err.Error()+stderr, "argumento") {
		t.Errorf("error should be in español; got %v / %s", err, stderr)
	}
}

func mockDaemonForStatus(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": map[string]any{
				"name": "max-scope", "schema_version": "1.0", "doctrine_version": "1.2.3", "source": "embed",
			},
			"last_reload_at":  "2026-05-03T12:00:00Z",
			"last_reload_ok":  true,
			"watcher_healthy": true,
			"pending_changes": []string{},
		})
	})
	return httptest.NewServer(mux)
}

func TestStatus_Default_PrintsActive(t *testing.T) {
	srv := mockDaemonForStatus(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"status"}, srv.URL)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"max-scope", "1.2.3", "watcher"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestStatus_ProjectFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/status", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("project")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active":          map[string]any{"name": "max-scope"},
			"last_reload_at":  "",
			"last_reload_ok":  true,
			"watcher_healthy": true,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"status", "--project=foo"}, srv.URL)
	if err != nil {
		t.Fatalf("status --project: %v", err)
	}
	if captured != "foo" {
		t.Errorf("server received project=%q, want foo", captured)
	}
}

func TestReadCommands_GroupIDs(t *testing.T) {
	root := cli.NewRoot()
	wantGroup := map[string]string{"list": "read", "show": "read", "status": "read"}
	for _, c := range root.Commands() {
		if want, ok := wantGroup[c.Use]; ok {
			if c.GroupID != want {
				t.Errorf("command %q: GroupID=%q, want %q", c.Use, c.GroupID, want)
			}
		}

		first := strings.SplitN(c.Use, " ", 2)[0]
		if want, ok := wantGroup[first]; ok && c.GroupID != want {
			t.Errorf("command %q: GroupID=%q, want %q", c.Use, c.GroupID, want)
		}
	}

	_ = os.Args
}

func mockDaemonForHistory(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{
					"type":    "DoctrineLoaded",
					"at_unix": 1714737600,
					"payload": map[string]any{"name": "max-scope"},
				},
				{
					"type":    "DoctrineReloaded",
					"at_unix": 1714738200,
					"payload": map[string]any{"source": "operator-edit"},
				},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestHistory_Default_RendersEvents(t *testing.T) {
	srv := mockDaemonForHistory(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"history"}, srv.URL)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	for _, want := range []string{"DoctrineLoaded", "DoctrineReloaded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestHistory_SinceFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/history", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("since")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"history", "--since=24h"}, srv.URL)
	if err != nil {
		t.Fatalf("history --since: %v", err)
	}
	if captured != "24h" {
		t.Errorf("server received since=%q, want 24h", captured)
	}
}

func TestHistory_FilterFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/history", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("filter")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"history", "--filter=category:cost"}, srv.URL)
	if err != nil {
		t.Fatalf("history --filter: %v", err)
	}
	if captured != "category:cost" {
		t.Errorf("server received filter=%q, want category:cost", captured)
	}
}

func mockDaemonForDiff(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/diff", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"from": r.URL.Query().Get("a"),
			"to":   r.URL.Query().Get("b"),
			"diffs": []map[string]any{
				{"path": "research.depth", "from": "shallow", "to": "deep", "status": "changed"},
				{"path": "merge.weights.cost", "from": "0.3", "to": "0.5", "status": "changed"},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestDiff_Default_RendersDiffRows(t *testing.T) {
	srv := mockDaemonForDiff(t)
	defer srv.Close()
	stdout, _, err := invokeRead(t, []string{"diff", "default", "max-scope"}, srv.URL)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, want := range []string{"research.depth", "merge.weights.cost", "changed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestDiff_RequiresTwoArgs(t *testing.T) {
	srv := mockDaemonForDiff(t)
	defer srv.Close()
	_, stderr, err := invokeRead(t, []string{"diff", "default"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: diff requires <name1> <name2>")
	}
	if !strings.Contains(err.Error()+stderr, "argumento") && !strings.Contains(err.Error()+stderr, "doctrina") {
		t.Errorf("error not in español: %v / %s", err, stderr)
	}
}

func TestDiff_SectionFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/diff", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("section")
		_ = json.NewEncoder(w).Encode(map[string]any{"from": "a", "to": "b", "diffs": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"diff", "default", "max-scope", "--section=merge"}, srv.URL)
	if err != nil {
		t.Fatalf("diff --section: %v", err)
	}
	if captured != "merge" {
		t.Errorf("server received section=%q, want merge", captured)
	}
}

func mockDaemonForValidate(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AgainstBaseline string `json:"against_baseline"`
			TOMLContent     string `json:"toml_content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.TOMLContent, "bogus") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":  false,
				"errors": []string{"unknown key: bogus"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":  true,
			"errors": []string{},
		})
	})
	return httptest.NewServer(mux)
}

func TestValidate_Default_OK(t *testing.T) {
	srv := mockDaemonForValidate(t)
	defer srv.Close()
	dir := t.TempDir()
	tmp := dir + "/doc.toml"
	if err := os.WriteFile(tmp, []byte(`name = "x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := invokeRead(t, []string{"validate", tmp}, srv.URL)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(stdout, "ok") && !strings.Contains(stdout, "válido") {
		t.Errorf("expected ok/válido marker in: %s", stdout)
	}
}

func TestValidate_Invalid(t *testing.T) {
	srv := mockDaemonForValidate(t)
	defer srv.Close()
	dir := t.TempDir()
	tmp := dir + "/doc.toml"
	if err := os.WriteFile(tmp, []byte(`bogus = 1`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := invokeRead(t, []string{"validate", tmp}, srv.URL)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidate_AgainstBaseline_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AgainstBaseline string `json:"against_baseline"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body.AgainstBaseline
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "errors": []string{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	tmp := dir + "/doc.toml"
	_ = os.WriteFile(tmp, []byte(`name = "x"`), 0o600)
	_, _, err := invokeRead(t, []string{"validate", tmp, "--against-baseline=max-scope"}, srv.URL)
	if err != nil {
		t.Fatalf("validate --against-baseline: %v", err)
	}
	if captured != "max-scope" {
		t.Errorf("server received against_baseline=%q, want max-scope", captured)
	}
}

func TestValidate_RequiresPositional(t *testing.T) {
	srv := mockDaemonForValidate(t)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"validate"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: validate requires <ruta>")
	}
}

func TestValidate_FileMissing_Errors(t *testing.T) {
	srv := mockDaemonForValidate(t)
	defer srv.Close()
	_, _, err := invokeRead(t, []string{"validate", "/nonexistent/path/to/file.toml"}, srv.URL)
	if err == nil {
		t.Fatal("expected error reading nonexistent file")
	}
	if !strings.Contains(err.Error(), "lectura") && !strings.Contains(err.Error(), "no") {
		t.Errorf("error should be about read failure: %v", err)
	}
}
