package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBackuperResolvesHomeFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", t.TempDir())
	b := NewBackuper(Config{})
	if b.stateDir == "" {
		t.Errorf("stateDir empty after home-fallback resolution")
	}
	if !filepath.IsAbs(b.stateDir) {
		t.Errorf("stateDir %q not absolute", b.stateDir)
	}
}

func TestIsPathWithinIdenticalReturnsTrue(t *testing.T) {
	if !isPathWithin("/a/b/c", "/a/b/c") {
		t.Errorf("isPathWithin equal paths: false, want true")
	}
}

func TestIsPathWithinAbsoluteSibling(t *testing.T) {
	if isPathWithin("/etc/passwd", "/home/operator") {
		t.Errorf("isPathWithin /etc/passwd under /home/operator: true, want false")
	}
}

func TestIsPathWithinRejectsBackrefAfterClean(t *testing.T) {
	if isPathWithin("/a/../b", "/a") {
		t.Errorf("isPathWithin /a/../b under /a: true, want false")
	}
}

func TestLoadManifestByIDSkipsNonDir(t *testing.T) {
	stateDir := t.TempDir()
	b := NewBackuper(Config{StateDir: stateDir})
	id := "20260101T000000Z"
	idDir := filepath.Join(b.backupRoot(), id)
	if err := os.MkdirAll(idDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(idDir, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := b.LoadManifestByID(nil, id)
	if err != ErrNotFound {
		t.Errorf("LoadManifestByID err=%v, want ErrNotFound", err)
	}
}

func TestLoadManifestByIDListErrorBubblesUp(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	stateDir := t.TempDir()
	b := NewBackuper(Config{StateDir: stateDir})
	id := "20260101T000000Z"

	if err := os.MkdirAll(b.backupRoot(), 0o700); err != nil {
		t.Fatalf("MkdirAll backupRoot: %v", err)
	}
	idDir := filepath.Join(b.backupRoot(), id)
	if err := os.Mkdir(idDir, 0o000); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(idDir, 0o700) }()
	_, err := b.LoadManifestByID(nil, id)
	if err == nil {
		t.Errorf("LoadManifestByID under unreadable dir: err=nil, want non-nil")
	}
}

func TestWriteTarGzExistingTempTarRejected(t *testing.T) {
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "x"), []byte("y"), 0o644)
	tarPath := filepath.Join(t.TempDir(), "content.tar.gz")

	if err := os.WriteFile(tarPath+".tmp", []byte("squat"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := writeTarGz(sourceDir, tarPath); err == nil {
		t.Errorf("writeTarGz with existing .tmp: err=nil, want non-nil")
	}
}
