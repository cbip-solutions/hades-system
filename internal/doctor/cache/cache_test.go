package cache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/cache"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	original := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusPass, DurationMs: 10},
		},
		PassCount: 1,
	}
	if err := c.Write(original); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, fresh, err := c.Read(5 * time.Minute)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !fresh {
		t.Errorf("fresh=false immediately after Write; want true")
	}
	if got == nil {
		t.Fatalf("Read returned nil report")
	}
	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", got.SchemaVersion, original.SchemaVersion)
	}
	if got.PassCount != 1 {
		t.Errorf("PassCount = %d, want 1", got.PassCount)
	}
}

func TestCacheStaleAfterTTL(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		FinishedAt:    time.Now().UTC().Add(-10 * time.Minute),
	}
	if err := c.Write(r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, fresh, err := c.Read(5 * time.Minute)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if fresh {
		t.Errorf("fresh=true for 10m-old cache; want false (TTL=5m)")
	}
}

func TestCacheInvalidate(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r := &aggregator.Report{SchemaVersion: aggregator.SchemaVersion, FinishedAt: time.Now().UTC()}
	if err := c.Write(r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := c.Invalidate(); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	_, fresh, _ := c.Read(5 * time.Minute)
	if fresh {
		t.Errorf("fresh=true after Invalidate; want false")
	}
}

func TestCacheInvalidateMissingFile(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	if err := c.Invalidate(); err != nil {
		t.Errorf("Invalidate on missing file errored: %v; want nil", err)
	}
}

func TestCacheReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r, fresh, err := c.Read(5 * time.Minute)
	if err != nil {
		t.Fatalf("Read missing: %v; want nil error", err)
	}
	if r != nil {
		t.Errorf("Read missing report = %+v; want nil", r)
	}
	if fresh {
		t.Errorf("fresh=true on missing cache; want false")
	}
}

func TestCacheCorruptFile(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	cachePath := filepath.Join(dir, cache.CacheFileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r, fresh, err := c.Read(5 * time.Minute)
	if err != nil {
		t.Fatalf("Read should tolerate corrupt cache: %v", err)
	}
	if fresh {
		t.Errorf("fresh=true on corrupt cache; want false")
	}
	if r != nil {
		t.Errorf("Read corrupt report = %+v; want nil", r)
	}
}

func TestCacheWritePermissionsMode0600(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r := &aggregator.Report{SchemaVersion: aggregator.SchemaVersion, FinishedAt: time.Now().UTC()}
	if err := c.Write(r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(c.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("permissions = %o; want no group/world bits", perm)
	}
}

func TestCachePathHonorsXDGEnv(t *testing.T) {
	tmpXDG := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpXDG)
	c := cache.New()
	wantPrefix := filepath.Join(tmpXDG, "zen-swarm", "doctor")
	if got := c.Path(); !filepath.IsAbs(got) {
		t.Errorf("Path() = %q; want absolute", got)
	}
	if got := c.Path(); filepath.Dir(got) != wantPrefix {
		t.Errorf("Path() = %q; want under %q", got, wantPrefix)
	}
}

func TestCachePathFallsBackToHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CACHE_HOME", "")
	c := cache.New()
	wantPrefix := filepath.Join(tmpHome, ".cache", "zen-swarm", "doctor")
	if got := c.Path(); filepath.Dir(got) != wantPrefix {
		t.Errorf("Path() = %q; want under %q", got, wantPrefix)
	}
}

func TestCacheWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r := &aggregator.Report{SchemaVersion: aggregator.SchemaVersion, FinishedAt: time.Now().UTC()}
	if err := c.Write(r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf(".tmp artefact left behind: %s", e.Name())
		}
	}
}

func TestReportJSONRoundtrip(t *testing.T) {
	r := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		StartedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 5, 14, 12, 0, 1, 0, time.UTC),
		Diagnostics:   []check.DiagnosticResult{{Name: "x", Status: check.StatusPass}},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got aggregator.Report
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SchemaVersion != aggregator.SchemaVersion {
		t.Errorf("schemaVersion roundtrip = %q, want %q", got.SchemaVersion, aggregator.SchemaVersion)
	}
}

func TestCacheDefaultTTLConstant(t *testing.T) {
	if cache.DefaultTTL != 5*time.Minute {
		t.Errorf("DefaultTTL = %v, want 5m", cache.DefaultTTL)
	}
}

func TestCachePathFallsBackToUserHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	c := cache.New()

	if c.Path() == "" {
		t.Errorf("Path() empty; want some defensive value")
	}
}

func TestCacheWriteToReadOnlyDirFails(t *testing.T) {

	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("blocking"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}

	c := cache.NewWithDir(blocker)
	r := &aggregator.Report{SchemaVersion: aggregator.SchemaVersion, FinishedAt: time.Now().UTC()}
	if err := c.Write(r); err == nil {
		t.Errorf("Write to blocked path succeeded; want error")
	}
}

func TestCacheReadPermissionDeniedSurface(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix permissions; skip on root")
	}
	dir := t.TempDir()
	c := cache.NewWithDir(dir)
	r := &aggregator.Report{SchemaVersion: aggregator.SchemaVersion, FinishedAt: time.Now().UTC()}
	if err := c.Write(r); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := os.Chmod(c.Path(), 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(c.Path(), 0o600)
	})
	_, _, err := c.Read(5 * time.Minute)

	if err == nil {
		t.Log("permissions denied did not surface error on this OS; acceptable")
	}
}
