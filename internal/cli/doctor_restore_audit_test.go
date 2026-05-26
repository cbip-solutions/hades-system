package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
)

func TestDoctorRestore_EmitsAuditEventOnSuccess(t *testing.T) {

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
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("orig"), 0o644)
	b := backup.NewBackuper(backup.Config{})
	m, err := b.BackupTarget(context.Background(), "test", source)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("modified"), 0o644)

	cmd := NewDoctorRestoreCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{m.BackupID, "--overwrite"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; out=%q", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("audit emit count = %d; want 1 (evt.doctor.restore.applied)", len(events))
	}
	if events[0].eventType != DoctorRestoreAuditEventType {
		t.Errorf("eventType = %q; want %q", events[0].eventType, DoctorRestoreAuditEventType)
	}
	if events[0].payload["backupID"] != m.BackupID {
		t.Errorf("payload.backupID = %v; want %s", events[0].payload["backupID"], m.BackupID)
	}
}

func TestDoctorRestore_AuditEmitBestEffortOnDaemonDown(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("daemon down"))
	}))
	defer srv.Close()

	prevFactory := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	defer func() { TestOnlyClientFactory = prevFactory }()

	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("orig"), 0o644)
	b := backup.NewBackuper(backup.Config{})
	m, err := b.BackupTarget(context.Background(), "test", source)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("modified"), 0o644)

	cmd := NewDoctorRestoreCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{m.BackupID, "--overwrite"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute should succeed even when audit emit fails: %v; out=%q", err, out.String())
	}
	if !strings.Contains(out.String(), "Restored backup") {
		t.Errorf("stdout missing 'Restored backup' (restore itself should have succeeded): %q", out.String())
	}

	if !strings.Contains(out.String(), "audit emit failed") {
		t.Errorf("stderr missing 'audit emit failed' warning: %q", out.String())
	}
}
