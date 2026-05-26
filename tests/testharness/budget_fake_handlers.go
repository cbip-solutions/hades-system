// SPDX-License-Identifier: MIT
package testharness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type BudgetFakeConfig struct {
	mu sync.Mutex

	RollupTotalUSD  float64
	RollupBreakdown map[string]float64
	RollupErr       int

	CapRemainingUSD float64
	CapAllowed      bool
	CapBlockedScope string
	CapStatusErr    int

	TagErr int

	AnomalyZScore  float64
	AnomalyMean    float64
	AnomalyStd     float64
	AnomalySamples int
	AnomalyErr     int

	PauseScope  string
	PauseActive bool
	PauseMode   string

	PauseReason string
	PauseErr    int
	ResumeErr   int

	Events   []map[string]any
	EventErr int
}

func NewBudgetFakeDaemon(t *testing.T, cfg *BudgetFakeConfig) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/budget/rollup", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.RollupErr != 0 {
			http.Error(w, "fake daemon error", cfg.RollupErr)
			return
		}
		breakdown := cfg.RollupBreakdown
		if breakdown == nil {
			breakdown = map[string]float64{}
		}
		writeBudgetJSON(w, map[string]any{
			"total_usd": cfg.RollupTotalUSD,
			"breakdown": breakdown,
		})
	})

	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.CapStatusErr != 0 {
			http.Error(w, "fake daemon error", cfg.CapStatusErr)
			return
		}

		writeBudgetJSON(w, map[string]any{
			"remaining_usd": cfg.CapRemainingUSD,
			"allowed":       cfg.CapAllowed,
			"blocked_scope": cfg.CapBlockedScope,
		})
	})

	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.TagErr != 0 {
			http.Error(w, "fake daemon error", cfg.TagErr)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeBudgetJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/v1/budget/anomaly", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.AnomalyErr != 0 {
			http.Error(w, "fake daemon error", cfg.AnomalyErr)
			return
		}
		writeBudgetJSON(w, map[string]any{
			"z_score": cfg.AnomalyZScore,
			"mean":    cfg.AnomalyMean,
			"std":     cfg.AnomalyStd,
			"samples": cfg.AnomalySamples,
		})
	})

	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.PauseErr != 0 {
			http.Error(w, "fake daemon error", cfg.PauseErr)
			return
		}
		body := map[string]any{
			"scope":      cfg.PauseScope,
			"active":     cfg.PauseActive,
			"pause_mode": cfg.PauseMode,
		}
		if cfg.PauseReason != "" {
			body["reason"] = cfg.PauseReason
		}
		writeBudgetJSON(w, body)
	})

	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.ResumeErr != 0 {
			http.Error(w, "fake daemon error", cfg.ResumeErr)
			return
		}
		body := map[string]any{
			"scope":      cfg.PauseScope,
			"active":     false,
			"pause_mode": cfg.PauseMode,
		}
		if cfg.PauseReason != "" {
			body["reason"] = cfg.PauseReason
		}
		writeBudgetJSON(w, body)
	})

	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		if cfg.EventErr != 0 {
			http.Error(w, "fake daemon error", cfg.EventErr)
			return
		}
		events := cfg.Events
		if events == nil {
			events = []map[string]any{}
		}
		writeBudgetJSON(w, events)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeBudgetJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "fake encode error", http.StatusInternalServerError)
	}
}

func SampleBudgetEvent(kind, scope string) map[string]any {
	return map[string]any{
		"id":         "evt-001",
		"type":       kind,
		"scope":      scope,
		"cost_usd":   0.5,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"payload": map[string]any{
			"kind":  kind,
			"scope": scope,
		},
	}
}

func LockBudgetFakeConfig(cfg *BudgetFakeConfig) func() {
	cfg.mu.Lock()
	return func() { cfg.mu.Unlock() }
}
