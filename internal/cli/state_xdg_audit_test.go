package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestStateCleanup_EmitsAuditEventPerDeletion(t *testing.T) {

	type recorded struct {
		eventType string
		payload   map[string]any
	}
	var (
		mu     sync.Mutex
		events []recorded
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/audit/emit" {
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
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	prevFactory := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	defer func() { TestOnlyClientFactory = prevFactory }()

	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	docBackups := filepath.Join(state, "zen-swarm", "doctor-backups")
	if err := os.MkdirAll(docBackups, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	old := time.Now().Add(-60 * 24 * time.Hour)
	for _, id := range []string{"20260101T000000Z", "20260102T000000Z"} {
		dir := filepath.Join(docBackups, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	rootCmd := NewStateCmd()
	rootCmd.SetContext(context.Background())
	rootCmd.SetArgs([]string{"cleanup"})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; out=%q", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("audit emit count = %d; want 2 (one per expired backup)", len(events))
	}
	for i, e := range events {
		if e.eventType != "evt.state.cleanup.deleted" {
			t.Errorf("event[%d].eventType = %q; want evt.state.cleanup.deleted", i, e.eventType)
		}
		if e.payload["subsystem"] != "doctor-backups" {
			t.Errorf("event[%d].payload.subsystem = %v; want doctor-backups", i, e.payload["subsystem"])
		}
	}
}
