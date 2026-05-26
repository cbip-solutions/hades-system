// SPDX-License-Identifier: MIT
// Package cli — coverage_gap_test.go (Plan 15 W5CLI coverage lift).
//
// Focused unit tests that exercise 0%-covered functions in internal/cli
// to lift coverage from 84.9% → ≥85.5% (with buffer above the 85% gate).
//
// Target functions (per go tool cover -func analysis):
//   - humanBytes           (bypass.go:431)         — pure formatter, 5 stmts
//   - notWiredYet          (audit_chain.go:89)      — closure factory, 2 stmts
//   - defaultProviderConfigDir (providers.go:35)    — $HOME wrapper, 1 stmt
//   - bypassNewClient      (bypass.go:65)           — client ctor, 2 stmts
//   - bypassNewClientWithURL (client_helper.go:21)  — dual-path ctor, 3 stmts
//   - findCtldBinary       (daemon.go:180)          — search function, 12 stmts
//   - findInstallScript    (daemon.go:216)          — search function, 10 stmts
//   - realEditorRun        (adr_editor.go:41)       — exec wrapper, 8 stmts
//   - buildBackend         (providers_extra.go:45)  — switch factory, 7 stmts
//   - runToolBinary        (bypass_extract.go:59)   — exec wrapper, 3 stmts
//   - productionCochangeClient.CoChange (cochange.go:33) — thin wrapper, 1 stmt
//   - productionWhyClient.Why (why.go:34)           — thin wrapper, 1 stmt
//   - productionRiskClient.Risk (risk.go:35)        — thin wrapper, 1 stmt
//   - bypassNewClient      (bypass.go:65)         — client ctor, 2 stmts
//   - bypassNewClientWithURL (client_helper.go:21) — dual-path ctor, 3 stmts
//   - findCtldBinary       (daemon.go:180)        — search function, 12 stmts
//   - findInstallScript    (daemon.go:216)        — search function, 10 stmts
//   - realEditorRun        (adr_editor.go:41)     — exec wrapper, 8 stmts
//   - buildBackend         (providers_extra.go:45) — switch factory, 7 stmts
//
// All tests are pure unit tests: no daemon, no real UDS, no Keychain.
// Env-var seams (ZEN_SWARM_CTLD) control findCtldBinary search order.
package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func TestHumanBytes_AllBranches(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{2048, "2.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{int64(2.5 * 1024 * 1024), "2.5 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.5 GiB"},
	}
	for _, tc := range cases {
		got := humanBytes(tc.n)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

func TestNotWiredYet_ReturnsErrorWithTaskRef(t *testing.T) {
	runE := notWiredYet("I-99")
	err := runE(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("notWiredYet RunE should always return an error")
	}
	if !strings.Contains(err.Error(), "I-99") {
		t.Errorf("error should contain task ref 'I-99'; got: %v", err)
	}
}

func TestDefaultProviderConfigDir_UsesHOME(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Setenv("HOME", "/tmp/fake-home")
	defer os.Setenv("HOME", orig)

	got := defaultProviderConfigDir()
	want := "/tmp/fake-home/.config/zen-swarm/providers"
	if got != want {
		t.Errorf("defaultProviderConfigDir() = %q; want %q", got, want)
	}
}

func TestBypassNewClient_ReturnsNonNilClient(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("uds", "/tmp/test.sock", "daemon socket")
	got := bypassNewClient(cmd)
	if got == nil {
		t.Fatal("bypassNewClient should return non-nil client")
	}
}

func TestBypassNewClientWithURL_EmptyURL_UsesUDS(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("uds", "/tmp/test.sock", "daemon socket")
	got := bypassNewClientWithURL(cmd, "")
	if got == nil {
		t.Fatal("bypassNewClientWithURL(empty) should return non-nil client")
	}
}

func TestBypassNewClientWithURL_NonEmptyURL_UsesURL(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("uds", "/tmp/test.sock", "daemon socket")

	got := bypassNewClientWithURL(cmd, "http://127.0.0.1:9999")
	if got == nil {
		t.Fatal("bypassNewClientWithURL(url) should return non-nil client")
	}

	var _ *client.Client = got
}

func TestFindCtldBinary_EnvVarPath(t *testing.T) {

	tmp := filepath.Join(t.TempDir(), "zen-swarm-ctld")
	if err := os.WriteFile(tmp, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}
	t.Setenv("ZEN_SWARM_CTLD", tmp)
	got, err := findCtldBinary()
	if err != nil {
		t.Fatalf("findCtldBinary with env set: %v", err)
	}
	if got != tmp {
		t.Errorf("findCtldBinary = %q; want %q", got, tmp)
	}
}

func TestFindCtldBinary_NotFound(t *testing.T) {

	emptyDir := t.TempDir()
	t.Setenv("ZEN_SWARM_CTLD", "")
	t.Setenv("PATH", emptyDir)
	_, err := findCtldBinary()
	if err == nil {
		t.Fatal("expected error when zen-swarm-ctld not found")
	}
	if !strings.Contains(err.Error(), "zen-swarm-ctld") {
		t.Errorf("error should mention zen-swarm-ctld; got: %v", err)
	}
}

func TestFindInstallScript_CwdCandidate(t *testing.T) {

	tmp := t.TempDir()
	scriptsDir := filepath.Join(tmp, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(scriptsDir, "install-launchd.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got, err := findInstallScript()
	if err != nil {
		t.Fatalf("findInstallScript with cwd candidate: %v", err)
	}
	if !strings.HasSuffix(got, "install-launchd.sh") {
		t.Errorf("findInstallScript = %q; expected path ending in install-launchd.sh", got)
	}
}

func TestFindInstallScript_NotFound(t *testing.T) {

	tmp := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, err = findInstallScript()
	if err == nil {
		t.Fatal("expected error when install-launchd.sh not found")
	}
	if !strings.Contains(err.Error(), "install-launchd.sh") {
		t.Errorf("error should mention install-launchd.sh; got: %v", err)
	}
}

func TestRealEditorRun_MissingEditor(t *testing.T) {
	t.Setenv("VISUAL", "/nonexistent-editor-for-test-do-not-create")
	t.Setenv("EDITOR", "")

	tmp := filepath.Join(t.TempDir(), "dummy.md")
	if err := os.WriteFile(tmp, []byte(""), 0o644); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	err := realEditorRun(tmp)
	if err == nil {
		t.Fatal("realEditorRun with missing editor should return an error")
	}

	if !strings.Contains(err.Error(), "editor") && !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention editor or the missing binary; got: %v", err)
	}
}

func TestBuildBackend_UnknownType(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name:     "test-unknown",
		Type:     "not-a-real-provider-type",
		Endpoint: "http://localhost",
		Model:    "model",
	}
	_, err := buildBackend(cfg)
	if err == nil {
		t.Fatal("buildBackend with unknown type should return error")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("error should contain 'unknown type'; got: %v", err)
	}
}

func TestBuildBackend_OllamaType(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name:     "test-ollama",
		Type:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "llama3.1",
		Family:   "llama",
	}
	backend, err := buildBackend(cfg)
	if err != nil {
		t.Fatalf("buildBackend(ollama): %v", err)
	}
	if backend == nil {
		t.Fatal("buildBackend(ollama) returned nil backend")
	}
	backend.Close()
}

func TestRunToolBinary_DefaultPath_MissingBinary(t *testing.T) {

	emptyDir := t.TempDir()
	t.Setenv("ZEN_DEV_TOOLS", "")
	t.Setenv("PATH", emptyDir)
	err := runToolBinary("capture", "-out", "/dev/null")
	if err == nil {
		t.Fatal("runToolBinary with missing binary should return error")
	}
}

func TestRunToolBinary_DevToolsPath_MissingGo(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("ZEN_DEV_TOOLS", "1")
	t.Setenv("PATH", emptyDir)
	err := runToolBinary("capture")
	if err == nil {
		t.Fatal("ZEN_DEV_TOOLS=1 without go on PATH should error")
	}
	if !strings.Contains(err.Error(), "ZEN_DEV_TOOLS") {
		t.Errorf("error should mention ZEN_DEV_TOOLS; got: %v", err)
	}
}

func TestProductionCochangeClient_CoChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.CoChangeResponse{
			File:  "a.go",
			Peers: []client.CoChangePeerDTO{{Path: "b.go", CouplingPercent: 50}},
		})
	}))
	defer srv.Close()

	c := &productionCochangeClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := c.CoChange(context.Background(), client.CoChangeRequest{File: "a.go"})
	if err != nil {
		t.Fatalf("CoChange: %v", err)
	}
	if resp.File != "a.go" {
		t.Errorf("CoChange resp.File = %q; want a.go", resp.File)
	}
}

func TestProductionWhyClient_Why(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.WhyResponse{
			Subject: "pkg.M",
		})
	}))
	defer srv.Close()

	c := &productionWhyClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := c.Why(context.Background(), client.WhyRequest{Symbol: "pkg.M"})
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if resp.Subject != "pkg.M" {
		t.Errorf("Why resp.Subject = %q; want pkg.M", resp.Subject)
	}
}

func TestProductionRiskClient_Risk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.RiskResponse{
			Level: "low",
			Score: 0.1,
		})
	}))
	defer srv.Close()

	c := &productionRiskClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := c.Risk(context.Background(), client.RiskRequest{ChangedSymbols: []string{"pkg.Sym"}})
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	if resp.Level != "low" {
		t.Errorf("Risk resp.Level = %q; want low", resp.Level)
	}
}
