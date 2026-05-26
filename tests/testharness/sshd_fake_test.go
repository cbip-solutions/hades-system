package testharness_test

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testharness"
	"golang.org/x/crypto/ssh"
)

func TestSSHDFakeBasic(t *testing.T) {
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:   "hello world\n",
			ExitCode: 0,
		}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()

	cfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password("ignored")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, err := ssh.Dial("tcp", srv.Addr(), cfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	out, err := sess.CombinedOutput("alembic upgrade head")
	if err != nil {
		t.Fatalf("CombinedOutput: %v (out=%q)", err, out)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Errorf("CombinedOutput = %q, want contains %q", out, "hello world")
	}
}

func TestSSHDFakeInteractivePrompt(t *testing.T) {
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:      "[sudo] password for testuser:",
			Interactive: true,
			Delay:       50 * time.Millisecond,
			ExitCode:    0,
		}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()

	cfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password("ignored")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, err := ssh.Dial("tcp", srv.Addr(), cfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := sess.Start("sudo apt update"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Read the first 64 bytes; the harness MUST emit the prompt.
	buf := make([]byte, 64)
	n, _ := io.ReadFull(stdout, buf)
	if !bytes.Contains(buf[:n], []byte("[sudo]")) {
		t.Errorf("first %d bytes = %q, want contains %q", n, buf[:n], "[sudo]")
	}
	_ = sess.Wait() // do not assert exit
}

func TestSSHDFakeScriptedBytes(t *testing.T) {

	scripted := []byte{0xfd, 0x18, 'l', 's', '\n'}
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			RawStdout: scripted,
			ExitCode:  0,
		}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()

	cfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password("ignored")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, err := ssh.Dial("tcp", srv.Addr(), cfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer conn.Close()
	sess, _ := conn.NewSession()
	defer sess.Close()
	out, _ := sess.Output("noop")
	if len(out) < 5 || !bytes.Equal(out[:5], scripted) {
		t.Errorf("got %x, want %x", out, scripted)
	}
}

func TestSSHDFakeRejectsTooManyConns(t *testing.T) {
	srv, err := testharness.NewFakeSSHDWithLimit(2, testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0, Delay: 200 * time.Millisecond}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHDWithLimit: %v", err)
	}
	defer srv.Close()
	conns := []net.Conn{}
	for i := 0; i < 4; i++ {
		c, err := net.Dial("tcp", srv.Addr())
		if err == nil {
			conns = append(conns, c)
		}
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if got := srv.ActiveConns(); got > 2 {
		t.Errorf("ActiveConns = %d, want <=2", got)
	}
}

func TestSSHDFakeNilHandler(t *testing.T) {
	if _, err := testharness.NewFakeSSHD(nil); err == nil {
		t.Fatal("NewFakeSSHD(nil) returned nil err; want non-nil")
	}
}

func TestSSHDFakeStderrAndDelay(t *testing.T) {
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:   "out\n",
			Stderr:   "err\n",
			Delay:    20 * time.Millisecond,
			ExitCode: 1,
		}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()
	cfg := &ssh.ClientConfig{
		User:            "t",
		Auth:            []ssh.AuthMethod{ssh.Password("x")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, _ := ssh.Dial("tcp", srv.Addr(), cfg)
	defer conn.Close()
	sess, _ := conn.NewSession()
	defer sess.Close()
	var sout, serr bytes.Buffer
	sess.Stdout = &sout
	sess.Stderr = &serr
	if err := sess.Run("alembic"); err == nil {
		t.Errorf("Run returned nil err; expected non-nil for ExitCode=1")
	}
	if !strings.Contains(sout.String(), "out") {
		t.Errorf("stdout = %q", sout.String())
	}
	if !strings.Contains(serr.String(), "err") {
		t.Errorf("stderr = %q", serr.String())
	}
}

func TestSSHDFakeRawStderr(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03}
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			RawStderr: raw,
			ExitCode:  0,
		}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()
	cfg := &ssh.ClientConfig{
		User:            "t",
		Auth:            []ssh.AuthMethod{ssh.Password("x")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, _ := ssh.Dial("tcp", srv.Addr(), cfg)
	defer conn.Close()
	sess, _ := conn.NewSession()
	defer sess.Close()
	var serr bytes.Buffer
	sess.Stderr = &serr
	_ = sess.Run("noop")
	if !bytes.Equal(serr.Bytes(), raw) {
		t.Errorf("stderr = %x, want %x", serr.Bytes(), raw)
	}
}
