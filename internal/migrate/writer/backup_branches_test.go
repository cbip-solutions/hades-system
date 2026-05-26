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

func TestBackup_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	real := filepath.Join(pluginRoot, "real.md")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(pluginRoot, "link.md")); err != nil {
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
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	hasReal := false
	hasLink := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch hdr.Name {
		case "zen-swarm/real.md":
			hasReal = true
		case "zen-swarm/link.md":
			hasLink = true
		}
	}
	if !hasReal {
		t.Errorf("real.md missing from backup")
	}
	if hasLink {
		t.Errorf("symlink link.md should be skipped, but appears in backup")
	}
}

func TestBackup_TouchedRootsDoubleCountAvoided(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin"),
		HermesConfigPath: filepath.Join(tmp, "shared", "config.yaml"),
		ZenConfigRoot:    filepath.Join(tmp, "shared"),
	})
	roots := w.touchedRoots()

	if len(roots) != 2 {
		t.Errorf("touchedRoots dedup: got %d, want 2 (HermesConfigPath dir overlaps ZenConfigRoot)", len(roots))
	}
}
