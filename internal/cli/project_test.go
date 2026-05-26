package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestProjectCmdHasDoctorArchiveRm(t *testing.T) {
	cmd := NewProjectCmd()
	got := map[string]bool{}
	for _, sc := range cmd.Commands() {
		got[sc.Name()] = true
	}
	for _, want := range []string{"doctor", "archive", "rm"} {
		if !got[want] {
			t.Errorf("subcommand %q missing", want)
		}
	}
}

func TestProjectDoctorOutputHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": true,
			"alias": "internal-platform-x",
			"id_sha256": "9f3a1c2d8b4e5f60111122223333444455556666777788889999aaaabbbbccccdd",
			"canonical_path": "/path/to/projects/internal-platform-x",
			"path_history": [
				{"path": "/path/to/projects/internal-platform-x", "first_seen": 1700000000, "last_seen": 1700001000}
			]
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err != nil {
		t.Fatalf("runProjectDoctor: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"internal-platform-x", "healthy", "9f3a1c2d", "/path/to/projects/internal-platform-x"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestProjectDoctorOutputMvDetected(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": false,
			"alias": "internal-platform-x",
			"id_sha256": "4ab8d172000000000000000000000000000000000000000000000000000000aa",
			"canonical_path": "/path/to/projects/internal-platform-x-relocated",
			"mv_detected": {
				"old_path": "/path/to/projects/internal-platform-x",
				"new_path": "/path/to/projects/internal-platform-x-relocated",
				"old_id_short": "9f3a1c2d",
				"new_id_short": "4ab8d172"
			},
			"hint": "To rebind: zen project doctor internal-platform-x --rebind\nTo register as a new project: rename in zenswarm.toml [project] id"
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Error("expected non-nil error on mv-detected (exit 1 path)")
	}
	out := buf.String()
	for _, want := range []string{"MV DETECTED", "9f3a1c2d", "4ab8d172", "rebind", "register as a new project"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}

	if !strings.Contains(out, "             To register as a new project") {
		t.Errorf("continuation line not indented properly\ngot:\n%s", out)
	}
}

func TestProjectArchiveSendsAlias(t *testing.T) {
	gotAlias := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotAlias, _ = body["alias"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	if err := runProjectArchive(context.Background(), c, "internal-platform-x", &buf); err != nil {
		t.Fatalf("runProjectArchive: %v", err)
	}
	if gotAlias != "internal-platform-x" {
		t.Errorf("server received alias=%q, want internal-platform-x", gotAlias)
	}
	if !strings.Contains(buf.String(), "archived: internal-platform-x") {
		t.Errorf("output missing archived line: %s", buf.String())
	}
}

func TestProjectRmRequiresYes(t *testing.T) {
	c := client.NewWithBaseURL("http://unused")
	var buf bytes.Buffer
	err := runProjectRm(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected error when --yes missing; got nil")
	}
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("output missing --yes hint: %s", buf.String())
	}
}

func TestProjectRmWithYesSendsAlias(t *testing.T) {
	gotAlias := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotAlias, _ = body["alias"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	if err := runProjectRm(context.Background(), c, "internal-platform-x", true, &buf); err != nil {
		t.Fatalf("runProjectRm: %v", err)
	}
	if gotAlias != "internal-platform-x" {
		t.Errorf("server received alias=%q, want internal-platform-x", gotAlias)
	}
	if !strings.Contains(buf.String(), "removed: internal-platform-x") {
		t.Errorf("output missing removed line: %s", buf.String())
	}
}

func TestProjectArchiveAliasMissingExitOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectArchive(context.Background(), c, "missing-alias", &buf)
	if err == nil {
		t.Error("expected error on 404; got nil")
	}
}

func TestProjectDoctorRebindFlagReserved(t *testing.T) {
	cmd := NewProjectCmd()
	var doctor *cobra.Command
	for _, sc := range cmd.Commands() {
		if sc.Name() == "doctor" {
			doctor = sc
			break
		}
	}
	if doctor == nil {
		t.Fatal("doctor subcommand missing")
	}
	if doctor.Flags().Lookup("rebind") == nil {
		t.Error("--rebind flag not registered on doctor (Phase A reserves; Phase B/J implements body)")
	}
}

func TestAliasOrCwdRendering(t *testing.T) {
	cases := []struct {
		alias, cwd, want string
	}{
		{"internal-platform-x", "", "internal-platform-x"},
		{"", "/tmp/x", "(cwd: /tmp/x)"},
		{"", "", "(no alias)"},
	}
	for _, c := range cases {
		got := aliasOrCwd(c.alias, c.cwd)
		if got != c.want {
			t.Errorf("aliasOrCwd(%q,%q) = %q, want %q", c.alias, c.cwd, got, c.want)
		}
	}
}

func TestProjectDoctorTransportError(t *testing.T) {

	c := client.NewWithBaseURL("http://127.0.0.1:1")
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected transport error; got nil")
	}
	if !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("expected ERROR in output; got: %s", buf.String())
	}
}

func TestProjectDoctorCwdBased(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": true,
			"alias": "test-proj",
			"id_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"canonical_path": "/tmp/test-proj",
			"path_history": []
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "", false, &buf)
	if err != nil {
		t.Fatalf("runProjectDoctor (cwd-based): %v", err)
	}
	if !strings.Contains(buf.String(), "test-proj") {
		t.Errorf("output missing alias: %s", buf.String())
	}
}

func TestProjectDoctorUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": false,
			"alias": "internal-platform-x",
			"id_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"canonical_path": "/path/to/projects/internal-platform-x",
			"path_history": []
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected error on unhealthy project; got nil")
	}
	if !strings.Contains(buf.String(), "UNHEALTHY") {
		t.Errorf("output missing UNHEALTHY: %s", buf.String())
	}
}

func TestProjectRmTransportError(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	var buf bytes.Buffer
	err := runProjectRm(context.Background(), c, "internal-platform-x", true, &buf)
	if err == nil {
		t.Fatal("expected transport error; got nil")
	}
	if !strings.Contains(buf.String(), "remove failed") {
		t.Errorf("expected 'remove failed' in output; got: %s", buf.String())
	}
}

func TestProjectArchiveCmdRunE(t *testing.T) {

	gotAlias := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotAlias, _ = body["alias"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs([]string{"project", "archive", "internal-platform-x"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout=%s", err, buf.String())
	}
	if gotAlias != "internal-platform-x" {
		t.Errorf("server received alias=%q, want internal-platform-x", gotAlias)
	}
}

func TestProjectRmCmdRunERefuses(t *testing.T) {

	root := NewRootCmd()
	root.SetArgs([]string{"project", "rm", "internal-platform-x"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.Execute()
	if err == nil {
		t.Fatalf("Execute: expected error without --yes; out=%s", buf.String())
	}
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("output missing --yes hint: %s", buf.String())
	}
}

func TestProjectDoctorCmdRunE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": true,
			"alias": "internal-platform-x",
			"id_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"canonical_path": "/path/to/projects/internal-platform-x",
			"path_history": []
		}`))
	}))
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs([]string{"project", "doctor", "internal-platform-x"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout=%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "healthy") {
		t.Errorf("output missing healthy: %s", buf.String())
	}
}

func TestIsRecoverableSentinelSemantics(t *testing.T) {
	if IsRecoverable(nil) {
		t.Error("IsRecoverable(nil) = true; want false")
	}
	if !IsRecoverable(ErrRecoverable) {
		t.Error("IsRecoverable(ErrRecoverable) = false; want true")
	}
	wrapped := fmt.Errorf("%w: details", ErrRecoverable)
	if !IsRecoverable(wrapped) {
		t.Error("IsRecoverable(wrapped) = false; want true")
	}
	if IsRecoverable(errors.New("plain unrelated")) {
		t.Error("IsRecoverable(plain) = true; want false")
	}
}

func TestRunProjectDoctorMvDetectedReturnsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": false,
			"alias": "internal-platform-x",
			"id_sha256": "4ab8d172000000000000000000000000000000000000000000000000000000aa",
			"canonical_path": "/path/to/projects/internal-platform-x-relocated",
			"mv_detected": {
				"old_path": "/old", "new_path": "/new",
				"old_id_short": "9f3a1c2d", "new_id_short": "4ab8d172"
			},
			"hint": "rebind hint"
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected non-nil error on mv-detected")
	}
	if !IsRecoverable(err) {
		t.Errorf("mv-detected error not recoverable: %v", err)
	}
}

func TestRunProjectDoctorUnhealthyReturnsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": false,
			"alias": "internal-platform-x",
			"id_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"canonical_path": "/path/to/projects/internal-platform-x",
			"path_history": []
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected non-nil error on unhealthy")
	}
	if !IsRecoverable(err) {
		t.Errorf("unhealthy error not recoverable: %v", err)
	}
}

func TestRunProjectDoctorAliasNotFoundReturnsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "missing", false, &buf)
	if err == nil {
		t.Fatal("expected non-nil error on 404")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 error not recoverable: %v", err)
	}
}

func TestRunProjectArchiveAliasNotFoundReturnsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectArchive(context.Background(), c, "missing", &buf)
	if err == nil {
		t.Fatal("expected non-nil error on 404")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 error not recoverable: %v", err)
	}
}

func TestRunProjectRmAliasNotFoundReturnsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectRm(context.Background(), c, "missing", true, &buf)
	if err == nil {
		t.Fatal("expected non-nil error on 404")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 error not recoverable: %v", err)
	}
}

func TestRunProjectRmYesOmittedReturnsRecoverable(t *testing.T) {
	c := client.NewWithBaseURL("http://unused")
	var buf bytes.Buffer
	err := runProjectRm(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected error when --yes missing")
	}
	if !IsRecoverable(err) {
		t.Errorf("--yes-omitted error not recoverable: %v", err)
	}
}

// TestRunProjectDoctorTransportErrorIsUnrecoverable — transport-layer
// failures (5xx, dial, decode) MUST NOT be marked recoverable: they
// surface as exit 2 so the operator distinguishes daemon health issues
// from operator-input issues.
func TestRunProjectDoctorTransportErrorIsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectDoctor(context.Background(), c, "internal-platform-x", false, &buf)
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 transport error wrongly marked recoverable: %v", err)
	}
}

func TestRunProjectArchiveServerErrorIsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectArchive(context.Background(), c, "x", &buf)
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 archive error wrongly marked recoverable: %v", err)
	}
}

func TestRunProjectRmServerErrorIsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	var buf bytes.Buffer
	err := runProjectRm(context.Background(), c, "x", true, &buf)
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 rm error wrongly marked recoverable: %v", err)
	}
}
