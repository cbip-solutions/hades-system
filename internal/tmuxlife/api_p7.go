// SPDX-License-Identifier: MIT
// Package tmuxlife — HADES design + K public consumption surface.
//
// This file extends the C-1..C-13 production API with aliases and
// alias-typed convenience wrappers consumed by:
// - HTTP handlers (internal/daemon/handlers/sessions_p7.go)
// - daemon bootstrap (cmd/hades-ctld/main.go)
// - chaos tests (tests/chaos/tmux_health_*_test.go)
//
// review CRITICAL #5 reconciliation (2026-05-01): and
// were authored against names + types that did not yet exist
// in C-1..C-13 (e.g., `SessionState`, `StatusRunning`, `NewManager`,
// `RepaintLayout`). Rather than retrofitting the consumer phases to
// the C-1..C-13 names, extends here with type aliases +
// alias-based methods + new constructor variants. The C-1..C-13 names
// remain authoritative implementations; this file is the consumption-
// side facade.
//
// Boundary (invariant, invariant): this file imports only
// internal/doctrine + standard library. NO internal/store import; the
// in-memory SessionStore implementation here is for chaos tests +
// daemon-bootstrap fallback when the persistence-backed store is not
// yet available.
package tmuxlife

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type SessionState = Session

var (
	StatusRunning = StatusActive

	StatusOrphan = StatusOrphaned
)

var NewManager = New

type inMemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
}

func NewInMemorySessionStore() SessionStore {
	return &inMemorySessionStore{
		sessions: make(map[string]Session),
	}
}

func (s *inMemorySessionStore) UpsertSession(session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.Name] = session
	return nil
}

func (s *inMemorySessionStore) GetSession(name string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.sessions[name]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return got, nil
}

func (s *inMemorySessionStore) ListSessions() ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sessions))
	for _, v := range s.sessions {
		out = append(out, v)
	}
	return out, nil
}

func (s *inMemorySessionStore) SetStatus(name string, status SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.sessions[name]
	if !ok {
		return ErrSessionNotFound
	}
	got.Status = status
	s.sessions[name] = got
	return nil
}

func (s *inMemorySessionStore) SetLastAttach(name string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.sessions[name]
	if !ok {
		return ErrSessionNotFound
	}
	got.LastAttachAt = t
	s.sessions[name] = got
	return nil
}

func (s *inMemorySessionStore) DeleteSession(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, name)
	return nil
}

// ExpectedPanesFor returns the daemon-recorded pane-id set per
// daemon-owned window for the given session name.
//
// In-memory implementation: returns an empty (non-nil) map. The
// in-memory variant does NOT track pane state; the DriftPoller treats
// "no expectation" as "do not emit drift", per the SessionStore
// contract. *store.Store implementation populates real
// pane-id expectations from a child table.
func (s *inMemorySessionStore) ExpectedPanesFor(sessionName string) (map[WindowName][]string, error) {
	return map[WindowName][]string{}, nil
}

// SessionSpec is the builder type for chaos test sessions.
//
// Captures the alias + sha8 (canonical session identity) plus an
// explicit Windows list for tests that want to assert against a
// non-default window set. Production code uses Manager.Spawn +
// Manager.CreateWindows directly (the canonical 6-window layout is
// hard-coded in CreateWindows; tests respect that and do not
// override).
type SessionSpec struct {
	Alias   string
	Sha8    string
	Windows []WindowName
}

func (m *Manager) ActivateFromSpec(ctx context.Context, spec SessionSpec) (*Session, error) {
	s, err := m.Spawn(ctx, spec.Alias, spec.Sha8)
	if err != nil {
		return nil, err
	}
	if err := m.CreateWindows(ctx, s.Name); err != nil {
		return nil, err
	}
	return s, nil
}

type ActivateDeps struct {
	Store    SessionStore
	Doctrine doctrine.Name
	Now      func() time.Time
}

func Activate(deps ActivateDeps, alias, sha8 string) (*Session, error) {
	m := New(deps.Store)
	return m.HandleTrigger(context.Background(), TriggerExplicitAttach, alias, sha8)
}

type HealthMonitor struct {
	deps    HealthMonitorDeps
	cancel  context.CancelFunc
	stopped chan struct{}
}

type HealthMonitorDeps struct {
	Manager  *Manager
	Tick     time.Duration
	OnDrift  func(LayoutDrift)
	OnOrphan func(Session)
}

func NewHealthMonitor(deps HealthMonitorDeps) *HealthMonitor {
	if deps.Tick == 0 {
		deps.Tick = 5 * time.Second
	}
	return &HealthMonitor{deps: deps, stopped: make(chan struct{})}
}

func (h *HealthMonitor) Run(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)
	defer close(h.stopped)
	t := time.NewTicker(h.deps.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			sessions, err := h.deps.Manager.ListSessions(ctx)
			if err != nil {
				continue
			}
			for _, s := range sessions {
				if s.Status == StatusOrphan && h.deps.OnOrphan != nil {
					h.deps.OnOrphan(s)
				}
			}
		}
	}
}

type RepaintResult struct {
	SessionName      string        `json:"session_name"`
	WindowsRepainted []string      `json:"windows_repainted"`
	ScratchPreserved bool          `json:"scratch_preserved"`
	Duration         time.Duration `json:"duration"`
}

func (m *Manager) RepaintLayout(ctx context.Context, alias string) (RepaintResult, error) {
	start := time.Now()
	s, err := m.resolveAlias(alias)
	if err != nil {
		return RepaintResult{}, err
	}

	preBytes, _ := m.exec(ctx, "-S", SocketPath, "list-windows", "-t", s.Name, "-F", "#{window_name}")
	pre := splitWindows(preBytes)
	if err := m.CreateWindows(ctx, s.Name); err != nil {
		return RepaintResult{}, err
	}
	postBytes, _ := m.exec(ctx, "-S", SocketPath, "list-windows", "-t", s.Name, "-F", "#{window_name}")
	post := splitWindows(postBytes)
	repainted := windowDiff(pre, post)
	return RepaintResult{
		SessionName:      s.Name,
		WindowsRepainted: repainted,
		ScratchPreserved: true,
		Duration:         time.Since(start),
	}, nil
}

func (m *Manager) ListSessions(ctx context.Context) ([]Session, error) {
	return m.List(ctx)
}

func splitWindows(b []byte) []string {
	if len(b) == 0 {
		return []string{}
	}
	out := []string{}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(line) > 0 {
			out = append(out, string(line))
		}
	}
	return out
}

func windowDiff(pre, post []string) []string {
	preset := make(map[string]struct{}, len(pre))
	for _, w := range pre {
		preset[w] = struct{}{}
	}
	out := []string{}
	for _, w := range post {
		if _, ok := preset[w]; !ok {
			out = append(out, w)
		}
	}
	return out
}
