// SPDX-License-Identifier: MIT
package reload

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (w *Watcher) healthCheckInterval() time.Duration {
	d := w.stallTimeout / 5
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	return d
}

func (w *Watcher) runHealthMonitor(ctx context.Context) {
	t := time.NewTicker(w.healthCheckInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.lastEventAtMu.Lock()
			last := w.lastEventAt
			w.lastEventAtMu.Unlock()
			now := w.clock.Now()

			if last.IsZero() {
				w.lastEventAtMu.Lock()
				w.lastEventAt = now
				w.lastEventAtMu.Unlock()
				continue
			}
			if now.Sub(last) > w.stallTimeout {
				w.emit(ctx, DoctrineWatcherStalled{
					LastEventAt:     last,
					StallTimeoutSec: int(w.stallTimeout.Seconds()),
					StaleSec:        int(now.Sub(last).Seconds()),
					At:              now.UTC(),
				})

				select {
				case w.restartNeeded <- struct{}{}:
				default:
				}

				w.lastEventAtMu.Lock()
				w.lastEventAt = now
				w.lastEventAtMu.Unlock()
			}
		}
	}
}

func (w *Watcher) performRestart(ctx context.Context, reason string) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("reload: restart: %w", err)
	}
	w.fsWatcherMu.Lock()
	if w.fsWatcher != nil {
		_ = w.fsWatcher.Close()
	}
	w.fsWatcher = fsw
	w.fsWatcherMu.Unlock()

	w.perProjectMap.Range(func(k, _ any) bool {
		path := k.(string)
		if err := fsw.Add(path); err != nil {
			w.emit(ctx, DoctrineReloadFailed{
				Path:   path,
				Phase:  "load",
				Errors: []string{fmt.Sprintf("re-add after restart: %v", err)},
				At:     w.clock.Now().UTC(),
			})
		}
		return true
	})
	w.emit(ctx, DoctrineWatcherRestarted{
		Reason: reason,
		At:     w.clock.Now().UTC(),
	})
	return nil
}

func (w *Watcher) handleOverflow(ctx context.Context, _ error) {
	count := 0
	affected := []string{}
	w.perProjectMap.Range(func(k, _ any) bool {
		path := k.(string)
		w.forcedSource.Store(path, "force-reload-all")
		w.runReloadAction(ctx, path)
		count++
		affected = append(affected, path)
		return true
	})
	w.emit(ctx, DoctrineWatcherOverflow{
		ReReadAllPaths: count,
		Action:         "force-reload-all",
		AffectedFiles:  affected,
		At:             w.clock.Now().UTC(),
	})

	select {
	case w.restartNeeded <- struct{}{}:
	default:
	}
}

func (w *Watcher) HandleOverflowForTest(ctx context.Context, err error) {
	w.handleOverflow(ctx, err)
}

// IsOverflowErrorForTest exposes isOverflowError to _test-package callers
// so coverage exercises the production predicate (not a copy-paste mirror).
// Production callers MUST go through the Start loop's error-channel branch,
// which calls isOverflowError directly. This wrapper is unused outside of
// tests and is intentionally named with the ForTest suffix so it stands out
// in production code review.
func (w *Watcher) IsOverflowErrorForTest(err error) bool {
	return isOverflowError(err)
}

// RestartNeededSignalForTest reports whether the restartNeeded channel
// has a pending signal at call time (drains the signal as a side
// effect). Used by invariant behavioural sister-tests to verify that
// handleOverflow dispatched a restart request to the Start loop after
// force-reload-all. Production callers MUST NOT consume this signal —
// Start is the sole owner of restartNeeded receives; in unit tests
// where Start is not running, the drain has no production impact.
func (w *Watcher) RestartNeededSignalForTest() bool {
	select {
	case <-w.restartNeeded:
		return true
	default:
		return false
	}
}

func isOverflowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "overflow") || strings.Contains(msg, "ewatchqueueoverflow")
}
