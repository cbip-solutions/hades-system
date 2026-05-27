// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// Package main — production_boot_smoke_test.go (
// fix-cycle re-review codification).
//
// End-to-end smoke test that boots a real daemon on a temp UDS + an
// ephemeral TCP loopback, exercises /v1/audit/emit, then GETs the
// resulting event back via /v1/audit/event/<id> over BOTH UDS and TCP,
// and verifies /v1/doctrine/active over UDS returns the wired
// registry's active doctrine.
//
// This is the codified version of the re-reviewer's manual shell+curl
// probe — both endpoints were broken in production before the
// bootDoctrineRegistry + connContextWithPeerCred wire-ups landed. With
// the fixes shipped the smoke MUST pass; a regression in either
// wiring re-breaks the smoke immediately.
//
// # Why a Go test, not a shell script
//
// scripts/ has the release smoke shell; adding to it would require a
// binary on disk + a launchd-managed daemon path. Reproducing the
// daemon's full boot sequence in Go gets us the same probes against
// an in-process build with no external state, and the test runs in
// `make test`.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func shortSockPathForSmokeTest(t *testing.T, name string) string {
	t.Helper()
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	suffix := hex.EncodeToString(buf[:])
	p := filepath.Join("/tmp", "zsmk-"+suffix+"-"+name)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func waitForUDSSmoke(t *testing.T, path string, max time.Duration) {
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

func waitForTCPSmoke(t *testing.T, addr string, max time.Duration) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("TCP at %q never became ready within %s", addr, max)
}

func TestProductionBootSmoke_AuditEmitAndQueryEndToEnd(t *testing.T) {

	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "smoke.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	udsPath := shortSockPathForSmokeTest(t, "smoke.sock")

	probeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe Listen tcp: %v", err)
	}
	tcpAddr := probeLn.Addr().String()
	_ = probeLn.Close()

	srv := daemon.New(st, daemon.Config{
		UDSPath:           udsPath,
		HTTPAddr:          tcpAddr,
		DisableAuditInfra: true,
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	waitForUDSSmoke(t, udsPath, 30*time.Second)
	waitForTCPSmoke(t, tcpAddr, 30*time.Second)

	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Stop(shutCtx)
		<-errCh
	})

	udsClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 5 * time.Second,
	}
	tcpClient := &http.Client{Timeout: 5 * time.Second}

	emitBody := map[string]any{
		"project_id": "smoke-project",
		"type":       "smoke.boot",
		"payload": map[string]any{
			"doctrine": "max-scope",
			"reason":   "production-boot smoke",
		},
	}
	emitBytes, _ := json.Marshal(emitBody)
	resp, err := udsClient.Post("http://unix/v1/audit/emit", "application/json", bytes.NewReader(emitBytes))
	if err != nil {
		t.Fatalf("audit/emit POST over UDS: %v", err)
	}
	bodyB, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("audit/emit: status=%d body=%s; want 202", resp.StatusCode, bodyB)
	}
	var emitResp struct {
		ID       string `json:"id"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal(bodyB, &emitResp); err != nil {
		t.Fatalf("decode emit resp: %v body=%s", err, bodyB)
	}
	if emitResp.ID == "" {
		t.Fatal("audit/emit returned empty id")
	}
	if !emitResp.Accepted {
		t.Fatal("audit/emit accepted=false")
	}

	// 5. GET /v1/audit/event/<id> over UDS — peer-cred MUST flow
	// through ConnContext, sessionDoctrine MUST resolve max-scope
	// via the registry, and the row MUST be visible.
	udsURL := fmt.Sprintf("http://unix/v1/audit/event/%s", emitResp.ID)
	resp, err = udsClient.Get(udsURL)
	if err != nil {
		t.Fatalf("audit/event GET over UDS: %v", err)
	}
	bodyB, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit/event over UDS: status=%d body=%s; want 200 (regression of Issue 1+2 fixes)",
			resp.StatusCode, bodyB)
	}

	if !bytes.Contains(bodyB, []byte(emitResp.ID)) {
		t.Errorf("audit/event UDS body missing id %q; body=%s", emitResp.ID, bodyB)
	}

	tcpURL := fmt.Sprintf("http://%s/v1/audit/event/%s", tcpAddr, emitResp.ID)
	resp, err = tcpClient.Get(tcpURL)
	if err != nil {
		t.Fatalf("audit/event GET over TCP: %v", err)
	}
	bodyB, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit/event over TCP loopback: status=%d body=%s; want 200",
			resp.StatusCode, bodyB)
	}
	if !bytes.Contains(bodyB, []byte(emitResp.ID)) {
		t.Errorf("audit/event TCP body missing id %q; body=%s", emitResp.ID, bodyB)
	}

	// 7. GET /v1/doctrine/active over UDS — the registry MUST resolve
	// a name (pre-fix: 404 "doctrine: name not found in registry").
	resp, err = udsClient.Get("http://unix/v1/doctrine/active")
	if err != nil {
		t.Fatalf("doctrine/active GET over UDS: %v", err)
	}
	bodyB, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("doctrine/active over UDS: status=%d body=%s; want 200 (regression of Issue 1 fix)",
			resp.StatusCode, bodyB)
	}
	var active struct {
		Name            string `json:"name"`
		SchemaVersion   string `json:"schema_version"`
		DoctrineVersion string `json:"doctrine_version"`
		Source          string `json:"source"`
	}
	if err := json.Unmarshal(bodyB, &active); err != nil {
		t.Fatalf("decode doctrine/active resp: %v body=%s", err, bodyB)
	}
	if active.Name != "max-scope" {
		t.Errorf("doctrine/active name=%q; want max-scope (registry fallback when no userDefault)", active.Name)
	}
	if active.SchemaVersion == "" {
		t.Errorf("doctrine/active schema_version empty; want non-empty (built-in TOML carries it)")
	}
	if active.DoctrineVersion == "" {
		t.Errorf("doctrine/active doctrine_version empty; want non-empty")
	}
	if active.Source != "embed" {
		t.Errorf("doctrine/active source=%q; want embed (registry built-in, no user override)", active.Source)
	}
}

func TestProductionBootSmoke_AugmentRouteUnwiredReturns503(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "smoke.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	udsPath := shortSockPathForSmokeTest(t, "augsmoke.sock")
	probeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe Listen tcp: %v", err)
	}
	tcpAddr := probeLn.Addr().String()
	_ = probeLn.Close()

	srv := daemon.New(st, daemon.Config{
		UDSPath:           udsPath,
		HTTPAddr:          tcpAddr,
		DisableAuditInfra: true,
	})
	// NOTE(plan-15): we deliberately do NOT call srv.SetAugmentHandler — this is
	// the unwired-substrate state buildAugmentation produces when the

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	waitForUDSSmoke(t, udsPath, 30*time.Second)
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Stop(shutCtx)
		<-errCh
	})

	udsClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	body := []byte(`{"project":"internal-platform-x","prompt":"x","mode":"interactive"}`)
	resp, err := udsClient.Post("http://unix/v1/augment", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("augment POST: %v", err)
	}
	bodyB, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/v1/augment status=%d body=%s; want 503 (route registered, handler nil — graceful-degrade)", resp.StatusCode, bodyB)
	}
	if !bytes.Contains(bodyB, []byte("augmentation not configured")) {
		t.Errorf("body=%q; want it to contain \"augmentation not configured\"", bodyB)
	}
}

func TestProductionBootSmoke_AugmentRouteWiredReturns200(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "smoke.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	udsPath := shortSockPathForSmokeTest(t, "augok.sock")
	probeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe Listen tcp: %v", err)
	}
	tcpAddr := probeLn.Addr().String()
	_ = probeLn.Close()

	srv := daemon.New(st, daemon.Config{
		UDSPath:           udsPath,
		HTTPAddr:          tcpAddr,
		DisableAuditInfra: true,
	})

	dr := augmentSmokeDoctrineReader{}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{
			StaticContext:   `{"project_meta":{"project_id":"` + req.ProjectID + `"}}`,
			VolatileContext: `{}`,
			Citations:       []byte(`[]`),
			AuditEventID:    "evt-smoke-1",
		}, nil
	}
	srv.SetAugmentHandler(handlers.AugmentWithPipeline(dr, runner))

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	waitForUDSSmoke(t, udsPath, 30*time.Second)
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Stop(shutCtx)
		<-errCh
	})

	udsClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	body := []byte(`{"project":"internal-platform-x","prompt":"refactor x","mode":"interactive"}`)
	resp, err := udsClient.Post("http://unix/v1/augment", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("augment max-scope POST: %v", err)
	}
	bodyB, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("augment max-scope: status=%d body=%s; want 200", resp.StatusCode, bodyB)
	}
	var envelope struct {
		StaticContext string `json:"static_context"`
		Doctrine      string `json:"doctrine"`
		AuditEventID  string `json:"audit_event_id"`
	}
	if err := json.Unmarshal(bodyB, &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, bodyB)
	}
	if envelope.StaticContext == "" {
		t.Errorf("envelope static_context empty")
	}
	if envelope.AuditEventID != "evt-smoke-1" {
		t.Errorf("envelope audit_event_id=%q; want evt-smoke-1", envelope.AuditEventID)
	}

	body = []byte(`{"project":"secret-proj","prompt":"x","mode":"interactive"}`)
	resp, err = udsClient.Post("http://unix/v1/augment", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("augment capa-firewall POST: %v", err)
	}
	bodyB, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("augment capa-firewall: status=%d body=%s; want 204 (inv-zen-170 disabled)", resp.StatusCode, bodyB)
	}
}

type augmentSmokeDoctrineReader struct{}

func (augmentSmokeDoctrineReader) AugmentationConfig(_ context.Context, project string) (handlers.AugmentationConfig, error) {
	if len(project) >= 6 && project[:6] == "secret" {
		return handlers.AugmentationConfig{
			Enable:       false,
			MaxKGTokens:  0,
			DoctrineName: "capa-firewall",
		}, nil
	}
	return handlers.AugmentationConfig{
		Enable:       true,
		MaxKGTokens:  25000,
		DoctrineName: "max-scope",
	}, nil
}
