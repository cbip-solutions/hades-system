package tmuxlife

import (
	"errors"
	"strings"
	"testing"
)

// TestSocketPathCanonical asserts the const matches invariant spec.
// SocketPath MUST be /tmp/zen-swarm.sock; deviation breaks tmux contamination
// prevention layer 1 (compile-time const).
func TestSocketPathCanonical(t *testing.T) {
	const want = "/tmp/zen-swarm.sock"
	if SocketPath != want {
		t.Errorf("SocketPath = %q, want %q (inv-zen-117)", SocketPath, want)
	}
}

func TestSocketPathNotDefaultTmux(t *testing.T) {
	if strings.HasPrefix(SocketPath, "/tmp/tmux-") {
		t.Errorf("SocketPath %q matches default tmux pattern; inv-zen-117 violated", SocketPath)
	}
}

func TestSnapshotDirSubpathStable(t *testing.T) {
	const want = ".config/zen-swarm/tmux-snapshots"
	if SnapshotDirSubpath != want {
		t.Errorf("SnapshotDirSubpath = %q, want %q", SnapshotDirSubpath, want)
	}
}

func TestIdleTTLInfinitySentinel(t *testing.T) {
	if IdleTTLInfinity != -1 {
		t.Errorf("IdleTTLInfinity = %d, want -1", IdleTTLInfinity)
	}
	if IdleTTLInfinity == 0 {
		t.Error("IdleTTLInfinity must be distinct from 0 (which would reap immediately)")
	}
}

func TestSentinelErrorsExist(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrSessionNotFound", ErrSessionNotFound},
		{"ErrSessionExists", ErrSessionExists},
		{"ErrTmuxNotInstalled", ErrTmuxNotInstalled},
		{"ErrTmuxVersionTooOld", ErrTmuxVersionTooOld},
		{"ErrSnapshotCorrupt", ErrSnapshotCorrupt},
		{"ErrScratchExclusionViolated", ErrScratchExclusionViolated},
	}
	for _, s := range sentinels {
		if s.err == nil {
			t.Errorf("%s is nil", s.name)
		}
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a.err, b.err) {
				t.Errorf("%s and %s compare equal; distinct sentinels required", a.name, b.name)
			}
		}
	}
}

func TestErrorMessagesActionable(t *testing.T) {
	cases := map[error][]string{
		ErrTmuxNotInstalled:         {"brew install tmux", "tmux"},
		ErrTmuxVersionTooOld:        {"3.4", "version"},
		ErrScratchExclusionViolated: {"scratch", "snapshot"},
	}
	for err, hints := range cases {
		msg := err.Error()
		for _, hint := range hints {
			if !strings.Contains(msg, hint) {
				t.Errorf("%v message %q missing hint %q", err, msg, hint)
			}
		}
	}
}

func TestErrorMessagesNonEmpty(t *testing.T) {
	all := []error{
		ErrSessionNotFound,
		ErrSessionExists,
		ErrTmuxNotInstalled,
		ErrTmuxVersionTooOld,
		ErrSnapshotCorrupt,
		ErrScratchExclusionViolated,
	}
	for _, e := range all {
		msg := e.Error()
		if msg == "" {
			t.Errorf("error %v has empty message", e)
		}
		if !strings.HasPrefix(msg, "tmuxlife:") {
			t.Errorf("error message %q lacks 'tmuxlife:' prefix; package convention", msg)
		}
	}
}
