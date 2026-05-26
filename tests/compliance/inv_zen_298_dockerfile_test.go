// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootInvZen298(t *testing.T) string {
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

func TestInvZen298_DockerfilePresent(t *testing.T) {
	root := repoRootInvZen298(t)
	paths := []string{
		"Dockerfile",
		".dockerignore",
		"cmd/verify-docker-image/main.go",
		"cmd/verify-docker-image/main_test.go",
		"scripts/release-gates/verify_docker_image_signed.sh",
	}
	for _, p := range paths {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("inv-zen-298 VIOLATED: %s missing: %v", p, err)
		}
	}
	scriptInfo, err := os.Stat(filepath.Join(root, "scripts/release-gates/verify_docker_image_signed.sh"))
	if err == nil && scriptInfo.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-298 VIOLATED: verify_docker_image_signed.sh not executable (mode %v)", scriptInfo.Mode())
	}
}

func TestInvZen298_DockerfileStructure(t *testing.T) {
	root := repoRootInvZen298(t)
	data, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatalf("inv-zen-298 VIOLATED: read Dockerfile: %v", err)
	}
	text := string(data)
	required := map[string]string{
		"FROM golang:1.25":                              "builder must use golang:1.25 base",
		"AS builder":                                    "multi-stage marker missing",
		"FROM gcr.io/distroless/cc-debian12":            "runtime must be distroless cc-debian12",
		"COPY --from=builder":                           "no COPY --from=builder; builder outputs not consumed",
		"/usr/local/bin/zen":                            "zen binary not copied to /usr/local/bin",
		"/usr/local/bin/zen-swarm-ctld":                 "zen-swarm-ctld binary not copied to /usr/local/bin",
		"ENTRYPOINT":                                    "no ENTRYPOINT declared",
		"USER nonroot":                                  "missing USER nonroot directive (CIS §4.1)",
		`LABEL org.opencontainers.image.licenses="MIT"`: "OCI license label missing (decisión 15)",
		`LABEL org.opencontainers.image.source="https://github.com/cbip-solutions/hades-system"`: "OCI source label missing (canonical hades-system)",
		`LABEL org.opencontainers.image.vendor="hades-system"`:                                   "OCI vendor label missing",
		"-trimpath":        "missing -trimpath reproducibility flag",
		"-buildid=":        "missing -buildid= reproducibility flag",
		"-X main.version=": "missing -X main.version= (inv-zen-294 ldflag shape)",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.version=": "missing -X internal/buildinfo.version= (inv-zen-297)",
	}
	for needle, why := range required {
		if !strings.Contains(text, needle) {
			t.Errorf("inv-zen-298 VIOLATED: Dockerfile %s (expected substring %q)", why, needle)
		}
	}
}

func TestInvZen298_DockerignoreExclusions(t *testing.T) {
	root := repoRootInvZen298(t)
	data, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatalf("inv-zen-298 VIOLATED: read .dockerignore: %v", err)
	}
	text := string(data)
	required := []string{
		"bin/",
		"dist/",
		".git/",
		"tests/integration/fixtures/",
		"docs/",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-298 VIOLATED: .dockerignore missing exclusion: %q", want)
		}
	}
}

func TestInvZen298_ReleaseWorkflowDockerJob(t *testing.T) {
	root := repoRootInvZen298(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("inv-zen-298 VIOLATED: read release.yml: %v", err)
	}
	text := string(data)
	required := []string{
		"linux/amd64,linux/arm64",
		"ghcr.io/cbip-solutions/hades-system",
		"docker/build-push-action@v6",
		"docker/setup-buildx-action@v3",
		"docker/setup-qemu-action@v3",
		"docker/login-action@v3",
		"cosign sign",
		"actions/attest-build-provenance@v2",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-298 VIOLATED: release.yml docker job missing %q", want)
		}
	}
}

func TestInvZen298_VerifyDockerImageSignedScript(t *testing.T) {
	root := repoRootInvZen298(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "release-gates", "verify_docker_image_signed.sh"))
	if err != nil {
		t.Fatalf("inv-zen-298 VIOLATED: read verify_docker_image_signed.sh: %v", err)
	}
	text := string(data)
	required := []string{
		"cosign verify",
		"certificate-identity-regexp",
		"cbip-solutions/hades-system/",
		"release.yml",
		"token.actions.githubusercontent.com",
		"gh attestation verify",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-298 VIOLATED: verify_docker_image_signed.sh missing token %q", want)
		}
	}
}
