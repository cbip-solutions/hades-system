// Package cleanup_test covers the state-retention policy enforcement
// per Q12=D + invariant.
//
// Coverage targets: ≥90% on the package (security-critical: data
// lifecycle enforcement).
package cleanup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/state/cleanup"
)

func TestApplyRetentionExpiresOldBackups(t *testing.T) {
	stateDir := t.TempDir()
	oldPath := filepath.Join(stateDir, "doctor-backups", "20260413T120000Z")
	if err := os.MkdirAll(oldPath, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)
	if err := os.Chtimes(oldPath, thirtyOneDaysAgo, thirtyOneDaysAgo); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if expired != 1 {
		t.Errorf("expired count = %d, want 1 (31d-old doctor-backup)", expired)
	}
	if _, sterr := os.Stat(oldPath); !os.IsNotExist(sterr) {
		t.Errorf("old path still exists; Apply failed to delete")
	}
}

func TestApplyRetentionPreservesYoungBackups(t *testing.T) {
	stateDir := t.TempDir()
	youngPath := filepath.Join(stateDir, "doctor-backups", "20260512T120000Z")
	if err := os.MkdirAll(youngPath, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	twentyNineDaysAgo := time.Now().AddDate(0, 0, -29)
	if err := os.Chtimes(youngPath, twentyNineDaysAgo, twentyNineDaysAgo); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if expired != 0 {
		t.Errorf("expired count = %d, want 0 (29d-old backup preserved)", expired)
	}
	if _, sterr := os.Stat(youngPath); os.IsNotExist(sterr) {
		t.Errorf("young path removed; want preserved")
	}
}

func TestApplyDryRunDoesNotDelete(t *testing.T) {
	stateDir := t.TempDir()
	oldPath := filepath.Join(stateDir, "doctor-backups", "20260413T120000Z")
	_ = os.MkdirAll(oldPath, 0o700)
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)
	_ = os.Chtimes(oldPath, thirtyOneDaysAgo, thirtyOneDaysAgo)

	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Apply --dry-run: %v", err)
	}
	if expired != 1 {
		t.Errorf("expired count = %d, want 1 (reported but not deleted)", expired)
	}
	if _, sterr := os.Stat(oldPath); os.IsNotExist(sterr) {
		t.Errorf("--dry-run deleted; want preserved")
	}
}

func TestApplyKeepIDExcepts(t *testing.T) {
	stateDir := t.TempDir()
	preservedID := "20260413T120000Z"
	preservedPath := filepath.Join(stateDir, "doctor-backups", preservedID)
	_ = os.MkdirAll(preservedPath, 0o700)
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)
	_ = os.Chtimes(preservedPath, thirtyOneDaysAgo, thirtyOneDaysAgo)

	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
		KeepIDs:  []string{preservedID},
	})
	if err != nil {
		t.Fatalf("Apply --keep: %v", err)
	}
	if expired != 0 {
		t.Errorf("expired = %d, want 0 (--keep excepts)", expired)
	}
	if _, sterr := os.Stat(preservedPath); os.IsNotExist(sterr) {
		t.Errorf("--keep ID was deleted; want preserved")
	}
}

func TestApplyEmitsAuditPerDeletion(t *testing.T) {
	stateDir := t.TempDir()
	for _, id := range []string{"20260301T120000Z", "20260301T130000Z"} {
		p := filepath.Join(stateDir, "doctor-backups", id)
		_ = os.MkdirAll(p, 0o700)
		old := time.Now().AddDate(0, 0, -100)
		_ = os.Chtimes(p, old, old)
	}
	emitter := &recordingEmitter{}
	_, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
		Emitter:  emitter,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(emitter.events) != 2 {
		t.Errorf("emitted = %d, want 2 (one per deleted path)", len(emitter.events))
	}
	for _, ev := range emitter.events {
		if ev.Type != cleanup.AuditEventType {
			t.Errorf("event type = %q, want %q", ev.Type, cleanup.AuditEventType)
		}
	}
}

func TestSpikeArtifactsIndefinite(t *testing.T) {
	stateDir := t.TempDir()
	spikePath := filepath.Join(stateDir, "spike-artifacts", "plan-13")
	_ = os.MkdirAll(spikePath, 0o700)
	veryOld := time.Now().AddDate(-5, 0, 0)
	_ = os.Chtimes(spikePath, veryOld, veryOld)

	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   cleanup.DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if expired != 0 {
		t.Errorf("expired = %d, want 0 (spike-artifacts indefinite per Q12=D)", expired)
	}
	if _, sterr := os.Stat(spikePath); os.IsNotExist(sterr) {
		t.Errorf("spike-artifacts deleted; want preserved indefinitely")
	}
}

func TestApplyOverrideShortensRetention(t *testing.T) {
	stateDir := t.TempDir()
	p := filepath.Join(stateDir, "doctor-backups", "20260514T120000Z")
	_ = os.MkdirAll(p, 0o700)
	twoDaysAgo := time.Now().AddDate(0, 0, -2)
	_ = os.Chtimes(p, twoDaysAgo, twoDaysAgo)

	policy := cleanup.DefaultPolicy().MergeOverride(cleanup.Override{DoctorBackupsDays: 1})
	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: stateDir,
		Policy:   policy,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if expired != 1 {
		t.Errorf("expired = %d, want 1 (2d-old > 1d shortened retention)", expired)
	}
}

func TestApplyCacheSubsystem(t *testing.T) {
	cacheDir := t.TempDir()

	cachedFile := filepath.Join(cacheDir, "old.json")
	_ = os.WriteFile(cachedFile, []byte("cached"), 0o644)
	eightDaysAgo := time.Now().AddDate(0, 0, -8)
	_ = os.Chtimes(cachedFile, eightDaysAgo, eightDaysAgo)

	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		CacheDir: cacheDir,
		Policy:   cleanup.DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if expired != 1 {
		t.Errorf("expired = %d, want 1 (8d-old cache)", expired)
	}
}

func TestApplyEmptyStateDirReturnsZero(t *testing.T) {
	expired, err := cleanup.Apply(context.Background(), cleanup.Options{
		StateDir: t.TempDir(),
		Policy:   cleanup.DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Apply on empty dir: %v", err)
	}
	if expired != 0 {
		t.Errorf("expired = %d, want 0", expired)
	}
}

func TestRenderJSONEmitsSchemaVersion(t *testing.T) {
	var buf bytes.Buffer
	entries := []cleanup.StateEntry{
		{Path: "/x", Subsystem: "doctor-backups", ID: "a"},
	}
	if err := cleanup.RenderJSON(context.Background(), &buf, entries); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var report cleanup.ListReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}
	if report.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want 1.0", report.SchemaVersion)
	}
	if len(report.Entries) != 1 {
		t.Errorf("Entries = %d, want 1", len(report.Entries))
	}
}

func TestEnumerateSkipsMissingRoots(t *testing.T) {
	entries, err := cleanup.Enumerate(context.Background(), "/does/not/exist/state", "/does/not/exist/cache")
	if err != nil {
		t.Fatalf("Enumerate missing roots: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

func TestEnumeratePopulatesFields(t *testing.T) {
	stateDir := t.TempDir()
	p := filepath.Join(stateDir, "doctor-backups", "test-id")
	_ = os.MkdirAll(p, 0o700)
	_ = os.WriteFile(filepath.Join(p, "f.txt"), []byte("hello"), 0o644)
	entries, err := cleanup.Enumerate(context.Background(), stateDir, t.TempDir())
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Path, "test-id") {
		t.Errorf("Path = %q, want substring 'test-id'", entries[0].Path)
	}
	if entries[0].Subsystem != "doctor-backups" {
		t.Errorf("Subsystem = %q, want doctor-backups", entries[0].Subsystem)
	}
	if entries[0].Size < 5 {
		t.Errorf("Size = %d, want >= 5", entries[0].Size)
	}
}

type recordingEmitter struct {
	events []recordedEvent
}

type recordedEvent struct {
	Type    string
	Payload []byte
}

func (r *recordingEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	r.events = append(r.events, recordedEvent{Type: eventType, Payload: payload})
	return "stub-hash", nil
}
