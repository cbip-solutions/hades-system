// SPDX-License-Identifier: MIT
// Package handlers — schedule_p7.go.
//
// Six routes for the release scheduler operator surface:
//
// POST /v1/schedules — create routine | task | loop
// GET /v1/schedules — list (filter via ?alias=)
// POST /v1/schedules/{id}/delete — soft-delete (Disabled then DELETE)
// POST /v1/schedules/{id}/run — manual trigger (Fire)
// GET /v1/schedules/{id}/history — fire history rows in window
// GET /v1/schedules/queue — next-24h fire queue
//
// These operate on the schedules + schedule_history substrate
// (migration 063) via internal/scheduler.Store + a typed
// ScheduleHandler that bridges scheduler.Schedule ↔ store.ScheduleRow
// per invariant: this handler package never imports internal/store
// directly for scheduler-side lifecycle; the scheduleradapter is the
// single bridge.
//
// Status-code mapping (mirrors the projects_p7 + priority patterns):
//
// 503 — ScheduleStore() not yet wired (cmd/hades-ctld registers
// the adapter at boot; tests inject fakes via SetScheduleStore).
// 400 — invalid JSON / missing required fields.
// 404 — schedule id not found (delete / run / history paths).
// 422 — validation rejected the input (unknown trigger, bad cron,
// interval below 1min floor, FireAt in the past, miss policy
// not in the four-policy union).
// 500 — opaque store error (transactional failure, sql I/O).
// 200 — success; bodies documented per route below.
//
// gap: the Run path requires a wired scheduler.FireDeps (quota
// + dispatcher + eventlog + ratelimit), which composes at boot.
// Until then the Run route returns 503 even when the store is wired —
// the operator surface is final-shape day 1 (`hades schedule routine run`
// reaches a real route) but the dispatch substrate ships in
//
// invariant boundary: the only scheduler-side imports are the
// scheduler.Schedule + scheduler.HistoryEntry value types and the
// scheduler.Tier / scheduler.MissPolicy / scheduler.TriggerType /
// scheduler.Status / scheduler.Outcome enums. No internal/store, no
// scheduleradapter direct dependency — the store reaches us through
// the Server's ScheduleStore() accessor.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
	"github.com/google/uuid"
)

type ScheduleStore interface {
	Insert(ctx context.Context, s *scheduler.Schedule) error
	Get(ctx context.Context, id string) (*scheduler.Schedule, error)
	List(ctx context.Context, alias string) ([]*scheduler.Schedule, error)
	SoftDelete(ctx context.Context, id string) error
	QueryHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]scheduler.HistoryEntry, error)
	ListDue(ctx context.Context, until time.Time) ([]*scheduler.Schedule, error)
}

type scheduleStoreAccessor interface {
	ScheduleStore() ScheduleStore
}

func resolveScheduleStore(s any) ScheduleStore {
	acc, ok := s.(scheduleStoreAccessor)
	if !ok {
		return nil
	}
	return acc.ScheduleStore()
}

func scheduleUnavailable(w http.ResponseWriter) {
	http.Error(w, "schedule store not configured", http.StatusServiceUnavailable)
}

const scheduleHandlerTimeout = 5 * time.Second

type CreateScheduleRequest struct {
	Kind          string        `json:"kind,omitempty"`
	ProjectAlias  string        `json:"project_alias"`
	Action        string        `json:"action"`
	Trigger       string        `json:"trigger,omitempty"`
	CronExpr      string        `json:"cron_expr,omitempty"`
	RepoURL       string        `json:"repo_url,omitempty"`
	Branch        string        `json:"branch,omitempty"`
	MissPolicyStr string        `json:"miss_policy,omitempty"`
	MissLookback  time.Duration `json:"miss_lookback_ns,omitempty"`
	In            time.Duration `json:"in_ns,omitempty"`
	Interval      time.Duration `json:"interval_ns,omitempty"`
}

type CreateScheduleResponse struct {
	ID             string    `json:"id"`
	Tier           string    `json:"tier"`
	NextRunAt      time.Time `json:"next_run_at,omitempty"`
	RawBearerToken string    `json:"raw_bearer_token,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
}

func ScheduleCreate(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		var req CreateScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ProjectAlias) == "" {
			http.Error(w, "project_alias required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Action) == "" {
			http.Error(w, "action required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()
		kind := req.Kind
		if kind == "" {
			kind = "routine"
		}
		switch kind {
		case "routine":
			scheduleCreateRoutine(ctx, w, store, req)
		case "task":
			scheduleCreateTask(ctx, w, store, req)
		case "loop":
			scheduleCreateLoop(ctx, w, store, req)
		default:
			http.Error(w, fmt.Sprintf("unknown schedule kind %q", kind), http.StatusUnprocessableEntity)
		}
	}
}

func scheduleCreateRoutine(ctx context.Context, w http.ResponseWriter, store ScheduleStore, req CreateScheduleRequest) {
	trigger := req.Trigger
	if trigger == "" {
		trigger = "cron"
	}
	policy, err := parseMissPolicy(req.MissPolicyStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	missLookback := req.MissLookback
	if missLookback <= 0 {
		missLookback = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	sch := &scheduler.Schedule{
		ID:           id,
		Tier:         scheduler.TierRoutine,
		ProjectAlias: req.ProjectAlias,
		Action:       req.Action,
		MissPolicy:   policy,
		MissLookback: missLookback,
		Status:       scheduler.StatusEnabled,
		CreatedAt:    now,
	}

	rawBearer := ""
	switch trigger {
	case "cron":
		if strings.TrimSpace(req.CronExpr) == "" {
			http.Error(w, "cron trigger requires cron_expr", http.StatusUnprocessableEntity)
			return
		}
		sch.TriggerType = scheduler.TriggerCron
		sch.TriggerConfig = scheduler.TriggerConfig{CronExpr: req.CronExpr}

		routine, rerr := scheduler.NewRoutine(sch, doctrine.NameDefault)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusUnprocessableEntity)
			return
		}
		sch.NextRunAt = routine.Plan(now)
	case "http":
		raw, hashHex, gerr := scheduler.GenerateBearerToken()
		if gerr != nil {
			http.Error(w, gerr.Error(), http.StatusInternalServerError)
			return
		}
		sch.TriggerType = scheduler.TriggerHTTP
		sch.TriggerConfig = scheduler.TriggerConfig{BearerTokenHash: hashHex}
		sch.BearerTokenHash = hashHex
		rawBearer = raw
	case "git-poll":
		if strings.TrimSpace(req.RepoURL) == "" {
			http.Error(w, "git-poll trigger requires repo_url", http.StatusUnprocessableEntity)
			return
		}
		branch := req.Branch
		if branch == "" {
			branch = "main"
		}
		sch.TriggerType = scheduler.TriggerGitPoll
		sch.TriggerConfig = scheduler.TriggerConfig{
			RepoURL: req.RepoURL,
			Branch:  branch,
		}
	default:
		http.Error(w, fmt.Sprintf("unknown trigger %q", trigger), http.StatusUnprocessableEntity)
		return
	}
	if err := sch.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := store.Insert(ctx, sch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, CreateScheduleResponse{
		ID:             sch.ID,
		Tier:           sch.Tier.String(),
		NextRunAt:      sch.NextRunAt,
		RawBearerToken: rawBearer,
	})
}

func scheduleCreateTask(ctx context.Context, w http.ResponseWriter, store ScheduleStore, req CreateScheduleRequest) {
	if req.In <= 0 {
		http.Error(w, "task requires positive --in duration", http.StatusUnprocessableEntity)
		return
	}
	now := time.Now().UTC()
	fireAt := now.Add(req.In)
	id := uuid.NewString()
	sch, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           id,
		ProjectAlias: req.ProjectAlias,
		Action:       req.Action,
		FireAt:       fireAt,
	}, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := store.Insert(ctx, sch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, CreateScheduleResponse{
		ID:        sch.ID,
		Tier:      sch.Tier.String(),
		NextRunAt: sch.NextRunAt,
	})
}

func scheduleCreateLoop(ctx context.Context, w http.ResponseWriter, store ScheduleStore, req CreateScheduleRequest) {
	if req.Interval <= 0 {
		http.Error(w, "loop requires positive --interval", http.StatusUnprocessableEntity)
		return
	}
	id := uuid.NewString()
	loop, err := scheduler.NewLoop(scheduler.LoopParams{
		ID:           id,
		ProjectAlias: req.ProjectAlias,
		Action:       req.Action,
		Interval:     req.Interval,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	now := time.Now().UTC()

	sch := &scheduler.Schedule{
		ID:           loop.ID(),
		Tier:         scheduler.TierLoop,
		ProjectAlias: loop.ProjectAlias(),
		Action:       loop.Action(),
		TriggerType:  scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{

			CronExpr: "* * * * *",
		},
		MissPolicy:   scheduler.MissPolicySkip,
		MissLookback: req.Interval,
		Status:       scheduler.StatusEnabled,
		CreatedAt:    now,
		NextRunAt:    now.Add(loop.Interval()),
	}
	if err := sch.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := store.Insert(ctx, sch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, CreateScheduleResponse{
		ID:        sch.ID,
		Tier:      sch.Tier.String(),
		SessionID: loop.SessionID(),
	})
}

type ScheduleListResponse struct {
	Schedules []ScheduleListRow `json:"schedules"`
}

type ScheduleListRow struct {
	ID           string    `json:"id"`
	ProjectAlias string    `json:"project_alias"`
	Action       string    `json:"action"`
	Tier         string    `json:"tier"`
	Status       string    `json:"status"`
	NextRunAt    time.Time `json:"next_run_at,omitempty"`
}

func ScheduleList(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()
		alias := r.URL.Query().Get("alias")
		schs, err := store.List(ctx, alias)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := ScheduleListResponse{Schedules: make([]ScheduleListRow, 0, len(schs))}
		for _, sch := range schs {
			if sch == nil {
				continue
			}
			out.Schedules = append(out.Schedules, ScheduleListRow{
				ID:           sch.ID,
				ProjectAlias: sch.ProjectAlias,
				Action:       sch.Action,
				Tier:         sch.Tier.String(),
				Status:       sch.Status.String(),
				NextRunAt:    sch.NextRunAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func ScheduleDelete(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		id := scheduleIDFromPath(r.URL.Path, "/delete")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()

		got, err := store.Get(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got == nil {
			http.Error(w, "schedule not found", http.StatusNotFound)
			return
		}
		if err := store.SoftDelete(ctx, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func ScheduleRun(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		id := scheduleIDFromPath(r.URL.Path, "/run")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()
		got, err := store.Get(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got == nil {
			http.Error(w, "schedule not found", http.StatusNotFound)
			return
		}

		http.Error(w,
			"manual run dispatch not yet configured (Phase I wires scheduler.FireDeps)",
			http.StatusServiceUnavailable)
	}
}

type ScheduleHistoryResponse struct {
	Rows []ScheduleHistoryRow `json:"rows"`
}

type ScheduleHistoryRow struct {
	ScheduleID string    `json:"schedule_id"`
	FiredAt    time.Time `json:"fired_at"`
	Outcome    int       `json:"outcome"`
	Reason     string    `json:"reason,omitempty"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

func ScheduleHistory(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		id := scheduleIDFromPath(r.URL.Path, "/history")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		from, ferr := parseTimeQuery(r, "from")
		if ferr != nil {
			http.Error(w, ferr.Error(), http.StatusBadRequest)
			return
		}
		to, terr := parseTimeQuery(r, "to")
		if terr != nil {
			http.Error(w, terr.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()

		got, err := store.Get(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got == nil {
			http.Error(w, "schedule not found", http.StatusNotFound)
			return
		}
		hist, err := store.QueryHistory(ctx, id, from, to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := ScheduleHistoryResponse{Rows: make([]ScheduleHistoryRow, 0, len(hist))}
		for _, h := range hist {
			out.Rows = append(out.Rows, ScheduleHistoryRow{
				ScheduleID: h.ScheduleID,
				FiredAt:    h.FiredAt,
				Outcome:    int(h.Outcome),
				Reason:     h.Reason,
				CostUSD:    h.CostUSD,
				DurationMs: h.DurationMs,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type ScheduleQueueResponse struct {
	Rows []ScheduleQueueRow `json:"rows"`
}

type ScheduleQueueRow struct {
	ID           string    `json:"id"`
	ProjectAlias string    `json:"project_alias"`
	Action       string    `json:"action"`
	NextRunAt    time.Time `json:"next_run_at"`
}

func ScheduleQueue(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveScheduleStore(s)
		if store == nil {
			scheduleUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scheduleHandlerTimeout)
		defer cancel()
		now := time.Now().UTC()
		until := now.Add(24 * time.Hour)
		schs, err := store.ListDue(ctx, until)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := ScheduleQueueResponse{Rows: make([]ScheduleQueueRow, 0, len(schs))}
		for _, sch := range schs {
			if sch == nil || sch.NextRunAt.IsZero() {
				continue
			}
			out.Rows = append(out.Rows, ScheduleQueueRow{
				ID:           sch.ID,
				ProjectAlias: sch.ProjectAlias,
				Action:       sch.Action,
				NextRunAt:    sch.NextRunAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func parseMissPolicy(s string) (scheduler.MissPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "doctrine", "skip":
		return scheduler.MissPolicySkip, nil
	case "catch-up-bounded":
		return scheduler.MissPolicyCatchUpBounded, nil
	case "coalesce":
		return scheduler.MissPolicyCoalesce, nil
	case "notify-only":
		return scheduler.MissPolicyNotifyOnly, nil
	default:
		return 0, fmt.Errorf("unknown miss policy %q (allowed: skip, catch-up-bounded, coalesce, notify-only, doctrine)", s)
	}
}

func scheduleIDFromPath(path, suffix string) string {
	const prefix = "/v1/schedules/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, suffix)

	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

func parseTimeQuery(r *http.Request, name string) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return time.Time{}, fmt.Errorf("query param %q required", name)
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("query param %q: %w", name, err)
	}
	return t.UTC(), nil
}
