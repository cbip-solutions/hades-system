package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
)

func TestBackupTargetMkdirFailsUnderUnwritableState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}

	roParent := t.TempDir()
	if err := os.Chmod(roParent, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(roParent, 0o700) }()
	b := backup.NewBackuper(backup.Config{StateDir: filepath.Join(roParent, "nested")})
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	_, err := b.BackupTarget(context.Background(), "check", sourceDir)
	if err == nil {
		t.Errorf("BackupTarget on read-only state parent: err=nil, want non-nil")
	}
}

func TestBackupTargetWalkFailsOnMissingSource(t *testing.T) {
	b := backup.NewBackuper(backup.Config{StateDir: t.TempDir()})
	_, err := b.BackupTarget(context.Background(), "check", "/does/not/exist/source")
	if err == nil {
		t.Errorf("BackupTarget missing source: err=nil, want non-nil")
	}
}

func TestBackupTargetSkipsSymlinks(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	real := filepath.Join(sourceDir, "real.txt")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(sourceDir, "alias.txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	m, err := b.BackupTarget(context.Background(), "symtest", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	for _, f := range m.Files {
		if f == "alias.txt" {
			t.Errorf("symlink alias.txt included in tarball; want skipped")
		}
	}
}

func TestLoadManifestByIDSkipsCorruptManifest(t *testing.T) {
	stateDir := t.TempDir()
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	m, err := b.BackupTarget(context.Background(), "valid", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	corruptDir := filepath.Join(filepath.Dir(filepath.Dir(m.Path)), "corrupt")
	_ = os.MkdirAll(corruptDir, 0o700)
	_ = os.WriteFile(filepath.Join(corruptDir, "manifest.json"), []byte("{not json"), 0o600)

	loaded, err := b.LoadManifestByID(context.Background(), m.BackupID)
	if err != nil {
		t.Fatalf("LoadManifestByID: %v", err)
	}
	if loaded.CheckName != "valid" {
		t.Errorf("loaded.CheckName = %q, want valid", loaded.CheckName)
	}
}

func TestRestoreFromManifestMissingTarball(t *testing.T) {
	b := backup.NewBackuper(backup.Config{StateDir: t.TempDir()})
	bogus := backup.Manifest{
		BackupID:    "fake",
		CheckName:   "fake",
		SourcePath:  t.TempDir(),
		TarballPath: "/does/not/exist.tar.gz",
	}
	err := b.RestoreFromManifest(context.Background(), bogus, backup.RestoreOptions{})
	if err == nil {
		t.Errorf("RestoreFromManifest missing tarball: err=nil, want non-nil")
	}
}

func TestRestoreFromManifestCorruptGzip(t *testing.T) {
	stateDir := t.TempDir()
	bogusTar := filepath.Join(stateDir, "not.tar.gz")
	if err := os.WriteFile(bogusTar, []byte("not a gzip stream"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest := backup.Manifest{
		TarballPath: bogusTar,
		SourcePath:  t.TempDir(),
	}
	err := b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{})
	if err == nil {
		t.Errorf("RestoreFromManifest corrupt gzip: err=nil, want non-nil")
	}
}

// TestRestoreFromManifestRejectsPathTraversal constructs a malicious
// tarball with a ../../etc/passwd entry; restore MUST reject.
func TestRestoreFromManifestRejectsPathTraversal(t *testing.T) {
	stateDir := t.TempDir()
	tarPath := filepath.Join(stateDir, "evil.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     "../../etc/escape.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     5,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = io.WriteString(tw, "evil!")
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest := backup.Manifest{
		TarballPath: tarPath,
		SourcePath:  t.TempDir(),
	}
	err = b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{})
	if err == nil {
		t.Errorf("RestoreFromManifest path-traversal: err=nil, want non-nil")
	}
}

func TestRestoreFromManifestRejectsAbsolutePath(t *testing.T) {
	stateDir := t.TempDir()
	tarPath := filepath.Join(stateDir, "abs.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     "/etc/abs.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     2,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = io.WriteString(tw, "no")
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	manifest := backup.Manifest{
		TarballPath: tarPath,
		SourcePath:  t.TempDir(),
	}
	err = b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{})
	if err == nil {
		t.Errorf("RestoreFromManifest absolute-path: err=nil, want non-nil")
	}
}

func TestRestoreFromManifestSkipsSpecialFiles(t *testing.T) {
	stateDir := t.TempDir()
	tarPath := filepath.Join(stateDir, "special.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:     "link",
		Typeflag: tar.TypeSymlink,
		Linkname: "/etc/passwd",
		Mode:     0o777,
	}
	_ = tw.WriteHeader(hdr)

	reg := &tar.Header{
		Name:     "ok.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     2,
	}
	_ = tw.WriteHeader(reg)
	_, _ = io.WriteString(tw, "ok")
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	target := t.TempDir()
	manifest := backup.Manifest{
		TarballPath: tarPath,
		SourcePath:  target,
	}
	if err := b.RestoreFromManifest(context.Background(), manifest, backup.RestoreOptions{}); err != nil {
		t.Errorf("RestoreFromManifest with skip-typed entry: err=%v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(target, "link")); !os.IsNotExist(err) {
		t.Errorf("symlink entry was extracted; want skipped")
	}
	body, err := os.ReadFile(filepath.Join(target, "ok.txt"))
	if err != nil {
		t.Errorf("regular entry skipped: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("regular entry content = %q, want ok", string(body))
	}
}

func TestRestoreFromManifestDirEntry(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceDir, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_ = os.WriteFile(filepath.Join(sourceDir, "nested", "f.txt"), []byte("ok"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	m, err := b.BackupTarget(context.Background(), "dir-test", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}
	restoreDir := t.TempDir()
	if err := b.RestoreFromManifest(context.Background(), m, backup.RestoreOptions{
		TargetOverride: restoreDir,
	}); err != nil {
		t.Fatalf("RestoreFromManifest: %v", err)
	}
	info, err := os.Stat(filepath.Join(restoreDir, "nested"))
	if err != nil {
		t.Fatalf("Stat nested: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("nested is not a dir; got mode %v", info.Mode())
	}
}

func TestStateDirAccessor(t *testing.T) {
	custom := "/custom/state"
	b := backup.NewBackuper(backup.Config{StateDir: custom})
	if b.StateDir() != custom {
		t.Errorf("StateDir = %q, want %q", b.StateDir(), custom)
	}
}

func TestRestoreFromManifestSourcePathFallback(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "fb.txt"), []byte("fb"), 0o644)
	b := backup.NewBackuper(backup.Config{StateDir: stateDir})
	m, err := b.BackupTarget(context.Background(), "fallback", sourceDir)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := b.RestoreFromManifest(context.Background(), m, backup.RestoreOptions{}); err != nil {
		t.Errorf("RestoreFromManifest fallback to SourcePath: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(sourceDir, "fb.txt"))
	if string(body) != "fb" {
		t.Errorf("post-restore content = %q, want fb", string(body))
	}
}
