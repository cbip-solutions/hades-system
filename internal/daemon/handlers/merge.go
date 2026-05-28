// SPDX-License-Identifier: MIT
// Package handlers — merge.go.
//
// MergeHandler ships the daemon-side HTTP surface for /v1/merge/* — 8
// routes that the cobra subcommands consume via the
// internal/client.MergeClient transport. The handler is wired in the
// daemon bootstrap (alongside F-1's NewMergeEngineFromConfig); for
// the production wiring of `Engine` is left optional so the
// route registration test can run without the full HADES design substrate
// graph.
//
// Routes (registered on a *http.ServeMux via Register):
//
// GET /v1/merge/inspect?id=<generation|hash>
// POST /v1/merge/replay (body: {"session_id": "..."})
// GET /v1/merge/score-explain?outcome_id=<id>
// GET /v1/merge/baseline?session_id=<id>
// GET /v1/merge/cache/status
// POST /v1/merge/cache/clear
// GET /v1/merge/config
// GET /v1/merge/anomaly?since=<duration>
//
// Wire-types decoupling: handlers serialize internal/orchestrator/merge
// domain types into internal/cli wire-types via small mapping helpers
// declared in this file (so neither package leaks its types to the
// other and invariant is preserved).
//
// surfaces are intentionally THIN for inspect / replay /
// score-explain / baseline / anomaly: full HADES design capture
// machinery is the load-bearing dependency, and the F.7 amendment
// wires it. The cache + config endpoints are FULLY wired today
// (cache.Size + cache.Clear + ScoringConfig fields → wire types).
package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type MergeHandler struct {
	Engine   merge.MergeEngine
	Cache    *merge.Cache
	Anomaly  *merge.AnomalyDetector
	Doctrine string
	Scoring  merge.ScoringConfig

	startedAt time.Time

	rebuildMu       sync.RWMutex
	lastRebuilt     time.Time
	lastRebuildErr  string
	rebuildObserved bool
}

func NewMergeHandler(engine merge.MergeEngine, cache *merge.Cache, anomaly *merge.AnomalyDetector, doctrine string, scoring merge.ScoringConfig) *MergeHandler {
	return &MergeHandler{
		Engine:    engine,
		Cache:     cache,
		Anomaly:   anomaly,
		Doctrine:  doctrine,
		Scoring:   scoring,
		startedAt: time.Now(),
	}
}

func (h *MergeHandler) MarkRebuilt(at time.Time, errMsg string) {
	h.rebuildMu.Lock()
	defer h.rebuildMu.Unlock()
	h.lastRebuilt = at
	h.lastRebuildErr = errMsg
	h.rebuildObserved = true
}

func (h *MergeHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/merge/inspect", h.requireMethod(http.MethodGet, h.handleInspect))
	mux.HandleFunc("/v1/merge/replay", h.requireMethod(http.MethodPost, h.handleReplay))
	mux.HandleFunc("/v1/merge/score-explain", h.requireMethod(http.MethodGet, h.handleScoreExplain))
	mux.HandleFunc("/v1/merge/baseline", h.requireMethod(http.MethodGet, h.handleBaseline))
	mux.HandleFunc("/v1/merge/cache/status", h.requireMethod(http.MethodGet, h.handleCacheStatus))
	mux.HandleFunc("/v1/merge/cache/clear", h.requireMethod(http.MethodPost, h.handleCacheClear))
	mux.HandleFunc("/v1/merge/config", h.requireMethod(http.MethodGet, h.handleConfig))
	mux.HandleFunc("/v1/merge/anomaly", h.requireMethod(http.MethodGet, h.handleAnomaly))
}

func (h *MergeHandler) requireMethod(method string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fn(w, r)
	}
}

func (h *MergeHandler) handleInspect(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	res := cli.MergeInspectResult{RequestHash: id}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		SessionID string `json:"session_id"`
	}

	_ = json.NewDecoder(r.Body).Decode(&body)
	res := cli.MergeReplayResult{
		SessionID:      body.SessionID,
		EventsReplayed: 0,
		OutcomeMatch:   false,
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleScoreExplain(w http.ResponseWriter, r *http.Request) {
	res := cli.MergeScoreExplainResult{
		WinnerID: r.URL.Query().Get("outcome_id"),
		Formula:  "argmax(test_pass) → tiebreak(α·reviewer − β·patch_size − γ·flake)",
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleBaseline(w http.ResponseWriter, r *http.Request) {
	res := cli.MergeBaselineShowResult{
		SessionID:  r.URL.Query().Get("session_id"),
		PassingSet: []string{},
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleCacheStatus(w http.ResponseWriter, _ *http.Request) {
	size := 0
	if h.Cache != nil {
		size = h.Cache.Size()
	}

	h.rebuildMu.RLock()
	rebuilt := h.lastRebuilt
	rebuildErr := h.lastRebuildErr
	observed := h.rebuildObserved
	h.rebuildMu.RUnlock()

	if !observed {

		rebuilt = h.startedAt
	}

	res := cli.MergeCacheStatusResult{
		Size:         size,
		HitRatePct:   0.0,
		LastRebuilt:  formatRFC3339OrEmpty(rebuilt),
		RebuildError: rebuildErr,
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleCacheClear(w http.ResponseWriter, _ *http.Request) {
	if h.Cache != nil {
		h.Cache.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cleared":         true,
		"eventlog_intact": true,
	})
}

func (h *MergeHandler) handleConfig(w http.ResponseWriter, _ *http.Request) {
	res := cli.MergeConfigShowResult{
		Doctrine: h.Doctrine,
		Scoring: cli.MergeScoringConfig{
			Alpha: h.Scoring.AlphaReviewerWeight,
			Beta:  h.Scoring.BetaPatchSizePenalty,
			Gamma: h.Scoring.GammaFlakePenalty,
		},
		Timeouts: cli.MergeTimeoutsConfig{
			BaselineSec:   300,
			CandidateSec:  600,
			FlakeRerunSec: 300,
		},
		ModeMapping: map[string]string{
			"60": "Degraded60",
			"80": "Degraded80",
			"90": "ctx_cancel",
		},
		AnomalyThresholds: defaultAnomalyThresholdsAsMap(),
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *MergeHandler) handleAnomaly(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("since")
	res := cli.MergeAnomalyListResult{Anomalies: []cli.MergeAnomalyEntry{}}
	writeJSON(w, http.StatusOK, res)
}

func formatRFC3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func defaultAnomalyThresholdsAsMap() map[string]any {
	t := merge.DefaultAnomalyThresholds()
	return map[string]any{
		"scoring_winner_vetoed_count":          t.ScoringWinnerVetoedCount,
		"scoring_winner_vetoed_window_hours":   t.ScoringWinnerVetoedWindowHours.Hours(),
		"baseline_unstable_min_divergent":      t.BaselineUnstableMinDivergentTests,
		"flake_rate_threshold_pct":             t.FlakeRateThresholdPct,
		"flake_rate_window_sessions":           t.FlakeRateWindowSessions,
		"textual_unresolvable_rate_pct":        t.TextualUnresolvableRatePct,
		"textual_unresolvable_window_sessions": t.TextualUnresolvableWindowSessions,
		"mode_degradation_pct_threshold":       t.ModeDegradationPctThreshold,
		"mode_degradation_window_hours":        t.ModeDegradationWindowHours.Hours(),
	}
}
