// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

const DefaultIdleReapInterval = 5 * time.Minute

type IdleDeps struct {
	HasOperatorAttach   bool
	HasAutonomousWorker bool
	HasScheduledJob     bool
	LastAttachAt        time.Time
}

type IdleReaper struct {
	manager     *Manager
	doctrineFor func(alias string) doctrine.Name

	depsFor func(s Session) IdleDeps

	interval time.Duration

	logger *log.Logger
}

func NewIdleReaper(manager *Manager, doctrineFor func(alias string) doctrine.Name) *IdleReaper {
	if manager == nil {
		panic("tmuxlife.NewIdleReaper: manager is nil")
	}
	if doctrineFor == nil {
		panic("tmuxlife.NewIdleReaper: doctrineFor is nil")
	}
	return &IdleReaper{
		manager:     manager,
		doctrineFor: doctrineFor,
		depsFor: func(s Session) IdleDeps {
			return IdleDeps{LastAttachAt: s.LastAttachAt}
		},
		interval: DefaultIdleReapInterval,
		logger:   log.Default(),
	}
}

// IsIdle returns true iff the session is eligible for teardown:
//
//  1. s != nil and s.Status == StatusActive (other statuses are not
//     reap candidates — the reaper has no work for Idle/Archived/Orphaned).
//  2. None of HasOperatorAttach / HasAutonomousWorker / HasScheduledJob
//     (any one is a hard veto; spec §1 Q7 D activity-veto contract).
//  3. doctrineFor(alias) is NOT max-scope (max-scope = never reap;
//     IdleTTLIsInfinity short-circuit; inv-zen-119 carrier).
//  4. time.Since(effective LastAttachAt) > DoctrineIdleTTL(doctrine).
//
// Effective-LastAttachAt failsafe ladder:
//   - deps.LastAttachAt if non-zero (daemon Phase I: precise tmux probe);
//   - else s.LastAttachAt (daemon.db row written by Manager.Attach);
//   - else s.CreatedAt (session never attached — measure from creation);
//   - else return false (no timestamp at all; do NOT reap an unanchored
//     session — over-cautious is the safe default per asymmetric-failure
//     analysis in IdleDeps doc).
//
// The doctrine TTL conversion uses int(ttl)*time.Hour because IdleTTL is
// declared in hours (lifecycle.go IdleTTL doc), and the IdleReaper is
// the only consumer that needs Duration arithmetic — keeping conversion
// at the boundary preserves the integer-hours contract for TOML round-trip.
//
// inv-zen-119: the doctrine TTL matrix is the load-bearing rule; tests
// TestIsIdleMaxScopeNeverReaped + TestIsIdleDefault24h +
// TestIsIdleCapaFirewall4h each verify one cell.
func (r *IdleReaper) IsIdle(s *Session, deps IdleDeps) bool {
	if s == nil || s.Status != StatusActive {
		return false
	}
	if deps.HasOperatorAttach || deps.HasAutonomousWorker || deps.HasScheduledJob {
		return false
	}
	d := r.doctrineFor(s.Alias)
	ttl := DoctrineIdleTTL(d)
	if IdleTTLIsInfinity(ttl) {
		return false
	}
	effective := deps.LastAttachAt
	if effective.IsZero() {
		effective = s.LastAttachAt
	}
	if effective.IsZero() {
		effective = s.CreatedAt
	}
	if effective.IsZero() {
		return false
	}
	return time.Since(effective) > time.Duration(int(ttl))*time.Hour
}

// Run is the long-running goroutine. Walks store every interval; tears
// down idle sessions. Returns when ctx is cancelled.
//
// Concurrency serializes per-tick work. Multiple Run() goroutines from
// one process is a programmer error (not enforced here; documented). The
// daemon (Phase I) starts exactly one Run() per process at startup.
//
// Tick errors (ListSessions failure, store unavailable) are logged but
// do NOT exit the loop — the reaper's contract is "best-effort sweeper",
// and surfacing transient store failures by exiting would leave idle
// sessions accumulating until daemon restart. Per-session teardown
// failures are similarly logged-and-continued inside tick().
func (r *IdleReaper) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.tick(ctx); err != nil {
				r.logger.Printf("tmuxlife.IdleReaper: tick error: %v", err)
			}
		}
	}
}

func (r *IdleReaper) tick(ctx context.Context) error {
	sessions, err := r.manager.store.ListSessions()
	if err != nil {
		return fmt.Errorf("tmuxlife.IdleReaper.tick: ListSessions: %w", err)
	}
	for _, s := range sessions {

		if !r.IsIdle(&s, r.depsFor(s)) {
			continue
		}

		if !r.IsIdle(&s, r.depsFor(s)) {
			continue
		}

		if err := r.manager.Teardown(ctx, s.Alias, true); err != nil {
			r.logger.Printf("tmuxlife.IdleReaper: Teardown(%q) error: %v", s.Name, err)

		}
	}
	return nil
}
