// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

type ExecStarter func(ctx context.Context, name string, arg ...string) *exec.Cmd

var ErrProjectAlreadyManaged = errors.New("litestream: project already managed; stop first")

type supervisor struct {
	projectID string
	cfgPath   string
	starter   ExecStarter
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	backoffInitial time.Duration
	backoffCap     time.Duration

	env []string
}

type Manager struct {
	starter ExecStarter
	mu      sync.Mutex
	supers  map[string]*supervisor

	backoffInitial time.Duration
	backoffCap     time.Duration

	// envForProject returns the env vars (KEY=VALUE) the litestream
	// subprocess should inherit in addition to os.Environ(). Daemon main
	// wires this via SetEnvForProject (lifecycle.go) to a
	// closure that reads S3 creds from Keychain at spawn time — handles
	// operator-rotation transparently. Tests substitute a deterministic
	// closure. Nil envForProject = no extra env (the subprocess inherits
	// the parent's env unfiltered, which is the daemon's env minus the
	// AWS keys — the daemon process MUST NOT hold those keys in its own
	// environment).
	envForProject func(projectID string) []string
}

func NewManager(starter ExecStarter) *Manager {
	if starter == nil {
		starter = exec.CommandContext
	}
	return &Manager{
		starter:        starter,
		supers:         make(map[string]*supervisor),
		backoffInitial: 1 * time.Second,
		backoffCap:     60 * time.Second,
	}
}

func NewManagerForTest(starter ExecStarter) *Manager {
	return NewManager(starter)
}

func (m *Manager) StartProject(ctx context.Context, projectID, cfgPath string) error {
	if projectID == "" {
		return errors.New("litestream: empty project_id")
	}
	if cfgPath == "" {
		return errors.New("litestream: empty config path")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.supers[projectID]; ok {
		return fmt.Errorf("%w: %s", ErrProjectAlreadyManaged, projectID)
	}
	supCtx, cancel := context.WithCancel(ctx)
	sup := &supervisor{
		projectID:      projectID,
		cfgPath:        cfgPath,
		starter:        m.starter,
		ctx:            supCtx,
		cancel:         cancel,
		done:           make(chan struct{}),
		backoffInitial: m.backoffInitial,
		backoffCap:     m.backoffCap,
	}
	if m.envForProject != nil {
		sup.env = m.envForProject(projectID)
	}
	m.supers[projectID] = sup
	go sup.run()
	return nil
}

func (m *Manager) StopProject(ctx context.Context, projectID string) error {
	m.mu.Lock()
	sup, ok := m.supers[projectID]
	if ok {
		delete(m.supers, projectID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	sup.cancel()
	select {
	case <-sup.done:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("litestream: stop timeout for %s", projectID)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.supers))
	for id := range m.supers {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var first error
	for _, id := range ids {
		if err := m.StopProject(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *supervisor) run() {
	defer close(s.done)
	backoff := s.backoffInitial
	for {
		if s.ctx.Err() != nil {
			return
		}
		cmd := s.starter(s.ctx, "litestream", "replicate", "-config", s.cfgPath)
		if len(s.env) > 0 {
			// Inherit parent env + append per-project secrets. Stdlib
			// semantics: non-nil cmd.Env REPLACES the parent inheritance,
			// so we must concatenate explicitly (defense-in-depth: the
			// daemon's own env MUST NOT contain AWS keys).
			cmd.Env = append(append([]string(nil), os.Environ()...), s.env...)
		}

		err := cmd.Run()
		if s.ctx.Err() != nil {
			return
		}
		_ = err

		select {
		case <-time.After(jittered(backoff)):
		case <-s.ctx.Done():
			return
		}

		backoff = minDuration(backoff*2, s.backoffCap)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}

	mod := time.Now().UnixNano() % 200
	scale := time.Duration(int64(d) * (mod - 100) / 1000)
	return d + scale
}
