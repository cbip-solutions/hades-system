// SPDX-License-Identifier: MIT
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

const (
	DefaultConfirmationWatcherInterval = 30 * time.Second

	DefaultConfirmationTimeout = time.Hour
)

var ErrConfirmationWatcherInvalidConfig = errors.New("orchestrator: confirmation watcher invalid config")

type ConfirmationWatcherConfig struct {
	Handler  *ConfirmationHandler
	Clock    clock.Clock
	Interval time.Duration
	Timeout  time.Duration
}

type ConfirmationWatcher struct {
	handler  *ConfirmationHandler
	clk      clock.Clock
	interval time.Duration
	timeout  time.Duration
}

func NewConfirmationWatcher(cfg ConfirmationWatcherConfig) (*ConfirmationWatcher, error) {
	if cfg.Handler == nil {
		return nil, fmt.Errorf("%w: handler is nil", ErrConfirmationWatcherInvalidConfig)
	}
	if cfg.Interval < 0 {
		return nil, fmt.Errorf("%w: interval is negative", ErrConfirmationWatcherInvalidConfig)
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("%w: timeout is negative", ErrConfirmationWatcherInvalidConfig)
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultConfirmationWatcherInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultConfirmationTimeout
	}
	return &ConfirmationWatcher{
		handler:  cfg.Handler,
		clk:      cfg.Clock,
		interval: cfg.Interval,
		timeout:  cfg.Timeout,
	}, nil
}

func (w *ConfirmationWatcher) Run(ctx context.Context) {
	ticker := w.clk.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			_ = w.sweepOnce(ctx)
		}
	}
}

func (w *ConfirmationWatcher) sweepOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pending, ok := w.handler.pendingSnapshot()
	if !ok {
		return nil
	}
	if w.clk.Since(pending.requestedAt) < w.timeout {
		return nil
	}
	err := w.handler.HandleDeny(ctx, DenyInput{
		EventID:   pending.eventID,
		Rationale: fmt.Sprintf("confirmation timeout after %s", w.timeout),
		Operator: OperatorIdentity{
			UID:    0,
			Reason: "confirmation_timeout_watcher",
		},
	})
	if errors.Is(err, ErrConfirmationStale) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("orchestrator: confirmation watcher deny timeout: %w", err)
	}
	return nil
}

func (h *ConfirmationHandler) pendingSnapshot() (pendingRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return pendingRequest{}, false
	}
	return *h.pending, true
}
