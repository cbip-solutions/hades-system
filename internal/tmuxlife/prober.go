// SPDX-License-Identifier: MIT
// Package tmuxlife — prober.go
//
// task adapter: exposes a Prober implementation that the
// cli/doctor_tmux.go layer consumes (cli.TmuxProber). Read-only.
//
// invariant anchor: ALL tmux invocations go through ExecTmux which
// enforces -S flag (forbids default socket /tmp/tmux-<uid>). The Prober
// follows the same discipline — its ServerReachable always passes the
// canonical SocketPath constant.
//
// Boundary (invariant): this package does NOT import internal/store.
// SessionStore is the existing interface; the daemon wires
// daemon.db's tmux_session_state via the matching adapter at boot.
//
// adds the cpu_budget / drift / socket-permissions surface to
// the existing C surface (Spawn / Reap / DriftPoller). The new code is
// a thin read-only layer; no mutation of session state happens here.
package tmuxlife

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ExecFunc is the signature used to invoke tmux. Tests pass a fake.
// Production wires to ExecTmux which enforces
// invariant (-S flag mandatory).
//
// MUST be safe for concurrent use.
type ExecFunc func(ctx context.Context, args ...string) ([]byte, error)

// StatFn is the signature used to stat the tmux socket. Tests inject a
// fake; production uses os.Stat.
//
// MUST be safe for concurrent use.
type StatFn func(name string) (os.FileInfo, error)

var ErrProberNilArg = errors.New("tmuxlife.NewProber: nil argument")

type Prober struct {
	store    SessionStore
	exec     ExecFunc
	statFile StatFn
}

func NewProber(store SessionStore, exec ExecFunc) *Prober {
	if store == nil {
		panic(fmt.Errorf("%w: store", ErrProberNilArg))
	}
	if exec == nil {
		panic(fmt.Errorf("%w: exec", ErrProberNilArg))
	}
	return &Prober{
		store:    store,
		exec:     exec,
		statFile: os.Stat,
	}
}

func (p *Prober) BinaryVersion(ctx context.Context) (string, bool, error) {
	out, err := p.exec(ctx, "-V")
	if err != nil {
		return "", false, fmt.Errorf("tmuxlife.Prober.BinaryVersion: %w", err)
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "tmux ")
	meets, err := tmuxVersionAtLeast(v, "3.4")
	if err != nil {
		return v, false, fmt.Errorf("tmuxlife.Prober.BinaryVersion parse: %w", err)
	}
	return v, meets, nil
}

func (p *Prober) ServerReachable(ctx context.Context) error {
	_, err := p.exec(ctx, "-S", SocketPath, "list-sessions")
	if err == nil {
		return nil
	}

	if strings.Contains(err.Error(), "no server running") {
		return nil
	}
	return fmt.Errorf("tmuxlife.Prober.ServerReachable: %w", err)
}

func (p *Prober) SessionCount(ctx context.Context) (int, error) {
	sessions, err := p.store.ListSessions()
	if err != nil {
		return 0, fmt.Errorf("tmuxlife.Prober.SessionCount: %w", err)
	}
	n := 0
	for _, s := range sessions {
		if s.Status == StatusActive {
			n++
		}
	}
	return n, nil
}

func (p *Prober) DriftCount(ctx context.Context) (int, error) {
	sessions, err := p.store.ListSessions()
	if err != nil {
		return 0, fmt.Errorf("tmuxlife.Prober.DriftCount: %w", err)
	}
	n := 0
	for _, s := range sessions {
		if s.Status == StatusOrphaned {
			n++
		}
	}
	return n, nil
}

func (p *Prober) SocketPermissions(ctx context.Context) (string, error) {
	info, err := p.statFile(SocketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("socket does not exist (daemon not running?)")
		}
		return "", fmt.Errorf("tmuxlife.Prober.SocketPermissions: %w", err)
	}
	return fmt.Sprintf("%#o", info.Mode().Perm()), nil
}

func tmuxVersionAtLeast(got, min string) (bool, error) {
	gMaj, gMin, err := splitTmuxProberVersion(got)
	if err != nil {
		return false, fmt.Errorf("parse got %q: %w", got, err)
	}
	mMaj, mMin, err := splitTmuxProberVersion(min)
	if err != nil {
		return false, fmt.Errorf("parse min %q: %w", min, err)
	}
	if gMaj != mMaj {
		return gMaj > mMaj, nil
	}
	return gMin >= mMin, nil
}

func splitTmuxProberVersion(v string) (int, int, error) {

	end := len(v)
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= '0' && c <= '9') && c != '.' {
			end = i
			break
		}
	}
	v = v[:end]
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("not major.minor: %q", v)
	}
	maj, err := atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse major %q: %w", parts[0], err)
	}
	min, err := atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse minor %q: %w", parts[1], err)
	}
	return maj, min, nil
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
