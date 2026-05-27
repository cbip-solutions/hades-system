// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type runOptions struct {
	dockerfile  string
	imageRef    string
	smoke       bool
	stdout      writer
	stderr      writer
	execCommand func(name string, args ...string) cmdRunner
}

type writer interface {
	Write(p []byte) (n int, err error)
}

type cmdRunner interface {
	CombinedOutput() ([]byte, error)
	Run() error
}

type realCmd struct{ c *exec.Cmd }

func (r realCmd) CombinedOutput() ([]byte, error) { return r.c.CombinedOutput() }
func (r realCmd) Run() error                      { return r.c.Run() }

func realExec(name string, args ...string) cmdRunner {
	return realCmd{c: exec.Command(name, args...)}
}

func defaultOptions() runOptions {
	return runOptions{
		dockerfile:  "Dockerfile",
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		execCommand: realExec,
	}
}

func main() {
	opts := defaultOptions()
	flag.StringVar(&opts.dockerfile, "dockerfile", opts.dockerfile, "Path to the Dockerfile under validation (default Dockerfile)")
	flag.StringVar(&opts.imageRef, "image", "", "Image reference to inspect / smoke-run (e.g. ghcr.io/cbip-solutions/hades-system:v1.0.0)")
	flag.BoolVar(&opts.smoke, "smoke", false, "Run `docker run --rm IMAGE zen --version` and parse the buildinfo Summary line")
	flag.Parse()

	if err := verify(opts); err != nil {
		fmt.Fprintf(opts.stderr, "verify-docker-image: %v\n", err)
		var cfg *configError
		if errors.As(err, &cfg) {
			os.Exit(2)
		}
		os.Exit(1)
	}
	fmt.Fprintln(opts.stdout, "verify-docker-image: OK (inv-zen-298)")
}

type configError struct{ inner error }

func (e *configError) Error() string { return e.inner.Error() }
func (e *configError) Unwrap() error { return e.inner }

func verify(opts runOptions) error {
	if err := validateDockerfile(opts); err != nil {
		return err
	}
	if opts.imageRef != "" {
		if err := inspectImage(opts); err != nil {
			return err
		}
		if opts.smoke {
			if err := smokeImage(opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDockerfile(opts runOptions) error {
	path := opts.dockerfile
	if !filepath.IsAbs(path) {

		path = filepath.Clean(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &configError{fmt.Errorf("read %s: %w", path, err)}
	}
	text := string(data)
	required := map[string]string{
		"FROM golang:1.26":                              "builder stage must use golang:1.26 base",
		"AS builder":                                    "multi-stage marker missing",
		"FROM gcr.io/distroless/cc-debian12":            "runtime stage must be distroless cc-debian12",
		"COPY --from=builder":                           "no COPY --from=builder line; builder stage outputs not consumed",
		"/usr/local/bin/zen":                            "zen binary not copied to /usr/local/bin",
		"/usr/local/bin/zen-swarm-ctld":                 "zen-swarm-ctld binary not copied to /usr/local/bin",
		"ENTRYPOINT":                                    "no ENTRYPOINT declared",
		"USER nonroot":                                  "no USER nonroot directive (CIS Docker §4.1)",
		`LABEL org.opencontainers.image.licenses="MIT"`: "OCI license label missing (decisión 15)",
		`LABEL org.opencontainers.image.source="https://github.com/cbip-solutions/hades-system"`: "OCI source label missing (canonical hades-system URL)",
	}
	for needle, why := range required {
		if !strings.Contains(text, needle) {
			return fmt.Errorf("Dockerfile %s: %s (expected substring %q)", path, why, needle)
		}
	}

	// ldflags injection — both -X main.* AND -X.../internal/buildinfo.*
	// MUST be present so the produced image's --version output parses.
	for _, ldflag := range []string{
		"-X main.version=",
		"-X main.commit=",
		"-X main.date=",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.version=",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.commit=",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.date=",
	} {
		if !strings.Contains(text, ldflag) {
			return fmt.Errorf("Dockerfile %s: missing ldflag %q (inv-zen-294 + inv-zen-297 cross-gate)", path, ldflag)
		}
	}

	for _, want := range []string{"-trimpath", "-buildid="} {
		if !strings.Contains(text, want) {
			return fmt.Errorf("Dockerfile %s: missing reproducibility flag %q", path, want)
		}
	}
	fmt.Fprintf(opts.stdout, "  OK  Dockerfile %s: multi-stage + distroless + ldflag-injected + non-root\n", path)
	return nil
}

type dockerInspectJSON struct {
	Config struct {
		Labels     map[string]string `json:"Labels"`
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
		User       string            `json:"User"`
	} `json:"Config"`
}

func inspectImage(opts runOptions) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return &configError{fmt.Errorf("docker CLI not in PATH: %w", err)}
	}
	out, err := opts.execCommand("docker", "image", "inspect", opts.imageRef).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image inspect %s: %w (output=%q)", opts.imageRef, err, string(out))
	}
	var arr []dockerInspectJSON
	if jerr := json.Unmarshal(out, &arr); jerr != nil {
		return fmt.Errorf("parse docker inspect JSON: %w", jerr)
	}
	if len(arr) == 0 {
		return fmt.Errorf("docker image inspect %s returned 0 entries", opts.imageRef)
	}
	cfg := arr[0].Config

	wantLabels := map[string]string{
		"org.opencontainers.image.licenses": "MIT",
		"org.opencontainers.image.source":   "https://github.com/cbip-solutions/hades-system",
		"org.opencontainers.image.vendor":   "hades-system",
	}
	for key, want := range wantLabels {
		got, ok := cfg.Labels[key]
		if !ok {
			return fmt.Errorf("image %s missing OCI label %q", opts.imageRef, key)
		}
		if got != want {
			return fmt.Errorf("image %s label %q=%q, want %q", opts.imageRef, key, got, want)
		}
	}
	if len(cfg.Entrypoint) == 0 || !strings.HasSuffix(cfg.Entrypoint[0], "zen-swarm-ctld") {
		return fmt.Errorf("image %s entrypoint=%v, want trailing /usr/local/bin/zen-swarm-ctld", opts.imageRef, cfg.Entrypoint)
	}
	if !strings.Contains(cfg.User, "nonroot") {
		return fmt.Errorf("image %s user=%q, want a nonroot user (CIS §4.1)", opts.imageRef, cfg.User)
	}
	fmt.Fprintf(opts.stdout, "  OK  image %s: OCI labels + entrypoint + nonroot user\n", opts.imageRef)
	return nil
}

func smokeImage(opts runOptions) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return &configError{fmt.Errorf("docker CLI not in PATH: %w", err)}
	}
	out, err := opts.execCommand("docker", "run", "--rm", opts.imageRef, "/usr/local/bin/zen", "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s zen --version: %w (output=%q)", opts.imageRef, err, string(out))
	}

	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "zen-swarm ") &&
			strings.Contains(ln, "commit:") &&
			strings.Contains(ln, "date:") &&
			strings.Contains(ln, "go:") &&
			strings.Contains(ln, "platform:") {
			fmt.Fprintf(opts.stdout, "  OK  smoke %s: buildinfo Summary line parsed: %s\n", opts.imageRef, strings.TrimSpace(ln))
			return nil
		}
	}
	return fmt.Errorf("smoke %s: no buildinfo Summary line in `zen --version` output (output=%q)", opts.imageRef, string(out))
}
