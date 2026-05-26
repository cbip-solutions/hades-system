package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateListEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cmd := newStateListCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute empty list: %v", err)
	}
	if !strings.Contains(out.String(), "no state entries") {
		t.Errorf("output = %q, want substring 'no state entries'", out.String())
	}
}

func TestStateListPopulatesEntries(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := filepath.Join(stateRoot, "zen-swarm", "doctor-backups", "20260513T120000Z")
	_ = os.MkdirAll(p, 0o700)
	_ = os.WriteFile(filepath.Join(p, "f"), []byte("data"), 0o644)
	cmd := newStateListCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "doctor-backups") {
		t.Errorf("output missing 'doctor-backups': %q", out.String())
	}
	if !strings.Contains(out.String(), "20260513T120000Z") {
		t.Errorf("output missing ID '20260513T120000Z': %q", out.String())
	}
}

func TestStateListJSONFlag(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := filepath.Join(stateRoot, "zen-swarm", "doctor-backups", "20260513T120000Z")
	_ = os.MkdirAll(p, 0o700)
	cmd := newStateListCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --json: %v", err)
	}
	var parsed struct {
		SchemaVersion string `json:"schemaVersion"`
		Entries       []any  `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, out.String())
	}
	if parsed.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want 1.0", parsed.SchemaVersion)
	}
	if len(parsed.Entries) != 1 {
		t.Errorf("Entries = %d, want 1", len(parsed.Entries))
	}
}

func TestStateCleanupDryRunReportsCount(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	p := filepath.Join(stateRoot, "zen-swarm", "doctor-backups", "20260301T120000Z")
	_ = os.MkdirAll(p, 0o700)
	old := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(p, old, old)

	cmd := newStateCleanupCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--dry-run"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "Would expire 1") {
		t.Errorf("output = %q, want 'Would expire 1'", out.String())
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Errorf("--dry-run deleted; want preserved")
	}
}

func TestStateCleanupRealRunDeletesAndReports(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := filepath.Join(stateRoot, "zen-swarm", "doctor-backups", "20260301T120000Z")
	_ = os.MkdirAll(p, 0o700)
	old := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(p, old, old)

	cmd := newStateCleanupCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "Expired 1") {
		t.Errorf("output = %q, want 'Expired 1'", out.String())
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected deletion; path still exists")
	}
}

func TestStateCleanupKeepIDExcepts(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	keepID := "20260301T120000Z"
	p := filepath.Join(stateRoot, "zen-swarm", "doctor-backups", keepID)
	_ = os.MkdirAll(p, 0o700)
	old := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(p, old, old)

	cmd := newStateCleanupCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--keep", keepID})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --keep: %v", err)
	}
	if !strings.Contains(out.String(), "Expired 0") {
		t.Errorf("output = %q, want 'Expired 0' (--keep excepts)", out.String())
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Errorf("--keep ID was deleted; want preserved")
	}
}

func TestResolveXDGPathsFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	stateDir, cacheDir := resolveXDGPaths()
	if !strings.Contains(stateDir, ".local/state/zen-swarm") {
		t.Errorf("stateDir = %q, want substring '.local/state/zen-swarm'", stateDir)
	}
	if !strings.Contains(cacheDir, ".cache/zen-swarm") {
		t.Errorf("cacheDir = %q, want substring '.cache/zen-swarm'", cacheDir)
	}
}

func TestFormatBytesUnits(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{500, "500 B"},
		{1500, "1.5 KB"},
		{1500 * 1024, "1.5 MB"},
		{1500 * 1024 * 1024, "1.5 GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFormatDurationUnits(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{48 * time.Hour, "2d"},
	}
	for _, tc := range tests {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
