package preflight

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonSocketAbsentSkips(t *testing.T) {
	c := &DaemonCheck{
		socketPath: "/tmp/nonexistent-zen-swarm-test-abc123.sock",
		stat:       os.Stat,
	}
	r := c.Run(context.Background())
	if r.Status != StatusSkip {
		t.Errorf("Daemon socket absent: Status = %v, want StatusSkip", r.Status)
	}
}

func TestDaemonSocketPresentAndDialsPass(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "fake.sock")

	if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	c := &DaemonCheck{
		socketPath: sockPath,
		stat:       os.Stat,
		dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			lc, rc := net.Pipe()
			_ = rc.Close()
			return lc, nil
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Daemon dial pass: Status = %v, want StatusPass", r.Status)
	}
}

func TestDaemonSocketPresentButDialFailsWarns(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "fake.sock")
	if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	c := &DaemonCheck{
		socketPath: sockPath,
		stat:       os.Stat,
		dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusWarn {
		t.Errorf("Daemon dial fail: Status = %v, want StatusWarn", r.Status)
	}
}

func TestDaemonNilStatFallsBack(t *testing.T) {
	c := &DaemonCheck{
		socketPath: "/tmp/nonexistent-zen-swarm-test-def456.sock",
		stat:       nil,
	}
	r := c.Run(context.Background())
	if r.Status != StatusSkip {
		t.Errorf("nil stat fallback: Status = %v, want StatusSkip", r.Status)
	}
}

func TestDaemonNilDialFallsBack(t *testing.T) {

	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "not-a-socket.sock")
	if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	c := &DaemonCheck{
		socketPath: sockPath,
		stat:       os.Stat,
		dial:       nil,
	}
	r := c.Run(context.Background())
	if r.Status != StatusWarn {
		t.Errorf("nil dial fallback: Status = %v, want StatusWarn (real dial fails on plain file)", r.Status)
	}
}

func TestDaemonForTestConstructor(t *testing.T) {
	c := NewDaemonCheckForTest(
		"/tmp/zen-test.sock",
		func(_ string) (os.FileInfo, error) {
			return nil, errors.New("not exist")
		},
		nil,
	)
	if c == nil {
		t.Fatal("NewDaemonCheckForTest returned nil")
	}
	r := c.Run(context.Background())
	if r.Status != StatusSkip {
		t.Errorf("ForTest stat-fail: Status = %v, want StatusSkip", r.Status)
	}
}

func TestDaemonProductionConstructor(t *testing.T) {
	c := NewDaemonCheck()
	if c == nil {
		t.Fatal("NewDaemonCheck returned nil")
	}
	if c.Name() != "daemon" {
		t.Errorf("Name = %q, want daemon", c.Name())
	}
	if c.socketPath != DefaultDaemonSocketPath {
		t.Errorf("socketPath = %q, want %q", c.socketPath, DefaultDaemonSocketPath)
	}
	_ = c.Run(context.Background())
}
