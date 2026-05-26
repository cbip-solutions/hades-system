// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

func shortSockPathForServerTest(t *testing.T, name string) string {
	t.Helper()
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	suffix := hex.EncodeToString(buf[:])
	p := filepath.Join("/tmp", "zsrv-"+suffix+"-"+name)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestServerStart_WiresConnContextForUDSPeerCred(t *testing.T) {
	st := newTestStore(t)

	udsPath := shortSockPathForServerTest(t, "ctx.sock")
	srv := New(st, Config{UDSPath: udsPath, DisableAuditInfra: true})

	probeCh := make(chan auth.PeerCred, 1)
	srv.mux.HandleFunc("GET /probe-peer-cred", func(w http.ResponseWriter, r *http.Request) {
		pc := auth.PeerCredFromContext(r.Context())
		select {
		case probeCh <- pc:
		default:
		}
		w.WriteHeader(http.StatusOK)
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	waitForUDS(t, udsPath, 2*time.Second)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://unix/probe-peer-cred")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case pc := <-probeCh:
		if !pc.HasSet {
			t.Fatal("PeerCred.HasSet=false — ConnContext not wired (or extraction failed)")
		}
		wantUID := uint32(os.Geteuid())
		if pc.UID != wantUID {
			t.Errorf("PeerCred.UID = %d; want %d (geteuid of this test process)", pc.UID, wantUID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe handler never observed a peer-cred in request context")
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Errorf("Start returned %v", err)
	}
}

func TestServerStart_ConnContextIsBenignForTCP(t *testing.T) {
	st := newTestStore(t)

	udsPath := shortSockPathForServerTest(t, "tcp.sock")
	srv := New(st, Config{UDSPath: udsPath, HTTPAddr: "127.0.0.1:0", DisableAuditInfra: true})

	probeCh := make(chan auth.PeerCred, 1)
	srv.mux.HandleFunc("GET /probe-peer-cred-tcp", func(w http.ResponseWriter, r *http.Request) {
		pc := auth.PeerCredFromContext(r.Context())
		select {
		case probeCh <- pc:
		default:
		}
		w.WriteHeader(http.StatusOK)
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	waitForUDS(t, udsPath, 2*time.Second)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://unix/probe-peer-cred-tcp")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	select {
	case pc := <-probeCh:
		// On the UDS path peer-cred MUST be present (HasSet=true)
		// regardless of the TCP listener being up.
		if !pc.HasSet {
			t.Errorf("UDS probe via TCP-also daemon: PeerCred.HasSet=false; ConnContext must still wire UDS")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe handler never reached")
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Errorf("Start returned %v", err)
	}
}

func TestConnContextWithPeerCred_PassesThroughNonUDS(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	type sentinelKey struct{}
	parent := context.WithValue(context.Background(), sentinelKey{}, "marker")
	out := connContextWithPeerCred(parent, c1)
	if v, _ := out.Value(sentinelKey{}).(string); v != "marker" {
		t.Errorf("non-UDS conn: ctx must be returned unchanged; sentinel lost (got %q)", v)
	}
	if pc := auth.PeerCredFromContext(out); pc.HasSet {
		t.Errorf("non-UDS conn: PeerCredFromContext.HasSet=true; want false")
	}
}

func TestConnContextWithPeerCred_FailClosedOnExtractError(t *testing.T) {
	sockPath := shortSockPathForServerTest(t, "fc.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	type acceptRes struct {
		c   *net.UnixConn
		err error
	}
	ch := make(chan acceptRes, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			ch <- acceptRes{err: err}
			return
		}
		uc, _ := conn.(*net.UnixConn)
		ch <- acceptRes{c: uc}
	}()

	dialer := &net.Dialer{Timeout: 1 * time.Second}
	c, err := dialer.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	r := <-ch
	if r.err != nil {
		t.Fatalf("accept: %v", r.err)
	}
	if r.c == nil {
		t.Fatal("nil UnixConn")
	}

	r.c.Close()

	parent := context.Background()
	out := connContextWithPeerCred(parent, r.c)
	if pc := auth.PeerCredFromContext(out); pc.HasSet {
		t.Errorf("UDS w/ extract failure: PeerCredFromContext.HasSet=true; want false (fail-closed)")
	}
}

func waitForUDS(t *testing.T, path string, max time.Duration) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", path)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("UDS at %q never became ready within %s", path, max)
}
