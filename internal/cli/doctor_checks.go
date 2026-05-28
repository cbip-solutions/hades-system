// SPDX-License-Identifier: MIT
// Package cli — doctor_checks.go.
//
// 10 spec §8.5 doctor checks that probe the daemon over the
// GET /v1/bypass/doctor?check=<name> endpoint and render with a
// remediation hint on warn/fail.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type CheckResult struct {
	Section string `json:"section,omitempty" yaml:"section,omitempty"`
	Name    string `json:"name" yaml:"name"`
	Status  string `json:"status" yaml:"status"`
	Detail  string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Hint    string `json:"hint,omitempty" yaml:"hint,omitempty"`
}

func runBypassChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkBypassCredentialsReadable,
		checkBypassCredentialsFresh,
		checkBypassKeychainAccessible,
		checkBypassConfigValid,
		checkBypassConfigFresh,
		checkBypassCFRangeFresh,
		checkBypassCertValid,
		checkBypassConnectivity,
		checkBypassConfigsRepoReachable,
		checkBypassMitmproxyAvailable,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		results = append(results, fn(cctx, c))
		cancel()
	}
	return results
}

func checkBypassCredentialsReadable(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "credentials.readable")
	return resultFrom("bypass.credentials.readable", r, err,
		"file ~/local agent config/credentials.json must exist with mode 0600")
}

func checkBypassCredentialsFresh(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "credentials.fresh")
	return resultFrom("bypass.credentials.fresh", r, err,
		"OAuth token expired or about to; run: hades bypass refresh-now")
}

func checkBypassKeychainAccessible(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "keychain.accessible")
	return resultFrom("bypass.keychain.accessible", r, err,
		"macOS Keychain unlock failed; check the hades-system-bypass entry in Keychain Access.app")
}

func checkBypassConfigValid(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "config.valid")
	return resultFrom("bypass.config.valid", r, err,
		"bypass-config.json failed schema validation; run: hades bypass update-config --check")
}

func checkBypassConfigFresh(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "config.fresh")
	return resultFrom("bypass.config.fresh", r, err,
		"bypass-config older than 30 days; run: hades bypass update-config")
}

func checkBypassCFRangeFresh(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "cf-range.fresh")
	return resultFrom("bypass.cf-range.fresh", r, err,
		"CF IP range cache stale (>48h); run: hades bypass cf-range --refresh")
}

func checkBypassCertValid(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "cert.valid")
	return resultFrom("bypass.cert.valid", r, err,
		"pinned intermediate cert expired or unreachable; run: hades bypass certs --show")
}

func checkBypassConnectivity(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "connectivity")
	return resultFrom("bypass.connectivity", r, err,
		"cannot reach api.anthropic.com; check DNS / VPN / network")
}

func checkBypassConfigsRepoReachable(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "sidecar config.repo-reachable")
	return resultFrom("bypass.sidecar config.repo-reachable", r, err,
		"local integration config repository unreachable; check authentication status")
}

func checkBypassMitmproxyAvailable(ctx context.Context, c *client.Client) CheckResult {
	r, err := c.BypassDoctor(ctx, "tools.mitmproxy-available")
	if err != nil {

		return CheckResult{
			Name:   "bypass.tools.mitmproxy-available",
			Status: "warn",
			Detail: err.Error(),
			Hint:   "optional; only needed for `hades bypass extract-config`. brew install mitmproxy",
		}
	}
	return resultFrom("bypass.tools.mitmproxy-available", r, nil,
		"optional; only needed for extract-config. brew install mitmproxy")
}

func resultFrom(name string, r *client.BypassDoctorResp, err error, hint string) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}

func renderCheck(r CheckResult) string {
	var glyph string
	switch r.Status {
	case "ok":
		glyph = "ok  "
	case "warn":
		glyph = "warn"
	default:
		glyph = "x   "
	}
	out := fmt.Sprintf("  %s %-40s", glyph, r.Name)
	if r.Detail != "" {
		out += "  " + r.Detail
	}
	if r.Hint != "" {
		out += "\n      hint: " + r.Hint
	}
	return out
}
