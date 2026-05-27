// SPDX-License-Identifier: MIT
package tessera

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

type Manager struct {
	dataRoot string
	cfg      Config

	witness    *Witness
	checkpoint *Checkpoint
	cosigner   *CoSigner

	mu       sync.Mutex
	adapters map[string]*Adapter
}

func NewManager(ctx context.Context, dataRoot string, cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dataRoot == "" {
		return nil, fmt.Errorf("tessera: manager dataRoot must be non-empty")
	}
	w := NewWitness()
	if _, err := w.Load(); err != nil {

		if !errors.Is(err, ErrWitnessKeyMissing) {
			return nil, fmt.Errorf("tessera: manager witness load: %w", err)
		}
		if _, err := w.Generate(); err != nil {
			return nil, fmt.Errorf("tessera: manager witness generate: %w", err)
		}
	}
	cpDir := filepath.Join(dataRoot, "global", "daemon_checkpoint")
	cp, err := NewCheckpoint(ctx, cpDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("tessera: manager checkpoint: %w", err)
	}
	cs := NewCoSigner(w, cp)
	return &Manager{
		dataRoot:   dataRoot,
		cfg:        cfg,
		witness:    w,
		checkpoint: cp,
		cosigner:   cs,
		adapters:   map[string]*Adapter{},
	}, nil
}

func (m *Manager) Witness() *Witness { return m.witness }

func (m *Manager) Checkpoint() *Checkpoint { return m.checkpoint }

func (m *Manager) CoSigner() *CoSigner { return m.cosigner }

// ProjectAdapter returns the per-project Adapter, lazy-creating it
// on first use. Subsequent calls with the same projectID return the
// cached instance — which is required: two distinct Adapters for the
// same project would each open a posixStorage handle pointed at the
// same on-disk tile-log dir, and Tessera's posix backend does NOT
// support concurrent appenders.
//
// Side effects on first creation:
//
// 1. NewProjectAdapter constructs the per-project tile-log under
// <dataRoot>/projects/<projectID>/audit/tessera and wires the
// POSIX storage + Tessera Appender.
// 2. The new Adapter is attached to the singleton witness via
// a.Attach(witness) so Adapter.WitnessCoSignSeal can produce
// daemon-witness signatures over partition-seal payloads (A-6b).
// Without Attach, every WitnessCoSignSeal call would return
// ErrWitnessKeyMissing.
// 3. The cosigner subscribes to the new Adapter's STH stream via
// CoSigner.SubscribeAdapter(a). Subscription returns an error
// post-A-6 fix (the Adapter rejects subscription on a closed
// handle); we propagate it after closing the Adapter to avoid
// leaking the storage + background goroutines for an Adapter that
// would never reach the daemon-global path.
//
// Returns the cached or newly-created Adapter. Caller MUST NOT call
// Close on the returned Adapter — Manager.Close handles the lifecycle.
func (m *Manager) ProjectAdapter(ctx context.Context, projectID string) (*Adapter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.adapters[projectID]; ok {
		return a, nil
	}
	a, err := NewProjectAdapter(ctx, projectID, m.dataRoot, m.cfg)
	if err != nil {
		return nil, err
	}

	a.Attach(m.witness)
	if err := m.cosigner.SubscribeAdapter(a); err != nil {

		_ = a.Close()
		return nil, fmt.Errorf("tessera: manager subscribe cosigner for project %s: %w", projectID, err)
	}
	m.adapters[projectID] = a
	return a, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	adapters := m.adapters
	m.adapters = nil
	m.mu.Unlock()
	var firstErr error
	for _, a := range adapters {
		if err := a.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if m.checkpoint != nil {
		if err := m.checkpoint.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
