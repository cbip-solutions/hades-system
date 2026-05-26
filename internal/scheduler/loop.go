// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type LoopBinding interface {
	ID() string
	Done() <-chan struct{}
}

type LoopParams struct {
	ID string

	ProjectAlias string

	Action string

	Interval time.Duration
}

type Loop struct {
	id           string
	projectAlias string
	action       string

	mu       sync.RWMutex
	interval time.Duration
	sessID   string

	cancel context.CancelFunc

	done     chan struct{}
	doneOnce sync.Once
}

func NewLoop(p LoopParams) (*Loop, error) {
	if p.ID == "" {
		return nil, fmt.Errorf("%w: empty ID", ErrInvalidSchedule)
	}
	if p.ProjectAlias == "" {
		return nil, fmt.Errorf("%w: empty ProjectAlias", ErrInvalidSchedule)
	}
	if p.Action == "" {
		return nil, fmt.Errorf("%w: empty Action", ErrInvalidSchedule)
	}
	if p.Interval < time.Minute {
		return nil, fmt.Errorf("%w: interval %v < 1min floor",
			ErrInvalidSchedule, p.Interval)
	}
	return &Loop{
		id:           p.ID,
		projectAlias: p.ProjectAlias,
		action:       p.Action,
		interval:     p.Interval,
		done:         make(chan struct{}),
	}, nil
}

func (l *Loop) ID() string { return l.id }

func (l *Loop) ProjectAlias() string { return l.projectAlias }

func (l *Loop) Action() string { return l.action }

// Bind attaches the loop to a session. MUST be called exactly once
// before relying on Done() / SessionID(). Spawns a watcher goroutine
// that closes Done() when:
//
//   - the parent context (`ctx`) cancels (daemon shutdown), OR
//   - the bound session's Done() fires (tmux session died).
//
// Either path drains atomically: `done` closes exactly once via
// sync.Once, the watcher returns, and Done() callers unblock.
//
// Refuses
//
//   - nil session         → ErrInvalidSchedule (defence in depth).
//   - second Bind on same → returns "already bound" error; the
//     watcher goroutine spawned by the first Bind owns the cancel
//     func and the close(done) responsibility.
//
// Why parent ctx is captured (not stored): we derive a child ctx with
// cancel and store ONLY the cancel func + bind result. The watcher
// goroutine holds the child ctx in a local variable for its select,
// so the parent ctx doesn't leak into Loop's struct (which would
// muddle the gc story).
func (l *Loop) Bind(ctx context.Context, sess LoopBinding) error {
	if sess == nil {
		return fmt.Errorf("%w: nil session", ErrInvalidSchedule)
	}
	l.mu.Lock()
	if l.cancel != nil {
		bound := l.sessID
		l.mu.Unlock()
		return fmt.Errorf("loop already bound to session %q", bound)
	}
	l.sessID = sess.ID()
	loopCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	l.mu.Unlock()

	go func() {
		defer l.doneOnce.Do(func() { close(l.done) })
		select {
		case <-sess.Done():
			cancel()
		case <-loopCtx.Done():

		}
	}()
	return nil
}

func (l *Loop) Done() <-chan struct{} { return l.done }

func (l *Loop) SessionID() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sessID
}

func (l *Loop) Interval() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.interval
}

func (l *Loop) SetInterval(d time.Duration) error {
	if d < time.Minute {
		return fmt.Errorf("%w: interval %v < 1min floor",
			ErrInvalidSchedule, d)
	}
	l.mu.Lock()
	l.interval = d
	l.mu.Unlock()
	return nil
}

func (l *Loop) Stop() {
	l.mu.RLock()
	cancel := l.cancel
	l.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}
