// SPDX-License-Identifier: MIT
// Package cli — doctor_hermes.go
//
// Hermes integration block.
//
// The CLI mirrors the daemon's live /v1/hermes/probe surface exactly: plugin
// payload linkage, active Hermes session registration, and transport
// reachability. Unknown or historical probe names are never mapped to OK.
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
		Short: "Hermes integration checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Hermes integration", runHermesChecks)
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
			probeName:  "plugin_installed",
			resultName: "hermes.plugin.installed",
			hint:       "link plugin: mkdir -p ~/.hermes/plugins && ln -sfn $(brew --prefix hades)/share/hades/hades ~/.hermes/plugins/hades (source checkout: make plugin-install)",
		},
		{
			probeName:  "session_active",
			resultName: "hermes.session.active",
			hint:       "start a Hermes session after installing the HADES plugin",
		},
		{
			probeName:  "transport_reachable",
			resultName: "hermes.transport.reachable",
			hint:       "run hades status; ensure hades-ctld is running",
		},
	}
	out := make([]CheckResult, 0, len(checks))
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
