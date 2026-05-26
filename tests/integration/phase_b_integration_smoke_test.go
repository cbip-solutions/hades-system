// SPDX-License-Identifier: MIT
//
// Package integration_test — Plan 15 Phase B Task B-16.3 end-to-end
// integration smoke test for the daemon ↔ sidecar ↔ notification flow.
//
// Per W7-B16 mandate (Wave 7 dispatch closure of W7-B8 deferred wiring),
// this file exercises three load-bearing seams of the daemon's
// Phase-B-side substrate:
//
//  1. SidecarBackend message routing: the dispatcher's TierBackend
//     façade delegates POST /v1/messages to the configured loopback
//     sidecar URL, forwards the request body verbatim, and returns
//     the upstream response without rewrite.
//
//  2. Notifications-flow daemon ingestion: the daemon endpoint
//     POST /v1/notifications/post receives a sidecar-emitted notification
//     payload (severity / source / ts / payload) and persists it through
//     the NotificationsInserter seam — the canonical bypass.HTTPNotifier
//     callback round-trip the sidecar uses for tier-switch /
//     refresh-permanent-fail / cert-pin / anomaly-threshold events.
//
//  3. Graceful degradation on sidecar stop: when the loopback sidecar
//     vanishes mid-test, SidecarBackend.Forward surfaces
//     ErrSidecarUnavailable so the dispatcher's name-based cascade
//     (inv-zen-066 / inv-zen-280) proceeds to the next configured
//     provider rather than hanging or returning a hidden 5xx.
//
// The three sub-tests are independent (each spins its own httptest.Server)
// so they document the three contracts in isolation; a regression in
// one does not mask the others.
//
// Build-tag posture (per Plan 15 spec line 2849): the file is gated by
// `//go:build integration` so `make test` in CI without the integration
// tag skips it (the brew-installed sidecar is not present on stock CI
// runners). Run locally via:
//
//	go test -tags=integration ./tests/integration/ -run TestPhaseB
//
// Boundary discipline (inv-zen-031): this file imports
// internal/providers (SidecarBackend) + internal/daemon/handlers
// (NotificationsPost) + internal/store (Notification value type) +
// stdlib. It does NOT import private-tier1-module (extracted to
// the private repo per Plan 15 Phase B-2).
//
// Plan-15 invariant placeholder: inv-zen-B16-3 (concrete inv-zen-NNN
// allocated at merge-time renumber reconciliation per the renumber-on-
// merge playbook; current high-water tracked in
// reference_invariant_numbering memory).

//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestPhaseB_SidecarBackend_RoutesMessages(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		messagesRcvd   int
		lastBodyRcvd   []byte
		lastIdemKeyRcv string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy","version":"v0.1.4"}`))
			return
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sidecar/info":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":                        "v0.1.4",
				"supported_features":             []string{"v0.17.8_refresh_protocol"},
				"config_hash":                    "fake-hash",
				"bypass_configs_version":         "test",
				"anthropic_api_envelope_version": "2025-12-31",
			})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			messagesRcvd++
			lastBodyRcvd = append([]byte(nil), body...)
			lastIdemKeyRcv = r.Header.Get("Idempotency-Key")
			mu.Unlock()

			resp := map[string]any{
				"id":    "msg_01PHASEB_INTEGRATION",
				"model": "claude-haiku-4-5",
				"content": []any{
					map[string]any{"type": "text", "text": "phase b integration ok"},
				},
				"usage": map[string]any{
					"input_tokens":  42,
					"output_tokens": 7,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		t.Errorf("unexpected route hit: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 30*time.Second)
	defer backend.Close()

	canonicalBody := []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi from phase b"}]}`)
	idemKey := "phase-b-integration-key-01"

	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Method:         http.MethodPost,
		Path:           "/v1/messages",
		Body:           canonicalBody,
		Model:          "claude-haiku-4-5",
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response")
	}

	mu.Lock()
	gotRcvd := messagesRcvd
	gotBody := append([]byte(nil), lastBodyRcvd...)
	gotIdemKey := lastIdemKeyRcv
	mu.Unlock()

	if gotRcvd != 1 {
		t.Errorf("fake sidecar received %d messages; want 1", gotRcvd)
	}
	if !bytes.Equal(gotBody, canonicalBody) {
		t.Errorf("body sent != body received:\n  sent: %s\n  recv: %s", string(canonicalBody), string(gotBody))
	}
	if gotIdemKey != idemKey {
		t.Errorf("Idempotency-Key header = %q; want %q", gotIdemKey, idemKey)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("Status = %d; want 200", resp.Status)
	}
	if resp.TierUsed != providers.TierInHouse {
		t.Errorf("TierUsed = %v; want TierInHouse (sidecar wraps Anthropic Max OAuth bypass)", resp.TierUsed)
	}
	if !strings.Contains(string(resp.Body), "phase b integration ok") {
		t.Errorf("response body missing expected marker:\n%s", string(resp.Body))
	}
	if resp.InputTokens != 42 || resp.OutputTokens != 7 {
		t.Errorf("usage tokens = (%d, %d); want (42, 7)", resp.InputTokens, resp.OutputTokens)
	}
	if resp.ModelUsed != "claude-haiku-4-5" {
		t.Errorf("ModelUsed = %q; want claude-haiku-4-5", resp.ModelUsed)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("LatencyMs = %d; want non-negative", resp.LatencyMs)
	}

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	if err := backend.Probe(probeCtx); err != nil {
		t.Errorf("Probe failed: %v (sidecar /health probe expected to succeed)", err)
	}
}

type fakeDaemonNotificationsInserter struct {
	mu       sync.Mutex
	rows     []store.Notification
	wantErr  error
	nextID   int64
	idCursor int64
}

func (f *fakeDaemonNotificationsInserter) InsertBypassNotification(_ context.Context, n store.Notification) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.wantErr != nil {
		return 0, f.wantErr
	}
	f.rows = append(f.rows, n)
	f.idCursor++
	if f.nextID != 0 {
		return f.nextID, nil
	}
	return f.idCursor, nil
}

func (f *fakeDaemonNotificationsInserter) snapshot() []store.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.Notification, len(f.rows))
	copy(out, f.rows)
	return out
}

type fakeNotificationsPostCtx struct {
	inserter handlers.NotificationsInserter
}

func (f *fakeNotificationsPostCtx) NotificationsInserter() handlers.NotificationsInserter {
	return f.inserter
}

func TestPhaseB_NotificationsFlow(t *testing.T) {
	t.Parallel()

	inserter := &fakeDaemonNotificationsInserter{nextID: 99}
	ctx := &fakeNotificationsPostCtx{inserter: inserter}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/notifications/post", handlers.NotificationsPost(ctx))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now().UTC()
	wire := map[string]any{
		"severity": "warn",
		"source":   "bypass.tier-switch",
		"ts":       now.Format(time.RFC3339),
		"payload": map[string]any{
			"title":  "HADES: bypass tier unavailable",
			"body":   "Bypass tier (in_house) unavailable: 401 after refresh. Orchestrator cascading to the next configured provider.",
			"from":   "in_house",
			"reason": "401 after refresh",
		},
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire body: %v", err)
	}

	postCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost,
		srv.URL+"/v1/notifications/post", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d; want 200; body = %s", resp.StatusCode, string(body))
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode 200 body: %v", err)
	}
	if inserted, _ := respBody["inserted"].(bool); !inserted {
		t.Errorf("respBody.inserted = %v; want true", respBody["inserted"])
	}

	rows := inserter.snapshot()
	if len(rows) != 1 {
		t.Fatalf("inserter rows = %d; want 1", len(rows))
	}
	got := rows[0]
	if got.Severity != "WARN" {
		t.Errorf("Severity = %q; want WARN (normalized from wire 'warn')", got.Severity)
	}
	if got.Source != "bypass.tier-switch" {
		t.Errorf("Source = %q; want bypass.tier-switch", got.Source)
	}
	if got.Title != "HADES: bypass tier unavailable" {
		t.Errorf("Title = %q; want HADES: bypass tier unavailable", got.Title)
	}
	if !strings.Contains(got.Body, "in_house") {
		t.Errorf("Body missing payload context: %q", got.Body)
	}

	if got.TS.IsZero() {
		t.Errorf("TS zero; expected RFC3339 parse of wire ts %q", wire["ts"])
	}
	if got.TS.After(time.Now().Add(60*time.Second)) ||
		got.TS.Before(time.Now().Add(-60*time.Second)) {
		t.Errorf("TS = %v; out of ±60s window of test wall-clock", got.TS)
	}

	certPinWire := map[string]any{
		"severity": "error",
		"source":   "bypass.cert",
		"ts":       time.Now().UTC().Format(time.RFC3339),
		"payload": map[string]any{
			"title":  "Bypass cert pin mismatch",
			"body":   "Possible MITM or Anthropic rotated CA. Detail: rogue-intermediate-spki-mismatch",
			"detail": "rogue-intermediate-spki-mismatch",
		},
	}
	raw2, _ := json.Marshal(certPinWire)
	req2, _ := http.NewRequestWithContext(postCtx, http.MethodPost,
		srv.URL+"/v1/notifications/post", bytes.NewReader(raw2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("cert-pin POST Do: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("cert-pin status = %d; want 200", resp2.StatusCode)
	}
	rows = inserter.snapshot()
	if len(rows) != 2 {
		t.Fatalf("after cert-pin: inserter rows = %d; want 2", len(rows))
	}
	if rows[1].Severity != "CRITICAL" {
		t.Errorf("cert-pin Severity = %q; want CRITICAL (normalized from wire 'error')",
			rows[1].Severity)
	}
}

func TestPhaseB_GracefulDegrade_SidecarStop(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"msg_01","model":"claude-haiku-4-5","usage":{"input_tokens":1,"output_tokens":1}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	// IMPORTANT: do NOT defer srv.Close() — we close it explicitly mid-test.

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	reqBody := []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"baseline"}]}`)
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Method: http.MethodPost,
		Path:   "/v1/messages",
		Body:   reqBody,
		Model:  "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("baseline Forward failed: %v (want success before sidecar stop)", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("baseline Status = %d; want 200", resp.Status)
	}

	srv.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	resp2, err := backend.Forward(ctx2, providers.TierRequest{
		Method: http.MethodPost,
		Path:   "/v1/messages",
		Body:   reqBody,
		Model:  "claude-haiku-4-5",
	})
	if err == nil {
		t.Fatalf("Forward after sidecar stop returned nil err; got resp %+v (want ErrSidecarUnavailable)", resp2)
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want errors.Is(ErrSidecarUnavailable) (dispatcher fallback signal)", err)
	}

	probeErr := backend.Probe(ctx2)
	if probeErr == nil {
		t.Error("Probe after sidecar stop returned nil err; want ErrSidecarUnavailable")
	}
	if probeErr != nil && !errors.Is(probeErr, providers.ErrSidecarUnavailable) {
		t.Errorf("Probe err = %v; want errors.Is(ErrSidecarUnavailable)", probeErr)
	}
}
