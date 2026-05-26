package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeTmuxProber struct {
	versionInstalled string
	versionErr       error
	versionMeetsMin  bool
	serverReachable  bool
	serverErr        error
	sessionCount     int
	driftCount       int
	socketMode       string
	socketErr        error
	statsErr         error
}

func (f *fakeTmuxProber) BinaryVersion(ctx context.Context) (version string, meetsMin bool, err error) {
	return f.versionInstalled, f.versionMeetsMin, f.versionErr
}

func (f *fakeTmuxProber) ServerReachable(ctx context.Context) error {
	if f.serverErr != nil {
		return f.serverErr
	}
	if !f.serverReachable {
		return errors.New("tmux server not reachable")
	}
	return nil
}

func (f *fakeTmuxProber) SessionCount(ctx context.Context) (int, error) {
	return f.sessionCount, f.statsErr
}

func (f *fakeTmuxProber) DriftCount(ctx context.Context) (int, error) {
	return f.driftCount, f.statsErr
}

func (f *fakeTmuxProber) SocketPermissions(ctx context.Context) (mode string, err error) {
	return f.socketMode, f.socketErr
}

func TestRunTmuxProbeAllOK(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		sessionCount:     2,
		driftCount:       0,
		socketMode:       "0600",
	}
	probes, err := RunTmuxProbe(context.Background(), p)
	if err != nil {
		t.Fatalf("RunTmuxProbe: %v", err)
	}
	if len(probes) != 5 {
		t.Fatalf("want 5 probes, got %d", len(probes))
	}
	for _, r := range probes {
		if r.Status != ProbeOK {
			t.Errorf("probe %s: status=%v message=%q", r.Name, r.Status, r.Message)
		}
	}
}

func TestRunTmuxProbeBinaryAbsentFail(t *testing.T) {
	p := &fakeTmuxProber{
		versionErr:      errors.New(`exec: "tmux": executable file not found in $PATH`),
		serverReachable: false,
		socketMode:      "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.binary.installed" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Hint, "brew install tmux") {
				t.Errorf("hint should suggest install: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("binary probe missing")
}

func TestRunTmuxProbeBinaryTooOldFail(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.0",
		versionMeetsMin:  false,
		serverReachable:  true,
		socketMode:       "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.binary.installed" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (3.0 < 3.4)", r.Status)
			}
			if !strings.Contains(r.Message, "3.4") {
				t.Errorf("message should mention min version 3.4: %q", r.Message)
			}
			return
		}
	}
	t.Fatal("binary probe missing")
}

func TestRunTmuxProbeServerUnreachableFail(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  false,
		socketMode:       "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.server.reachable" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Hint, "zen-swarm.sock") {
				t.Errorf("hint should mention socket path: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("server probe missing")
}

func TestRunTmuxProbeSessionCountError(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		statsErr:         errors.New("daemon.db locked"),
		socketMode:       "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.session.count" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			return
		}
	}
	t.Fatal("session.count probe missing")
}

func TestRunTmuxProbeDriftWarn(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		driftCount:       1,
		socketMode:       "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.drift.count" {
			if r.Status != ProbeWarn {
				t.Errorf("status=%v, want Warn (1 drift)", r.Status)
			}
			return
		}
	}
	t.Fatal("drift probe missing")
}

func TestRunTmuxProbeDriftChronicFail(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		driftCount:       4,
		socketMode:       "0600",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.drift.count" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (>=3 drift)", r.Status)
			}
			return
		}
	}
	t.Fatal("drift probe missing")
}

func TestRunTmuxProbeSocketWorldReadableFail(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		socketMode:       "0644",
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.socket.permissions" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (0644 != 0600)", r.Status)
			}
			if !strings.Contains(r.Hint, "chmod 0600") {
				t.Errorf("hint should suggest chmod: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("socket probe missing")
}

func TestRunTmuxProbeSocketStatErrorFails(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		socketMode:       "",
		socketErr:        errors.New("socket missing"),
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "tmux.socket.permissions" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (stat error)", r.Status)
			}
			return
		}
	}
	t.Fatal("socket probe missing")
}

func TestRunTmuxProbeProberError(t *testing.T) {
	p := &fakeTmuxProber{
		versionInstalled: "3.5a",
		versionMeetsMin:  true,
		serverReachable:  true,
		socketMode:       "0600",
		statsErr:         errors.New("daemon.db locked"),
	}
	probes, _ := RunTmuxProbe(context.Background(), p)
	if len(probes) != 5 {
		t.Errorf("expected 5 probes, got %d", len(probes))
	}
	hasFail := false
	for _, r := range probes {
		if r.Status == ProbeFail {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected at least one Fail probe")
	}
}
