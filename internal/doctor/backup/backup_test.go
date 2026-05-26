package backup_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
)

func TestBackupRoundtrip(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(sourceDir, "file1.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "subdir", "file2.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})

	manifest, err := b.BackupTarget(context.Background(), "test-check", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	if _, sterr := os.Stat(manifest.Path); sterr != nil {
		t.Fatalf("manifest path stat: %v", sterr)
	}

	body, err := os.ReadFile(manifest.Path)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	var got backup.Manifest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.CheckName != "test-check" {
		t.Errorf("CheckName = %q, want test-check", got.CheckName)
	}
	if got.SourcePath != sourceDir {
		t.Errorf("SourcePath = %q, want %s", got.SourcePath, sourceDir)
	}
	if got.BackupID == "" {
		t.Errorf("BackupID empty; want non-empty ISO8601")
	}
	if got.TarballPath == "" {
		t.Errorf("TarballPath empty")
	}
	if got.RestoreCommand == "" {
		t.Errorf("RestoreCommand empty; want paste-ready operator command")
	}
	if len(got.Files) != 2 {
		t.Errorf("Files = %v, want 2 entries (file1.txt + subdir/file2.txt)", got.Files)
	}

	if _, sterr := os.Stat(got.TarballPath); sterr != nil {
		t.Fatalf("tarball stat: %v", sterr)
	}

	restoreDir := t.TempDir()
	if err := b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{
		TargetOverride: restoreDir,
		Overwrite:      false,
	}); err != nil {
		t.Fatalf("RestoreFromManifest: %v", err)
	}

	body1, err := os.ReadFile(filepath.Join(restoreDir, "file1.txt"))
	if err != nil {
		t.Fatalf("ReadFile restored file1: %v", err)
	}
	if string(body1) != "hello world" {
		t.Errorf("file1.txt content = %q, want hello world", string(body1))
	}
	body2, err := os.ReadFile(filepath.Join(restoreDir, "subdir", "file2.txt"))
	if err != nil {
		t.Fatalf("ReadFile restored file2: %v", err)
	}
	if string(body2) != "nested" {
		t.Errorf("file2.txt content = %q, want nested", string(body2))
	}
}

func TestBackupManifestMode0600(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest, err := b.BackupTarget(context.Background(), "test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	info, err := os.Stat(manifest.Path)
	if err != nil {
		t.Fatalf("Stat manifest: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("manifest mode = %o, want 0600", mode)
	}
}

func TestRestoreHaltsOnConflict(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "f"), []byte("a"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest, err := b.BackupTarget(context.Background(), "test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	restoreDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(restoreDir, "f"), []byte("existing"), 0o644)

	err = b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{
		TargetOverride: restoreDir,
		Overwrite:      false,
	})
	if err == nil {
		t.Fatalf("RestoreFromManifest succeeded on conflict; want ErrConflict")
	}
	if !backup.IsConflictError(err) {
		t.Errorf("err = %v, want IsConflictError", err)
	}
}

func TestRestoreOverwriteForcesReplacement(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "f"), []byte("backup-content"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest, err := b.BackupTarget(context.Background(), "test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	restoreDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(restoreDir, "f"), []byte("existing"), 0o644)

	if err := b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{
		TargetOverride: restoreDir,
		Overwrite:      true,
	}); err != nil {
		t.Fatalf("RestoreFromManifest --overwrite: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(restoreDir, "f"))
	if string(body) != "backup-content" {
		t.Errorf("post-overwrite f = %q, want backup-content", string(body))
	}
}

func TestLoadManifestByID(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	written, err := b.BackupTarget(context.Background(), "test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	loaded, err := b.LoadManifestByID(context.Background(), written.BackupID)
	if err != nil {
		t.Fatalf("LoadManifestByID: %v", err)
	}
	if loaded.BackupID != written.BackupID {
		t.Errorf("BackupID = %q, want %q", loaded.BackupID, written.BackupID)
	}
	if loaded.Path == "" {
		t.Errorf("loaded.Path empty; want non-empty manifest path")
	}
}

func TestLoadManifestByIDNotFound(t *testing.T) {
	stateDir := t.TempDir()
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	_, err := b.LoadManifestByID(context.Background(), "does-not-exist")
	if !errors.Is(err, backup.ErrNotFound) {
		t.Errorf("LoadManifestByID missing ID: err=%v, want ErrNotFound", err)
	}
}

func TestRemoveAfterBackup(t *testing.T) {
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: t.TempDir()})
	if err := b.RemoveAfterBackup(context.Background(), sourceDir); err != nil {
		t.Errorf("RemoveAfterBackup: %v", err)
	}
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Errorf("post-RemoveAfterBackup: source still exists")
	}
}

func TestBackupResolvesXDGStateHomeWhenEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	b := backup.NewBackuper(backup.Config{})
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	m, err := b.BackupTarget(context.Background(), "xdg-test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	if !filepath.IsAbs(m.Path) {
		t.Errorf("manifest path %q not absolute", m.Path)
	}
}

func TestRestorePreservesFileMode(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	exePath := filepath.Join(sourceDir, "exe.sh")
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\necho hi"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	m, err := b.BackupTarget(context.Background(), "mode-test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	restoreDir := t.TempDir()
	if err := b.RestoreFromManifest(context.Background(), m, backup.RestoreOptions{
		TargetOverride: restoreDir,
	}); err != nil {
		t.Fatalf("RestoreFromManifest: %v", err)
	}
	info, err := os.Stat(filepath.Join(restoreDir, "exe.sh"))
	if err != nil {
		t.Fatalf("Stat restored exe: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("restored mode lacks executable bit; got %o", info.Mode().Perm())
	}
}
