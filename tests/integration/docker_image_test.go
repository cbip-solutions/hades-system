// SPDX-License-Identifier: MIT

// Package integration_test — Plan 15 Phase D-9 Docker image build
// integration. Exercises `docker buildx build` against the in-tree
// Dockerfile when docker is available; otherwise skips cleanly.
//
//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootForDocker(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatalf("repo root not found")
	return ""
}

func TestDockerfileExists(t *testing.T) {
	root := repoRootForDocker(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile")); err != nil {
		t.Fatalf("Dockerfile not present at repo root: %v", err)
	}
}

func TestDockerignoreExists(t *testing.T) {
	root := repoRootForDocker(t)
	if _, err := os.Stat(filepath.Join(root, ".dockerignore")); err != nil {
		t.Fatalf(".dockerignore not present at repo root: %v", err)
	}
}

func TestDockerfileMultiStage(t *testing.T) {
	root := repoRootForDocker(t)
	data, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(data)
	required := []string{
		"FROM golang:1.25",
		"AS builder",
		"FROM gcr.io/distroless/cc-debian12",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("Dockerfile missing required substring: %q", want)
		}
	}
}

func TestDockerfileBuildSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short mode")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not in PATH: %v", err)
	}

	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker daemon not running: %v\noutput=%s", err, out)
	}
	root := repoRootForDocker(t)
	tag := "zen-swarm:integration-test"

	cmd := exec.Command("docker", "buildx", "build",
		"--load",
		"--tag", tag,
		"--build-arg", "VERSION=v9.9.9-test",
		"--build-arg", "COMMIT=integration",
		"--build-arg", "DATE=2026-05-25T00:00:00Z",
		"-f", "Dockerfile",
		".",
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker buildx build: %v\noutput:\n%s", err, out)
	}

	runOut, err := exec.Command("docker", "run", "--rm", tag, "/usr/local/bin/zen", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s --version: %v\noutput:\n%s", tag, err, runOut)
	}
	got := string(runOut)
	if !strings.Contains(got, "HADES system v") {
		t.Errorf("docker-run --version missing HADES brand line:\n%s", got)
	}
	if !strings.Contains(got, "zen-swarm v9.9.9-test commit:integration") {
		t.Errorf("docker-run --version missing expected buildinfo summary:\n%s", got)
	}
}

func TestDockerfileImageSize(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not in PATH: %v", err)
	}
	tag := "zen-swarm:integration-test"
	out, err := exec.Command("docker", "image", "inspect", tag, "--format", "{{.Size}}").CombinedOutput()
	if err != nil {
		t.Skipf("image not built; run TestDockerfileBuildSnapshot first: %v", err)
	}
	sizeStr := strings.TrimSpace(string(out))
	if sizeStr == "" {
		t.Skip("no size reported")
	}

	var size int64
	if _, err := scanInt64(sizeStr, &size); err != nil {
		t.Fatalf("parse size %q: %v", sizeStr, err)
	}
	const cap = 250 * 1024 * 1024
	if size > cap {
		t.Errorf("Docker image size %d bytes exceeds %d-byte cap (distroless target ~50-150 MB)", size, cap)
	}
}

func scanInt64(s string, out *int64) (int, error) {

	*out = 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errScanInt
		}
		*out = (*out)*10 + int64(r-'0')
	}
	return len(s), nil
}

var errScanInt = errInt("scanInt64: non-digit byte")

type errInt string

func (e errInt) Error() string { return string(e) }
