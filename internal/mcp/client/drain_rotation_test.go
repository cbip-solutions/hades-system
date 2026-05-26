package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestDrainBuffer_RotationConcurrentEmitNoLoss(t *testing.T) {
	type recvd struct {
		mu    sync.Mutex
		early int
		live  int
		other int
	}
	var r recvd

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var ev client.AuditEvent
		if err := json.NewDecoder(req.Body).Decode(&ev); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		switch {
		case strings.HasPrefix(ev.Payload, "early"):
			r.early++
		case strings.HasPrefix(ev.Payload, "live"):
			r.live++
		default:
			r.other++
		}
		r.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("rotate-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "rotate-test",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)

	const earlyN = 500
	f, err := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open buffer for staging: %v", err)
	}
	for i := 0; i < earlyN; i++ {
		evt := client.AuditEvent{
			Type:    "early.event",
			Payload: fmt.Sprintf("early-%d", i),
		}
		line, _ := json.Marshal(evt)
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("stage line: %v", err)
		}
	}
	_ = f.Close()

	if data, err := os.ReadFile(ec.BufferPath()); err != nil {
		t.Fatalf("read live buffer: %v", err)
	} else {
		lineCount := strings.Count(strings.TrimSpace(string(data)), "\n") + 1
		if lineCount != earlyN {
			t.Fatalf("live buffer line count = %d, want %d", lineCount, earlyN)
		}
	}

	const liveN = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = ec.DrainBuffer(context.Background())
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < liveN; i++ {
			_ = ec.Emit(context.Background(), client.AuditEvent{
				Type:    "live.event",
				Payload: fmt.Sprintf("live-%d", i),
			})
		}
	}()
	wg.Wait()

	for i := 0; i < 5; i++ {
		_, _ = ec.DrainBuffer(context.Background())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.early != earlyN {
		t.Errorf("early events received = %d, want %d (loss = %d)",
			r.early, earlyN, earlyN-r.early)
	}

	if r.live != liveN {
		t.Errorf("live events received = %d, want %d (loss = %d)",
			r.live, liveN, liveN-r.live)
	}

	if r.other != 0 {
		t.Errorf("unexpected payloads received: %d", r.other)
	}
}

func TestDrainBuffer_FullDrainRemovesDrainingFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "full-drain"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	for i := 0; i < 3; i++ {
		evt := client.AuditEvent{Type: "t", Payload: fmt.Sprintf("e%d", i)}
		line, _ := json.Marshal(evt)
		f, _ := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}
	if n != 3 {
		t.Errorf("drained %d, want 3", n)
	}

	if _, statErr := os.Stat(ec.BufferPath()); !os.IsNotExist(statErr) {
		t.Errorf("live buffer should be gone; got: %v", statErr)
	}
	drainingPath := ec.BufferPath() + ".draining"
	if _, statErr := os.Stat(drainingPath); !os.IsNotExist(statErr) {
		t.Errorf(".draining file should be gone after full drain; got: %v", statErr)
	}
}

func TestDrainBuffer_PartialDrainPreservesRemainingInDraining(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "partial-drain"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	for i := 0; i < 3; i++ {
		evt := client.AuditEvent{Type: "t", Payload: fmt.Sprintf("e%d", i)}
		line, _ := json.Marshal(evt)
		f, _ := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}

	n, err := ec.DrainBuffer(context.Background())
	if err == nil {
		t.Fatal("expected partial-drain error, got nil")
	}
	if n < 1 {
		t.Errorf("at least one event should have been drained, got %d", n)
	}

	drainingPath := ec.BufferPath() + ".draining"
	data, statErr := os.ReadFile(drainingPath)
	if os.IsNotExist(statErr) {
		t.Fatal(".draining file must survive partial drain failure")
	}
	if statErr != nil {
		t.Fatalf("read draining: %v", statErr)
	}
	remainingLines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1
	if remainingLines != 3-n {
		t.Errorf("draining file has %d lines, want %d (3-drained=%d)",
			remainingLines, 3-n, 3-n)
	}
}

func TestDrainBuffer_RecoversOrphanedDrainingFile(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "orphan-drain"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	f, err := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open draining: %v", err)
	}
	for i := 0; i < 3; i++ {
		evt := client.AuditEvent{Type: "orphan", Payload: fmt.Sprintf("o%d", i)}
		line, _ := json.Marshal(evt)
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer should recover orphan: %v", err)
	}
	if n != 3 {
		t.Errorf("orphan recovery drained %d, want 3", n)
	}
	if got := received.Load(); got != 3 {
		t.Errorf("daemon received %d events, want 3", got)
	}

	if _, statErr := os.Stat(drainingPath); !os.IsNotExist(statErr) {
		t.Errorf("orphaned .draining must be removed after success; got %v", statErr)
	}
}

func TestDrainBuffer_RecoversBothDrainingAndLive(t *testing.T) {
	type recvd struct {
		mu sync.Mutex
		ev []string
	}
	var r recvd
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var ev client.AuditEvent
		_ = json.NewDecoder(req.Body).Decode(&ev)
		r.mu.Lock()
		r.ev = append(r.ev, ev.Payload)
		r.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "both-drain"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	df, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	for i := 0; i < 2; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "orphan", Payload: fmt.Sprintf("o%d", i)})
		_, _ = df.Write(append(line, '\n'))
	}
	_ = df.Close()

	lf, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "live", Payload: fmt.Sprintf("l%d", i)})
		_, _ = lf.Write(append(line, '\n'))
	}
	_ = lf.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}
	if n != 5 {
		t.Errorf("drained %d, want 5 (2 orphan + 3 live)", n)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ev) != 5 {
		t.Errorf("daemon received %d events, want 5", len(r.ev))
	}

	if _, e := os.Stat(drainingPath); !os.IsNotExist(e) {
		t.Errorf(".draining survived: %v", e)
	}
	if _, e := os.Stat(ec.BufferPath()); !os.IsNotExist(e) {
		t.Errorf("live buffer survived: %v", e)
	}
}

func TestEmit_ConcurrentDuringDrainAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in -short")
	}
	type recvd struct {
		mu sync.Mutex
		n  int
	}
	var r recvd

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var ev client.AuditEvent
		if err := json.NewDecoder(req.Body).Decode(&ev); err == nil && ev.Payload != "" {
			r.mu.Lock()
			r.n++
			r.mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "scale-drain"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	const staged = 1000
	f, err := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("stage open: %v", err)
	}
	for i := 0; i < staged; i++ {
		evt := client.AuditEvent{Type: "scale.event", Payload: fmt.Sprintf("scale-%d", i)}
		line, _ := json.Marshal(evt)
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	const live = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = ec.DrainBuffer(context.Background())
	}()
	go func() {
		defer wg.Done()
		for i := staged; i < staged+live; i++ {
			_ = ec.Emit(context.Background(), client.AuditEvent{
				Type:    "scale.event",
				Payload: fmt.Sprintf("scale-%d", i),
			})
		}
	}()
	wg.Wait()

	for i := 0; i < 5; i++ {
		if _, drainErr := ec.DrainBuffer(context.Background()); drainErr == nil {
			break
		}
	}

	r.mu.Lock()
	got := r.n
	r.mu.Unlock()

	if got != staged+live {
		t.Errorf("scale: received %d events, want %d (loss = %d)",
			got, staged+live, staged+live-got)
	}
}

func TestDrainBuffer_NoBufferOrDrainingReturnsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "nothing-to-drain")
	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer with nothing to drain: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

var _ = errors.New
var _ = time.Second
