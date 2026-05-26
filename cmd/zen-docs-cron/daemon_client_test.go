package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startUDSTestServer spins up an httptest server with a custom Unix-socket
// listener. The returned socketPath should be passed to newDaemonCronClient.
// Caller MUST defer server.Close() and os.Remove(socketPath).
//
// macOS sun_path is 104 bytes — t.TempDir() can produce a too-long path
// under deeply nested test names. We use os.MkdirTemp in /tmp (short root)
// and clean up via t.Cleanup so the test still meets isolation guarantees.
func startUDSTestServer(t *testing.T, handler http.HandlerFunc) (server *httptest.Server, socketPath string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "zen-cron-uds-")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	socketPath = filepath.Join(tmpDir, "c.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen unix %s: %v", socketPath, err)
	}
	server = &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second},
	}
	server.Start()
	return server, socketPath
}

type captured struct {
	method  string
	path    string
	body    []byte
	headers http.Header
}

func makeCapturingHandler(t *testing.T, status int, captureFn func(c captured)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captureFn(captured{
			method:  r.Method,
			path:    r.URL.Path,
			body:    body,
			headers: r.Header.Clone(),
		})
		w.WriteHeader(status)
	}
}

func TestDaemonCronClient_IngestDelta_RoutesAndBody(t *testing.T) {
	var got captured
	srv, sock := startUDSTestServer(t, makeCapturingHandler(t, http.StatusOK, func(c captured) {
		got = c
	}))
	defer srv.Close()
	defer os.Remove(sock)

	c := newDaemonCronClient(sock)
	if err := c.IngestDelta(context.Background(), "go"); err != nil {
		t.Fatalf("IngestDelta: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method: want POST, got %s", got.method)
	}
	if got.path != "/v1/ecosystem/ingest-delta" {
		t.Errorf("path: want /v1/ecosystem/ingest-delta, got %s", got.path)
	}
	var parsed map[string]string
	if err := json.Unmarshal(got.body, &parsed); err != nil {
		t.Fatalf("body unmarshal: %v (body=%q)", err, got.body)
	}
	if parsed["ecosystem"] != "go" {
		t.Errorf("body ecosystem: want go, got %q", parsed["ecosystem"])
	}
	if ct := got.headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
}

func TestDaemonCronClient_SweepEndpoints_RouteCorrectly(t *testing.T) {
	cases := []struct {
		name     string
		wantPath string
		call     func(c *daemonCronClient) error
	}{
		{
			name:     "fingerprints",
			wantPath: "/v1/ecosystem/sweep/fingerprints",
			call:     func(c *daemonCronClient) error { return c.SweepChunkFingerprints(context.Background(), "python") },
		},
		{
			name:     "change-nodes",
			wantPath: "/v1/ecosystem/sweep/change-nodes",
			call:     func(c *daemonCronClient) error { return c.SweepChangeNodes(context.Background(), "typescript") },
		},
		{
			name:     "rebuild-symbol-index",
			wantPath: "/v1/ecosystem/sweep/rebuild-symbol-index",
			call:     func(c *daemonCronClient) error { return c.RebuildSymbolIndex(context.Background(), "rust") },
		},
		{
			name:     "cas-gc",
			wantPath: "/v1/ecosystem/sweep/cas-gc",
			call:     func(c *daemonCronClient) error { return c.CASGarbageCollect(context.Background()) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got captured
			srv, sock := startUDSTestServer(t, makeCapturingHandler(t, http.StatusOK, func(c captured) {
				got = c
			}))
			defer srv.Close()
			defer os.Remove(sock)

			c := newDaemonCronClient(sock)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got.method != http.MethodPost {
				t.Errorf("%s method: want POST, got %s", tc.name, got.method)
			}
			if got.path != tc.wantPath {
				t.Errorf("%s path: want %s, got %s", tc.name, tc.wantPath, got.path)
			}
		})
	}
}

func TestDaemonCronClient_Non2xx_ReturnsError(t *testing.T) {
	srv, sock := startUDSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "daemon unavailable: degraded")
	})
	defer srv.Close()
	defer os.Remove(sock)

	c := newDaemonCronClient(sock)
	err := c.IngestDelta(context.Background(), "go")
	if err == nil {
		t.Fatal("expected error from non-2xx response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
	if !strings.Contains(err.Error(), "daemon unavailable") {
		t.Errorf("error should include body snippet: %v", err)
	}
}

func TestDaemonCronClient_Non2xx_4xx(t *testing.T) {
	srv, sock := startUDSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "invalid ecosystem")
	})
	defer srv.Close()
	defer os.Remove(sock)

	c := newDaemonCronClient(sock)
	err := c.SweepChunkFingerprints(context.Background(), "lua")
	if err == nil {
		t.Fatal("expected error from 4xx response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention 400: %v", err)
	}
}

func TestDaemonCronClient_NoSocket_DialError(t *testing.T) {
	c := newDaemonCronClient("/tmp/nonexistent-zen-cron-test.sock")
	err := c.IngestDelta(context.Background(), "go")
	if err == nil {
		t.Fatal("expected error when socket does not exist")
	}

	if !strings.Contains(err.Error(), "/v1/ecosystem/ingest-delta") {
		t.Errorf("error should mention the endpoint: %v", err)
	}
}

func TestDaemonCronClient_CtxCancellation(t *testing.T) {

	block := make(chan struct{})
	defer close(block)
	srv, sock := startUDSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
	})
	defer srv.Close()
	defer os.Remove(sock)

	c := newDaemonCronClient(sock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.IngestDelta(ctx, "go")
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}

	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context cancellation error; got %v", err)
	}
}

func TestBuildProductionDeps_SameClient_AllInterfaces(t *testing.T) {
	ingester, sweeper, detector := buildProductionDeps("/tmp/zen-build-deps-test.sock")
	if ingester == nil {
		t.Fatal("ingester is nil")
	}
	if sweeper == nil {
		t.Fatal("sweeper is nil")
	}
	if detector == nil {
		t.Fatal("detector is nil")
	}

	if _, ok := ingester.(*daemonCronClient); !ok {
		t.Errorf("ingester: want *daemonCronClient, got %T", ingester)
	}
	if _, ok := sweeper.(*daemonCronClient); !ok {
		t.Errorf("sweeper: want *daemonCronClient, got %T", sweeper)
	}
	if _, ok := detector.(*daemonCronClient); !ok {
		t.Errorf("detector: want *daemonCronClient, got %T", detector)
	}
}

func TestDaemonCronClient_CASGarbageCollect_NoBody(t *testing.T) {
	var got captured
	srv, sock := startUDSTestServer(t, makeCapturingHandler(t, http.StatusOK, func(c captured) {
		got = c
	}))
	defer srv.Close()
	defer os.Remove(sock)

	c := newDaemonCronClient(sock)
	if err := c.CASGarbageCollect(context.Background()); err != nil {
		t.Fatalf("CASGarbageCollect: %v", err)
	}
	if len(got.body) != 0 {
		t.Errorf("expected empty body; got %q", got.body)
	}

	if ct := got.headers.Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type should be empty for no-body request; got %q", ct)
	}
}
