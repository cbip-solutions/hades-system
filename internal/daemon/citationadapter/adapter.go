// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

// Package citationadapter bridges the citation package (substrate
// renderer) to the HADES design audit chain so CitationRendered events
// persist to audit_events_raw + chain-anchor through the same
// daemon write path that HADES design already uses for every other audit
// event (sshexec, budget, doctrine-reload, etc.).
//
// Why a dedicated adapter:
//
// - invariant: the citation package MUST NOT import internal/store
// (boundary preserved by interface segregation; citation.AuditEmitter
// is the contract). The adapter package lives in internal/daemon/
// specifically so it can pull together internal/citation +
// daemon.AuditEmitCtx without forcing the citation package into
// the daemon's import set.
//
// - invariant: same HADES design boundary discipline as
// bypassadapter/dispatcheradapter — the bridge belongs in
// internal/daemon/, not on the citation side.
//
// - Single-egress for audit writes: forwarding through Server.AuditEmit
// means the CitationRendered event walks the same hot path
// (audit_events_raw INSERT + HADES design chain compute via
// OnEmitRaw when wired) as every other event — there's no
// parallel write path to keep in sync.
//
// Wire-up (cmd/hades-ctld/main.go):
//
// bridge := citationadapter.New(srv)
// reg := citation.NewRegistry()
// reg.Register(citation.NewMarkdownFallback(bridge))
// srv.SetCitationRegistry(reg)
//
// TTS, web HTML) will register against the same Registry via
// SetCitationRegistry hook; each renderer reuses the same bridge so
// every renderer's emit-on-render call flows through the same audit
// chain — invariant: one CitationRendered event per Render() call.
package citationadapter

import (
	"context"
	"errors"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/google/uuid"
)

type AuditEmitter interface {
	AuditEmit(event handlers.AuditEventIn) error
}

// Adapter satisfies citation.AuditEmitter by forwarding events to
// AuditEmitter.AuditEmit. The CitationRendered event lands in
// audit_events_raw with:
//
// id = UUIDv4 (stamped by the adapter — internal emit
// path bypasses the HTTP handler that normally
// owns this)
// type = "CitationRendered"
// project_id = ev.ProjectID // doctrine privacy scope
// emitted_at = ev.RenderedAt OR // caller-supplied unix sec
// time.Now().Unix() // when caller passed 0
// payload = { // JSON
// "citation_id": "c-XXXX",
// "platform": "markdown" | "ink" |...,
// "audit_event_link": "hades://audit/<id>",
// "rendered_at": <ev.RenderedAt copy>,
// }
//
// The payload schema mirrors spec §4.6 "CitationRendered anchored
// payload includes platform + citation_id + audit_event_link" plus the
// caller's RenderedAt timestamp surfaced separately (for renderer
// audit traceability) while the row's authoritative emitted_at is
// stamped here.
//
// ID + emitted_at stamping: Server.AuditEmit blindly forwards both
// fields to the audit_events_raw INSERT with no defaulting (per HADES design
// design — the HTTP handler at handlers/audit_emit.go stamps
// both fields before calling Server.AuditEmit). This internal emit path
// bypasses the HTTP handler, so the adapter MUST stamp them itself or
// the migration-055 CHECK (emitted_at > 0) + primary-key uniqueness
// constraints fail-loud on every render.
//
// Failure semantics: AuditEmit error surfaces verbatim; MarkdownFallback
// treats it as soft-fail (logs + continues rendering) per design contract
// design choice+'s "audit emission is best-effort; render must not block on
// chain failure".
type Adapter struct {
	emitter AuditEmitter

	nowFn func() time.Time
	idFn  func() string
}

// New constructs a citation.AuditEmitter bridge. emitter MUST be
// non-nil; production wiring passes *daemon.Server, tests pass a
// fake satisfying AuditEmitter.
//
// Panics on nil emitter: a nil bridge silently drops every
// CitationRendered event downstream, which the operator cannot
// notice until they query the audit chain — fail loud at daemon
// boot per project instructions hard rule "fail fast in dev; self-heal in prod"
// (a missing emitter at bootstrap is a wiring bug, not a runtime
// degradation).
func New(emitter AuditEmitter) *Adapter {
	if emitter == nil {
		panic("citationadapter.New: emitter is nil — daemon wiring bug")
	}
	return &Adapter{emitter: emitter}
}

func (a *Adapter) EmitCitationRendered(ctx context.Context, ev citation.CitationRenderedEvent) error {
	_ = ctx

	if err := ev.CitationID.Validate(); err != nil {
		return errors.New("citationadapter: invalid citation id: " + err.Error())
	}

	payload := map[string]any{
		"citation_id":      string(ev.CitationID),
		"platform":         ev.Platform,
		"audit_event_link": ev.AuditEventURL,
		"rendered_at":      ev.RenderedAt,
	}

	if ev.Doctrine != "" {
		payload["doctrine"] = ev.Doctrine
	}

	emittedAt := ev.RenderedAt
	if emittedAt <= 0 {
		emittedAt = a.now().Unix()
	}

	return a.emitter.AuditEmit(handlers.AuditEventIn{
		ID:        a.newID(),
		ProjectID: ev.ProjectID,
		Type:      "CitationRendered",
		Payload:   payload,
		EmittedAt: emittedAt,
	})
}

func (a *Adapter) now() time.Time {
	if a.nowFn != nil {
		return a.nowFn()
	}
	return time.Now()
}

func (a *Adapter) newID() string {
	if a.idFn != nil {
		return a.idFn()
	}
	return uuid.NewString()
}

func (a *Adapter) WithClock(nowFn func() time.Time) *Adapter {
	cp := *a
	cp.nowFn = nowFn
	return &cp
}

func (a *Adapter) WithIDGenerator(idFn func() string) *Adapter {
	cp := *a
	cp.idFn = idFn
	return &cp
}

var _ citation.AuditEmitter = (*Adapter)(nil)
