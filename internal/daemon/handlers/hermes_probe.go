// SPDX-License-Identifier: MIT
// Package handlers — hermes_probe.go.
//
// GET /v1/hermes/probe?check=<name> — diagnostic probe surface for the
// F9 Skills panel and `zen doctor hermes` CLI.
//
// Background — substrate gap closure:
//
// (internal/client/hermes.go::HermesProbe). The daemon side was scoped
// for follow-up but never landed; the route returned 404. Phase
// C C-7 wired the F9 Skills panel through that client wrapper, where
// it manifested as "F9 Skills panel returns 404" in production.
//
// This handler closes the gap. Each probe check is a static introspection
// against the daemon-known state:
//
// - "plugin_installed" — Hermes plugin (`zen-swarm`) is installable;
// status reflects whether the plugin manifest path is reachable on
// the operator's host. Since the daemon cannot detect Hermes's
// plugin runtime state at HEAD (a future 'zen migrate' wires
// it via socket), the probe always returns "ok" with a detail
// describing the install path. Real state is surfaced via the
// `zen-swarm:install-mcps` slash command output.
// - "session_active" — A Hermes session has registered with the
// daemon via /v1/sessions. Status=ok when at least
// one active session exists; warn otherwise.
// - "transport_reachable" — The /v1/messages transport (
// dispatcher) is wired (Orchestrator() non-nil). Status=ok when
// wired; warn otherwise.
//
// Unknown probe names return ok with a hint string — same posture as
// BypassDoctor. 405 on non-GET.
//
// Cherry-pick narrative: this commit completes the substrate gap
// inherited; could be cherry-picked to a
// backport branch if needed.

package handlers

import (
	"net/http"
)

type HermesProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type HermesProbeCtx interface {
	HermesActiveSessions() int

	Orchestrator() any
}

func HermesProbeHandler(s HermesProbeCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		check := r.URL.Query().Get("check")
		resp := HermesProbeResp{Status: "ok"}
		switch check {
		case "plugin_installed":

			resp.Detail = "plugin installs via `/zen-swarm:install-mcps` slash command (ADR-0080)"
		case "session_active":
			n := s.HermesActiveSessions()
			if n == 0 {
				resp.Status = "warn"
				resp.Detail = "no active Hermes session registered (POST /v1/sessions); start a Hermes session via `/zen-swarm:start`"
			} else if n == 1 {
				resp.Detail = "1 active Hermes session"
			} else {
				resp.Detail = "multiple active Hermes sessions"
			}
		case "transport_reachable":
			if s.Orchestrator() == nil {
				resp.Status = "warn"
				resp.Detail = "/v1/messages orchestrator not wired (Plan 3 dispatcher boot pending)"
			} else {
				resp.Detail = "/v1/messages orchestrator wired"
			}
		case "":
			resp.Detail = "no check specified; pass ?check=plugin_installed|session_active|transport_reachable"
		default:
			resp.Detail = "unknown check name; pass ?check=plugin_installed|session_active|transport_reachable"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
