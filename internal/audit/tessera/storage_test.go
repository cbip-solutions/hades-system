package tessera

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tessera "github.com/transparency-dev/tessera"
	posix "github.com/transparency-dev/tessera/storage/posix"
)

func withPosixDriverFactory(t *testing.T, fake func(ctx context.Context, cfg posix.Config) (tessera.Driver, error)) {
	t.Helper()
	orig := posixDriverFactory
	posixDriverFactory = fake
	t.Cleanup(func() { posixDriverFactory = orig })
}

func TestPosixStorageOpenCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(tsDir, sub), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	if err := os.Chmod(tsDir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	s, err := openPosixStorage(context.Background(), tsDir)
	if err != nil {
		t.Fatalf("openPosixStorage: %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.Dir() != tsDir {
		t.Errorf("Dir = %q, want %q", s.Dir(), tsDir)
	}
	if s.Driver() == nil {
		t.Error("Driver() returned nil; want non-nil *posix.Storage")
	}
}

func TestPosixStorageOpenRejectsMissingDir(t *testing.T) {
	_, err := openPosixStorage(context.Background(), "/nonexistent/zen-swarm/path")
	if err == nil {
		t.Fatal("openPosixStorage on missing dir: want error, got nil")
	}
}

func TestPosixStorageOpenRejectsNonDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(path, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := openPosixStorage(context.Background(), path)
	if err == nil {
		t.Fatal("want error on non-directory path, got nil")
	}
}

func TestPosixStorageRejectsLooseDirPerms(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(tsDir, sub), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	if err := os.Chmod(tsDir, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	_, err := openPosixStorage(context.Background(), tsDir)
	if err == nil {
		t.Fatal("want refusal on 0755 perms, got nil")
	}
}

func TestPosixStorageDirReturnsConfiguredDir(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(tsDir, sub), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	if err := os.Chmod(tsDir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	s, err := openPosixStorage(context.Background(), tsDir)
	if err != nil {
		t.Fatalf("openPosixStorage: %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.Dir() != tsDir {
		t.Errorf("Dir = %q, want %q", s.Dir(), tsDir)
	}
}

func TestPosixStorageCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(tsDir, sub), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	if err := os.Chmod(tsDir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	s, err := openPosixStorage(context.Background(), tsDir)
	if err != nil {
		t.Fatalf("openPosixStorage: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAdapterWiresPosixStorageOnConstruction(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	if a.storage == nil {
		t.Fatal("Adapter.storage is nil; expected wired posixStorage after NewProjectAdapter")
	}
	if a.storage.Dir() != a.Dir() {
		t.Errorf("storage.Dir = %q, adapter.Dir = %q; mismatch", a.storage.Dir(), a.Dir())
	}
}

func TestPosixStorageOpenSurfacesDriverFactoryError(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	if err := os.MkdirAll(tsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(tsDir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	sentinel := errors.New("simulated posix.New failure")
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return nil, sentinel
	})
	_, err := openPosixStorage(context.Background(), tsDir)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain missing sentinel: %v", err)
	}
}

func TestPosixStorageOpenRejectsWrongDriverType(t *testing.T) {
	dir := t.TempDir()
	tsDir := filepath.Join(dir, "audit", "tessera")
	if err := os.MkdirAll(tsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(tsDir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	type wrongDriver struct{}
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return &wrongDriver{}, nil
	})
	_, err := openPosixStorage(context.Background(), tsDir)
	if err == nil {
		t.Fatal("want error on wrong driver type, got nil")
	}
}
