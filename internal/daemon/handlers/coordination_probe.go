// SPDX-License-Identifier: MIT
// Package handlers — coordination_probe.go.
//
// GET /v1/coordination/probe?check=<name> — diagnostic probe surface for
// the `hades doctor coordination` CLI section.
//
// Background — release substrate gap closure (mirrors hermes_probe.go):
//
// - CLI dispatch in internal/cli/doctor_coordination.go for the two
// coordination checks below.
// - Client wrapper internal/client/codegraph.go::CoordinationProbe
// (request/response types CoordinationProbeResp).
// - Test fakes in internal/cli/doctor_plan11_integration_test.go that
// register /v1/coordination/probe on a httptest mux.
//
// What it did NOT ship: the daemon-side route. `make smoke` therefore
// surfaces 404 → "fail" for both checks. This file closes the gap.
//
// Probe checks (closed enum on ?check=):
// - "release-d-substrate" — repo-level file presence check that
// internal/knowledge/aggregator/aggregator.go exists at the resolved
// repo root. Surfaces the release aggregator-substrate
// installation step; absence implies an incomplete checkout or
// misconfigured HADES_SYSTEM_REPO_ROOT.
//
// RETIRED (v0.20.7, inv-hades-290): "release-h-prime-executed" probe was
// retired because the underlying landing test (presence of
// plugin/hades-system/plugin.yaml + Hermes markers) is obsolete per ADR-0080.
// replaced that path with the Hermes plugin at plugin/hades/ (different
// canonical location + format). The probe-target plugin/hades-system/plugin.yaml
// never existed at HEAD and always reported "fail" — a misleading active
// signal in `hades doctor coordination` output. Q1 substrate decision +
// ADR-0080 supersede release H'; no Claude-Code-plugin conversion is
// planned, so the probe has no underlying behaviour to assert.
//
// Unknown probe names return status=ok with a hint string — same posture
// as AugmentProbeHandler/BypassDoctor. 405 on non-GET.
//
// Repo root resolution: HADES_SYSTEM_REPO_ROOT env override → os.Getwd()
// fallback. Mirrors the pattern in cmd/hades-ctld/main.go:403-410
// (OrchestratorPlan5Service repoRoot); kept self-contained per-handler
// so the handler stays testable without a full Server fixture.

package handlers

import (
	"net/http"
	"os"
	"path/filepath"
)

// CoordinationProbeResp mirrors client.CoordinationProbeResp
// (internal/client/codegraph.go:152-156). Field tags MUST match
// (status, detail with omitempty) so the JSON round-trips cleanly.
type CoordinationProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func CoordinationProbeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		check := r.URL.Query().Get("check")
		resp := CoordinationProbeResp{Status: "ok"}

		repoRoot := os.Getenv("HADES_SYSTEM_REPO_ROOT")
		if repoRoot == "" {
			if cwd, err := os.Getwd(); err == nil {
				repoRoot = cwd
			}
		}

		switch check {
		case "plan-9-d-substrate":

			p := filepath.Join(repoRoot, "internal", "knowledge", "aggregator", "aggregator.go")
			if _, err := os.Stat(p); err != nil {
				resp.Status = "fail"
				resp.Detail = "aggregator substrate missing: " + p
			} else {
				resp.Detail = "aggregator substrate present at " + p
			}
		case "":
			resp.Detail = "no check specified; pass ?check=plan-9-d-substrate"
		default:
			resp.Detail = "unknown check name; pass ?check=plan-9-d-substrate"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
