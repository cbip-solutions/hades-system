package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func invokeWrite(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	if baseURL != "" {
		prev := cli.TestOnlyClientFactory
		cli.TestOnlyClientFactory = func() *cli.Client { return cli.NewClient(baseURL) }
		t.Cleanup(func() { cli.TestOnlyClientFactory = prev })
	} else {

		prev := cli.TestOnlyClientFactory
		cli.TestOnlyClientFactory = nil
		t.Cleanup(func() { cli.TestOnlyClientFactory = prev })
	}
	root := cli.NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func withFakeHomeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

func TestInit_Default_WritesToConfigDoctrines(t *testing.T) {
	home := withFakeHomeDir(t)
	stdout, _, err := invokeWrite(t, []string{"init", "max-scope"}, "")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	want := filepath.Join(home, ".config", "zen-swarm", "doctrines", "max-scope.toml")
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", want, err)
	}
	if !bytes.Contains(body, []byte("schema_version")) {
		t.Errorf("written file missing schema_version key:\n%s", body)
	}
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout should announce destination path %s; got: %s", want, stdout)
	}
}

func TestInit_OutputFlag_OverridesPath(t *testing.T) {
	_ = withFakeHomeDir(t)
	dir := t.TempDir()
	custom := filepath.Join(dir, "my-doctrines", "custom.toml")
	stdout, _, err := invokeWrite(t, []string{"init", "default", "--output=" + custom}, "")
	if err != nil {
		t.Fatalf("init --output: %v", err)
	}
	body, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", custom, err)
	}
	if !bytes.Contains(body, []byte("schema_version")) {
		t.Errorf("written file missing schema_version key:\n%s", body)
	}
	if !strings.Contains(stdout, custom) {
		t.Errorf("stdout should announce custom destination; got: %s", stdout)
	}
}

func TestInit_RefusesOverwrite_WithoutForce(t *testing.T) {
	home := withFakeHomeDir(t)
	dest := filepath.Join(home, ".config", "zen-swarm", "doctrines", "max-scope.toml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("# pre-existing operator content"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := invokeWrite(t, []string{"init", "max-scope"}, "")
	if err == nil {
		t.Fatal("expected error: refuse overwrite without --force")
	}
	if !strings.Contains(err.Error()+stderr, "existe") {
		t.Errorf("error should mention existing file in español; got: %v / %s", err, stderr)
	}
	body, _ := os.ReadFile(dest)
	if !bytes.Contains(body, []byte("pre-existing operator content")) {
		t.Errorf("operator content was clobbered:\n%s", body)
	}
}

func TestInit_ForceOverwrites(t *testing.T) {
	home := withFakeHomeDir(t)
	dest := filepath.Join(home, ".config", "zen-swarm", "doctrines", "default.toml")
	_ = os.MkdirAll(filepath.Dir(dest), 0o700)
	_ = os.WriteFile(dest, []byte("# old"), 0o600)
	_, _, err := invokeWrite(t, []string{"init", "default", "--force"}, "")
	if err != nil {
		t.Fatalf("init --force: %v", err)
	}
	body, _ := os.ReadFile(dest)
	if bytes.Contains(body, []byte("# old")) {
		t.Errorf("--force should overwrite; old content remains:\n%s", body)
	}
}

func TestInit_UnknownDoctrine_Errors(t *testing.T) {
	_ = withFakeHomeDir(t)
	_, stderr, err := invokeWrite(t, []string{"init", "nonexistent-doctrine"}, "")
	if err == nil {
		t.Fatal("expected error: unknown built-in doctrine")
	}
	if !strings.Contains(err.Error()+stderr, "desconocida") && !strings.Contains(err.Error()+stderr, "no encontrada") {
		t.Errorf("error should be in español; got: %v / %s", err, stderr)
	}
}

func TestInit_RequiresName(t *testing.T) {
	_ = withFakeHomeDir(t)
	_, _, err := invokeWrite(t, []string{"init"}, "")
	if err == nil {
		t.Fatal("expected error: missing positional <nombre>")
	}
}

func mockDaemonForMigrate(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/migrate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TOMLContent       string `json:"toml_content"`
			FromSchemaVersion string `json:"from_schema_version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.TOMLContent, `schema_version = "0.5"`) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "schema_version 0.5 too old; chain V0.5→V1.0 unavailable",
			})
			return
		}

		newBody := strings.ReplaceAll(body.TOMLContent, `schema_version = "1.0"`, `schema_version = "2.0"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_schema_version": "2.0",
			"toml_content":      newBody,
			"warnings":          []string{},
		})
	})
	return httptest.NewServer(mux)
}

func TestMigrate_Default_PreviewsWithoutWriting(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	original := []byte(`schema_version = "1.0"
doctrine_version = "1.2.3"
name = "max-scope"
`)
	if err := os.WriteFile(src, original, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := invokeWrite(t, []string{"migrate", src}, srv.URL)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	body, _ := os.ReadFile(src)
	if !bytes.Contains(body, []byte(`schema_version = "1.0"`)) {
		t.Errorf("file modified without --confirm:\n%s", body)
	}
	if !strings.Contains(stdout, "2.0") || !strings.Contains(stdout, "--confirm") {
		t.Errorf("preview output should mention 2.0 + --confirm; got: %s", stdout)
	}
}

func TestMigrate_Confirm_WritesAndBacksUp(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	original := []byte(`schema_version = "1.0"
doctrine_version = "1.2.3"
name = "max-scope"
`)
	_ = os.WriteFile(src, original, 0o600)

	_, _, err := invokeWrite(t, []string{"migrate", src, "--confirm"}, srv.URL)
	if err != nil {
		t.Fatalf("migrate --confirm: %v", err)
	}
	body, _ := os.ReadFile(src)
	if !bytes.Contains(body, []byte(`schema_version = "2.0"`)) {
		t.Errorf("file not migrated; got:\n%s", body)
	}
	backup := src + ".v1.bak"
	bbody, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup %s missing: %v", backup, err)
	}
	if !bytes.Equal(bbody, original) {
		t.Errorf("backup content drift; got:\n%s\nwant:\n%s", bbody, original)
	}
}

func TestMigrate_RefusesUnsupportedFromVersion(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(src, []byte(`schema_version = "0.5"
name = "ancient"
`), 0o600)
	_, _, err := invokeWrite(t, []string{"migrate", src}, srv.URL)
	if err == nil {
		t.Fatal("expected error: schema 0.5 unsupported")
	}
}

func TestMigrate_RefusesDowngrade(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/migrate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_schema_version": "1.0",
			"toml_content":      `schema_version = "1.0"` + "\n",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(src, []byte(`schema_version = "2.0"
name = "future"
`), 0o600)
	_, _, err := invokeWrite(t, []string{"migrate", src, "--confirm"}, srv.URL)
	if err == nil {
		t.Fatal("expected downgrade rejection (inv-zen-142)")
	}
	if !strings.Contains(err.Error(), "downgrade") && !strings.Contains(err.Error(), "menor") {
		t.Errorf("error should mention downgrade refusal; got: %v", err)
	}
}

func TestMigrate_BackupSuffixMatchesFromVersion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/migrate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_schema_version": "2.0",
			"toml_content":      `schema_version = "2.0"` + "\n",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(src, []byte(`schema_version = "1.0"
name = "x"
`), 0o600)
	_, _, err := invokeWrite(t, []string{"migrate", src, "--confirm"}, srv.URL)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(src + ".v1.bak"); err != nil {
		t.Errorf("expected %s.v1.bak; got: %v", src, err)
	}
}

func TestMigrate_RequiresPath(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	_, _, err := invokeWrite(t, []string{"migrate"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: missing positional <ruta>")
	}
}

func TestMigrate_NoSchemaVersionInFile_Errors(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.toml")
	_ = os.WriteFile(src, []byte(`name = "no-version"
`), 0o600)
	_, _, err := invokeWrite(t, []string{"migrate", src}, srv.URL)
	if err == nil {
		t.Fatal("expected error: missing schema_version")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error should mention missing schema_version: %v", err)
	}
}

func TestMigrate_FileMissing_Errors(t *testing.T) {
	srv := mockDaemonForMigrate(t)
	defer srv.Close()
	_, _, err := invokeWrite(t, []string{"migrate", "/nonexistent/path.toml"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: missing file")
	}
}

func withFakeEditor(t *testing.T, scriptBody string) {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-editor.sh")
	body := "#!/bin/sh\n" + scriptBody + "\n"
	if err := os.WriteFile(scriptPath, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", scriptPath)
}

func TestOverrideEdit_CreatesStub_WhenAbsent(t *testing.T) {
	withFakeEditor(t, `cat >> "$1" <<'EOF'
# Operator's tighten-only override
research = { depth = "very-deep" }
EOF`)
	dir := t.TempDir()

	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	stdout, _, err := invokeWrite(t, []string{"override", "edit", "--no-validate"}, "")
	if err != nil {
		t.Fatalf("override edit: %v", err)
	}
	dest := filepath.Join(dir, ".zen", "doctrine-override.toml")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", dest, err)
	}
	if !bytes.Contains(body, []byte("tighten-only")) && !bytes.Contains(body, []byte("very-deep")) {
		t.Errorf("file missing stub header or editor content:\n%s", body)
	}
	if !strings.Contains(stdout, dest) {
		t.Errorf("stdout should announce path: %s", stdout)
	}
}

func TestOverrideEdit_RespectsExistingFile(t *testing.T) {
	withFakeEditor(t, `echo '# edited by fake editor' >> "$1"`)
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	dest := filepath.Join(dir, ".zen", "doctrine-override.toml")
	_ = os.MkdirAll(filepath.Dir(dest), 0o700)
	if err := os.WriteFile(dest, []byte("# operator's pre-existing content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := invokeWrite(t, []string{"override", "edit", "--no-validate"}, "")
	if err != nil {
		t.Fatalf("override edit: %v", err)
	}
	body, _ := os.ReadFile(dest)
	if !bytes.Contains(body, []byte("operator's pre-existing")) {
		t.Errorf("operator content was clobbered:\n%s", body)
	}
	if !bytes.Contains(body, []byte("edited by fake editor")) {
		t.Errorf("editor changes not preserved:\n%s", body)
	}
}

func TestOverrideEdit_PathFlag_OverridesCWD(t *testing.T) {
	withFakeEditor(t, `echo '# edited' >> "$1"`)
	dir := t.TempDir()
	customProject := filepath.Join(dir, "myproject")
	_ = os.MkdirAll(customProject, 0o700)
	_, _, err := invokeWrite(t, []string{"override", "edit", "--path=" + customProject, "--no-validate"}, "")
	if err != nil {
		t.Fatalf("override edit --path: %v", err)
	}
	expected := filepath.Join(customProject, ".zen", "doctrine-override.toml")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected %s to exist: %v", expected, err)
	}
}

func TestOverrideEdit_EditorFailure_Errors(t *testing.T) {
	withFakeEditor(t, "exit 1")
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, _, err := invokeWrite(t, []string{"override", "edit", "--no-validate"}, "")
	if err == nil {
		t.Fatal("expected error: editor returned non-zero")
	}
	if !strings.Contains(err.Error(), "editor") {
		t.Errorf("error should mention editor; got: %v", err)
	}
}

func TestOverrideEdit_ValidationFailure_LeavesFileButErrors(t *testing.T) {
	withFakeEditor(t, `echo 'bogus_key = 1' >> "$1"`)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":  false,
			"errors": []string{"unknown key: bogus_key"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, _, err := invokeWrite(t, []string{"override", "edit"}, srv.URL)
	if err == nil {
		t.Fatal("expected validation error to surface")
	}
	dest := filepath.Join(dir, ".zen", "doctrine-override.toml")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file should remain for operator inspection: %v", err)
	}
}

func TestOverrideEdit_ProjectFlag_NotYetSupported(t *testing.T) {
	withFakeEditor(t, `echo '# edited' >> "$1"`)
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, _, err := invokeWrite(t, []string{"override", "edit", "--project=foo", "--no-validate"}, "")
	if err == nil {
		t.Fatal("expected error: --project alias resolver not yet wired")
	}
	if !strings.Contains(err.Error(), "Plan 7") && !strings.Contains(err.Error(), "alias") {
		t.Errorf("error should mention Plan 7 alias resolver gap; got: %v", err)
	}
}

func mockDaemonForReload(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reloaded": true,
			"state": map[string]any{
				"name": "max-scope", "schema_version": "1.0", "doctrine_version": "1.2.3", "source": "user",
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestReload_Default_TriggersReload(t *testing.T) {
	srv := mockDaemonForReload(t)
	defer srv.Close()
	stdout, _, err := invokeWrite(t, []string{"reload"}, srv.URL)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, want := range []string{"recargada", "max-scope"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestReload_PathFlag_PassesToDaemon(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reloaded": true,
			"state":    map[string]any{"name": "max-scope"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeWrite(t, []string{"reload", "--path=/tmp/foo.toml"}, srv.URL)
	if err != nil {
		t.Fatalf("reload --path: %v", err)
	}
	if captured != "/tmp/foo.toml" {
		t.Errorf("server received path=%q, want /tmp/foo.toml", captured)
	}
}

func TestReload_DaemonReportsFailure_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reloaded": false,
			"errors":   []string{"tighten violation: research.depth"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeWrite(t, []string{"reload"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from reload=false response")
	}
	if !strings.Contains(err.Error(), "rechazada") && !strings.Contains(err.Error(), "tighten") {
		t.Errorf("error should mention rejection; got: %v", err)
	}
}

func TestReload_DaemonReturnsBareErrorString_StillSurfaces(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reloaded": false,
			"error":    "schema deprecated",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeWrite(t, []string{"reload"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from reload=false + error string")
	}
	if !strings.Contains(err.Error(), "schema deprecated") && !strings.Contains(err.Error(), "falló") {
		t.Errorf("error should surface daemon's error; got: %v", err)
	}
}
