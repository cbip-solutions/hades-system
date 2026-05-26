// SPDX-License-Identifier: MIT
package reload

import (
	"context"
	"sync"
	"time"
)

type Debouncer struct {
	window time.Duration
	action func(ctx context.Context, path string)

	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
}

func NewDebouncer(window time.Duration, action func(ctx context.Context, path string)) *Debouncer {
	return &Debouncer{
		window: window,
		action: action,
		timers: map[string]*time.Timer{},
	}
}

func (d *Debouncer) Trigger(ctx context.Context, path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if t, ok := d.timers[path]; ok {
		t.Stop()
	}
	d.timers[path] = time.AfterFunc(d.window, func() {
		d.mu.Lock()

		delete(d.timers, path)
		closed := d.closed
		d.mu.Unlock()
		if closed {
			return
		}

		d.action(ctx, path)
	})
}

func (d *Debouncer) cancel(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[path]; ok {
		t.Stop()
		delete(d.timers, path)
	}
}

func (d *Debouncer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	for _, t := range d.timers {
		t.Stop()
	}
	d.timers = map[string]*time.Timer{}
}
