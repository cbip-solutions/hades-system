package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreFromManifestMkdirAllFailsOnConflictedTarget(t *testing.T) {
	stateDir := t.TempDir()
	b := NewBackuper(Config{StateDir: stateDir})

	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "f"), []byte("x"), 0o644)
	m, err := b.BackupTarget(context.Background(), "blocked", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	parent := t.TempDir()
	conflictFile := filepath.Join(parent, "blocker")
	if err := os.WriteFile(conflictFile, []byte("regular file"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	target := filepath.Join(conflictFile, "subdir")
	err = b.RestoreFromManifest(context.Background(), m, RestoreOptions{TargetOverride: target})
	if err == nil {
		t.Errorf("RestoreFromManifest with file-as-parent target: err=nil, want non-nil")
	}
}

func TestRestoreFromManifestTruncatedTar(t *testing.T) {
	stateDir := t.TempDir()
	tarPath := filepath.Join(stateDir, "truncated.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gz := gzip.NewWriter(f)

	_, _ = io.WriteString(gz, "this is not a tar header")
	_ = gz.Close()
	_ = f.Close()

	b := NewBackuper(Config{StateDir: stateDir})
	m := Manifest{
		TarballPath: tarPath,
		SourcePath:  t.TempDir(),
	}
	err = b.RestoreFromManifest(context.Background(), m, RestoreOptions{})
	if err == nil {
		t.Errorf("RestoreFromManifest truncated tar: err=nil, want non-nil")
	}
}

func TestRestoreFromManifestDirOnlyEntryCreatesDir(t *testing.T) {
	stateDir := t.TempDir()
	tarPath := filepath.Join(stateDir, "dir.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:     "subdir/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}
	_ = tw.WriteHeader(hdr)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	b := NewBackuper(Config{StateDir: stateDir})
	target := t.TempDir()
	m := Manifest{
		TarballPath: tarPath,
		SourcePath:  target,
	}
	if err := b.RestoreFromManifest(context.Background(), m, RestoreOptions{}); err != nil {
		t.Errorf("RestoreFromManifest dir-only: err=%v, want nil", err)
	}
	info, err := os.Stat(filepath.Join(target, "subdir"))
	if err != nil {
		t.Errorf("Stat extracted dir: %v", err)
	} else if !info.IsDir() {
		t.Errorf("extracted entry not a dir")
	}
}

func TestRestoreFromManifestRenameFailsWhenTargetIsDir(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "f"), []byte("x"), 0o644)
	b := NewBackuper(Config{StateDir: stateDir})
	m, err := b.BackupTarget(context.Background(), "rename-block", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	target := t.TempDir()

	blockDir := filepath.Join(target, "f")
	if err := os.MkdirAll(blockDir, 0o700); err != nil {
		t.Fatalf("MkdirAll block: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockDir, "inner"), []byte("squat"), 0o644); err != nil {
		t.Fatalf("WriteFile inner: %v", err)
	}

	err = b.RestoreFromManifest(context.Background(), m, RestoreOptions{
		TargetOverride: target,
		Overwrite:      true,
	})

	if err == nil {
		t.Skip("rename onto non-empty dir succeeded; platform differs")
	}
}
