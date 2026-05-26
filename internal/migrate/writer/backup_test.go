package writer

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestBackup_TarGzCreated(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "existing.md"), []byte("operator"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       filepath.Join(tmp, "backups"),
		ForceOverwrite:   true,
	})
	if err := w.backupIfNeeded(&mapping.Plan{}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(tmp, "backups"))
	if len(entries) == 0 {
		t.Fatal("no backup created")
	}

	f, err := os.Open(filepath.Join(tmp, "backups", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "zen-swarm/existing.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("existing.md not in tarball")
	}
}

func TestBackup_SkippedWhenBackupRootUnset(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		ForceOverwrite:   true,
	})
	if err := w.backupIfNeeded(&mapping.Plan{}); err != nil {
		t.Errorf("expected nil error when BackupRoot unset: %v", err)
	}
}

func TestBackup_SkippedWhenNoForceAndEmptyTargets(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin", "zen-swarm"),
		BackupRoot:       filepath.Join(tmp, "backups"),
		ForceOverwrite:   false,
	})
	if err := w.backupIfNeeded(&mapping.Plan{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(tmp, "backups"))
	if err == nil && len(entries) > 0 {
		t.Errorf("unexpected backup created: %v", entries)
	}
}

func TestBackup_AnyTargetExists_TruePath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")

	skillDir := filepath.Join(pluginRoot, "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       filepath.Join(tmp, "backups"),
		ForceOverwrite:   false,
	})
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/alpha/SKILL.md", BodyBytes: []byte("new")},
	}}
	if !w.anyTargetExists(plan) {
		t.Errorf("expected anyTargetExists to return true for pre-populated target")
	}
}

func TestBackup_TouchedRootsContainsAllConfigured(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin", "zen-swarm"),
		HermesConfigPath: filepath.Join(tmp, "hermes", "config.yaml"),
		ZenConfigRoot:    filepath.Join(tmp, "zen-config"),
	})
	roots := w.touchedRoots()
	if len(roots) < 3 {
		t.Errorf("touchedRoots: got %d, want ≥3 (one per configured root)", len(roots))
	}
}
