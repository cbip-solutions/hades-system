// SPDX-License-Identifier: MIT
// Package cli — doctor_coordination.go
//
// Cross-plan coordination checks per design contract
// Currently 1 active check:
// - HADES design
//
// The daemon's coordination probe inspects:
// - internal/knowledge/aggregator/aggregator.go (presence)
//
// RETIRED (v0.20.7, invariant): HADES design (Hermes plugin
// format converted) was retired because the underlying landing test
// (presence of plugin/hades-system/plugin.yaml + Hermes markers) is obsolete
// per ADR-0080. HADES design H' was the deferred Claude-Code-plugin conversion
// path; HADES design replaced that path with the Hermes plugin at
// plugin/hades/ (different canonical location + format). The probe-target
// plugin/hades-system/plugin.yaml never existed at HEAD and always reported
// "fail" — a misleading active signal in doctor output. No Claude-Code
// plugin conversion is planned (design choice substrate decision + ADR-0080 supersede
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
		Short: "Cross-plan coordination checks (HADES design; 1 active check per design contract; HADES design retired in v0.20.7 per invariant)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Coordination (HADES design)", runCoordinationChecks)
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
			probe: "HADES design",
			name:  "HADES design",
			hint:  "HADES design D substrate missing; verify: ls internal/knowledge/aggregator/aggregator.go",
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
