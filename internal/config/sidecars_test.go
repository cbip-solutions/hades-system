// SPDX-License-Identifier: MIT
// internal/config/sidecars_test.go
//
// External-package tests for the Sidecars TOML loader (Plan 15 Phase B-5).
// Covers happy path, missing-file-not-error, malformed TOML, validation rules
// (loopback-only URL, tier=1, interval bounds), and the XDG_CONFIG_HOME-aware
// default path resolution.
package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadSidecarsHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sidecars.toml", `
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
required = false
`)
	cfg, err := config.LoadSidecars(path)
	if err != nil {
		t.Fatalf("LoadSidecars: %v", err)
	}
	if cfg.Tier1Bypass == nil {
		t.Fatal("Tier1Bypass = nil; want populated section")
	}
	if got, want := cfg.Tier1Bypass.URL, "http://127.0.0.1:39823"; got != want {
		t.Errorf("URL = %q; want %q", got, want)
	}
	if got, want := cfg.Tier1Bypass.Tier, 1; got != want {
		t.Errorf("Tier = %d; want %d", got, want)
	}
	if got, want := cfg.Tier1Bypass.HealthProbeIntervalSeconds, 30; got != want {
		t.Errorf("HealthProbeIntervalSeconds = %d; want %d", got, want)
	}
	if got, want := cfg.Tier1Bypass.RequestTimeoutSeconds, 30; got != want {
		t.Errorf("RequestTimeoutSeconds = %d; want %d", got, want)
	}
	if cfg.Tier1Bypass.Required != false {
		t.Errorf("Required = true; want false (default graceful)")
	}
}

func TestLoadSidecarsRequiredTrue(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sidecars.toml", `
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 60
request_timeout_s = 45
required = true
`)
	cfg, err := config.LoadSidecars(path)
	if err != nil {
		t.Fatalf("LoadSidecars: %v", err)
	}
	if !cfg.Tier1Bypass.Required {
		t.Errorf("Required = false; want true")
	}
}

func TestLoadSidecarsMissingFileNotError(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.LoadSidecars(filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatalf("LoadSidecars(absent): err = %v; want nil", err)
	}
	if cfg.Tier1Bypass != nil {
		t.Errorf("Tier1Bypass = %+v; want nil", cfg.Tier1Bypass)
	}
}

func TestLoadSidecarsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sidecars.toml", "")
	cfg, err := config.LoadSidecars(path)
	if err != nil {
		t.Fatalf("LoadSidecars(empty): err = %v; want nil", err)
	}
	if cfg.Tier1Bypass != nil {
		t.Errorf("Tier1Bypass = %+v; want nil", cfg.Tier1Bypass)
	}
}

func TestLoadSidecarsReadErrorPropagates(t *testing.T) {

	dir := t.TempDir()
	sub := filepath.Join(dir, "sidecars.toml")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sub, err)
	}
	_, err := config.LoadSidecars(sub)
	if err == nil {
		t.Fatal("LoadSidecars(dir): err = nil; want non-nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("err is ErrNotExist; want a different read failure: %v", err)
	}
}

func TestLoadSidecarsMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sidecars.toml", `
[tier1.bypass
url = "http://127.0.0.1:39823"
`)
	_, err := config.LoadSidecars(path)
	if err == nil {
		t.Fatal("LoadSidecars: err = nil; want toml syntax error")
	}
	if !strings.Contains(err.Error(), "toml") {
		t.Errorf("err = %v; want it to mention toml", err)
	}
}

func TestLoadSidecarsURLNonLoopbackRejected(t *testing.T) {
	cases := []string{
		"http://192.168.1.10:39823",
		"http://example.com:39823",
		"https://127.0.0.1:39823",
		"http://0.0.0.0:39823",
		"http://10.0.0.5:39823",
		"file:///tmp/socket",
		"http://",
		"127.0.0.1:39823",
		"http://127.0.0.1",
		"http://localhost",
		"http://[::1:39823",
	}
	for _, badURL := range cases {
		t.Run(badURL, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = %q
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
`, badURL))
			_, err := config.LoadSidecars(path)
			if err == nil {
				t.Fatalf("LoadSidecars(%q): err = nil; want loopback validation rejection", badURL)
			}
			if !strings.Contains(err.Error(), "loopback") && !strings.Contains(err.Error(), "url") {
				t.Errorf("err = %v; want it to mention loopback/url", err)
			}
		})
	}
}

func TestLoadSidecarsURLLoopbackAccepted(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:39823",
		"http://localhost:39823",
		"http://127.0.0.1:8080",
		"http://localhost:1",
	}
	for _, goodURL := range cases {
		t.Run(goodURL, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = %q
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
`, goodURL))
			cfg, err := config.LoadSidecars(path)
			if err != nil {
				t.Fatalf("LoadSidecars(%q): err = %v; want nil", goodURL, err)
			}
			if cfg.Tier1Bypass.URL != goodURL {
				t.Errorf("URL = %q; want %q", cfg.Tier1Bypass.URL, goodURL)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// LoadSidecars — validation: tier MUST equal 1.
// ----------------------------------------------------------------------------

func TestLoadSidecarsTierNotOneRejected(t *testing.T) {
	for _, tier := range []int{0, 2, 3, -1, 42} {
		t.Run(fmt.Sprintf("tier=%d", tier), func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = %d
health_probe_interval_s = 30
request_timeout_s = 30
`, tier))
			_, err := config.LoadSidecars(path)
			if err == nil {
				t.Fatalf("LoadSidecars(tier=%d): err = nil; want tier validation rejection", tier)
			}
			if !strings.Contains(err.Error(), "tier") {
				t.Errorf("err = %v; want it to mention tier", err)
			}
		})
	}
}

func TestLoadSidecarsHealthProbeIntervalOutOfRange(t *testing.T) {
	for _, sec := range []int{-1, 0, 4, 3601, 100000} {
		t.Run(fmt.Sprintf("interval=%d", sec), func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = %d
request_timeout_s = 30
`, sec))
			_, err := config.LoadSidecars(path)
			if err == nil {
				t.Fatalf("LoadSidecars(interval=%d): err = nil; want bound rejection", sec)
			}
			if !strings.Contains(err.Error(), "health_probe_interval_s") {
				t.Errorf("err = %v; want it to mention health_probe_interval_s", err)
			}
		})
	}
}

func TestLoadSidecarsHealthProbeIntervalBoundaryAccepted(t *testing.T) {
	for _, sec := range []int{5, 3600} {
		t.Run(fmt.Sprintf("interval=%d", sec), func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = %d
request_timeout_s = 30
`, sec))
			cfg, err := config.LoadSidecars(path)
			if err != nil {
				t.Fatalf("LoadSidecars(interval=%d): err = %v; want nil", sec, err)
			}
			if cfg.Tier1Bypass.HealthProbeIntervalSeconds != sec {
				t.Errorf("HealthProbeIntervalSeconds = %d; want %d", cfg.Tier1Bypass.HealthProbeIntervalSeconds, sec)
			}
		})
	}
}

func TestLoadSidecarsRequestTimeoutOutOfRange(t *testing.T) {
	for _, sec := range []int{-1, 0, 601, 100000} {
		t.Run(fmt.Sprintf("timeout=%d", sec), func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = %d
`, sec))
			_, err := config.LoadSidecars(path)
			if err == nil {
				t.Fatalf("LoadSidecars(timeout=%d): err = nil; want bound rejection", sec)
			}
			if !strings.Contains(err.Error(), "request_timeout_s") {
				t.Errorf("err = %v; want it to mention request_timeout_s", err)
			}
		})
	}
}

func TestLoadSidecarsRequestTimeoutBoundaryAccepted(t *testing.T) {
	for _, sec := range []int{1, 600} {
		t.Run(fmt.Sprintf("timeout=%d", sec), func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "sidecars.toml", fmt.Sprintf(`
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = %d
`, sec))
			cfg, err := config.LoadSidecars(path)
			if err != nil {
				t.Fatalf("LoadSidecars(timeout=%d): err = %v; want nil", sec, err)
			}
			if cfg.Tier1Bypass.RequestTimeoutSeconds != sec {
				t.Errorf("RequestTimeoutSeconds = %d; want %d", cfg.Tier1Bypass.RequestTimeoutSeconds, sec)
			}
		})
	}
}

func TestLoadSidecarsMissingURL(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sidecars.toml", `
[tier1.bypass]
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
`)
	_, err := config.LoadSidecars(path)
	if err == nil {
		t.Fatal("LoadSidecars: err = nil; want missing-url rejection")
	}
	if !strings.Contains(err.Error(), "url") {
		t.Errorf("err = %v; want it to mention url", err)
	}
}

func TestSidecarsPathXDGSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/cfg")
	got := config.SidecarsPath()
	want := "/xdg/cfg/hades/sidecars.toml"
	if got != want {
		t.Errorf("SidecarsPath = %q; want %q", got, want)
	}
}

func TestSidecarsPathXDGUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := config.SidecarsPath()
	want := "/home/test/.config/hades/sidecars.toml"
	if got != want {
		t.Errorf("SidecarsPath = %q; want %q", got, want)
	}
}

func TestSidecarsZeroValueHasNilTier1Bypass(t *testing.T) {
	var cfg config.Sidecars
	if cfg.Tier1Bypass != nil {
		t.Errorf("zero-value Sidecars.Tier1Bypass = %+v; want nil", cfg.Tier1Bypass)
	}
}
