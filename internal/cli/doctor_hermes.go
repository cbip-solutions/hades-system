// SPDX-License-Identifier: MIT
// Package cli — doctor_hermes.go
//
// §7.4 Hermes integration block.
//
// Q5=A hard required posture: hermes.installed surfaces as `fail` when
// the binary is absent (NOT `warn`); aggregate `hades doctor` exits
// non-zero. The hint points at brew install hermes-agent.
//
// Probe pattern mirrors doctor_checks.go::runBypassChecks: 4 probe
// helpers, each with a 3s timeout, dispatched to the same /v1/*/probe
// shape the daemon exposes. The HermesProber interface is the seam
// tests inject.
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const hermesProbeTimeout = 3 * time.Second

type HermesProber interface {
	HermesProbe(ctx context.Context, check string) (*client.HermesProbeResp, error)
}

func NewDoctorHermesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hermes",
		Short: "Hermes integration checks (Plan 11; 4 checks per spec §7.4)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Hermes integration (Plan 11)", runHermesChecks)
		},
	}
	return cmd
}

func runHermesChecks(ctx context.Context, c *client.Client) []CheckResult {
	return runHermesChecksWith(ctx, c)
}

func runHermesChecksWith(ctx context.Context, p HermesProber) []CheckResult {
	checks := []struct {
		probeName  string
		resultName string
		hint       string
	}{
		{
			probeName:  "installed",
			resultName: "hermes.installed",
			hint:       "brew install hermes-agent (Q5=A hard required dependency; daemon refuses bootstrap without)",
		},
		{
			probeName:  "plugin-hades-system-loaded",
			resultName: "hermes.plugin-hades-system-loaded",
			hint:       "verify plugin/hades-system/plugin.yaml exists; run: hades migrate hermes (Plan 13)",
		},
		{
			probeName:  "config-mcp-reachable",
			resultName: "hermes.config.mcp_servers.hades-system-reachable",
			hint:       "check ~/.hermes/config.yaml mcp_servers.hades-system.url; run: hades daemon status",
		},
		{
			probeName:  "curator-last-run",
			resultName: "hermes.curator.last-run",
			hint:       "Hermes Curator hasn't graded skills recently; run: hermes curator run",
		},
	}
	out := make([]CheckResult, 0, 4)
	for _, ch := range checks {
		cctx, cancel := context.WithTimeout(ctx, hermesProbeTimeout)
		r, err := p.HermesProbe(cctx, ch.probeName)
		cancel()
		out = append(out, hermesResultFrom(ch.resultName, r, err, ch.hint))
	}
	return out
}

func hermesResultFrom(name string, r *client.HermesProbeResp, err error, hint string) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}
