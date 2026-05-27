// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func osexecLookPath(name string) (string, error) {
	return osexec.LookPath(name)
}

func repoRootForTest(t *testing.T) string {
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

func TestValidateDockerfile_InTree(t *testing.T) {
	root := repoRootForTest(t)
	opts := defaultOptions()
	opts.dockerfile = filepath.Join(root, "Dockerfile")
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	if err := validateDockerfile(opts); err != nil {
		t.Fatalf("validateDockerfile in-tree failed: %v\nlog:\n%s", err, buf.String())
	}
}

func writeFixtureDockerfile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

var validBody = `FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY . .
RUN go build -trimpath -ldflags="-buildid= -X main.version=dev -X main.commit=unknown -X main.date=unknown -X github.com/cbip-solutions/hades-system/internal/buildinfo.version=dev -X github.com/cbip-solutions/hades-system/internal/buildinfo.commit=unknown -X github.com/cbip-solutions/hades-system/internal/buildinfo.date=unknown" -o /out/zen ./cmd/zen
RUN go build -o /out/zen-swarm-ctld ./cmd/zen-swarm-ctld

FROM gcr.io/distroless/cc-debian12:latest
LABEL org.opencontainers.image.source="https://github.com/cbip-solutions/hades-system"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="hades-system"
COPY --from=builder /out/zen /usr/local/bin/zen
COPY --from=builder /out/zen-swarm-ctld /usr/local/bin/zen-swarm-ctld
ENTRYPOINT ["/usr/local/bin/zen-swarm-ctld"]
USER nonroot:nonroot
`

func TestValidateDockerfile_FixtureValid(t *testing.T) {
	path := writeFixtureDockerfile(t, validBody)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	if err := validateDockerfile(opts); err != nil {
		t.Fatalf("fixture-valid Dockerfile rejected: %v", err)
	}
}

func TestValidateDockerfile_MissingMultiStage(t *testing.T) {
	body := strings.Replace(validBody, "AS builder", "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without AS builder")
	}
	if !strings.Contains(err.Error(), "multi-stage marker") {
		t.Errorf("error=%q; want substring 'multi-stage marker'", err.Error())
	}
}

func TestValidateDockerfile_MissingDistroless(t *testing.T) {
	body := strings.Replace(validBody, "FROM gcr.io/distroless/cc-debian12:latest", "FROM debian:12", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without distroless runtime")
	}
	if !strings.Contains(err.Error(), "distroless") {
		t.Errorf("error=%q; want substring 'distroless'", err.Error())
	}
}

func TestValidateDockerfile_MissingMITLabel(t *testing.T) {
	body := strings.Replace(validBody, `LABEL org.opencontainers.image.licenses="MIT"`, "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without MIT license label")
	}
	if !strings.Contains(err.Error(), "license") {
		t.Errorf("error=%q; want substring 'license'", err.Error())
	}
}

func TestValidateDockerfile_MissingBuildinfoLDFlag(t *testing.T) {
	body := strings.Replace(validBody, "-X github.com/cbip-solutions/hades-system/internal/buildinfo.version=dev", "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without buildinfo.version ldflag")
	}
	if !strings.Contains(err.Error(), "buildinfo.version") {
		t.Errorf("error=%q; want substring 'buildinfo.version'", err.Error())
	}
}

func TestValidateDockerfile_MissingMainLDFlag(t *testing.T) {
	body := strings.Replace(validBody, "-X main.version=dev", "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without main.version ldflag")
	}
}

func TestValidateDockerfile_MissingUSERnonroot(t *testing.T) {
	body := strings.Replace(validBody, "USER nonroot:nonroot", "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without USER nonroot")
	}
	if !strings.Contains(err.Error(), "USER nonroot") {
		t.Errorf("error=%q; want substring 'USER nonroot'", err.Error())
	}
}

func TestValidateDockerfile_MissingTrimpath(t *testing.T) {
	body := strings.Replace(validBody, "-trimpath", "", 1)
	path := writeFixtureDockerfile(t, body)
	opts := defaultOptions()
	opts.dockerfile = path
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted Dockerfile without -trimpath")
	}
	if !strings.Contains(err.Error(), "-trimpath") {
		t.Errorf("error=%q; want substring '-trimpath'", err.Error())
	}
}

func TestValidateDockerfile_NotFound(t *testing.T) {
	opts := defaultOptions()
	opts.dockerfile = filepath.Join(t.TempDir(), "non-existent-Dockerfile")
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := validateDockerfile(opts)
	if err == nil {
		t.Fatal("validateDockerfile accepted nonexistent path")
	}
	var cfg *configError
	if !errors.As(err, &cfg) {
		t.Errorf("err=%v not a *configError; want exit-code-2 classification", err)
	}
}

type rememberRunner struct {
	out []byte
	err error
}

func (r *rememberRunner) CombinedOutput() ([]byte, error) { return r.out, r.err }
func (r *rememberRunner) Run() error                      { return r.err }

func TestInspectImage_HappyPath(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	const inspectJSON = `[{
		"Config": {
			"Labels": {
				"org.opencontainers.image.licenses": "MIT",
				"org.opencontainers.image.source": "https://github.com/cbip-solutions/hades-system",
				"org.opencontainers.image.vendor": "hades-system"
			},
			"Entrypoint": ["/usr/local/bin/zen-swarm-ctld"],
			"Cmd": ["--version"],
			"User": "nonroot:nonroot"
		}
	}]`
	opts := defaultOptions()
	opts.imageRef = "ghcr.io/cbip-solutions/hades-system:test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(inspectJSON)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	if err := inspectImage(opts); err != nil {
		t.Fatalf("inspectImage happy-path: %v\nlog:\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "OK  image") {
		t.Errorf("inspectImage stdout missing OK line:\n%s", buf.String())
	}
}

func lookPath(name string) (string, error) {
	p, err := osexecLookPath(name)
	if err != nil {
		return "", fmt.Errorf("LookPath %s: %w", name, err)
	}
	return p, nil
}

func TestInspectImage_LabelMissing(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	const inspectJSON = `[{
		"Config": {
			"Labels": {
				"org.opencontainers.image.licenses": "Apache-2.0",
				"org.opencontainers.image.source": "https://github.com/cbip-solutions/hades-system",
				"org.opencontainers.image.vendor": "hades-system"
			},
			"Entrypoint": ["/usr/local/bin/zen-swarm-ctld"],
			"User": "nonroot:nonroot"
		}
	}]`
	opts := defaultOptions()
	opts.imageRef = "ghcr.io/cbip-solutions/hades-system:test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(inspectJSON)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := inspectImage(opts)
	if err == nil {
		t.Fatal("inspectImage accepted Apache-2.0 license; expected MIT enforcement")
	}
}

func TestInspectImage_EntrypointWrong(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	const inspectJSON = `[{
		"Config": {
			"Labels": {
				"org.opencontainers.image.licenses": "MIT",
				"org.opencontainers.image.source": "https://github.com/cbip-solutions/hades-system",
				"org.opencontainers.image.vendor": "hades-system"
			},
			"Entrypoint": ["/bin/sh"],
			"User": "nonroot:nonroot"
		}
	}]`
	opts := defaultOptions()
	opts.imageRef = "test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(inspectJSON)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := inspectImage(opts)
	if err == nil {
		t.Fatal("inspectImage accepted /bin/sh entrypoint")
	}
}

func TestInspectImage_UserNotNonroot(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	const inspectJSON = `[{
		"Config": {
			"Labels": {
				"org.opencontainers.image.licenses": "MIT",
				"org.opencontainers.image.source": "https://github.com/cbip-solutions/hades-system",
				"org.opencontainers.image.vendor": "hades-system"
			},
			"Entrypoint": ["/usr/local/bin/zen-swarm-ctld"],
			"User": "root"
		}
	}]`
	opts := defaultOptions()
	opts.imageRef = "test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(inspectJSON)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := inspectImage(opts)
	if err == nil {
		t.Fatal("inspectImage accepted root user; want nonroot enforcement")
	}
}

func TestSmokeImage_HappyPath(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	out := "HADES system v1.0.0 (binary: zen)\nzen-swarm v1.0.0 commit:abc1234 date:2026-05-25T10:00:00Z go:1.25.6 platform:linux/amd64\n"
	opts := defaultOptions()
	opts.imageRef = "ghcr.io/cbip-solutions/hades-system:test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(out)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	if err := smokeImage(opts); err != nil {
		t.Fatalf("smokeImage happy-path: %v\nlog:\n%s", err, buf.String())
	}
}

func TestSmokeImage_NoSummaryLine(t *testing.T) {
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("docker CLI not in PATH: %v", err)
	}
	out := "HADES system v1.0.0 (binary: zen)\n"
	opts := defaultOptions()
	opts.imageRef = "test"
	opts.execCommand = func(name string, args ...string) cmdRunner {
		return &rememberRunner{out: []byte(out)}
	}
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := smokeImage(opts)
	if err == nil {
		t.Fatal("smokeImage accepted output without Summary line")
	}
}
