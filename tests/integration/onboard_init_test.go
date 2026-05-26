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

func runInitIntegration(t *testing.T, args []string, cwd string) (string, string, int) {
	t.Helper()
	if cwd != "" {
		prev, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(prev) })
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("chdir %q: %v", cwd, err)
		}
	}
	cmd := cli.NewInitCmd()
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

func TestIntegration_InitBrownfield_GoProjectRecognized(t *testing.T) {
	sandboxOnboardEnv(t, false)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/x\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInitIntegration(t, []string{
		"--non-interactive", "--accept-inference", "--yes",
	}, repo)
	if code != 0 {
		t.Fatalf("exit %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, "Go") && !strings.Contains(stdout, "go") {
		t.Errorf("missing Go inference output:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".zen", "config.toml")); err != nil {
		t.Errorf("expected .zen/config.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".zen", "scaffold.toml")); err != nil {
		t.Errorf("expected .zen/scaffold.toml: %v", err)
	}
}

func TestIntegration_InitBrownfield_MonorepoWalkUp(t *testing.T) {
	sandboxOnboardEnv(t, false)
	root := t.TempDir()
	web := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"name":"web","dependencies":{"next":"^16"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, code := runInitIntegration(t, []string{
		"--non-interactive", "--accept-inference", "--yes",
	}, web)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, ".zen", "config.toml")); err != nil {
		t.Errorf("expected .zen at workspace root %q: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(web, ".zen", "config.toml")); err == nil {
		t.Errorf("unexpected .zen at apps/web (should be at root)")
	}
}

func TestIntegration_InitBrownfield_AdditiveOnly(t *testing.T) {
	sandboxOnboardEnv(t, false)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/x\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainContent := []byte("package main\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), mainContent, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, code := runInitIntegration(t, []string{
		"--non-interactive", "--accept-inference", "--yes",
	}, repo)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	after, _ := os.ReadFile(filepath.Join(repo, "main.go"))
	if string(after) != string(mainContent) {
		t.Error("main.go modified by init")
	}
}

func TestIntegration_InitBrownfield_ConflictRecoverable(t *testing.T) {
	sandboxOnboardEnv(t, false)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/x\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".zen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".zen", "config.toml"), []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, code := runInitIntegration(t, []string{
		"--non-interactive", "--accept-inference",
	}, repo)
	if code != 1 {
		t.Errorf("exit %d, want 1 (conflict)", code)
	}
}

func TestIntegration_InitBrownfield_NonInteractiveMissingAccept(t *testing.T) {
	sandboxOnboardEnv(t, false)
	repo := t.TempDir()
	_, _, code := runInitIntegration(t, []string{"--non-interactive"}, repo)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}
