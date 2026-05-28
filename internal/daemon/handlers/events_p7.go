// SPDX-License-Identifier: MIT
// Package handlers — events_p7.go.
//
// HandoffPosted endpoint — POST /v1/events/handoff_posted.
//
// The plugin `/handoff` slash command writes .hades/session.md and
// then emits a structured HandoffPostedEvent via this endpoint so
// `hades day --eod` digest can render a ProjectStatusSection
// per project. The endpoint:
//
// 1. Parses application/json body matching eventlog.HandoffPostedEvent
// .
// 2. Validates per-field defense-in-depth Layer 1 (sha256-hex
// project_id, alias regex, RFC3339 timestamp non-zero, length caps,
// state enum).
// 3. Persists via HandoffEmitter ( concrete writer
// adapter; in production *eventlog.Emitter / a daemon adapter
// that wraps it).
// 4. Returns 202 Accepted on success — the underlying eventlog
// emitter is asynchronous (writes per-project state.db on its own
// goroutine; aggregator hot-update flows via the existing
// notification pipeline).
//
// Status-code contract (consumed by hades plugin + future
// retry/backoff machinery):
//
// 202 — accepted, event_id returned. Plugin logs success + done.
// 400 — bad JSON or per-field validation rejection. Plugin surfaces
// the stable error code (`bad_json`, `bad_project_id`,
// `bad_alias`, `bad_timestamp`, `summary_too_long`,
// `commits_too_many`, `commit_too_long`, `bad_state`,
// `blockers_too_many`, `blocker_too_long`,
// `next_session_too_long`) so the operator can fix the
// offending field.
// 401 — bearer auth (RequireDaemonBearer middleware in server.go;
// not the handler's responsibility — middleware short-circuits
// before the handler sees the request).
// 500 — emitter returned an opaque error (sql I/O, encoding failure).
// The upstream error text is NOT leaked in the response (avoids
// surfacing internal disk paths / SQLite errno strings).
// 503 — HandoffEmitter() returned nil (daemon boot race window — the
// emitter / its daemon-side adapter has not been wired
// via SetHandoffEmitter yet). Plugin surfaces "feature not
// configured" rather than retrying indefinitely.
//
// invariant boundary: this handler imports
// internal/orchestrator/eventlog value types only (HandoffPostedEvent
// type alias). It NEVER imports internal/store directly — the daemon
// adapter that satisfies HandoffEmitter is the single bridge to the
// write path.
//
// review IMPORTANT #15 reconciliation (2026-05-01): an earlier
// draft of this file declared its own `HandoffPostedEvent` struct with
// a "// TODO sync with when it lands" marker. That violated
// the no-defer doctrine; ships the canonical type before any
// stage needs it; the current file uses a Go type alias
// (`type HandoffPostedEvent = eventlog.HandoffPostedEvent`) so the wire
// schema, validation, and downstream digest all consume the same
// struct definition with no possibility of silent drift.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type HandoffPostedEvent = eventlog.HandoffPostedEvent

// HandoffEmitter is the abstract emitter interface consumed by the
// handler. The single concrete implementation lives daemon-side
// (cmd/hades-ctld composes it from *eventlog.Log + per-project
// dispatch); tests substitute fakeHandoffEmitter.
//
// Emit is asynchronous in spirit: callers MUST NOT assume the event
// has been durably persisted at the moment Emit returns. The contract
// is "accepted for emission" — if the underlying write fails, the
// event surfaces as a audit anomaly + the handler returns 500.
type HandoffEmitter interface {
	Emit(ctx context.Context, ev HandoffPostedEvent) (eventID string, err error)
}

type HandoffEmitterCtx interface {
	HandoffEmitter() HandoffEmitter
}

var hexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

var aliasRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// validStates is the canonical 4-value autonomous-state enum
// (spec §3 design choice + feedback_eventlog_handoff_event.md). MUST stay in sync
// with internal/orchestrator/eventlog AutonomousState constants
// (active|paused|idle|complete) — drift would let a malformed value
// reach the eventlog.HandoffPostedEvent unmarshal path and either
// (a) silently round-trip a bogus value into the EOD digest, or
// (b) trip the JSON-decode rejection at consumer side. Validating
// here is defense-in-depth Layer 1.
var validStates = map[string]struct{}{
	"active":   {},
	"paused":   {},
	"idle":     {},
	"complete": {},
}

const (
	maxSummary     = 4096
	maxCommits     = 32
	maxCommitLen   = 200
	maxBlockers    = 32
	maxBlockerLen  = 500
	maxNextSession = 1000
)

func validateHandoffEvent(ev HandoffPostedEvent) (code, msg string) {
	// project_id: sha256 hex (64 lowercase chars). The regex
	// `^[0-9a-f]{64}$` is the canonical validator — by construction
	// every match is also valid hex, so a redundant hex.DecodeString
	// step would be dead code. If the regex is ever relaxed, the
	// caller MUST update this check to re-add a separate hex round-trip.
	if ev.ProjectID == "" || !hexRe.MatchString(ev.ProjectID) {
		return "bad_project_id", "project_id must be 64 lowercase hex chars (sha256)"
	}

	if !aliasRe.MatchString(ev.ProjectAlias) {
		return "bad_alias", "project_alias must match [a-z0-9-]{1,64}"
	}

	if ev.Timestamp.IsZero() {
		return "bad_timestamp", "timestamp must be non-zero RFC3339"
	}

	if len(ev.Summary) > maxSummary {
		return "summary_too_long", "summary exceeds 4096 chars"
	}

	if len(ev.RecentCommits) > maxCommits {
		return "commits_too_many", "recent_commits exceeds 32 entries"
	}
	for _, c := range ev.RecentCommits {
		if len(c) > maxCommitLen {
			return "commit_too_long", "individual commit exceeds 200 chars"
		}
	}

	if _, ok := validStates[ev.AutonomousState]; !ok {
		return "bad_state", "autonomous_state must be one of: active, paused, idle, complete"
	}

	if len(ev.Blockers) > maxBlockers {
		return "blockers_too_many", "blockers exceeds 32 entries"
	}
	for _, b := range ev.Blockers {
		if len(b) > maxBlockerLen {
			return "blocker_too_long", "individual blocker exceeds 500 chars"
		}
	}

	if len(ev.NextSession) > maxNextSession {
		return "next_session_too_long", "next_session_action exceeds 1000 chars"
	}
	return "", ""
}

func HandoffPosted(s HandoffEmitterCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var ev HandoffPostedEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			RenderError(r.Context(), w, http.StatusBadRequest,
				"bad_json", "request body is not valid JSON")
			return
		}
		if code, msg := validateHandoffEvent(ev); code != "" {
			RenderError(r.Context(), w, http.StatusBadRequest, code, msg)
			return
		}
		emitter := s.HandoffEmitter()
		if emitter == nil {

			RenderError(r.Context(), w, http.StatusServiceUnavailable,
				"emitter_unavailable",
				"event log emitter unavailable (daemon boot in progress)")
			return
		}
		eventID, err := emitter.Emit(r.Context(), ev)
		if err != nil {
			// Upstream error body MUST NOT be surfaced to the caller —
			// it may contain disk paths, SQLite errno strings, or other
			// internal-state leakage. Stable opaque code is enough for
			// the plugin's retry decision.
			RenderError(r.Context(), w, http.StatusInternalServerError,
				"emit_failed", "event log write failed")
			return
		}
		RenderJSON(r.Context(), w, http.StatusAccepted, map[string]any{
			"event_id":      eventID,
			"project_alias": ev.ProjectAlias,
		})
	}
}
