// go:build darwin || linux

package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func shortSockPath(t *testing.T, name string) string {
	t.Helper()
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	suffix := hex.EncodeToString(buf[:])
	p := filepath.Join("/tmp", "zauth-"+suffix+"-"+name)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestExtractPeerCred_RealUDS(t *testing.T) {
	sockPath := shortSockPath(t, "test.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen unix: %v", err)
	}
	defer ln.Close()

	type result struct {
		pc  PeerCred
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer conn.Close()
		pc, err := ExtractPeerCred(conn)
		resCh <- result{pc: pc, err: err}
	}()

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	c, err := dialer.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial unix: %v", err)
	}
	defer c.Close()

	select {
	case r := <-resCh:
		if r.err != nil {
			t.Fatalf("ExtractPeerCred: %v", r.err)
		}
		if !r.pc.HasSet {
			t.Fatalf("HasSet=false")
		}
		wantUID := uint32(os.Geteuid())
		if r.pc.UID != wantUID {
			t.Errorf("UID = %d, want %d (geteuid)", r.pc.UID, wantUID)
		}

		_ = r.pc.GID

		if os.Geteuid() != 0 && r.pc.UID == 0 {
			t.Errorf("UID = 0 but euid != 0; SO_PEERCRED bug? GOOS=%s", runtime.GOOS)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
}

func TestExtractPeerCred_NotUnixConn_RealOS(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	pc, err := ExtractPeerCred(c1)
	if err == nil {
		t.Fatal("err = nil, want non-nil for non-UnixConn")
	}
	if pc.HasSet {
		t.Errorf("HasSet=true, want false")
	}
}

func TestExtractPeerCred_ClosedConn(t *testing.T) {
	sockPath := shortSockPath(t, "closed.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	type acceptResult struct {
		conn *net.UnixConn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			acceptCh <- acceptResult{err: err}
			return
		}
		uc, _ := c.(*net.UnixConn)
		acceptCh <- acceptResult{conn: uc}
	}()

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	c, err := dialer.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	r := <-acceptCh
	if r.err != nil {
		t.Fatalf("accept: %v", r.err)
	}
	if r.conn == nil {
		t.Fatalf("nil UnixConn from Accept")
	}

	r.conn.Close()
	pc, err := ExtractPeerCred(r.conn)
	if err == nil {
		t.Fatal("err = nil on closed conn, want non-nil")
	}
	if pc.HasSet {
		t.Errorf("HasSet=true on failed extraction")
	}
}
