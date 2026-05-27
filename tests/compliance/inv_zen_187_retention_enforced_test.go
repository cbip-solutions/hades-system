// tests/compliance/inv_zen_187_retention_enforced_test.go
//
// Spec §8.6 invariant compliance test: the state cleanup substrate
// (internal/state/cleanup) MUST enforce retention TTLs per Q12=D +
// spec §2.12. Every expired path MUST emit one evt.state.cleanup.deleted
// audit event (best-effort; non-blocking).
//
// location per spec §8.6.
package compliance

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/state/cleanup"
)

type recordingEmitter187 struct {
	mu     sync.Mutex
	events []recordedEvent187
}

type recordedEvent187 struct {
	eventType string
	payload   []byte
}

func (r *recordingEmitter187) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent187{eventType: eventType, payload: payload})
	return "hash", nil
}

func TestInvZen187_RetentionEnforcedAndAuditEmitted(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	cache := t.TempDir()

	docBackups := filepath.Join(state, "doctor-backups")
	if err := os.MkdirAll(docBackups, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)
	expired := []string{"20260101T000000Z", "20260102T000000Z"}
	for _, id := range expired {
		dir := filepath.Join(docBackups, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	freshDir := filepath.Join(docBackups, "20260516T000000Z")
	if err := os.MkdirAll(freshDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chtimes(freshDir, fresh, fresh); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	emitter := &recordingEmitter187{}
	count, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: state,
		CacheDir: cache,
		Policy:   cleanup.DefaultPolicy(),
		Emitter:  emitter,
	})
	if err != nil {
		t.Fatalf("cleanup.Apply: %v", err)
	}

	if count != 2 {
		t.Errorf("expired count = %d; want 2", count)
	}

	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if len(emitter.events) != 2 {
		t.Fatalf("audit emit count = %d; want 2 (inv-zen-187: one per deletion)", len(emitter.events))
	}
	for i, e := range emitter.events {
		if e.eventType != "evt.state.cleanup.deleted" {
			t.Errorf("event[%d].eventType = %q; want evt.state.cleanup.deleted", i, e.eventType)
		}
	}

	for _, id := range expired {
		_, statErr := os.Stat(filepath.Join(docBackups, id))
		if !os.IsNotExist(statErr) {
			t.Errorf("expired %s still exists; cleanup didn't delete", id)
		}
	}

	if _, statErr := os.Stat(freshDir); statErr != nil {
		t.Errorf("fresh dir removed unexpectedly: %v", statErr)
	}
}

func TestInvZen187_DryRunSuppressesDelete(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	cache := t.TempDir()

	docBackups := filepath.Join(state, "doctor-backups")
	if err := os.MkdirAll(docBackups, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	dir := filepath.Join(docBackups, "20260101T000000Z")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	emitter := &recordingEmitter187{}
	count, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: state,
		CacheDir: cache,
		Policy:   cleanup.DefaultPolicy(),
		Emitter:  emitter,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("cleanup.Apply dry-run: %v", err)
	}

	if count != 1 {
		t.Errorf("dry-run expired count = %d; want 1", count)
	}

	if len(emitter.events) != 0 {
		t.Errorf("dry-run audit emit count = %d; want 0 (dry-run is preview-only)", len(emitter.events))
	}

	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("dry-run deleted path: %v", statErr)
	}
}

func TestInvZen187_KeepIDExceptsRetentionExpiry(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	cache := t.TempDir()

	docBackups := filepath.Join(state, "doctor-backups")
	if err := os.MkdirAll(docBackups, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	keepID := "20260101T000000Z"
	expireID := "20260102T000000Z"
	for _, id := range []string{keepID, expireID} {
		dir := filepath.Join(docBackups, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	emitter := &recordingEmitter187{}
	count, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: state,
		CacheDir: cache,
		Policy:   cleanup.DefaultPolicy(),
		KeepIDs:  []string{keepID},
		Emitter:  emitter,
	})
	if err != nil {
		t.Fatalf("cleanup.Apply: %v", err)
	}
	if count != 1 {
		t.Errorf("expired count = %d; want 1 (keep exception)", count)
	}

	if _, statErr := os.Stat(filepath.Join(docBackups, keepID)); statErr != nil {
		t.Errorf("keep ID deleted: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(docBackups, expireID)); !os.IsNotExist(statErr) {
		t.Errorf("expire ID still present: %v", statErr)
	}
}
