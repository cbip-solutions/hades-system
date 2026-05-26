package integration_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
)

func sandboxOnboardEnv(t *testing.T, withCCInstall bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hermes"), []byte("#!/bin/sh\necho 'Hermes Agent 0.13.0 present at fake'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if withCCInstall {
		ccDir := filepath.Join(home, ".claude")
		_ = os.MkdirAll(filepath.Join(ccDir, "commands"), 0o755)
		_ = os.WriteFile(filepath.Join(ccDir, "settings.json"), []byte("{}"), 0o644)
		_ = os.WriteFile(filepath.Join(ccDir, "commands", "example.md"), []byte("# example\n"), 0o644)
	}
	return home
}

func runNewIntegration(t *testing.T, args []string) (string, string, int) {
	t.Helper()
	cmd := cli.NewNewCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	code := 0
	if err != nil {
		switch {
		case cli.IsPreflightFailure(err):
			code = 3
		case cli.IsRecoverable(err):
			code = 1
		case errors.Is(err, context.Canceled):
			code = 130
		default:
			code = 2
		}
	}
	return stdout.String(), stderr.String(), code
}

func TestIntegration_NewGreenfield_RecommendedScaffold(t *testing.T) {
	sandboxOnboardEnv(t, false)
	dst := filepath.Join(t.TempDir(), "my-plugin")
	stdout, stderr, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "my-plugin",
		"--path", dst,
		"--yes",
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
	for _, f := range []string{"plugin.yaml", "__init__.py", "README.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("expected %q in scaffold: %v", f, err)
		}
	}
}

func TestIntegration_NewGreenfield_GoCLITemplate(t *testing.T) {
	sandboxOnboardEnv(t, false)

	modCache, err := os.MkdirTemp("", "zen-test-gomodcache-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(modCache) })
	t.Setenv("GOMODCACHE", modCache)
	t.Setenv("GOFLAGS", "-mod=mod")

	dst := filepath.Join(t.TempDir(), "svc")
	stdout, stderr, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "go-cli",
		"--project-name", "svc",
		"--path", dst,
		"--yes",
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
	for _, f := range []string{"go.mod", "main.go", "Makefile"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("expected %q in go-cli scaffold: %v", f, err)
		}
	}
}

func TestIntegration_NewGreenfield_CCDetectedSurfacesMigrateHint(t *testing.T) {
	sandboxOnboardEnv(t, true)
	dst := filepath.Join(t.TempDir(), "x")
	stdout, _, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--yes",
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "~/.claude/") {
		t.Errorf("missing CC-detected hint:\n%s", stdout)
	}
	if !strings.Contains(stdout, "zen migrate") {
		t.Errorf("missing migrate suggestion:\n%s", stdout)
	}
}

func TestIntegration_NewGreenfield_ExistingRepoSkipGitInit(t *testing.T) {
	sandboxOnboardEnv(t, false)
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(parent, "child")
	_, _, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "child",
		"--path", dst,
		"--yes",
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Error("child .git should not exist (parent is repo)")
	}
}

func TestIntegration_NewGreenfield_NonInteractiveMissingFlag(t *testing.T) {
	sandboxOnboardEnv(t, false)
	_, stderr, code := runNewIntegration(t, []string{"--non-interactive"})
	if code != 1 {
		t.Errorf("exit %d, want 1\nSTDERR:\n%s", code, stderr)
	}
}

func TestIntegration_NewGreenfield_ListTemplates(t *testing.T) {
	sandboxOnboardEnv(t, false)
	stdout, _, code := runNewIntegration(t, []string{"--list-templates"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, n := range []string{
		"hermes-plugin-only", "hermes-plugin+daemon",
		"go-cli", "python-cli", "ts-saas", "ml-pipeline",
	} {
		if !strings.Contains(stdout, n) {
			t.Errorf("list-templates missing %q", n)
		}
	}
}

func TestIntegration_NewGreenfield_TargetExistsNonEmpty(t *testing.T) {
	sandboxOnboardEnv(t, false)
	dst := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
	})
	if code != 1 {
		t.Errorf("exit %d, want 1 (target exists)", code)
	}
}

func TestIntegration_NewGreenfield_ForceOverrides(t *testing.T) {
	sandboxOnboardEnv(t, false)
	dst := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, code := runNewIntegration(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--force",
		"--yes",
	})
	if code != 0 {
		t.Errorf("exit %d, want 0 (--force should override)", code)
	}
}
