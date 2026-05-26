// SPDX-License-Identifier: MIT
package orchestrator

import (
	"context"
	"sync/atomic"
	"time"
)

type DepHealth struct {
	Up     bool
	Detail string
	Extra  int
}

type HealthSnapshot struct {
	SampledAt time.Time
	Deps      map[string]DepHealth
}

func (s HealthSnapshot) Get(name string) (DepHealth, bool) {
	d, ok := s.Deps[name]
	return d, ok
}

type healthSnapshotPtr = atomic.Pointer[HealthSnapshot]

type HealthSampler struct {
	compute  func(ctx context.Context) HealthSnapshot
	interval time.Duration
	snap     atomic.Pointer[HealthSnapshot]
}

func NewHealthSampler(compute func(ctx context.Context) HealthSnapshot, interval time.Duration) *HealthSampler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s := &HealthSampler{compute: compute, interval: interval}
	empty := HealthSnapshot{Deps: map[string]DepHealth{}}
	s.snap.Store(&empty)
	return s
}

func (s *HealthSampler) Current() HealthSnapshot { return *s.snap.Load() }

func (s *HealthSampler) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		sample := func() {
			snap := s.compute(ctx)
			s.snap.Store(&snap)
		}
		sample()
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sample()
			}
		}
	}()
	return done
}
