// SPDX-License-Identifier: MIT
// Package handlers — doctrine.go (Plan 8 Phase J — REWRITE supersedes Plan 4 G-6).
//
// per spec §2.5, replacing the Plan 4 Phase G-6 3-route surface (state,
// validate, reload — DoctrineCtx interface) with a Plan 8-doctrine-package-
// aware DoctrineHandlerCtx interface. The 10 endpoints mirror the 15-command
// CLI inventory at spec §6.1 (Q14 C: HTTP API mirrors CLI subcommand surface)
// so Phase I CLI dispatches over Unix-socket HTTP.
//
//	Read endpoints:
//	  GET  /v1/doctrine/active                  active doctrine + version + source (per-project via ?project=X)
//	  GET  /v1/doctrine/list                    enumerate doctrines (embed/user/project) [?source=...]
//	  GET  /v1/doctrine/show?name=X             render doctrine in toml/json/markdown (?format= + ?section=)
//	  GET  /v1/doctrine/status                  active + last_reload + watcher_healthy + pending_changes
//	  GET  /v1/doctrine/history?since=X         doctrine event log slice (DoctrineLoaded..DoctrineAmendmentApplied..)
//	  GET  /v1/doctrine/diff?a=X&b=Y            two doctrines section/path delta
//
//	Write/admin endpoints:
//	  POST /v1/doctrine/validate                body: {against_baseline, toml_content}; response: {valid, errors[]}
//	  POST /v1/doctrine/reload                  body: {path}; trigger reload.NotifyForce + wait DoctrineReloaded
//	  POST /v1/doctrine/migrate                 body: {toml_content, from_schema_version}; IN-MEMORY ONLY (inv-zen-137)
//	  POST /v1/doctrine/reinforce               body: {task_kind, project_alias, stage, phase, plan_id}; response: {rendered}
//
// Wire shapes follow Phase I client DTOs at internal/client/doctrine_v2.go
// VERBATIM (Phase I shipped its CLI httptest mocks before Phase J landed; the
// daemon shape is the contract). DoctrineHandlerCtx is the Phase J accessor
// surface; *daemon.Server satisfies it via thin getter methods that route to
// the doctrine package singletons (active.Active, builtin.LoadAll,
// parser.ParseStrict, etc.). Defined here to avoid the daemon→handlers→daemon
// import cycle (mirrors orchestratorAccessor pattern at handlers/orchestrator.go).
//
// Boundary discipline (inv-zen-031 generalized as inv-zen-133):
//
//	handlers consume internal/doctrine/{errors,reload,schema/v1} +
//	internal/client (DTO-only package; no orchestrator/store transitive
//	pull); NEVER imports internal/store. Concrete doctrine accessors
//	(active/builtin/parser/migrate/reinforcement) are reached through the
//	*Server accessor methods, NOT through direct package imports here —
//	the *Server lives in package daemon and IS allowed to import them.
//
// Atomicity (inv-zen-138): the /reload handler does NOT mutate active.Accessor
// directly — it delegates to ctx.DoctrineReload which forwards to
// reload.Watcher.NotifyForce, going through the same atomic-Store path the
// file-watcher uses. /migrate (inv-zen-137) is in-memory ONLY; the daemon
// never auto-writes; only the CLI's `zen doctrine-v2 migrate <path> --confirm`
// writes back via os.Rename AFTER consuming this handler's response.
//
// Error discrimination: discriminateDoctrineError walks the sentinel-error
// tree per Hard Rule 11; every handler delegates errors through it for a
// uniform wire shape (avoids duplicating the switch in each handler).
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

const maxDoctrineBodyBytes = 1 << 20

type DoctrineHandlerCtx interface {
	DoctrineActive(projectID string) (name string, schema *v1.Schema, source string, err error)
	DoctrineList(sourceFilter string) ([]client.DoctrineV2ListItem, error)

	DoctrineShow(name, format, section string) (renderedFormat string, body string, err error)

	DoctrineValidate(tomlContent, againstBaseline string) error
	DoctrineStatus(projectAlias string) (DoctrineStatusSnapshot, error)
	DoctrineHistory(since time.Time, filter string, limit int) ([]DoctrineHistoryEventRow, error)

	DoctrineDiff(a, b, section string) (from string, to string, diffs []DoctrineDiffEntry, err error)
	DoctrineMigrate(tomlContent, fromSchemaVersion string) (toSchemaVersion string, migratedTOML string, warnings []string, err error)
	DoctrineReinforce(req client.DoctrineV2ReinforceReq) (rendered string, err error)

	DoctrineReload(path string) error
	DoctrineReloadEvents() <-chan reload.DoctrineReloaded
	DoctrineReloadFailedEvents() <-chan reload.DoctrineReloadFailed
	DoctrineReloadTimeout() time.Duration
	DoctrineUnsubscribeReloadEvents(<-chan reload.DoctrineReloaded)
	DoctrineUnsubscribeReloadFailedEvents(<-chan reload.DoctrineReloadFailed)
}

type DoctrineStatusSnapshot struct {
	Active         client.DoctrineV2ActiveResp
	LastReloadAt   time.Time
	LastReloadOk   bool
	WatcherHealthy bool
	PendingChanges []string
}

type DoctrineHistoryEventRow struct {
	Type    string
	AtUnix  int64
	Payload map[string]any
}

type DoctrineDiffEntry = client.DoctrineV2DiffEntry

// resolveDoctrineHandlerCtx type-asserts s against DoctrineHandlerCtx.
// Returns nil if s is not the *daemon.Server-shaped value (e.g. a test
// passes a typed nil or an unrelated mock). Handlers MUST guard for nil
// and degrade to 503 rather than panic during the brief startup window
// before main.go finishes wiring.
func resolveDoctrineHandlerCtx(s any) DoctrineHandlerCtx {
	if s == nil {
		return nil
	}
	if ctx, ok := s.(DoctrineHandlerCtx); ok {
		return ctx
	}
	return nil
}

func discriminateDoctrineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, doctrineerrors.ErrParseFailed):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"code":  "parse_failed",
		})
	case errors.Is(err, doctrineerrors.ErrTightenViolation):
		writeJSON(w, http.StatusUnprocessableEntity, client.DoctrineV2ValidateResp{
			Valid:  false,
			Errors: []string{err.Error()},
		})
	case errors.Is(err, doctrineerrors.ErrValidationFailed):
		writeJSON(w, http.StatusUnprocessableEntity, client.DoctrineV2ValidateResp{
			Valid:  false,
			Errors: []string{err.Error()},
		})
	case errors.Is(err, doctrineerrors.ErrSchemaVersionUnsupported):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"code":  "schema_version_unsupported",
		})
	case errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"code":  "schema_version_too_old",
		})
	case errors.Is(err, doctrineerrors.ErrDoctrineNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": err.Error(),
		})
	case errors.Is(err, doctrineerrors.ErrMigrationFailed):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": err.Error(),
			"code":  "migration_failed",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
	}
}

func DoctrineActive(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		projectID := r.URL.Query().Get("project")
		name, schema, source, err := ctx.DoctrineActive(projectID)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2ActiveResp{
			Name:            name,
			SchemaVersion:   schema.SchemaVersion,
			DoctrineVersion: schema.DoctrineVersion,
			Source:          source,
		})
	}
}

func DoctrineList(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		sourceFilter := r.URL.Query().Get("source")
		switch sourceFilter {
		case "", "all", "embed", "user", "project":

		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "source must be one of: all, embed, user, project",
			})
			return
		}
		rows, err := ctx.DoctrineList(sourceFilter)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		if rows == nil {
			rows = []client.DoctrineV2ListItem{}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Source != rows[j].Source {
				return rows[i].Source < rows[j].Source
			}
			return rows[i].Name < rows[j].Name
		})
		writeJSON(w, http.StatusOK, client.DoctrineV2ListResp{Items: rows})
	}
}

func DoctrineShow(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		q := r.URL.Query()
		name := q.Get("name")
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "name query param required",
			})
			return
		}
		format := q.Get("format")
		section := q.Get("section")

		switch format {
		case "", "toml", "json", "md", "markdown":

		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "format must be one of: toml, json, md, markdown",
			})
			return
		}
		fmtOut, body, err := ctx.DoctrineShow(name, format, section)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2ShowResp{
			Name:    name,
			Format:  fmtOut,
			Section: section,
			Body:    body,
		})
	}
}

func DoctrineValidate(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxDoctrineBodyBytes)
		defer r.Body.Close()
		var body client.DoctrineV2ValidateReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
					"error": fmt.Sprintf("body exceeds %d bytes", maxDoctrineBodyBytes),
				})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "bad json: " + err.Error(),
			})
			return
		}
		if body.TOMLContent == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "toml_content required",
			})
			return
		}
		if err := ctx.DoctrineValidate(body.TOMLContent, body.AgainstBaseline); err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2ValidateResp{
			Valid:  true,
			Errors: []string{},
		})
	}
}

func DoctrineStatus(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		st, err := ctx.DoctrineStatus(r.URL.Query().Get("project"))
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		var lastReloadStr string
		if !st.LastReloadAt.IsZero() {
			lastReloadStr = st.LastReloadAt.UTC().Format(time.RFC3339)
		}
		pending := st.PendingChanges
		if pending == nil {
			pending = []string{}
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2StatusResp{
			Active:         st.Active,
			LastReloadAt:   lastReloadStr,
			LastReloadOk:   st.LastReloadOk,
			WatcherHealthy: st.WatcherHealthy,
			PendingChanges: pending,
		})
	}
}

func DoctrineHistory(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		q := r.URL.Query()
		sinceRaw := q.Get("since")
		var sinceT time.Time
		if sinceRaw == "" {
			sinceT = time.Now().Add(-7 * 24 * time.Hour)
		} else {
			if d, derr := parseDurationLoose(sinceRaw); derr == nil {
				sinceT = time.Now().Add(-d)
			} else if n, nerr := strconv.ParseInt(sinceRaw, 10, 64); nerr == nil && n >= 0 {
				sinceT = time.Unix(n, 0)
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "since must be a Go duration (24h, 7d) or unix epoch seconds",
				})
				return
			}
		}
		limit := 100
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 500 {
					n = 500
				}
				limit = n
			}
		}
		filter := q.Get("filter")
		rows, err := ctx.DoctrineHistory(sinceT, filter, limit)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		out := make([]client.DoctrineV2HistoryEvent, 0, len(rows))
		for _, r := range rows {
			out = append(out, client.DoctrineV2HistoryEvent{
				Type:    r.Type,
				AtUnix:  r.AtUnix,
				Payload: r.Payload,
			})
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2HistoryResp{Events: out})
	}
}

func parseDurationLoose(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") && !strings.ContainsAny(s, ".eE") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("duration %q must be positive", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

func DoctrineDiff(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		q := r.URL.Query()
		a := q.Get("a")
		b := q.Get("b")
		if a == "" || b == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "a and b query params required (doctrine names)",
			})
			return
		}
		section := q.Get("section")
		from, to, diffs, err := ctx.DoctrineDiff(a, b, section)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		if diffs == nil {
			diffs = []DoctrineDiffEntry{}
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2DiffResp{
			From:  from,
			To:    to,
			Diffs: diffs,
		})
	}
}

func DoctrineMigrate(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxDoctrineBodyBytes)
		defer r.Body.Close()
		var body client.DoctrineV2MigrateReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
					"error": fmt.Sprintf("body exceeds %d bytes", maxDoctrineBodyBytes),
				})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "bad json: " + err.Error(),
			})
			return
		}
		if body.TOMLContent == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "toml_content required",
			})
			return
		}
		toVer, migratedTOML, warnings, err := ctx.DoctrineMigrate(body.TOMLContent, body.FromSchemaVersion)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2MigrateResp{
			ToSchemaVersion: toVer,
			TOMLContent:     migratedTOML,
			Warnings:        warnings,
		})
	}
}

func DoctrineReinforce(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}
		defer r.Body.Close()
		var body client.DoctrineV2ReinforceReq

		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "bad json: " + err.Error(),
				})
				return
			}
		}

		q := r.URL.Query()
		if body.TaskKind == "" {
			body.TaskKind = q.Get("task_kind")
		}
		if body.ProjectAlias == "" {
			body.ProjectAlias = q.Get("project")
		}
		if body.Stage == "" {
			body.Stage = q.Get("stage")
		}
		if body.Phase == "" {
			body.Phase = q.Get("phase")
		}
		if body.PlanID == "" {
			body.PlanID = q.Get("plan_id")
		}
		if body.TaskKind == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "task_kind required",
			})
			return
		}
		rendered, err := ctx.DoctrineReinforce(body)
		if err != nil {
			discriminateDoctrineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, client.DoctrineV2ReinforceResp{
			Rendered: rendered,
		})
	}
}

// DoctrineReload implements POST /v1/doctrine/reload.
//
// Body (JSON): client.DoctrineV2ReloadReq{Path: string}  (empty = reload-all)
//
// Flow (inv-zen-138 atomic via reload.NotifyForce → sync.Pointer.Store path):
//  1. Decode body.path
//  2. Subscribe to DoctrineReloaded + DoctrineReloadFailed events BEFORE
//     NotifyForce (avoid losing fast-publisher events; Phase G's
//     Subscribe* returns a per-subscriber buffered channel)
//  3. Call DoctrineReload(path) — kicks file-watcher's force-reload
//  4. select { ev := <-success: respond reloaded=true with active state;
//     ev := <-failure: respond per-phase status code (422/500);
//     <-time.After(timeout): respond 408 }
//  5. Always Unsubscribe before return (defer)
//
// Response shapes:
//
//	200 client.DoctrineV2ReloadResp{Reloaded:true, State:{...}}
//	422 client.DoctrineV2ReloadResp{Reloaded:false, Errors:["..."]}     — parse/validate/tighten
//	408 client.DoctrineV2ReloadResp{Reloaded:false, Error:"timeout..."}
//	500 client.DoctrineV2ReloadResp{Reloaded:false, Error:"..."}        — IO/system or NotifyForce error
//	400 {"error": "..."}                                                — bad json
//
// Concurrency the handler subscribes to a NEW channel per request via
// Phase G's SubscribeReloadEvents (per-subscriber fanout); concurrent
// requests do not steal each other's events.
func DoctrineReload(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := resolveDoctrineHandlerCtx(s)
		if ctx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "doctrine subsystem not yet wired",
			})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxDoctrineBodyBytes)
		defer r.Body.Close()
		var body client.DoctrineV2ReloadReq
		raw, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(readErr, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
					"error": fmt.Sprintf("body exceeds %d bytes", maxDoctrineBodyBytes),
				})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "read body: " + readErr.Error(),
			})
			return
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "bad json: " + err.Error(),
				})
				return
			}
		}

		successCh := ctx.DoctrineReloadEvents()
		failureCh := ctx.DoctrineReloadFailedEvents()
		if successCh == nil || failureCh == nil {
			writeJSON(w, http.StatusServiceUnavailable, client.DoctrineV2ReloadResp{
				Reloaded: false,
				Error:    "reload event channel not available",
			})
			return
		}
		defer ctx.DoctrineUnsubscribeReloadEvents(successCh)
		defer ctx.DoctrineUnsubscribeReloadFailedEvents(failureCh)

		if err := ctx.DoctrineReload(body.Path); err != nil {
			writeJSON(w, http.StatusInternalServerError, client.DoctrineV2ReloadResp{
				Reloaded: false,
				Error:    err.Error(),
			})
			return
		}
		timeout := ctx.DoctrineReloadTimeout()
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		// inv-zen-138 strictly: when two operators reload distinct files
		// concurrently, both subscribers see both events on the per-Watcher
		// fan-out channels. The handler MUST consume only events whose
		// Path matches the request body — otherwise concurrent /reload
		// calls steal each other's events. body.Path == "" is the
		// reload-all signal; in that case ANY event is acceptable.
		//
		// Loop continues consuming non-matching events until: (a) a matched
		// success/failure event arrives, (b) the channel closes, or (c)
		// the timeout fires. Cross-path events are discarded silently —
		// they are intended for the OTHER subscriber. Note that each
		// subscriber gets its OWN buffered channel (Phase G's per-call
		// SubscribeReloadEvents), so discarding here does not starve the
		// other request's subscription.
		deadline := time.After(timeout)
		for {
			select {
			case ev, ok := <-successCh:
				if !ok {
					writeJSON(w, http.StatusInternalServerError, client.DoctrineV2ReloadResp{
						Reloaded: false,
						Error:    "reload event channel closed",
					})
					return
				}
				if body.Path != "" && ev.Path != body.Path {

					continue
				}
				writeJSON(w, http.StatusOK, client.DoctrineV2ReloadResp{
					Reloaded: true,
					State: client.DoctrineV2ActiveResp{
						Name:            ev.DoctrineName,
						DoctrineVersion: ev.ToDoctrineVersion,
						SchemaVersion:   "1.0",
						Source:          ev.Source,
					},
				})
				return
			case fev, ok := <-failureCh:
				if !ok {
					writeJSON(w, http.StatusInternalServerError, client.DoctrineV2ReloadResp{
						Reloaded: false,
						Error:    "reload failed event channel closed",
					})
					return
				}
				if body.Path != "" && fev.Path != body.Path {
					continue
				}
				code := failurePhaseToStatusCode(fev.Phase, fev.Reason)
				errs := fev.Errors
				if len(errs) == 0 && fev.Detail != "" {
					errs = []string{fev.Detail}
				}
				if len(errs) == 0 && fev.Reason != "" {
					errs = []string{fev.Reason}
				}
				if code >= 500 {
					writeJSON(w, code, client.DoctrineV2ReloadResp{
						Reloaded: false,
						Error:    strings.Join(errs, "; "),
					})
					return
				}
				writeJSON(w, code, client.DoctrineV2ReloadResp{
					Reloaded: false,
					Errors:   errs,
				})
				return
			case <-deadline:
				writeJSON(w, http.StatusRequestTimeout, client.DoctrineV2ReloadResp{
					Reloaded: false,
					Error:    fmt.Sprintf("timeout (%s) waiting for DoctrineReloaded event from file-watcher", timeout),
				})
				return
			}
		}
	}
}

func failurePhaseToStatusCode(phase, reason string) int {
	switch phase {
	case "read", "load", "io":
		return http.StatusInternalServerError
	}
	switch reason {
	case "io_error", "system":
		return http.StatusInternalServerError
	}
	return http.StatusUnprocessableEntity
}
