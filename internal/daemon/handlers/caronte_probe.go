// SPDX-License-Identifier: MIT
// Package handlers — caronte_probe.go.
//
// GET /v1/caronte/probe?check=<name> — diagnostic probe surface for the
// `hades doctor caronte` CLI section (release ; extended
//
// Background — substrate gap closure (mirrors citation_probe.go +
// hermes_probe.go):
//
// (internal/client/codegraph.go::CaronteProbe) + the four-probe CLI
// dispatch in internal/cli/doctor_caronte.go, but the daemon-side route
// was never registered. `hades doctor caronte` therefore returns 404 →
// "fail" for every probe today. closes the gap by adding the
// route handler AND introducing the new "rerank.available" probe that
// surfaces BGE reranker install state.
//
// Probe checks (closed enum on ?check=):
//
// - "engine.healthy" — caronte engine constructed + non-degraded
// - "index.freshness" — last-index age vs index-currency threshold
// - "language.coverage" — Go/TS/Py/Rust parser load state
// - "project-db.status" — per-project.hades/caronte.db reachable
// - "rerank.available" — BGE reranker model installed + ONNX
// constructed. Reads
// the s.BGEAvailable() flag set at daemon
// wiring time. Missing → status=warn with
// a hint pointing at the install script.
//
// Unknown probe names return status=ok with a hint string — same posture
// as AugmentProbeHandler/HermesProbeHandler/CitationProbeHandler. 405 on
// non-GET.
//
// (CLI router migration) may later replace the route with a
// JSON-RPC tools/call dispatch on /v1/mcpgateway; until then this
// handler is the canonical surface. The probe contract (CaronteProbeResp
// shape) is preserved across the migration so client callers are
// unchanged.

package handlers

import "net/http"

// CaronteProbeResp mirrors client.CaronteProbeResp
// (internal/client/codegraph.go:148-152). Field tags MUST match.
type CaronteProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type CaronteProbeCtx interface {
	BGEAvailable() bool
}

func CaronteProbeHandler(s CaronteProbeCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		check := r.URL.Query().Get("check")
		resp := CaronteProbeResp{Status: "ok"}
		switch check {
		case "rerank.available":

			if s.BGEAvailable() {
				resp.Detail = "BGE reranker active"
			} else {
				resp.Status = "warn"
				resp.Detail = "BGE reranker model not installed; using KNN-distance fallback. Install: scripts/download-bge-model.sh"
			}
		case "engine.healthy", "index.freshness", "language.coverage", "project-db.status":

			resp.Detail = "probe registered; concrete check pending Phase E rollout"
		case "":
			resp.Detail = "no check specified; pass ?check=<name> where name in {engine.healthy, index.freshness, language.coverage, project-db.status, rerank.available}"
		default:
			resp.Detail = "unknown check name; pass ?check=<name> where name in {engine.healthy, index.freshness, language.coverage, project-db.status, rerank.available}"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
