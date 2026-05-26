// SPDX-License-Identifier: MIT
//
// Provides:
//   - KillProcess(pid) — SIGKILL a subprocess.
//   - WaitProcessGone(pid) — block until process is gone.
//   - RegisterTrigger / FireTrigger — named callback events.
//   - SendSignal(pid, sig) — arbitrary signal delivery.
//
// Used by tests/chaos/audit_chain_chaos_test.go (K-7) to drive
// daemon mid-operation kills (mid-batch, mid-seal, Litestream crash).
//
// No SQLite dependency — pure `os`/`syscall`/`time` — so this file
// stays in the testhelpers root (unlike the tamper injector which
// requires sub-package isolation for SQLite driver collision avoidance).
package testhelpers

import (
	"context"
	"os"
	"sync"
	"syscall"
	"time"
)

type CrashInjector struct {
	mu       sync.Mutex
	triggers map[string]func()
}

func NewCrashInjector() *CrashInjector {
	return &CrashInjector{triggers: make(map[string]func())}
}

func (c *CrashInjector) KillProcess(ctx context.Context, pid int) error {
	proc, _ := os.FindProcess(pid)
	_ = ctx
	return proc.Signal(syscall.SIGKILL)
}

func (c *CrashInjector) WaitProcessGone(ctx context.Context, pid int) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:

			proc, _ := os.FindProcess(pid)

			if err := proc.Signal(syscall.Signal(0)); err != nil {
				return nil
			}
		}
	}
}

func (c *CrashInjector) RegisterTrigger(name string, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.triggers[name] = fn
}

func (c *CrashInjector) FireTrigger(name string) {
	c.mu.Lock()
	fn := c.triggers[name]
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *CrashInjector) SendSignal(pid int, sig syscall.Signal) error {
	proc, _ := os.FindProcess(pid)
	return proc.Signal(sig)
}
