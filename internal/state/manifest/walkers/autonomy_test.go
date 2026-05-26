package walkers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutonomyWalker_ReadsLastCheckResult(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, "autonomy_check.json")
	body := map[string]any{
		"prerequisites_met": true,
		"last_check_at":     "2026-05-06T08:00:00Z",
	}
	b, _ := json.Marshal(body)
	if err := os.WriteFile(stamp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewAutonomyWalker(stamp)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !r.PrerequisitesMet {
		t.Error("PrerequisitesMet: got false, want true")
	}
	want := time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC)
	if !r.LastCheck.Equal(want) {
		t.Errorf("LastCheck = %v, want %v", r.LastCheck, want)
	}
}

func TestAutonomyWalker_PrerequisitesNotMet(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, "autonomy_check.json")
	body := map[string]any{
		"prerequisites_met": false,
		"last_check_at":     "2026-05-07T10:00:00Z",
	}
	b, _ := json.Marshal(body)
	if err := os.WriteFile(stamp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewAutonomyWalker(stamp)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.PrerequisitesMet {
		t.Error("PrerequisitesMet: got true, want false")
	}
	want := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	if !r.LastCheck.Equal(want) {
		t.Errorf("LastCheck = %v, want %v", r.LastCheck, want)
	}
}

func TestAutonomyWalker_MissingStamp_ReportsMissing(t *testing.T) {
	w := NewAutonomyWalker("/nonexistent/autonomy_check.json")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "autonomy-stamp") {
		t.Errorf("MissingSources: got %v", r.MissingSources)
	}
	if r.PrerequisitesMet {
		t.Error("PrerequisitesMet: got true on missing stamp, want false")
	}
}

func TestAutonomyWalker_MalformedJSON_ReportsMissing(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, "autonomy_check.json")
	if err := os.WriteFile(stamp, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewAutonomyWalker(stamp)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "autonomy-stamp") {
		t.Errorf("MissingSources on malformed json: got %v", r.MissingSources)
	}
}

func TestAutonomyWalker_MissingLastCheckAt_ZeroTime(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, "autonomy_check.json")

	if err := os.WriteFile(stamp, []byte(`{"prerequisites_met":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewAutonomyWalker(stamp)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.LastCheck != (time.Time{}) {
		t.Errorf("LastCheck should be zero when field absent, got %v", r.LastCheck)
	}
	if !r.PrerequisitesMet {
		t.Error("PrerequisitesMet: got false, want true")
	}
}
