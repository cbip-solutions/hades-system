// SPDX-License-Identifier: MIT
package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrPlan5BackgroundSupervisorInvalidConfig = errors.New("daemon/plan5 background supervisor: invalid config")

var ErrPlan5BackgroundSupervisorStarted = errors.New("daemon/plan5 background supervisor: already started")

type Plan5BackgroundRunner struct {
	Name  string
	Slots int
	Run   func(context.Context)
}

type Plan5BackgroundSupervisor struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	started bool

	wg    sync.WaitGroup
	count atomic.Int32
	names []string
}

func NewPlan5BackgroundSupervisor() *Plan5BackgroundSupervisor {
	return &Plan5BackgroundSupervisor{}
}

func (s *Plan5BackgroundSupervisor) Start(parent context.Context, runners ...Plan5BackgroundRunner) error {
	if parent == nil {
		return fmt.Errorf("%w: parent context is nil", ErrPlan5BackgroundSupervisorInvalidConfig)
	}
	for i, r := range runners {
		if r.Name == "" {
			return fmt.Errorf("%w: runner %d name is empty", ErrPlan5BackgroundSupervisorInvalidConfig, i)
		}
		if r.Slots < 0 {
			return fmt.Errorf("%w: runner %q slots must be >= 0", ErrPlan5BackgroundSupervisorInvalidConfig, r.Name)
		}
		if r.Run == nil {
			return fmt.Errorf("%w: runner %q Run is nil", ErrPlan5BackgroundSupervisorInvalidConfig, r.Name)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrPlan5BackgroundSupervisorStarted
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.started = true
	s.names = make([]string, 0, len(runners))
	for _, r := range runners {
		runner := r
		s.names = append(s.names, runner.Name)
		s.wg.Add(1)
		s.count.Add(int32(runner.Slots))
		go func() {
			defer s.wg.Done()
			defer s.count.Add(-int32(runner.Slots))
			defer func() { _ = recover() }()
			runner.Run(ctx)
		}()
	}
	return nil
}

func (s *Plan5BackgroundSupervisor) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}

func (s *Plan5BackgroundSupervisor) Count() int {
	if s == nil {
		return 0
	}
	return int(s.count.Load())
}

func (s *Plan5BackgroundSupervisor) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("%w: stop context is nil", ErrPlan5BackgroundSupervisorInvalidConfig)
	}
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
