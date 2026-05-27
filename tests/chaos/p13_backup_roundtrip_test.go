// go:build chaos

// Package chaos — p13_backup_roundtrip_test.go (
// IMPORTANT 7 missing-tests completion).
//
// Chaos: backup → restore roundtrip MUST preserve file contents bit-for-
// bit even under adversarial input (sparse files, special characters in
// names, nested directories). Per spec §2.5 + §2.12 + §5.1 +
// invariant backup-before-modify substrate.
//
// Build tag `chaos` excludes from default CI.
package chaos

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
)

func TestChaos_BackupRoundtrip_NestedDirs(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	sourceDir := t.TempDir()

	nested := filepath.Join(sourceDir, "sub", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string][]byte{
		filepath.Join(sourceDir, "root.txt"):     []byte("root content"),
		filepath.Join(sourceDir, "sub", "a.txt"): []byte("level-1 content"),
		filepath.Join(nested, "b.txt"):           []byte("level-2 content with special chars: \x00\x01\x02"),
	}
	for path, content := range files {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest, err := b.BackupTarget(context.Background(), "test.roundtrip", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	if manifest.BackupID == "" {
		t.Errorf("BackupID empty; backup did not produce a usable manifest")
	}
	if manifest.TarballPath == "" {
		t.Errorf("TarballPath empty")
	}

	stat, err := os.Stat(manifest.TarballPath)
	if err != nil {
		t.Fatalf("tarball stat: %v", err)
	}
	if stat.Size() == 0 {
		t.Errorf("tarball size = 0; backup did not write content")
	}

	restoreTarget := t.TempDir()
	if err := b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{TargetOverride: restoreTarget}); err != nil {
		t.Fatalf("RestoreFromManifest: %v", err)
	}

	for path, want := range files {
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			t.Fatalf("Rel: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(restoreTarget, rel))
		if err != nil {
			t.Errorf("read restored %s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("content mismatch for %s: got %q want %q", rel, got, want)
		}
	}
}

func TestChaos_BackupRoundtrip_EmptyDir(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	sourceDir := t.TempDir()

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest, err := b.BackupTarget(context.Background(), "test.empty", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget on empty dir: %v", err)
	}
	if manifest.BackupID == "" {
		t.Errorf("BackupID empty on empty-dir backup")
	}

	if len(manifest.Files) != 0 {
		t.Errorf("Files = %v; want empty for empty source", manifest.Files)
	}
}
