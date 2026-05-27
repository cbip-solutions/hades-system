// SPDX-License-Identifier: MIT
// Package handlers — priority.go.
//
// Three routes for the Layer 3 operator override surface:
//
// POST /v1/priority/boost — install or replace an override
// POST /v1/priority/reset — remove the override (idempotent)
// GET /v1/priority/list — enumerate active overrides
//
// These operate on the priority_overrides table via
// internal/quota.OverrideStore (concrete: internal/daemon/quotaadapter)
// per invariant: this package never imports internal/quota types
// transitively from internal/store; the adapter does the field copy.
//
// Status-code mapping (mirrors the projects_p7 + budget_plan4 patterns):
//
// 503 — OverrideStore() not yet wired (cmd/zen-swarm-ctld registers
// the adapter at boot; tests inject fakes via SetOverrideStore).
// 400 — invalid JSON / required fields missing.
// 422 — quota.ErrInvalidOverride: validation rejected the input
// (multiplier out of range, ExpiresAt in the past, empty reason).
// 500 — opaque store error (transactional failure, sql I/O).
// 200 — success; body is `{"ok":true}` for boost/reset, or
// `{"overrides":[...]}` for list.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/quota"
)

type overrideStoreAccessor interface {
	OverrideStore() quota.OverrideStore
}

func resolveOverrideStore(s any) quota.OverrideStore {
	acc, ok := s.(overrideStoreAccessor)
	if !ok {
		return nil
	}
	return acc.OverrideStore()
}

func priorityUnavailable(w http.ResponseWriter) {
	http.Error(w, "priority override store not configured", http.StatusServiceUnavailable)
}

type PriorityBoostRequest struct {
	Alias      string    `json:"alias"`
	Multiplier float64   `json:"multiplier"`
	ExpiresAt  time.Time `json:"expires_at"`
	Reason     string    `json:"reason"`
}

type PriorityResetRequest struct {
	Alias string `json:"alias"`
}

type PriorityOverrideRow struct {
	Alias      string    `json:"alias"`
	Multiplier float64   `json:"multiplier"`
	ExpiresAt  time.Time `json:"expires_at"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

type PriorityListResponse struct {
	Overrides []PriorityOverrideRow `json:"overrides"`
}

func PriorityBoost(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveOverrideStore(s)
		if store == nil {
			priorityUnavailable(w)
			return
		}
		var req PriorityBoostRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Alias) == "" {
			http.Error(w, "alias required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if err := store.Set(ctx, req.Alias, req.Multiplier, req.ExpiresAt, req.Reason); err != nil {

			if errors.Is(err, quota.ErrInvalidOverride) {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func PriorityReset(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveOverrideStore(s)
		if store == nil {
			priorityUnavailable(w)
			return
		}
		var req PriorityResetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Alias) == "" {
			http.Error(w, "alias required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if err := store.Reset(ctx, req.Alias); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func PriorityList(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveOverrideStore(s)
		if store == nil {
			priorityUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		rows, err := store.List(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := PriorityListResponse{
			Overrides: make([]PriorityOverrideRow, 0, len(rows)),
		}
		for _, ov := range rows {
			resp.Overrides = append(resp.Overrides, PriorityOverrideRow{
				Alias:      ov.Alias,
				Multiplier: ov.Multiplier,
				ExpiresAt:  ov.ExpiresAt,
				Reason:     ov.Reason,
				CreatedAt:  ov.CreatedAt,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
