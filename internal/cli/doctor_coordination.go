// SPDX-License-Identifier: MIT
// Package cli — doctor_coordination.go
//
// Cross-plan coordination checks per spec §7.4 Coordination block.
// Currently 1 active check:
//   - plan-9-d.aggregator.db-substrate-available
//
// The daemon's coordination probe inspects:
//   - internal/knowledge/aggregator/aggregator.go (presence)
//
// RETIRED (v0.20.7, inv-zen-290): plan-1-h-prime.executed (Hermes plugin
// format converted) was retired because the underlying landing test
// (presence of plugin/zen-swarm/plugin.yaml + Hermes markers) is obsolete
// per ADR-0080. Plan 1 H' was the deferred Claude-Code-plugin conversion
// path; Plan 18b replaced that path with the Hermes plugin at
// plugin/hades/ (different canonical location + format). The probe-target
// plugin/zen-swarm/plugin.yaml never existed at HEAD and always reported
// "fail" — a misleading active signal in doctor output. No Claude-Code
// plugin conversion is planned (Q1 substrate decision + ADR-0080 supersede
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const coordinationProbeTimeout = 3 * time.Second

type CoordinationProber interface {
	CoordinationProbe(ctx context.Context, check string) (*client.CoordinationProbeResp, error)
}

func NewDoctorCoordinationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "coordination",
		Short: "Cross-plan coordination checks (Plan 11; 1 active check per spec §7.4; plan-1-h-prime.executed retired in v0.20.7 per inv-zen-290)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Coordination (Plan 11)", runCoordinationChecks)
		},
	}
}

func runCoordinationChecks(ctx context.Context, c *client.Client) []CheckResult {
	return runCoordinationChecksWith(ctx, c)
}

func runCoordinationChecksWith(ctx context.Context, p CoordinationProber) []CheckResult {
	checks := []struct {
		probe string
		name  string
		hint  string
	}{
		{
			probe: "plan-9-d-substrate",
			name:  "plan-9-d.aggregator.db-substrate-available",
			hint:  "Plan 9 D substrate missing; verify: ls internal/knowledge/aggregator/aggregator.go",
		},
	}
	out := make([]CheckResult, 0, 1)
	for _, ch := range checks {
		cctx, cancel := context.WithTimeout(ctx, coordinationProbeTimeout)
		r, err := p.CoordinationProbe(cctx, ch.probe)
		cancel()
		out = append(out, coordinationResultFrom(ch.name, r, err, ch.hint))
	}
	return out
}

func coordinationResultFrom(name string, r *client.CoordinationProbeResp, err error, hint string) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}
