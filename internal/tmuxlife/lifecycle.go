// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

// review reconciliation: the canonical Doctrine taxonomy is
// owned by `internal/doctrine.Name`.
// consumes that type directly; declaring a parallel
// `tmuxlife.Doctrine` would create a duplicate taxonomy that violates
// the no-defer + no-tech-debt doctrine. All callers of public
// API ( scheduler, quota adapter, daemon HTTP
// handlers, CLI flag parsers) consume `doctrine.Name` consistently.
//
// Canonical values (see `internal/doctrine` package):
//
// - doctrine.NameMaxScope ("max-scope") — persistent state, never auto-reap
// - doctrine.NameDefault ("default") — 24h idle, 100% hard cap
// - doctrine.NameCapaFirewall ("capa-firewall") — 4h idle, 95% hard cap
//
// Validation of doctrine names from untrusted input (hadessystem.toml,
// HTTP request body) MUST go through `doctrine.IsValid` BEFORE calling
// `DoctrineIdleTTL` — the latter panics on unknown to surface
// programmer error, NOT to be a graceful runtime gate.

type IdleTTL int

// DoctrineIdleTTL returns the idle TTL in hours for the given doctrine.
//
// Mapping (invariant, spec §1 design choice D):
//
// max-scope → IdleTTLInfinity (-1)
// default → 24
// capa-firewall → 4
//
// Per-project override (hadessystem.toml [project.tmux] idle_ttl_hours = X)
// is consumed by IdleReaper.doctrineFor callback; this function
// returns ONLY the doctrine-default. Override resolution lives at the
// callsite, NOT here, so the doctrine-default mapping stays the single
// source of truth for invariant enforcement.
//
// Panics on unknown doctrine to surface drift
// (programmer-error-must-surface principle). Callers consuming
// untrusted input — TOML parse, HTTP request body, CLI flag — MUST
// validate via `doctrine.IsValid(d)` BEFORE invoking this function.
// Unknown-input recovery is the loader's responsibility, not this
// function's.
//
// The fallback policy here intentionally diverges from
// quota.DoctrineDefaults (which falls back to "default" on unknown):
// quota's fallback is conservative because cost-side overshoot is
// recoverable (operator notices, refunds, adjusts threshold), whereas
// tmuxlife mismapping silently leaves stale tmux sessions running for
// hours past the intended TTL — an invariant violation. The panic
// path keeps the bug visible.
func DoctrineIdleTTL(d doctrine.Name) IdleTTL {
	switch d {
	case doctrine.NameMaxScope:
		return IdleTTL(IdleTTLInfinity)
	case doctrine.NameDefault:
		return IdleTTL(24)
	case doctrine.NameCapaFirewall:
		return IdleTTL(4)
	default:
		panic(fmt.Sprintf("tmuxlife.DoctrineIdleTTL: unknown doctrine %q (allowed: max-scope, default, capa-firewall)", d))
	}
}

func IdleTTLIsInfinity(t IdleTTL) bool {
	return int(t) == IdleTTLInfinity
}

type Trigger int

const (
	TriggerCwd Trigger = 0

	TriggerExplicitAttach Trigger = 1

	TriggerScheduledJob Trigger = 2

	TriggerAutonomousResume Trigger = 3
)

func (t Trigger) String() string {
	switch t {
	case TriggerCwd:
		return "cwd"
	case TriggerExplicitAttach:
		return "explicit-attach"
	case TriggerScheduledJob:
		return "scheduled-job"
	case TriggerAutonomousResume:
		return "autonomous-resume"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// IsValidTrigger returns true iff t is one of the four declared values.
//
// Centralised gate so callers (HandleTrigger panic guard, future stage
// I HTTP handler request-body validators, CLI flag parsers) consume one
// predicate. Mirrors doctrine.IsValid pattern for the same reason —
// validation of untrusted Trigger values from request bodies / TOML /
// flags MUST go through this gate BEFORE invoking HandleTrigger, which
// panics on unknown to surface programmer error.
func IsValidTrigger(t Trigger) bool {
	return t == TriggerCwd || t == TriggerExplicitAttach ||
		t == TriggerScheduledJob || t == TriggerAutonomousResume
}

func (m *Manager) HandleTrigger(ctx context.Context, t Trigger, alias, sha8 string) (*Session, error) {
	if !IsValidTrigger(t) {
		panic(fmt.Sprintf("tmuxlife.HandleTrigger: invalid Trigger %d", int(t)))
	}

	name := SessionName(alias, sha8)

	existing, err := m.store.GetSession(name)
	switch {
	case errors.Is(err, ErrSessionNotFound):

		s, spawnErr := m.Spawn(ctx, alias, sha8)
		if spawnErr != nil {
			return nil, fmt.Errorf("tmuxlife.HandleTrigger(%v): Spawn: %w", t, spawnErr)
		}
		if cwErr := m.CreateWindows(ctx, name); cwErr != nil {

			_, _ = m.exec(context.Background(), "-S", SocketPath, "kill-session", "-t", name)
			_ = m.store.DeleteSession(name)
			return nil, fmt.Errorf("tmuxlife.HandleTrigger(%v): CreateWindows: %w", t, cwErr)
		}
		now := time.Now().UTC()
		_ = m.store.SetLastAttach(name, now)
		s.LastAttachAt = now
		return s, nil

	case err != nil:

		return nil, fmt.Errorf("tmuxlife.HandleTrigger(%v): store.GetSession: %w", t, err)
	}

	now := time.Now().UTC()

	switch existing.Status {
	case StatusActive:

		switch hsErr := m.hasSession(ctx, name); {
		case hsErr == nil:

			_ = m.store.SetLastAttach(name, now)
			existing.LastAttachAt = now
			return &existing, nil
		case errors.Is(hsErr, ErrSessionNotFound):

			_ = m.store.SetStatus(name, StatusOrphaned)
			return m.respawnFresh(ctx, alias, sha8, name)
		default:
			// Transport error: do NOT flip to Orphaned (we don't know
			// yet); surface wrapped so caller sees the cause.
			return nil, hsErr
		}
	case StatusIdle, StatusArchived:

		if restoreErr := m.restoreFn(ctx, alias); restoreErr == nil {
			_ = m.store.SetStatus(name, StatusActive)
			_ = m.store.SetLastAttach(name, now)
			restored, _ := m.store.GetSession(name)
			return &restored, nil
		}

		return m.respawnFresh(ctx, alias, sha8, name)
	case StatusOrphaned:

		return m.respawnFresh(ctx, alias, sha8, name)
	default:
		return nil, fmt.Errorf("tmuxlife.HandleTrigger: unexpected status %v", existing.Status)
	}
}

func (m *Manager) respawnFresh(ctx context.Context, alias, sha8, name string) (*Session, error) {

	_, _ = m.exec(context.Background(), "-S", SocketPath, "kill-session", "-t", name)

	s, err := m.Spawn(ctx, alias, sha8)
	if err != nil && !errors.Is(err, ErrSessionExists) {
		return nil, fmt.Errorf("tmuxlife.respawnFresh: Spawn: %w", err)
	}
	if cwErr := m.CreateWindows(ctx, name); cwErr != nil {
		return nil, fmt.Errorf("tmuxlife.respawnFresh: CreateWindows: %w", cwErr)
	}
	now := time.Now().UTC()
	_ = m.store.SetStatus(name, StatusActive)
	_ = m.store.SetLastAttach(name, now)
	if s != nil {
		s.LastAttachAt = now
		s.Status = StatusActive
		return s, nil
	}

	out, _ := m.store.GetSession(name)
	return &out, nil
}
