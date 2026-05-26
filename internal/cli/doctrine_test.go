package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func invokeDoctrineCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctrineCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockDoctrineServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":           "max-scope",
			"schema_version": 1,
			"research": map[string]any{
				"depth":            "deep",
				"agentic_max_iter": 5,
			},
		})
	})
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		var req client.DoctrineValidateReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.TOMLContent, "bogus") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{
				Valid: false, Errors: []string{"unknown key: bogus"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{Valid: true, Errors: []string{}})
	})
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineReloadResp{
			Reloaded: true,
			State:    client.DoctrineState{"name": "max-scope"},
		})
	})
	return httptest.NewServer(mux)
}

func TestDoctrineShow(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"show"}, srv.URL)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	for _, want := range []string{"max-scope", "research.depth", "deep"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctrineShow_JSON(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"show", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if m["name"] != "max-scope" {
		t.Errorf("got %+v", m)
	}
}

func TestDoctrineList(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"list"}, srv.URL)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctrineValidate_RequiresFile(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"validate"}, srv.URL)
	if err == nil {
		t.Fatal("expected --file error")
	}
}

func TestDoctrineValidate_OK(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(tmp, []byte(`name = "my-doctrine"`), 0644)
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"validate", "--file=" + tmp}, srv.URL)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("got %s", stdout)
	}
}

func TestDoctrineValidate_Invalid(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(tmp, []byte(`bogus = 1`), 0644)
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"validate", "--file=" + tmp}, srv.URL)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDoctrineWhich(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"which"}, srv.URL)
	if err != nil {
		t.Fatalf("which: %v", err)
	}
	if !strings.Contains(stdout, "max-scope") || !strings.Contains(stdout, "Resolution chain") {
		t.Errorf("got %s", stdout)
	}
}

func TestDoctrineReload_RequiresYes(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"reload"}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestDoctrineReload_HappyPath(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"reload", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(stdout, "reloaded") || !strings.Contains(stdout, "max-scope") {
		t.Errorf("got %s", stdout)
	}
}

func TestDoctrineDiff(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"diff", "--from=default", "--to=max-scope"}, srv.URL)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(stdout, "research") {
		t.Errorf("expected research in diff: %s", stdout)
	}
}

func TestDoctrineDiff_RequiresFlags(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"diff"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDoctrineDiff_BadName(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"diff", "--from=nope", "--to=default"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDoctrineSchema(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"schema"}, srv.URL)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, want := range []string{"name =", "schema_version =", "additive-only", "research"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctrineSubcommandsRegistered(t *testing.T) {
	root := NewDoctrineCmd()
	want := []string{"show", "list", "validate", "which", "reload", "diff", "schema"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: doctrine %s", w)
		}
	}
}

func TestDoctrineShow_YAML(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"show", "--format=yaml"}, srv.URL)
	if err != nil {
		t.Fatalf("show --yaml: %v", err)
	}
	if !strings.Contains(stdout, "max-scope") {
		t.Errorf("got %s", stdout)
	}
}

func TestDoctrineShow_ExclusiveFlags(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"show", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestDoctrineList_ExclusiveFlags(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"list", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestDoctrineValidate_ReadError(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"validate", "--file=/tmp/does-not-exist-doctrine.toml"}, srv.URL)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error should mention read: %v", err)
	}
}

func TestDoctrineValidate_DaemonRejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {

		_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{
			Valid:  false,
			Errors: []string{"unknown field foo", "range violation bar"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	tmp := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(tmp, []byte(`name = "broken"`), 0644)

	stdout, _, err := invokeDoctrineCmd(t, []string{"validate", "--file=" + tmp}, srv.URL)
	if err == nil {
		t.Fatal("expected validation rejection")
	}
	if !strings.Contains(stdout, "INVALID") {
		t.Errorf("output should contain INVALID: %s", stdout)
	}
}

func TestDoctrineValidate_OkWithWarnings(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{
			Valid:  true,
			Errors: []string{"deprecated field foo", "use bar instead"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	tmp := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(tmp, []byte(`name = "ok"`), 0644)

	stdout, _, err := invokeDoctrineCmd(t, []string{"validate", "--file=" + tmp}, srv.URL)
	if err != nil {
		t.Fatalf("valid-with-warnings: %v", err)
	}
	if !strings.Contains(stdout, "Warnings") {
		t.Errorf("output should contain Warnings: %s", stdout)
	}
}

func TestDoctrineWhich_ExclusiveFlags(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"which", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestDoctrineWhich_JSON(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"which", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("which --json: %v", err)
	}
	if !strings.Contains(stdout, "max-scope") {
		t.Errorf("got %s", stdout)
	}
}

func TestDoctrineReload_DaemonRejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineReloadResp{
			Reloaded: false,
			Errors:   []string{"validation failed: x"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"reload", "--yes"}, srv.URL)
	if err == nil {
		t.Fatal("expected reload rejection")
	}
	if !strings.Contains(stdout, "INVALID") {
		t.Errorf("output should mention INVALID: %s", stdout)
	}
}

func TestDoctrineReload_SystemError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineReloadResp{
			Reloaded: false,
			Error:    "disk full",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"reload", "--yes"}, srv.URL)
	if err == nil {
		t.Fatal("expected system error surfaced")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error should propagate: %v", err)
	}
}

func TestDoctrineReload_UnknownError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineReloadResp{
			Reloaded: false,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeDoctrineCmd(t, []string{"reload", "--yes"}, srv.URL)
	if err == nil {
		t.Fatal("expected unknown error")
	}
}

func TestFlattenForTable_MapAnyAny(t *testing.T) {
	in := map[any]any{
		"k1": "v1",
		"k2": 42,
	}
	rows := flattenForTable("prefix", in)
	if len(rows) != 2 {
		t.Errorf("got %d rows: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.Key, "prefix.") {
			t.Errorf("missing prefix: %+v", r)
		}
	}
}

func TestSchemaToMap_RoundTrip(t *testing.T) {
	s := doctrineSchemaForRoundtrip()
	m := schemaToMap(s)
	if m["name"] == nil {
		t.Errorf("name missing in roundtrip: %+v", m)
	}
}

func doctrineSchemaForRoundtrip() doctrine.Schema {
	return doctrine.MaxScopeBuiltin()
}

func TestDoctrineShow_StateAlias(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"state"}, srv.URL)
	if err != nil {
		t.Fatalf("state alias should resolve to show: %v", err)
	}
	if !strings.Contains(stdout, "max-scope") {
		t.Errorf("alias didn't render show output: %s", stdout)
	}
}

func TestDoctrineShow_QuietSuppressesPrelude(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"show", "--quiet"}, srv.URL)
	if err != nil {
		t.Fatalf("show --quiet: %v", err)
	}
	if strings.Contains(stdout, "Active doctrine:") {
		t.Errorf("--quiet should suppress prelude: %s", stdout)
	}

	if !strings.Contains(stdout, "research.depth") {
		t.Errorf("expected dot-path rows in body: %s", stdout)
	}
}

func TestDoctrineShow_FilterPathRows(t *testing.T) {
	srv := mockDoctrineServer(t)
	defer srv.Close()
	stdout, _, err := invokeDoctrineCmd(t, []string{"show", "--filter", "path~^research"}, srv.URL)
	if err != nil {
		t.Fatalf("show --filter: %v", err)
	}
	// research.* rows survive; name/schema_version do not.
	if !strings.Contains(stdout, "research.depth") {
		t.Errorf("filter dropped expected research.* row: %s", stdout)
	}
	if strings.Contains(stdout, "schema_version") {
		t.Errorf("filter should drop non-matching rows: %s", stdout)
	}
}
