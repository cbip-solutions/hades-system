// SPDX-License-Identifier: MIT
// Package handlers — operator_gate.go.
//
// OperatorGate state transition endpoints:
//
// GET /v1/workforce/gate/state — read current gate state
// POST /v1/workforce/gate/pause — transition to paused_descriptive|paused_quiet|paused_after_apply
// POST /v1/workforce/gate/resume — transition back to running
//
// Gate states:
//
// running | paused_descriptive | paused_quiet | paused_after_apply
//
// Pause is idempotent: calling pause on an already-paused gate returns the
// current state with HTTP 200 (no error). This prevents race-condition 4xx
// responses when multiple callers (budget anomaly + operator CLI) both pause.
//
// inv-hades-031: never imports internal/workforce/gate directly.
package handlers

import (
	"encoding/json"
	"net/http"
)

type OperatorGateCtx interface {
	OperatorGateState() (string, error)

	OperatorGatePause(mode, reason string) (string, error)

	OperatorGateResume() (string, error)
}

func OperatorGateState(s OperatorGateCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := s.OperatorGateState()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state":      state,
			"can_pause":  state == "running",
			"can_resume": state != "running",
		})
	}
}

func OperatorGatePause(s OperatorGateCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode   string `json:"mode"`
			Reason string `json:"reason"`
		}
		if r.Body != nil && r.ContentLength != 0 {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "invalid JSON: " + err.Error(),
				})
				return
			}
		}
		if body.Mode == "" {
			body.Mode = "paused_descriptive"
		}
		state, err := s.OperatorGatePause(body.Mode, body.Reason)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state":  state,
			"paused": state != "running",
		})
	}
}

func OperatorGateResume(s OperatorGateCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := s.OperatorGateResume()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": state, "running": state == "running"})
	}
}
