// SPDX-License-Identifier: MIT
// internal/config/sidecars.go
//
// LoadSidecars reads ~/.config/hades/sidecars.toml (or any caller-supplied
// path) into a Sidecars struct. sidecars.toml is the daemon's discovery file
// for declaring zero or more sidecar HTTP backends; the file ships the
// [tier1.bypass] consumer only, with the schema open for future tier sidecars.
//
// The daemon's RegisterSidecars (dispatcheradapter.RegisterSidecars) consumes
// the loaded Sidecars value and registers a SidecarBackend by name into
// providers.Registry when the sidecar's /health probe succeeds. A missing
// sidecars.toml is a NORMAL state — the daemon falls through to the
// providers.toml cascade. invariant graceful degradation is preserved by
// the dispatcher: a SidecarBackend that returns ErrSidecarUnavailable causes
// the cascade to proceed to the next named provider.
//
// invariant boundary: this file does NOT import internal/store; it is a
// pure configuration loader. The companion XDGConfigDir helper lives in
// internal/onboard/plugin/ — reused here to keep the XDG-resolution rule
// in one place.
//
// Validation rules (fail-fast at load):
//
// - url MUST start with "http://127.0.0.1:" or "http://localhost:"
// (loopback only; defense-in-depth vs operator mis-config pointing at
// a remote host; mirrors the sidecar's bind discipline).
// - tier MUST equal 1 for the [tier1.bypass] table (table-name encodes
// the tier number; mismatch would silently mis-route).
// - health_probe_interval_s MUST be in [5, 3600] seconds (5s lower bound
// prevents probe-storm; 3600s upper bound prevents a never-probing
// sidecar masquerading as healthy).
// - request_timeout_s MUST be in [1, 600] seconds (1s lower bound is
// useful for ChaosNet smoke tests; 600s upper bound caps any single
// stuck request below 10 minutes).
// - required is optional bool (default false → graceful degrade allowed).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/onboard/plugin"
)

type Sidecars struct {
	Tier1Bypass *Tier1BypassSidecar `toml:"-"`
}

type Tier1BypassSidecar struct {
	// URL is the sidecar's HTTP base URL (no trailing slash). MUST be
	// loopback (http://127.0.0.1:PORT or http://localhost:PORT).
	URL string `toml:"url"`
	// Tier MUST equal 1 (table-name encodes tier; drift would silently
	// mis-route).
	Tier int `toml:"tier"`

	HealthProbeIntervalSeconds int `toml:"health_probe_interval_s"`

	RequestTimeoutSeconds int `toml:"request_timeout_s"`

	Required bool `toml:"required"`
}

type sidecarsConfigFile struct {
	Tier1 *tier1Group `toml:"tier1"`
}

type tier1Group struct {
	Bypass *Tier1BypassSidecar `toml:"bypass"`
}

func LoadSidecars(path string) (Sidecars, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {

			return Sidecars{}, nil
		}
		return Sidecars{}, fmt.Errorf("config.LoadSidecars(%s): %w", path, err)
	}
	var doc sidecarsConfigFile
	if err := toml.Unmarshal(body, &doc); err != nil {
		return Sidecars{}, fmt.Errorf("config.LoadSidecars(%s): toml: %w", path, err)
	}
	out := Sidecars{}
	if doc.Tier1 != nil && doc.Tier1.Bypass != nil {
		if err := validateTier1Bypass(doc.Tier1.Bypass); err != nil {
			return Sidecars{}, fmt.Errorf("config.LoadSidecars(%s): %w", path, err)
		}
		out.Tier1Bypass = doc.Tier1.Bypass
	}
	return out, nil
}

func validateTier1Bypass(c *Tier1BypassSidecar) error {
	if c.URL == "" {
		return errors.New("sidecars.tier1.bypass: url is empty (required)")
	}
	if err := validateLoopbackURL(c.URL); err != nil {
		return fmt.Errorf("sidecars.tier1.bypass: %w", err)
	}
	if c.Tier != 1 {
		return fmt.Errorf("sidecars.tier1.bypass: tier = %d; must equal 1 (table name encodes tier)", c.Tier)
	}
	if c.HealthProbeIntervalSeconds < 5 || c.HealthProbeIntervalSeconds > 3600 {
		return fmt.Errorf("sidecars.tier1.bypass: health_probe_interval_s = %d; must be in [5, 3600]", c.HealthProbeIntervalSeconds)
	}
	if c.RequestTimeoutSeconds < 1 || c.RequestTimeoutSeconds > 600 {
		return fmt.Errorf("sidecars.tier1.bypass: request_timeout_s = %d; must be in [1, 600]", c.RequestTimeoutSeconds)
	}
	return nil
}

// validateLoopbackURL accepts http://127.0.0.1:PORT and http://localhost:PORT
// only. https is rejected (loopback HTTP only — TLS adds no security over
// loopback while complicating the operator's startup). All-interfaces
// (0.0.0.0) and routable IPs are rejected.
func validateLoopbackURL(raw string) error {

	if !strings.HasPrefix(raw, "http://") {
		return fmt.Errorf("url %q: must use http:// scheme on loopback (https / file / other rejected)", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url %q: parse: %w", raw, err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("url %q: must use http:// scheme on loopback", raw)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url %q: missing host (must be 127.0.0.1 or localhost)", raw)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("url %q: host %q is not loopback (only 127.0.0.1 or localhost accepted; defense-in-depth vs remote mis-config)", raw, host)
	}
	if u.Port() == "" {
		return fmt.Errorf("url %q: missing port (loopback HTTP requires :PORT)", raw)
	}
	return nil
}

func SidecarsPath() string {
	return filepath.Join(plugin.XDGConfigDir("hades"), "sidecars.toml")
}
