// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"fmt"
)

type WindowName string

const (
	WindowOrch WindowName = "orch"

	WindowLeads WindowName = "leads"

	WindowWorkers WindowName = "workers"

	WindowHRA WindowName = "hra"

	WindowLogs WindowName = "logs"

	WindowScratch WindowName = "scratch"
)

// DaemonOwnedWindows is the canonical ordered list of windows the daemon
// creates and maintains. Order matches tmux index assignment after
// CreateWindows 0=orch, 1=leads, 2=workers, 3=hra, 4=logs.
//
// inv-hades-118 invariant: WindowScratch MUST NOT appear in this list.
// Test TestDaemonOwnedExcludesScratch enforces.
var DaemonOwnedWindows = []WindowName{
	WindowOrch,
	WindowLeads,
	WindowWorkers,
	WindowHRA,
	WindowLogs,
}

var OperatorOwnedWindows = []WindowName{
	WindowScratch,
}

var AllWindows = func() []WindowName {
	out := make([]WindowName, 0, len(DaemonOwnedWindows)+len(OperatorOwnedWindows))
	out = append(out, DaemonOwnedWindows...)
	out = append(out, OperatorOwnedWindows...)
	return out
}()

func IsValidWindowName(w WindowName) bool {
	for _, v := range AllWindows {
		if v == w {
			return true
		}
	}
	return false
}

func (m *Manager) CreateWindows(ctx context.Context, sessionName string) error {

	if _, err := m.exec(ctx, "-S", SocketPath,
		"rename-window", "-t", sessionName+":0", string(WindowOrch),
	); err != nil {
		return fmt.Errorf("tmuxlife.CreateWindows: rename-window 0 → orch: %w", err)
	}

	// Step 2: create the remaining 5 windows in canonical order. scratch
	// MUST come last so it lands at the highest tmux index, simplifying
	// tmux-resurrect's exclusion script (Q6 D + inv-hades-118).
	rest := []WindowName{
		WindowLeads, WindowWorkers, WindowHRA, WindowLogs, WindowScratch,
	}
	for _, w := range rest {
		if _, err := m.exec(ctx, "-S", SocketPath,
			"new-window", "-t", sessionName, "-n", string(w),
		); err != nil {
			return fmt.Errorf("tmuxlife.CreateWindows: new-window %s: %w", w, err)
		}
	}
	return nil
}
