// SPDX-License-Identifier: MIT
// Package handlers — hermes_probe.go.
//
// GET /v1/hermes/probe?check=<name> — diagnostic probe surface for the
// dashboard Skills panel and `hades doctor hermes` CLI.
//
// - "plugin_installed" — Hermes plugin (`hades`) is installable;
// status is warn until a future Hermes runtime API can confirm the
// operator's plugin link.
// - "session_active" — A Hermes session has registered with the
// daemon via /v1/sessions. Status=ok when at least
// one active session exists; warn otherwise.
// - "transport_reachable" — The /v1/messages transport (HADES design
// dispatcher) is wired (Orchestrator() non-nil). Status=ok when
// wired; warn otherwise.
//
// Unknown probe names return warn so callers cannot silently treat stale
// probe contracts as healthy. 405 on non-GET.

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

type HermesProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type HermesProbeCtx interface {
	HermesActiveSessions() int

	Orchestrator() any
}

type hermesPluginManifestPathProvider interface {
	HermesPluginManifestPath() string
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
			resp = probeHermesPluginInstalled(s)
		case "session_active":
			n := s.HermesActiveSessions()
			if n == 0 {
				resp.Status = "warn"
				resp.Detail = "no active Hermes session registered (POST /v1/sessions); start a Hermes session via `/hades:start`"
			} else if n == 1 {
				resp.Detail = "1 active Hermes session"
			} else {
				resp.Detail = "multiple active Hermes sessions"
			}
		case "transport_reachable":
			if s.Orchestrator() == nil {
				resp.Status = "warn"
				resp.Detail = "/v1/messages orchestrator unavailable (HADES design dispatcher boot pending)"
			} else {
				resp.Detail = "/v1/messages orchestrator wired"
			}
		case "":
			resp.Status = "warn"
			resp.Detail = "no check specified; pass ?check=plugin_installed|session_active|transport_reachable"
		default:
			resp.Status = "warn"
			resp.Detail = "unknown check name; pass ?check=plugin_installed|session_active|transport_reachable"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func probeHermesPluginInstalled(s HermesProbeCtx) HermesProbeResp {
	manifest := hermesPluginManifestPath(s)
	if manifest == "" {
		return HermesProbeResp{
			Status: "warn",
			Detail: "cannot resolve Hermes plugin path; link plugin/hades to ~/.hermes/plugins/hades; then run /hades:install-mcps in Hermes",
		}
	}
	if _, err := os.Stat(manifest); err == nil {
		return HermesProbeResp{
			Status: "ok",
			Detail: fmt.Sprintf("HADES Hermes plugin manifest found at %s", manifest),
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return HermesProbeResp{
			Status: "warn",
			Detail: fmt.Sprintf("plugin manifest not found at %s; link plugin/hades to ~/.hermes/plugins/hades; then run /hades:install-mcps in Hermes", manifest),
		}
	} else {
		return HermesProbeResp{
			Status: "warn",
			Detail: fmt.Sprintf("cannot inspect Hermes plugin manifest at %s: %v", manifest, err),
		}
	}
}

func hermesPluginManifestPath(s HermesProbeCtx) string {
	if provider, ok := s.(hermesPluginManifestPathProvider); ok {
		if path := provider.HermesPluginManifestPath(); path != "" {
			return path
		}
	}
	if dir := os.Getenv("HERMES_PLUGIN_DIR"); dir != "" {
		return filepath.Join(dir, "plugin.yaml")
	}
	if dir := os.Getenv("HERMES_PLUGINS_DIR"); dir != "" {
		return filepath.Join(dir, "hades", "plugin.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".hermes", "plugins", "hades", "plugin.yaml")
}
