// SPDX-License-Identifier: MIT
// Package clock provides an injectable wall-clock + timer abstraction
// (Q14 C). Production code uses Real{}; tests inject *Fake to drive
// deterministic time advancement for HRA cadence, recovery
// heartbeat, amendment cooldown, and the
// time-accelerated test tier.
//
// Invariant Real and Fake satisfy identical Clock semantics modulo
// monotonicity (Fake's Now is operator-controlled; Real is wall-clock).
package clock

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Clock is the injectable wall-clock + timer abstraction.
// All orchestrator/* timed code MUST take a Clock parameter and never
// call time.Now/time.NewTimer/time.NewTicker/time.Sleep/time.AfterFunc
// directly. A vet-style lint enforces this rule.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker

	Sleep(d time.Duration)

	AfterFunc(d time.Duration, fn func()) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) Since(t time.Time) time.Duration { return time.Since(t) }

func (Real) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

func (Real) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

func (Real) Sleep(d time.Duration) { time.Sleep(d) }

func (Real) AfterFunc(d time.Duration, fn func()) Timer {
	return realTimer{time.AfterFunc(d, fn)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

type Fake struct {
	mu      sync.Mutex
	now     time.Time
	pending []*pendingFire

	ticks atomic.Int32
}

type pendingFire struct {
	deadline time.Time
	period   time.Duration
	ch       chan time.Time
	stopped  atomic.Bool
}

func NewFake(base time.Time) *Fake { return &Fake{now: base} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Since(t time.Time) time.Duration { return f.Now().Sub(t) }

func (f *Fake) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &pendingFire{
		deadline: f.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	if d <= 0 {
		p.ch <- f.now
		p.stopped.Store(true)
		return &fakeTimer{p: p}
	}
	f.pending = append(f.pending, p)
	return &fakeTimer{p: p}
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: Fake.NewTicker non-positive duration")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &pendingFire{
		deadline: f.now.Add(d),
		period:   d,
		ch:       make(chan time.Time, 1),
	}
	f.pending = append(f.pending, p)
	return &fakeTicker{p: p}
}

func (f *Fake) Advance(d time.Duration) {
	if d < 0 {
		panic("clock: Fake.Advance with negative duration")
	}
	f.mu.Lock()
	target := f.now.Add(d)

	for {

		due := make([]*pendingFire, 0, len(f.pending))
		for _, p := range f.pending {
			if p.stopped.Load() {
				continue
			}
			if !p.deadline.After(target) {
				due = append(due, p)
			}
		}
		if len(due) == 0 {
			break
		}

		sort.SliceStable(due, func(i, j int) bool {
			return due[i].deadline.Before(due[j].deadline)
		})
		for i, p := range due {

			if p.stopped.Load() {
				continue
			}
			fireAt := p.deadline
			f.now = fireAt
			f.mu.Unlock()

			p.ch <- fireAt
			runtime.Gosched()

			if i+1 < len(due) {
				for k := 0; k < 4; k++ {
					runtime.Gosched()
				}
			}
			f.ticks.Add(1)
			f.mu.Lock()
			if p.period > 0 {
				p.deadline = fireAt.Add(p.period)
			} else {
				p.stopped.Store(true)
			}
		}
	}
	f.now = target

	live := f.pending[:0]
	for _, p := range f.pending {
		if !p.stopped.Load() {
			live = append(live, p)
		}
	}
	f.pending = live
	f.mu.Unlock()
}

func (f *Fake) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	tm := f.NewTimer(d)
	<-tm.C()
}

func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	tm := f.NewTimer(d).(*fakeTimer)
	tm.cancel = make(chan struct{})
	go func() {
		select {
		case fired, ok := <-tm.p.ch:
			_ = fired
			_ = ok

			if !tm.cancelled.Load() {
				fn()
			}
		case <-tm.cancel:

		}
	}()
	return tm
}

func (f *Fake) BlockUntilN(n int32, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.ticks.Load() >= n {

			for i := 0; i < 4; i++ {
				runtime.Gosched()
			}
			time.Sleep(time.Millisecond)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *Fake) BlockUntilCondition(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}

	return cond()
}

type fakeTimer struct {
	p *pendingFire

	cancel    chan struct{}
	cancelled atomic.Bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.p.ch }
func (t *fakeTimer) Stop() bool {
	wasLive := !t.p.stopped.Load()
	t.p.stopped.Store(true)

	if t.cancel != nil && t.cancelled.CompareAndSwap(false, true) {
		close(t.cancel)
	}
	return wasLive
}

type fakeTicker struct {
	p *pendingFire
}

func (t *fakeTicker) C() <-chan time.Time { return t.p.ch }
func (t *fakeTicker) Stop()               { t.p.stopped.Store(true) }
