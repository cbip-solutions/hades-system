package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestZenPrevBinaryBuilds(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}

func TestZenPrevRejectsWhenNotInstalled(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "zen-prev")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "doctor")
	cmd.Env = append(os.Environ(),
		"ZEN_PREV_TARGET_PATH="+filepath.Join(tmp, "does-not-exist"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit; out=%s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %v want 2; out=%s", err, out)
	}
	if !strings.Contains(string(out), "not installed") {
		t.Errorf("stderr should mention 'not installed': %s", out)
	}
}

func TestZenPrevRoutesArgsToTarget(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fake-prev-bin")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho \"target:$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(tmp, "zen-prev")
	build := exec.Command("go", "build", "-o", wrapper, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(wrapper, "doctor", "--quick")
	cmd.Env = append(os.Environ(), "ZEN_PREV_TARGET_PATH="+target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := string(out); got != "target:doctor --quick\n" {
		t.Fatalf("stdout drift: %q", got)
	}
}

func TestZenPrevPropagatesTargetExitCode(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fake-prev-bin")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(tmp, "zen-prev")
	build := exec.Command("go", "build", "-o", wrapper, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(wrapper, "any-arg")
	cmd.Env = append(os.Environ(), "ZEN_PREV_TARGET_PATH="+target)
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %v want 7", err)
	}
}

func TestResolveTarget_ExplicitOverride(t *testing.T) {
	t.Setenv("ZEN_PREV_TARGET_PATH", "/explicit/zen-prev-target")
	t.Setenv("XDG_DATA_HOME", "/should-not-be-used")
	got, err := resolveTarget()
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got != "/explicit/zen-prev-target" {
		t.Errorf("got %q want %q", got, "/explicit/zen-prev-target")
	}
}

func TestResolveTarget_XDG(t *testing.T) {
	t.Setenv("ZEN_PREV_TARGET_PATH", "")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	got, err := resolveTarget()
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	want := filepath.Join("/xdg/data", "zen-swarm", "prev", "zen")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolveTarget_HomeFallback(t *testing.T) {
	t.Setenv("ZEN_PREV_TARGET_PATH", "")
	t.Setenv("XDG_DATA_HOME", "")
	got, err := resolveTarget()
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".local", "share", "zen-swarm", "prev", "zen")) {
		t.Errorf("expected path under .local/share/zen-swarm/prev/zen, got %q", got)
	}
}

func TestRun_ResolveError(t *testing.T) {

	t.Setenv("ZEN_PREV_TARGET_PATH", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	rc := run(nil)
	if rc != 2 {
		t.Errorf("rc = %d want 2", rc)
	}
}

func TestRun_NotInstalled(t *testing.T) {
	t.Setenv("ZEN_PREV_TARGET_PATH", filepath.Join(t.TempDir(), "missing"))
	if rc := run([]string{"x"}); rc != 2 {
		t.Errorf("rc = %d want 2", rc)
	}
}

func TestRun_TargetSucceeds(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "ok")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_PREV_TARGET_PATH", target)
	if rc := run([]string{"x"}); rc != 0 {
		t.Errorf("rc = %d want 0", rc)
	}
}

func TestRun_TargetFails(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fail")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_PREV_TARGET_PATH", target)
	if rc := run([]string{"x"}); rc != 9 {
		t.Errorf("rc = %d want 9", rc)
	}
}

func TestRun_TargetNonExecutable(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "no-shebang")

	if err := os.WriteFile(target, []byte("not a script"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_PREV_TARGET_PATH", target)
	if rc := run(nil); rc != 2 {
		t.Errorf("rc = %d want 2", rc)
	}
}
