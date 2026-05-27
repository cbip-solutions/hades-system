// SPDX-License-Identifier: MIT
// Package handlers — citation_probe.go.
//
// GET /v1/citation/probe?check=<name> — diagnostic probe surface for
// the `zen doctor citation` CLI section.
//
// Background — release substrate gap closure (mirrors hermes_probe.go):
//
// (internal/client/citation.go::CitationProbe) + CLI dispatch in
// internal/cli/doctor_citation.go, but never registered the daemon-side
// route. `make smoke` therefore surfaces 404 → "fail" for the audit
// chain handler check (zen://audit-handler-functional). This file
// closes the gap.
//
// Probe checks (closed enum on ?check=):
// - "audit-handler-functional" — self-introspection that the
// /v1/audit/event/* route family is wired (release audit
// chain + release D-5 audit-event resolver). Checked via
// CitationProbeCtx.HasAuditEventRoute() — non-nil auditWriter
// implies the startAuditInfra boot succeeded and the route is
// registered (registerRoutes line 782).
//
// Unknown probe names return status=ok with a hint string — same posture
// as AugmentProbeHandler/HermesProbeHandler. 405 on non-GET.

package handlers

import "net/http"

// CitationProbeResp mirrors client.CitationProbeResp
// (internal/client/citation.go:40-44). Field tags MUST match.
type CitationProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type CitationProbeCtx interface {
	HasAuditEventRoute() bool
}

func CitationProbeHandler(s CitationProbeCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		check := r.URL.Query().Get("check")
		resp := CitationProbeResp{Status: "ok"}
		switch check {
		case "audit-handler-functional":
			if s.HasAuditEventRoute() {
				resp.Detail = "/v1/audit/event/* route registered (audit writer wired)"
			} else {
				resp.Status = "fail"
				resp.Detail = "/v1/audit/event/* route NOT registered (audit writer nil; startAuditInfra failed or pending)"
			}
		case "":
			resp.Detail = "no check specified; pass ?check=audit-handler-functional"
		default:
			resp.Detail = "unknown check name; pass ?check=audit-handler-functional"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
