// SPDX-License-Identifier: MIT
package reload

import (
	"context"
	"fmt"
)

const subscriberBufferSize = 8

// NotifyForce triggers an immediate (post-debounce-bypass) reload of path.
// Used Applier post-Qx-4-approval to ensure synchronous reload
// kick after writing the TOML to filesystem; the resulting DoctrineReloaded
// event carries Source="amendment-apply".
//
// Returns an error if path is not registered (caller MUST AddPath first).
// On a registered path, NotifyForce cancels any pending debounce timer for
// the path then runs the validate-then-swap pipeline inline; the call
// returns when the pipeline (or its short-circuit failure) completes.
//
// Re-entrancy: concurrent NotifyForce calls for distinct paths run in
// parallel. For the same path, calls serialize naturally because
// runReloadAction's failureCounter + active accessor delegations are all
// concurrency-safe; back-to-back calls for the same path observe the
// effects of the prior call (e.g., cleared failure counter on success).
func (w *Watcher) NotifyForce(path string) error {
	if _, ok := w.perProjectMap.Load(path); !ok {
		return fmt.Errorf("reload: NotifyForce(%q): path not registered (call AddPath first)", path)
	}

	if w.debouncer != nil {
		w.debouncer.cancel(path)
	}

	w.forcedSource.Store(path, "amendment-apply")
	w.runReloadAction(context.Background(), path)
	return nil
}

// SubscribeReloadEvents returns a channel that emits DoctrineReloaded
// events. HADES design Applier subscribes to wait for reload completion
// synchronously after NotifyForce. Channel is buffered; emit drops on
// full buffer (slow subscriber does not block fan-out).
//
// Caller MUST call UnsubscribeReloadEvents to free resources.
func (w *Watcher) SubscribeReloadEvents() <-chan DoctrineReloaded {
	w.reloadEventMu.Lock()
	defer w.reloadEventMu.Unlock()
	ch := make(chan DoctrineReloaded, subscriberBufferSize)
	w.reloadEventSubs = append(w.reloadEventSubs, ch)
	return ch
}

func (w *Watcher) UnsubscribeReloadEvents(ch <-chan DoctrineReloaded) {
	w.reloadEventMu.Lock()
	defer w.reloadEventMu.Unlock()
	for i, s := range w.reloadEventSubs {

		if (<-chan DoctrineReloaded)(s) == ch {
			w.reloadEventSubs = append(w.reloadEventSubs[:i], w.reloadEventSubs[i+1:]...)
			return
		}
	}
}

func (w *Watcher) SubscribeReloadFailedEvents() <-chan DoctrineReloadFailed {
	w.reloadFailedEventMu.Lock()
	defer w.reloadFailedEventMu.Unlock()
	ch := make(chan DoctrineReloadFailed, subscriberBufferSize)
	w.reloadFailedEventSubs = append(w.reloadFailedEventSubs, ch)
	return ch
}

func (w *Watcher) UnsubscribeReloadFailedEvents(ch <-chan DoctrineReloadFailed) {
	w.reloadFailedEventMu.Lock()
	defer w.reloadFailedEventMu.Unlock()
	for i, s := range w.reloadFailedEventSubs {
		if (<-chan DoctrineReloadFailed)(s) == ch {
			w.reloadFailedEventSubs = append(w.reloadFailedEventSubs[:i], w.reloadFailedEventSubs[i+1:]...)
			return
		}
	}
}

func (w *Watcher) broadcastReloadEvent(ev DoctrineReloaded) {
	w.reloadEventMu.Lock()
	defer w.reloadEventMu.Unlock()
	for _, s := range w.reloadEventSubs {
		select {
		case s <- ev:
		default:

		}
	}
}

func (w *Watcher) broadcastReloadFailedEvent(ev DoctrineReloadFailed) {
	w.reloadFailedEventMu.Lock()
	defer w.reloadFailedEventMu.Unlock()
	for _, s := range w.reloadFailedEventSubs {
		select {
		case s <- ev:
		default:
		}
	}
}
