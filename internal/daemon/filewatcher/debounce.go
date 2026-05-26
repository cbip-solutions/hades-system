// SPDX-License-Identifier: MIT
package filewatcher

import (
	"sync"
	"time"
)

type Debouncer struct {
	window time.Duration

	mu      sync.Mutex
	timers  map[string]*time.Timer
	pending map[string]Event
}

func NewDebouncer(window time.Duration) *Debouncer {
	if window == 0 {
		window = 1500 * time.Millisecond
	}
	return &Debouncer{
		window:  window,
		timers:  make(map[string]*time.Timer),
		pending: make(map[string]Event),
	}
}

func (d *Debouncer) Submit(e Event, emit func(Event)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pending[e.Path] = e
	if t, ok := d.timers[e.Path]; ok {
		t.Stop()
	}
	d.timers[e.Path] = time.AfterFunc(d.window, func() {
		d.mu.Lock()
		ev, ok := d.pending[e.Path]
		delete(d.pending, e.Path)
		delete(d.timers, e.Path)
		d.mu.Unlock()
		if ok {
			emit(ev)
		}
	})
}

func (d *Debouncer) Window() time.Duration { return d.window }
