// SPDX-License-Identifier: MIT
// Package handlers — state.go.
//
// 5 NEW operator-facing system-state endpoints surfacing
// substrate (docs/system-state.toml auto-derived + manual per Q9 E)
// over /v1/state/*. Boundary constraints:
//
// - invariant: handler never imports internal/state/manifest directly.
// All access goes through the StateService interface; H-10 wires
// *daemon.Server to satisfy StateService via the production
// state/manifest.Walker + Pinner.
// - invariant: pin endpoint validates non-empty reason → 400 otherwise
// ("auto-pin bypass structurally impossible from this surface").
//
// Graceful degradation: any nil StateService passed to a
// constructor returns an http.HandlerFunc that immediately responds with
// HTTP 503 {"error":"feature not configured","code":"plan9_state_unavailable"}.
//
// Endpoints
//
// GET /v1/state/show — render full manifest (TOML + parsed)
// POST /v1/state/regenerate — walk auth sources, rewrite auto-derived sections
// POST /v1/state/verify — regenerate-and-diff (CI gate)
// POST /v1/state/pin — set manual field + emit state.manual_field_changed
// GET /v1/state/history — chain replay filtered by manual events + field
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

type StateManifestP9 struct {
	LastRegenerateUnix int64  `json:"last_regenerate_unix"`
	ManualFieldCount   int    `json:"manual_field_count"`
	MissingSourceCount int    `json:"missing_source_count,omitempty"`
	TomlContent        string `json:"toml_content"`
}

type StateRegenerateRespP9 struct {
	DryRun        bool     `json:"dry_run"`
	ChangedFields []string `json:"changed_fields"`
	Diff          string   `json:"diff,omitempty"`
}

type StateDiffP9 struct {
	Match bool   `json:"match"`
	Diff  string `json:"diff,omitempty"`
}

type StateChangeP9 struct {
	Field      string `json:"field"`
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	Reason     string `json:"reason"`
	At         int64  `json:"at_unix"`
	OperatorID string `json:"operator_id"`
}

type StateService interface {
	Show(ctx context.Context) (StateManifestP9, error)

	Regenerate(ctx context.Context, dryRun bool) (StateRegenerateRespP9, error)

	Verify(ctx context.Context) (StateDiffP9, error)

	// Pin sets a manual field value and emits state.manual_field_changed into
	// the chain. reason MUST be non-empty; callers
	// validate before calling Pin.
	// Returns error when the field is not flagged x-manual-field=true in the
	// schema (rejected fields must not be silently accepted).
	Pin(ctx context.Context, field, value, reason, operatorID string) error

	History(ctx context.Context, field string) ([]StateChangeP9, error)
}

func stateUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "feature not configured",
		"code":  "plan9_state_unavailable",
	})
}

func stateOperatorFromContext(ctx context.Context) string {
	return auth.OperatorIDFromContext(ctx)
}

func StateShow(s StateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			stateUnavailable(w)
			return
		}
		m, err := s.Show(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

func StateRegenerate(s StateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			stateUnavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			DryRun bool `json:"dry_run"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		res, err := s.Regenerate(r.Context(), req.DryRun)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func StateVerify(s StateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			stateUnavailable(w)
			return
		}
		d, err := s.Verify(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, d)
	}
}

func StatePin(s StateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			stateUnavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Field      string `json:"field"`
			Value      string `json:"value"`
			Reason     string `json:"reason"`
			OperatorID string `json:"operator_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Field == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "field required",
			})
			return
		}
		if req.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "value required",
			})
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "reason required (inv-zen-146; auto-pin forbidden)",
			})
			return
		}

		operatorID := stateOperatorFromContext(r.Context())
		if operatorID == "" {
			operatorID = req.OperatorID
		}
		if err := s.Pin(r.Context(), req.Field, req.Value, req.Reason, operatorID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func StateHistory(s StateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			stateUnavailable(w)
			return
		}
		field := r.URL.Query().Get("field")
		rows, err := s.History(r.Context(), field)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []StateChangeP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}
