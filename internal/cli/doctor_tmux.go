// SPDX-License-Identifier: MIT
// Package cli — doctor_tmux.go
//
// task: tmux subsystem doctor probe. Five aspects per
// spec §6.7 (binary.installed, server.reachable, session.count,
// drift.count, socket.permissions).
//
// invariant anchor: every tmux invocation MUST go through ExecTmux
// which enforces -S flag (forbids default socket /tmp/tmux-<uid>).
// The Prober honours this discipline by delegating to the live tmux
// adapter rather than shelling tmux directly.
package cli

import (
	"context"
	"fmt"
)

type TmuxProber interface {
	BinaryVersion(ctx context.Context) (version string, meetsMin bool, err error)

	ServerReachable(ctx context.Context) error

	SessionCount(ctx context.Context) (int, error)

	DriftCount(ctx context.Context) (int, error)

	SocketPermissions(ctx context.Context) (mode string, err error)
}

const (
	tmuxDriftWarnAt   = 1
	tmuxDriftFailAt   = 3
	tmuxSocketModeMin = "0600"
)

func RunTmuxProbe(ctx context.Context, p TmuxProber) ([]ProbeResult, error) {
	out := make([]ProbeResult, 0, 5)
	out = append(out, runTmuxBinary(ctx, p))
	out = append(out, runTmuxServer(ctx, p))
	out = append(out, runTmuxSessionCount(ctx, p))
	out = append(out, runTmuxDrift(ctx, p))
	out = append(out, runTmuxSocketPerms(ctx, p))
	return out, nil
}

func runTmuxBinary(ctx context.Context, p TmuxProber) ProbeResult {
	r := ProbeResult{Name: "tmux.binary.installed"}
	v, meetsMin, err := p.BinaryVersion(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "tmux binary not found"
		r.Detail = err.Error()
		r.Hint = "macOS: brew install tmux; Linux: apt/dnf install tmux"
		return r
	}
	if !meetsMin {
		r.Status = ProbeFail
		r.Message = fmt.Sprintf("tmux %s installed; min required 3.4", v)
		r.Hint = "upgrade tmux: brew upgrade tmux (macOS); spec §1 design choice requires ≥3.4 for nested-session window-rename feature"
		return r
	}
	r.Status = ProbeOK
	r.Message = fmt.Sprintf("tmux %s (≥3.4)", v)
	return r
}

func runTmuxServer(ctx context.Context, p TmuxProber) ProbeResult {
	r := ProbeResult{Name: "tmux.server.reachable"}
	if err := p.ServerReachable(ctx); err != nil {
		r.Status = ProbeFail
		r.Message = "tmux server not responding"
		r.Detail = err.Error()
		r.Hint = "tmux daemon socket /tmp/hades-system.sock unresponsive; restart daemon: hades daemon restart"
		return r
	}
	r.Status = ProbeOK
	r.Message = "/tmp/hades-system.sock responsive"
	return r
}

func runTmuxSessionCount(ctx context.Context, p TmuxProber) ProbeResult {
	r := ProbeResult{Name: "tmux.session.count"}
	n, err := p.SessionCount(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "session_count query failed"
		r.Detail = err.Error()
		return r
	}
	r.Status = ProbeOK
	r.Message = fmt.Sprintf("%d active sessions", n)
	return r
}

func runTmuxDrift(ctx context.Context, p TmuxProber) ProbeResult {
	r := ProbeResult{Name: "tmux.drift.count"}
	n, err := p.DriftCount(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "drift_count query failed"
		r.Detail = err.Error()
		return r
	}
	r.Message = fmt.Sprintf("%d orphaned sessions", n)
	switch {
	case n >= tmuxDriftFailAt:
		r.Status = ProbeFail
		r.Hint = "chronic drift suggests drift-poller goroutine wedged; restart daemon: hades daemon restart"
	case n >= tmuxDriftWarnAt:
		r.Status = ProbeWarn
		r.Hint = "drift recovers on next activation per design contract; if persistent: hades sessions ls"
	default:
		r.Status = ProbeOK
	}
	return r
}

func runTmuxSocketPerms(ctx context.Context, p TmuxProber) ProbeResult {
	r := ProbeResult{Name: "tmux.socket.permissions"}
	mode, err := p.SocketPermissions(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "socket permissions query failed"
		r.Detail = err.Error()
		r.Hint = "verify daemon running and /tmp/hades-system.sock exists; otherwise chmod 0600 /tmp/hades-system.sock"
		return r
	}
	if mode != tmuxSocketModeMin {
		r.Status = ProbeFail
		r.Message = fmt.Sprintf("socket mode = %s (want %s)", mode, tmuxSocketModeMin)
		r.Hint = "chmod 0600 /tmp/hades-system.sock; spec §7.3 mandates owner-only socket access"
		return r
	}
	r.Status = ProbeOK
	r.Message = fmt.Sprintf("mode=%s (owner-only)", mode)
	return r
}
