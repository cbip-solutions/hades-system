// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Session struct {
	Alias        string        `json:"alias"`
	Sha8         string        `json:"sha8"`
	Name         string        `json:"name"`
	CreatedAt    time.Time     `json:"created_at"`
	LastAttachAt time.Time     `json:"last_attach_at"`
	Status       SessionStatus `json:"status"`
}

// SessionStatus enumerates the 4 lifecycle states. Numeric values are
// load-bearing for daemon.db serialization (CHECK constraint in migration
// 057 references these). DO NOT renumber without a migration.
type SessionStatus int

const (
	StatusActive SessionStatus = 0

	StatusIdle SessionStatus = 1

	StatusOrphaned SessionStatus = 2

	StatusArchived SessionStatus = 3
)

func (s SessionStatus) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusIdle:
		return "idle"
	case StatusOrphaned:
		return "orphaned"
	case StatusArchived:
		return "archived"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

func SessionName(alias, sha8 string) string {
	if alias == "" {
		panic("tmuxlife.SessionName: alias is empty")
	}
	if !isValidSha8(sha8) {
		panic(fmt.Sprintf("tmuxlife.SessionName: sha8 %q is not 8 lowercase hex chars", sha8))
	}
	return "hades-" + alias + "-" + sha8
}

func isValidSha8(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// SessionStore is the inv-hades-031 boundary interface: tmuxlife declares
// what it needs from storage; the daemon implements via *store.Store
// .
//
// Implementations MUST be safe for concurrent use (multiple goroutines
// from drift poller + idle reaper + CLI handlers). Implementation-side
// transactions encapsulate multi-row updates atomically.
type SessionStore interface {
	UpsertSession(s Session) error

	GetSession(name string) (Session, error)

	ListSessions() ([]Session, error)

	DeleteSession(name string) error

	SetLastAttach(name string, t time.Time) error

	SetStatus(name string, st SessionStatus) error

	// ExpectedPanesFor returns the daemon-recorded pane-id set per
	// daemon-owned window for the given session name. Used by
	// DriftPoller (C-9) to compare daemon.db expected state against
	// the live `tmux list-panes` output.
	//
	// Contract
	// - Returns a non-nil (possibly empty) map on success. An empty
	// map means "no panes registered yet" (pre-CreateWindows or
	// stale row); the poller treats this as "no expectation" and
	// emits no drift, NOT as an error.
	// - The poller iterates DaemonOwnedWindows; only those keys are
	// consulted. inv-hades-118: implementations MUST never return
	// WindowScratch as a key (compile-time guarded by the poller's
	// iteration over DaemonOwnedWindows, but a defense-in-depth
	// check at the implementation site is good practice).
	// - Errors surface storage-layer failures (e.g., daemon.db
	// unavailable). The DriftPoller logs and continues to the
	// next session — one bad row must not block the sweep.
	//
	// Wiring: the daemon's *store.Store implements via a
	// JOIN over tmux_session_state and the (future) tmux_session_pane
	// child table populated by Manager.CreateWindows + spawn paths.
	ExpectedPanesFor(sessionName string) (map[WindowName][]string, error)
}

type Manager struct {
	store SessionStore

	exec func(ctx context.Context, args ...string) ([]byte, error)

	snapshotDir string

	resurrect resurrectExec

	now func() time.Time

	statFn func(name string) (os.FileInfo, error)

	restoreFn func(ctx context.Context, alias string) error
}

func New(store SessionStore) *Manager {
	if store == nil {
		panic("tmuxlife.New: store is nil")
	}
	m := &Manager{
		store:     store,
		exec:      ExecTmux,
		resurrect: realResurrectExec{},
		now:       func() time.Time { return time.Now().UTC() },
		statFn:    os.Stat,
	}

	m.restoreFn = m.Restore
	return m
}

func (m *Manager) hasSession(ctx context.Context, name string) error {
	_, err := m.exec(ctx, "-S", SocketPath, "has-session", "-t", name)
	if err == nil {
		return nil
	}
	msg := err.Error()

	if strings.Contains(msg, "can't find session") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no current session") ||
		strings.Contains(msg, "no such session") {
		return ErrSessionNotFound
	}
	return fmt.Errorf("tmuxlife.hasSession: %w", err)
}

func (m *Manager) Spawn(ctx context.Context, alias, sha8 string) (*Session, error) {
	name := SessionName(alias, sha8)

	switch err := m.hasSession(ctx, name); {
	case err == nil:

		return nil, ErrSessionExists
	case errors.Is(err, ErrSessionNotFound):

	default:
		return nil, err
	}

	if _, err := m.exec(ctx, "-S", SocketPath,
		"new-session", "-d", "-s", name, "-x", "200", "-y", "50",
	); err != nil {

		if strings.Contains(err.Error(), "duplicate session") {
			return nil, ErrSessionExists
		}
		return nil, fmt.Errorf("tmuxlife.Spawn: new-session: %w", err)
	}

	now := time.Now().UTC()
	s := Session{
		Alias:        alias,
		Sha8:         sha8,
		Name:         name,
		CreatedAt:    now,
		LastAttachAt: time.Time{},
		Status:       StatusActive,
	}
	if err := m.store.UpsertSession(s); err != nil {

		_, _ = m.exec(context.Background(), "-S", SocketPath,
			"kill-session", "-t", name,
		)
		return nil, fmt.Errorf("tmuxlife.Spawn: store.UpsertSession: %w", err)
	}
	return &s, nil
}

func (m *Manager) resolveAlias(alias string) (Session, error) {
	all, err := m.store.ListSessions()
	if err != nil {
		return Session{}, fmt.Errorf("tmuxlife.resolveAlias: store.ListSessions: %w", err)
	}
	for _, s := range all {
		if s.Alias == alias {
			return s, nil
		}
	}
	return Session{}, ErrSessionNotFound
}

// Attach builds the tmux args the caller (CLI handler) should syscall.Exec
// to attach the operator's terminal to the session. Returned slice
// includes "tmux" as args[0] (the binary name) so callers can pass it
// directly to syscall.Exec.
//
// Side effects:
// - validates window argument (must be in AllWindows);
// - resolves alias → Session via daemon.db scan;
// - verifies the tmux session is alive via has-session (transitions row
// to StatusOrphaned and returns error if not);
// - updates LastAttachAt in daemon.db (best-effort).
//
// Why this signature instead of m.Attach being interactive: the CLI
// process MUST replace itself with tmux to inherit the operator's TTY.
// Daemon cannot perform attach. The args-slice handoff is the cleanest
// boundary; tested in C-12 (CLI integration).
//
// Returns
// - ErrSessionNotFound if alias is absent from daemon.db.
// - non-sentinel wrapped error if has-session reports the tmux session
// is missing (row is flipped to StatusOrphaned for drift poller /
// brief generator to surface).
// - non-sentinel wrapped error if has-session itself fails for transport
// reasons (socket unreachable etc.); row is NOT flipped to Orphaned
// because we don't know whether the session is actually gone.
func (m *Manager) Attach(ctx context.Context, alias string, window WindowName) ([]string, error) {
	if !IsValidWindowName(window) {
		return nil, fmt.Errorf("tmuxlife.Attach: window %q invalid (allowed: %v)",
			window, AllWindows)
	}
	s, err := m.resolveAlias(alias)
	if err != nil {
		return nil, err
	}
	switch hsErr := m.hasSession(ctx, s.Name); {
	case hsErr == nil:

	case errors.Is(hsErr, ErrSessionNotFound):

		_ = m.store.SetStatus(s.Name, StatusOrphaned)
		return nil, fmt.Errorf("tmuxlife.Attach: tmux session %q lost; marked orphaned", s.Name)
	default:
		return nil, hsErr
	}

	now := time.Now().UTC()
	_ = m.store.SetLastAttach(s.Name, now)

	target := s.Name + ":" + string(window)
	return []string{"tmux", "-S", SocketPath, "attach", "-t", target}, nil
}

func (m *Manager) List(ctx context.Context) ([]Session, error) {
	return m.store.ListSessions()
}

func (m *Manager) Teardown(ctx context.Context, alias string, snapshot bool) error {
	s, err := m.resolveAlias(alias)
	if err != nil {
		return err
	}
	if s.Status == StatusArchived {
		return nil
	}

	if snapshot {
		if _, sErr := m.Save(ctx, alias); sErr != nil {
			return fmt.Errorf("tmuxlife.Teardown: snapshot first: %w", sErr)
		}
	}

	if _, err := m.exec(ctx, "-S", SocketPath, "kill-session", "-t", s.Name); err != nil {

		_ = m.store.SetStatus(s.Name, StatusArchived)
		return fmt.Errorf("tmuxlife.Teardown: kill-session: %w", err)
	}
	return m.store.SetStatus(s.Name, StatusArchived)
}
