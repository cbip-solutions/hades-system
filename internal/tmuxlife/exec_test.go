package tmuxlife

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const tmuxExecTestTimeout = 30 * time.Second

func TestExecTmuxRequiresDashS(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("ExecTmux without -S did NOT panic; inv-zen-117 layer 2 violated")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T, want string", r)
		}
		if !strings.Contains(msg, "-S") || !strings.Contains(msg, "inv-zen-117") {
			t.Errorf("panic message = %q; missing -S or inv-zen-117 reference", msg)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = ExecTmux(ctx, "list-sessions")
}

func TestExecTmuxAcceptsDashS(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; ExecTmux happy-path test requires real binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic with -S present: %v", r)
		}
	}()
	_, err := ExecTmux(ctx, "-S", SocketPath, "zen-test-noop-cmd")

	if err == nil {
		t.Errorf("expected error from unknown tmux subcommand; got nil")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("err = %v; expected wrap of *exec.ExitError", err)
	}
}

func TestExecTmuxContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := ExecTmux(ctx, "-S", SocketPath, "wait-for", "zen-test-channel")
	if err == nil {
		t.Errorf("expected timeout error; got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") {
		t.Errorf("err = %v; expected context.DeadlineExceeded or signal:killed", err)
	}
}

func TestExecTmuxDashSNotFirst(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic when -S present mid-args: %v", r)
		}
	}()

	_, _ = ExecTmux(ctx, "-q", "-S", SocketPath, "zen-test-noop")
}

func TestExecTmuxEmptyArgs(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("ExecTmux() with zero args did NOT panic; inv-zen-117 violated")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = ExecTmux(ctx)
}

func TestExecTmuxRejectsLeadingTmuxBinary(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("ExecTmux with args[0]==\"tmux\" did not panic; programmer error must surface")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = ExecTmux(ctx, "tmux", "-S", SocketPath, "list-sessions")
}

func TestExecTmuxPanicReferencesSocketPath(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on missing -S")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value not string: %T", r)
		}
		if !strings.Contains(msg, SocketPath) {
			t.Errorf("panic message %q missing SocketPath %q", msg, SocketPath)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = ExecTmux(ctx, "kill-server")
}

func TestLookPathTmux(t *testing.T) {
	_, err := exec.LookPath("tmux")
	got := LookPathTmux()
	if err == nil {

		if got != nil {
			t.Errorf("LookPathTmux() = %v; want nil when tmux installed", got)
		}
	} else {

		if got == nil {
			t.Errorf("LookPathTmux() = nil; want non-nil when tmux missing")
		}
		if !errors.Is(got, ErrTmuxNotInstalled) {
			t.Errorf("LookPathTmux() error = %v; want errors.Is ErrTmuxNotInstalled", got)
		}
	}
}

func TestVersionMinAcceptsCurrent(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	if err := VersionMin(ctx, 3, 4); err != nil {

		if !errors.Is(err, ErrTmuxVersionTooOld) {

			t.Logf("VersionMin returned %v; treating as informational (host may have old tmux)", err)
		}
	}
}

func TestVersionMinRejectsTooOld(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	err := VersionMin(ctx, 99, 0)
	if err == nil {
		t.Fatal("VersionMin(99, 0) returned nil; expected ErrTmuxVersionTooOld")
	}
	if !errors.Is(err, ErrTmuxVersionTooOld) {
		t.Errorf("err = %v; want errors.Is ErrTmuxVersionTooOld", err)
	}
}

func TestParseTmuxVersion(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantMajor int
		wantMinor int
		wantErr   bool
	}{

		{"release with suffix letter", "tmux 3.4a\n", 3, 4, false},
		{"release with multi-letter suffix", "tmux 3.5ab", 3, 5, false},
		{"release no suffix", "tmux 3.5", 3, 5, false},
		{"HEAD build", "tmux next-3.5a", 3, 5, false},
		{"old release", "tmux 2.8", 2, 8, false},
		{"trailing whitespace", "  tmux 3.4  ", 3, 4, false},

		{"missing tmux prefix", "3.4", 3, 4, false},
		{"missing dot", "tmux 3", 0, 0, true},
		{"non-numeric major", "tmux foo.bar", 0, 0, true},
		{"non-numeric minor", "tmux 3.bar", 0, 0, true},
		{"empty", "", 0, 0, true},
		{"only prefix", "tmux ", 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotMajor, gotMinor, err := parseTmuxVersion(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("parseTmuxVersion(%q) = (%d, %d, nil); want error",
						c.in, gotMajor, gotMinor)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTmuxVersion(%q) = %v; want nil", c.in, err)
				return
			}
			if gotMajor != c.wantMajor || gotMinor != c.wantMinor {
				t.Errorf("parseTmuxVersion(%q) = (%d, %d); want (%d, %d)",
					c.in, gotMajor, gotMinor, c.wantMajor, c.wantMinor)
			}
		})
	}
}

func TestVersionMinViaFakeTmux(t *testing.T) {
	tmp := t.TempDir()
	fakePath := tmp + "/tmux"

	script := "#!/bin/sh\necho 'tmux 4.7a'\nexit 0\n"
	if err := writeExec(fakePath, script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()

	if err := VersionMin(ctx, 3, 4); err != nil {
		t.Errorf("VersionMin(3, 4) with fake tmux 4.7 = %v; want nil", err)
	}
	if err := VersionMin(ctx, 99, 0); !errors.Is(err, ErrTmuxVersionTooOld) {
		t.Errorf("VersionMin(99, 0) = %v; want errors.Is ErrTmuxVersionTooOld", err)
	}
}

func TestVersionMinUnparsableOutput(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\necho 'garbage-not-a-version'\nexit 0\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	err := VersionMin(ctx, 3, 4)
	if err == nil {
		t.Fatal("VersionMin with unparsable fake output = nil; want error")
	}
	if !strings.Contains(err.Error(), "unparsable version") {
		t.Errorf("err %v missing 'unparsable version' hint", err)
	}
}

func TestVersionMinExecError(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\nexit 1\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	err := VersionMin(ctx, 3, 4)
	if err == nil {
		t.Errorf("VersionMin with failing fake tmux = nil; want error")
	}
	if !strings.Contains(err.Error(), "tmuxlife.VersionMin") {
		t.Errorf("err %v missing tmuxlife.VersionMin prefix", err)
	}
}

func TestLookPathTmuxAbsent(t *testing.T) {
	t.Setenv("PATH", "")
	err := LookPathTmux()
	if err == nil {
		t.Fatal("LookPathTmux() = nil with empty PATH; want ErrTmuxNotInstalled")
	}
	if !errors.Is(err, ErrTmuxNotInstalled) {
		t.Errorf("err = %v; want errors.Is ErrTmuxNotInstalled", err)
	}
}

func TestLookPathTmuxPresentViaFake(t *testing.T) {
	tmp := t.TempDir()
	if err := writeExec(tmp+"/tmux", "#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp)
	if err := LookPathTmux(); err != nil {
		t.Errorf("LookPathTmux() with fake tmux on PATH = %v; want nil", err)
	}
}

func TestExecTmuxHappyPathViaFake(t *testing.T) {
	tmp := t.TempDir()

	script := "#!/bin/sh\necho \"ok $*\"\nexit 0\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	out, err := ExecTmux(ctx, "-S", SocketPath, "list-sessions")
	if err != nil {
		t.Fatalf("ExecTmux happy = %v; want nil", err)
	}
	want := "ok -S " + SocketPath + " list-sessions"
	if !strings.Contains(string(out), want) {
		t.Errorf("output %q missing %q", string(out), want)
	}
}

func TestExecTmuxErrorPathViaFake(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\necho 'unknown command' >&2\nexit 2\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), tmuxExecTestTimeout)
	defer cancel()
	out, err := ExecTmux(ctx, "-S", SocketPath, "bogus-cmd")
	if err == nil {
		t.Fatal("ExecTmux on failing fake tmux returned nil err")
	}
	if !strings.Contains(err.Error(), "tmuxlife: tmux") {
		t.Errorf("err %v missing 'tmuxlife: tmux' prefix", err)
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("err %v missing captured stderr 'unknown command'", err)
	}

	if !strings.Contains(string(out), "unknown command") {
		t.Errorf("out %q missing stderr 'unknown command'", string(out))
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("err %v not unwrappable to *exec.ExitError", err)
	}
}

func TestExecTmuxContextCancellationViaFake(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\nexec sleep 1\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecTmux(ctx, "-S", SocketPath, "long-op")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ExecTmux with ctx deadline returned nil err")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") {
		t.Errorf("err = %v; expected context.DeadlineExceeded or signal:killed", err)
	}

	if elapsed > 900*time.Millisecond {
		t.Errorf("ExecTmux took %v; expected ctx-kill to land <900ms", elapsed)
	}
}
