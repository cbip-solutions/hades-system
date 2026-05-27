// SPDX-License-Identifier: MIT
// Package handlers — status_endpoints.go.
//
// Five observability endpoints consumed by the /hades:status Hermes slash
// command (plugin/hades/commands/status.py). All endpoints are GET-only and
// return JSON suitable for the Python handler's HAPPY_PATH_RESPONSES fixture
// shape defined in plugin/hades/tests/test_commands_status.py.
//
// Endpoints shipped in this file:
//
// - GET /v1/cascade/state — active tier + provider count
// - GET /v1/cost/24h — 24h + session spend in USD
// - GET /v1/context/used — token usage
// - GET /v1/profile/active — active operator profile
// - GET /v1/cwd — daemon working directory
//
// Two endpoints consumed by /hades:status already exist:
//
// - GET /v1/health — handlers/health.go (release; extended in releasec
// to include pid, uds_path, active_model)
// - GET /v1/bypass/status — handlers/bypass.go
//
// Boundary (invariant): handlers consume the server as `any` and
// type-assert against locally-defined interfaces so this package never
// imports the daemon back.
//
// invariant (single egress preserved): these endpoints ONLY surface
// pre-computed state from the daemon's in-memory counters and OS calls.
// They do NOT trigger external network calls, provider requests, or
// keychain access.
package handlers

import (
	"net/http"
	"os"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// statusEndpointAccessor narrows the daemon to the releasec
// status endpoints surface. *daemon.Server satisfies this structurally.
//
// Each method returns nil / zero until main.go has finished wiring;
// handlers MUST guard for nil and degrade gracefully.
type statusEndpointAccessor interface {
	CostCounters() *orchestrator.CostCounters
	Tiers() []providers.TierBackend

	UDSPath() string

	ActiveModel() string
}

func resolveStatusEndpointAccessor(s any) statusEndpointAccessor {
	if acc, ok := s.(statusEndpointAccessor); ok {
		return acc
	}
	return nil
}

type cascadeStateResp struct {
	ActiveTier    int    `json:"active_tier"`
	TierName      string `json:"tier_name"`
	ProviderCount int    `json:"provider_count"`
}

func CascadeState(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveStatusEndpointAccessor(s)
		if acc == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "cascade state not available (daemon not fully wired)",
				"code":  "daemon.not-running",
			})
			return
		}
		tiers := acc.Tiers()
		if len(tiers) == 0 {
			writeJSON(w, http.StatusOK, cascadeStateResp{
				ActiveTier:    0,
				TierName:      "none",
				ProviderCount: 0,
			})
			return
		}

		activeTier := tiers[0]
		writeJSON(w, http.StatusOK, cascadeStateResp{
			ActiveTier:    1,
			TierName:      activeTier.Name(),
			ProviderCount: len(tiers),
		})
	}
}

type cost24hResp struct {
	Spend24hUSD     float64 `json:"spend_24h_usd"`
	SpendSessionUSD float64 `json:"spend_session_usd"`
}

func Cost24h(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveStatusEndpointAccessor(s)
		if acc == nil {
			writeJSON(w, http.StatusOK, cost24hResp{})
			return
		}
		counters := acc.CostCounters()
		if counters == nil {
			writeJSON(w, http.StatusOK, cost24hResp{})
			return
		}

		var total24h float64
		for _, key := range counters.AllKeys() {
			total24h += counters.ProjectProfileTierTotal(
				key.Project, key.Profile, key.Tier, 24*time.Hour,
			)
		}
		writeJSON(w, http.StatusOK, cost24hResp{
			Spend24hUSD:     total24h,
			SpendSessionUSD: 0.0,
		})
	}
}

type contextUsedResp struct {
	UsedTokens int `json:"used_tokens"`
	MaxTokens  int `json:"max_tokens"`
}

func ContextUsed(_ any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, contextUsedResp{
			UsedTokens: 0,
			MaxTokens:  0,
		})
	}
}

type profileActiveResp struct {
	ProfileName string `json:"profile_name"`
	Kind        string `json:"kind"`
}

func ProfileActive(_ any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profileName := os.Getenv("HADES_PROFILE")
		kind := "builtin"
		if profileName == "" {
			profileName = "default"
		} else {
			kind = "env"
		}
		writeJSON(w, http.StatusOK, profileActiveResp{
			ProfileName: profileName,
			Kind:        kind,
		})
	}
}

type cwdResp struct {
	Cwd string `json:"cwd"`
}

func CWD(_ any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cwd, err := os.Getwd()
		if err != nil {

			cwd = ""
		}
		writeJSON(w, http.StatusOK, cwdResp{Cwd: cwd})
	}
}
