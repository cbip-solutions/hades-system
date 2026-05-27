// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

const (
	DefaultDriftWatcherInterval = 5 * time.Minute

	DefaultDriftWatcherWindow = 50
)

var ErrDriftWatcherInvalidConfig = errors.New("safetynet/driftwatcher: invalid config")

type DriftValidator interface {
	Validate(ctx context.Context, n int) (Report, error)
}

type DriftWatcherConfig struct {
	Validator DriftValidator
	Clock     clock.Clock
	Interval  time.Duration
	Window    int
}

type DriftWatcher struct {
	validator DriftValidator
	clk       clock.Clock
	interval  time.Duration
	window    int
	running   atomic.Bool
}

func NewDriftWatcher(cfg DriftWatcherConfig) (*DriftWatcher, error) {
	if cfg.Validator == nil {
		return nil, fmt.Errorf("%w: validator is nil", ErrDriftWatcherInvalidConfig)
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultDriftWatcherInterval
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultDriftWatcherWindow
	}
	return &DriftWatcher{
		validator: cfg.Validator,
		clk:       cfg.Clock,
		interval:  cfg.Interval,
		window:    cfg.Window,
	}, nil
}

func (w *DriftWatcher) Run(ctx context.Context) {
	ticker := w.clk.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			w.startOnce(ctx)
		}
	}
}

func (w *DriftWatcher) startOnce(ctx context.Context) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer w.running.Store(false)
		_, _ = w.validator.Validate(ctx, w.window)
	}()
}
