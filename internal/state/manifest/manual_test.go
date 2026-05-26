package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAppender struct {
	events []EventPayload
	failOn string
}

func (f *fakeAppender) AppendEvent(ctx context.Context, ev EventPayload) error {
	if f.failOn != "" && ev.Type == f.failOn {
		return errors.New("simulated append failure")
	}
	f.events = append(f.events, ev)
	return nil
}

func newTracker(t *testing.T) (*ManualTracker, *Regenerator, *fakeAppender, string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegenerator(schema)
	a := &fakeAppender{}
	mt := NewManualTracker(schema, r, a)
	return mt, r, a, dir
}

func TestManualTracker_PinEmitsChainEvent(t *testing.T) {
	mt, r, a, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	initial := makeFreshManifest()
	initial.ZenSwarm.SubstrateMinVersion = "0.7.0"
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}

	change := PinRequest{
		Path:       "zen-swarm.substrate_min_version",
		NewValue:   "0.7.1",
		Reason:     "OpenClaude 0.7.0 has CVE-2026-X",
		OperatorID: "testuser",
		Timestamp:  time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
	}
	if err := mt.Pin(context.Background(), manifestPath, change); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if len(a.events) != 1 {
		t.Fatalf("expected 1 chain event, got %d", len(a.events))
	}
	ev := a.events[0]
	if ev.Type != "state.manual_field_changed" {
		t.Errorf("Type: %q, want state.manual_field_changed", ev.Type)
	}
	if ev.Field != "zen-swarm.substrate_min_version" {
		t.Errorf("Field: %q", ev.Field)
	}
	if ev.OldValue != "0.7.0" || ev.NewValue != "0.7.1" {
		t.Errorf("Values: old=%v new=%v want 0.7.0 / 0.7.1", ev.OldValue, ev.NewValue)
	}
	if ev.Reason != change.Reason {
		t.Errorf("Reason: %q", ev.Reason)
	}
	if ev.OperatorID != "testuser" {
		t.Errorf("OperatorID: %q", ev.OperatorID)
	}
}

func TestManualTracker_PinPersistsValueOnDisk(t *testing.T) {
	mt, r, _, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	initial := makeFreshManifest()
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `substrate_min_version = "0.7.1"`) {
		t.Errorf("Pin did not persist new value to disk:\n%s", body)
	}
}

func TestManualTracker_EmptyReasonRejected(t *testing.T) {
	mt, _, _, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "  ", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if !errors.Is(err, ErrEmptyReason) {
		t.Errorf("want ErrEmptyReason, got %v", err)
	}
}

func TestManualTracker_NonManualPathRejected(t *testing.T) {
	mt, r, _, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	initial := makeFreshManifest()
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path:       "zen-swarm.version",
		NewValue:   "9.9.9",
		Reason:     "x",
		OperatorID: "testuser",
		Timestamp:  time.Now().UTC(),
	})
	if !errors.Is(err, ErrManualFieldNotFound) {
		t.Errorf("want ErrManualFieldNotFound, got %v", err)
	}
}

func TestManualTracker_AppendFailureRollsBackOnDisk(t *testing.T) {
	mt, r, a, dir := newTracker(t)
	a.failOn = "state.manual_field_changed"
	manifestPath := filepath.Join(dir, "system-state.toml")
	initial := makeFreshManifest()
	initial.ZenSwarm.SubstrateMinVersion = "0.7.0"
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if !errors.Is(err, ErrEventEmissionFailed) {
		t.Errorf("want ErrEventEmissionFailed, got %v", err)
	}
	body, _ := os.ReadFile(manifestPath)
	if strings.Contains(string(body), `substrate_min_version = "0.7.1"`) {
		t.Error("Pin must roll back on-disk write when event emission fails")
	}
	if !strings.Contains(string(body), `substrate_min_version = "0.7.0"`) {
		t.Errorf("expected rollback to 0.7.0, body:\n%s", body)
	}
}

func TestManualTracker_MalformedTOMLRejected(t *testing.T) {
	mt, _, _, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	if err := os.WriteFile(manifestPath, []byte("not { valid toml }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if !errors.Is(err, ErrManifestInvalid) {
		t.Errorf("want ErrManifestInvalid for malformed TOML, got %v", err)
	}
}

func TestManualTracker_PinNoExistingFile(t *testing.T) {
	mt, _, a, dir := newTracker(t)

	manifestPath := filepath.Join(dir, "missing-state.toml")
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Pin on missing manifest: %v", err)
	}
	if len(a.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(a.events))
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `substrate_min_version = "0.7.1"`) {
		t.Errorf("missing-manifest Pin did not write new value:\n%s", body)
	}
}

func TestManualTracker_ReadErrorPropagated(t *testing.T) {
	mt, _, _, dir := newTracker(t)

	manifestPath := filepath.Join(dir, "is-a-dir")
	if err := os.MkdirAll(manifestPath, 0o755); err != nil {
		t.Fatal(err)
	}
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error reading directory as file, got nil")
	}
}

func TestSetValueAtPath_ShortPath(t *testing.T) {
	var m Manifest
	err := setValueAtPath(&m, "noDot", "v")
	if !errors.Is(err, ErrManualFieldNotFound) {
		t.Errorf("want ErrManualFieldNotFound for short path, got %v", err)
	}
}

func TestSetValueAtPath_UnknownSection(t *testing.T) {
	var m Manifest
	err := setValueAtPath(&m, "nonexistent-section.field", "v")
	if !errors.Is(err, ErrManualFieldNotFound) {
		t.Errorf("want ErrManualFieldNotFound for unknown section, got %v", err)
	}
}

func TestSetValueAtPath_UnknownField(t *testing.T) {
	var m Manifest
	err := setValueAtPath(&m, "zen-swarm.no_such_leaf", "v")
	if !errors.Is(err, ErrManualFieldNotFound) {
		t.Errorf("want ErrManualFieldNotFound for unknown field, got %v", err)
	}
}

func TestSetValueAtPath_TypeMismatch(t *testing.T) {
	var m Manifest

	err := setValueAtPath(&m, "autonomous-mode.prerequisites-met", 42)
	if err == nil {
		t.Fatal("expected type-mismatch error, got nil")
	}
}

func TestGetValueAtPath_ShortPath(t *testing.T) {
	m := makeFreshManifest()
	v := getValueAtPath(m, "noDot")
	if v.IsValid() {
		t.Errorf("expected invalid Value for short path, got %v", v)
	}
}

func TestGetValueAtPath_UnknownSection(t *testing.T) {
	m := makeFreshManifest()
	v := getValueAtPath(m, "nonexistent-section.field")
	if v.IsValid() {
		t.Errorf("expected invalid Value for unknown section, got %v", v)
	}
}

func TestSetValueAtPath_NilValue(t *testing.T) {
	var m Manifest
	err := setValueAtPath(&m, "zen-swarm.substrate_min_version", nil)
	if err == nil {
		t.Fatal("expected error for nil pin value, got nil")
	}
}

func TestSetValueAtPath_NonStringTypeMatch(t *testing.T) {
	var m Manifest

	if err := setValueAtPath(&m, "autonomous-mode.prerequisites-met", true); err != nil {
		t.Fatalf("unexpected error for bool field: %v", err)
	}
	if !m.AutonomousMode.PrerequisitesMet {
		t.Error("expected prerequisites-met to be set to true")
	}
}

func TestManualTracker_PinSetValueError(t *testing.T) {
	mt, r, _, dir := newTracker(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	initial := makeFreshManifest()
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}

	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: 42,
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error for type-incompatible pin value, got nil")
	}
}

func TestManualTracker_AtomicWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses filesystem permissions; skip")
	}
	mt, r, _, dir := newTracker(t)

	readonlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(readonlyDir, "system-state.toml")
	initial := makeFreshManifest()
	if err := r.RegenerateAndWrite(context.Background(), initial, manifestPath); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(readonlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readonlyDir, 0o755) })
	err := mt.Pin(context.Background(), manifestPath, PinRequest{
		Path: "zen-swarm.substrate_min_version", NewValue: "0.7.1",
		Reason: "x", OperatorID: "testuser", Timestamp: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error writing to read-only dir, got nil")
	}
}
