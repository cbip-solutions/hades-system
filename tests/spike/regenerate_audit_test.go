package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRunSpike_RegenerateEmitsAuditEvent(t *testing.T) {

	type recorded struct {
		eventType string
		payload   map[string]any
	}
	var (
		mu     sync.Mutex
		events []recorded
	)
	auditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit/emit" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			mu.Lock()
			events = append(events, recorded{eventType: req.Type, payload: req.Payload})
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "evt-1", "accepted": true})
	}))
	defer auditSrv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "audit_sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		regenerate:   true,
		apiBase:      apiBase,
		rawBase:      rawBase,
		auditEmitURL: auditSrv.URL,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("audit emit count = %d; want 1 (evt.spike.hermes_contract.regenerated)\nrecorded: %+v", len(events), events)
	}
	if events[0].eventType != "evt.spike.hermes_contract.regenerated" {
		t.Errorf("eventType = %q; want evt.spike.hermes_contract.regenerated", events[0].eventType)
	}
	if got, _ := events[0].payload["hermes_head_sha"].(string); got != "audit_sha" {
		t.Errorf("payload.hermes_head_sha = %q; want audit_sha\nfull: %+v", got, events[0].payload)
	}
	if _, ok := events[0].payload["artifact_path"].(string); !ok {
		t.Errorf("payload.artifact_path missing or wrong type: %+v", events[0].payload)
	}

	if _, ok := events[0].payload["artifact_bytes"]; !ok {
		t.Errorf("payload.artifact_bytes missing: %+v", events[0].payload)
	}
}

// TestRunSpike_RegenerateAuditEmitBestEffortOnDaemonDown asserts the
// graceful-degradation contract: when the audit daemon is unreachable,
// the regenerator MUST still write the artifact + exit 0. The audit
// event is forensic; the artifact regeneration is load-bearing.
//
// CI workflows typically run `go run ./tests/spike --regenerate` without
// a running daemon, so this is the expected path in production. The
// test simulates daemon-down via a closed server (connection refused).
func TestRunSpike_RegenerateAuditEmitBestEffortOnDaemonDown(t *testing.T) {

	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	deadSrv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "graceful_sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		regenerate:   true,
		apiBase:      apiBase,
		rawBase:      rawBase,
		auditEmitURL: deadSrv.URL,
	})
	if err != nil {
		t.Fatalf("regenerate should succeed even when daemon is down: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join("docs", "superpowers", "specs", "*plan-13-spike-hermes-mcp-contract*.md"))
	if len(matches) == 0 {
		t.Errorf("artifact not written when daemon is down")
	}
}

func TestRunSpike_RegenerateAuditEmitSkippedWhenNoURL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "skip_sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		regenerate: true,
		apiBase:    apiBase,
		rawBase:    rawBase,
	})
	if err != nil {
		t.Fatalf("regenerate without auditEmitURL: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join("docs", "superpowers", "specs", "*plan-13-spike-hermes-mcp-contract*.md"))
	if len(matches) == 0 {
		t.Errorf("artifact not written")
	}
}

func TestRunSpike_CheckModeDoesNotEmit(t *testing.T) {
	type recorded struct {
		eventType string
	}
	var (
		mu     sync.Mutex
		events []recorded
	)
	auditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type string `json:"type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		events = append(events, recorded{eventType: req.Type})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "evt-1", "accepted": true})
	}))
	defer auditSrv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	apiBase, rawBase, cleanup := newMockHermesServers(t, "check_sha", samplePluginsPy)
	defer cleanup()

	info := &hermesHeadInfo{
		HeadSHA:      "check_sha",
		ValidHooks:   []string{"pre_tool_call", "post_tool_call", "on_session_start"},
		ManifestKeys: []string{"name"},
	}
	wantBytes := renderArtifact(info)
	artifactPath := filepath.Join(specsDir, "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md")
	if err := os.WriteFile(artifactPath, wantBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{
		check:        true,
		apiBase:      apiBase,
		rawBase:      rawBase,
		auditEmitURL: auditSrv.URL,
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 {
		t.Errorf("check mode emitted %d event(s); want 0 (--regenerate only): %+v", len(events), events)
	}
}
