// SPDX-License-Identifier: MIT
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type EventsCtx interface {
	BatcherSubmit(ev EventRowLike)
	BatcherQueueDepth() int
	EventsListPaged(filter EventListFilter) ([]EventRowLike, error)
}

type EventListFilter struct {
	Project   string
	SessionID string
	SwarmID   string
	TaskID    string
	Type      string
	SinceTS   int64
	UntilTS   int64
	Limit     int
	Offset    int
}

func EventsIngest(s EventsCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var input []EventRowLike
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		for _, ev := range input {
			s.BatcherSubmit(ev)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(input)})
	}
}

func EventsList(s EventsCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := EventListFilter{
			Project:   q.Get("project"),
			SessionID: q.Get("session_id"),
			SwarmID:   q.Get("swarm_id"),
			TaskID:    q.Get("task_id"),
			Type:      q.Get("type"),
		}
		if v := q.Get("since"); v != "" {
			filter.SinceTS, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("until"); v != "" {
			filter.UntilTS, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("limit"); v != "" {
			filter.Limit, _ = strconv.Atoi(v)
		}
		if v := q.Get("offset"); v != "" {
			filter.Offset, _ = strconv.Atoi(v)
		}

		rows, err := s.EventsListPaged(filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func notImplemented(w http.ResponseWriter, plan int, planRef string) {
	w.Header().Set("X-HADES-Plan", strconv.Itoa(plan))
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "endpoint scaffolded; implementation pending",
		"plan":  plan,
		"ref":   planRef,
		"see":   "docs/superpowers/plans/",
	})
}
