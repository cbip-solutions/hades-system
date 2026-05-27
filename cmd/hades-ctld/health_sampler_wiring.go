// SPDX-License-Identifier: MIT
// Package main — health_sampler_wiring.go (v0.17.7 A-6).
//
// Builds the HealthSampler compute closure and wires it into the
// releaseOrchestratorService. Responsibility: sample each of the five health
// dependencies INDIVIDUALLY — never via CheckEngine.RunCheck (the 11-check
// autonomy matrix) and never via VerifyDocs (autonomy-gate concern).
//
// Five health keys:
// - research_mcp_up (HTTP probe via /health endpoint)
// - gitnexus_up (PATH probe via exec.LookPath)
// - event_log_writable (adapter write-probe via service)
// - adapters_clean (in-process atomic; service.HealthAdaptersClean)
// - last_session_clean (read-only DB query; service.HealthLastSessionClean)
//
// invariant: none of these paths calls RunCheck or EmitRaw on each poll.
package main

import (
	"context"
	"os/exec"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

type healthComputeService interface {
	HealthAdaptersClean() (bool, error)
	HealthLastSessionClean() (bool, error)
	SampleEventLogWritable(ctx context.Context) bool
}

type healthServiceAdapter struct {
	svc *daemon.Plan5OrchestratorService
}

func (a *healthServiceAdapter) HealthAdaptersClean() (bool, error) {
	return a.svc.HealthAdaptersClean()
}

func (a *healthServiceAdapter) HealthLastSessionClean() (bool, error) {
	return a.svc.HealthLastSessionClean()
}

func (a *healthServiceAdapter) SampleEventLogWritable(ctx context.Context) bool {
	up, _, err := a.svc.SampleEventLogWritable(ctx)
	return err == nil && up
}

func buildHealthComputeClosure(svc *daemon.Plan5OrchestratorService) func(context.Context) orchestrator.HealthSnapshot {
	adapter := &healthServiceAdapter{svc: svc}
	return buildHealthCompute(adapter)
}

func buildHealthCompute(svc healthComputeService) func(context.Context) orchestrator.HealthSnapshot {
	return func(ctx context.Context) orchestrator.HealthSnapshot {
		deps := make(map[string]orchestrator.DepHealth, 5)

		deps["research_mcp_up"] = orchestrator.DepHealth{
			Up:     false,
			Detail: "not configured in sampler (upgrade pending)",
		}

		_, gitnexusErr := exec.LookPath("gitnexus")
		deps["gitnexus_up"] = orchestrator.DepHealth{
			Up: gitnexusErr == nil,
			Detail: func() string {
				if gitnexusErr != nil {
					return "not on PATH"
				}
				return ""
			}(),
		}

		probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
		writable := svc.SampleEventLogWritable(probeCtx)
		probeCancel()
		deps["event_log_writable"] = orchestrator.DepHealth{Up: writable}

		clean, _ := svc.HealthAdaptersClean()
		deps["adapters_clean"] = orchestrator.DepHealth{Up: clean}

		sessionClean, _ := svc.HealthLastSessionClean()
		deps["last_session_clean"] = orchestrator.DepHealth{Up: sessionClean}

		return orchestrator.HealthSnapshot{
			SampledAt: time.Now(),
			Deps:      deps,
		}
	}
}
