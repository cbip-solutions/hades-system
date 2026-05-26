//go:build cgo
// +build cgo

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestCAS(t *testing.T) (*CAS, string) {
	t.Helper()
	root := t.TempDir()
	cas, err := NewCAS(root)
	if err != nil {
		t.Fatalf("NewCAS(%q): %v", root, err)
	}
	return cas, root
}

func TestCASWriteAndRead(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("hello, content-addressed world")
	hash, err := cas.Write(body, "txt")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if len(hash) != 64 {
		t.Fatalf("expected 64-char hash, got %d: %q", len(hash), hash)
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("hash contains non-hex char %q: %q", c, hash)
		}
	}

	got, err := cas.Read(hash, "txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, body)
	}
}

func TestCASPathLayout(t *testing.T) {
	cas, root := newTestCAS(t)

	body := []byte("layout test")
	hash, err := cas.Write(body, "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	want := filepath.Join(root, hash[:2], hash+".bin")
	if cas.Path(hash, "bin") != want {
		t.Fatalf("Path: got %q, want %q", cas.Path(hash, "bin"), want)
	}

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file at %q: %v", want, err)
	}
}

func TestCASDedup(t *testing.T) {
	cas, root := newTestCAS(t)

	body := []byte("dedup me")
	h1, err := cas.Write(body, "txt")
	if err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	h2, err := cas.Write(body, "txt")
	if err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	if h1 != h2 {
		t.Fatalf("dedup: same body produced different hashes %q vs %q", h1, h2)
	}

	prefixDir := filepath.Join(root, h1[:2])
	entries, err := os.ReadDir(prefixDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", prefixDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in prefix dir, got %d: %v", len(entries), entries)
	}
}

func TestCASIdempotentReWriteSafe(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("idempotent content")
	var first string
	for i := 0; i < 5; i++ {
		h, err := cas.Write(body, "json")
		if err != nil {
			t.Fatalf("Write iteration %d: %v", i, err)
		}
		if first == "" {
			first = h
		} else if h != first {
			t.Fatalf("iteration %d: hash changed %q → %q", i, first, h)
		}
	}
}

func TestCASReadErrBlobMissing(t *testing.T) {
	cas, _ := newTestCAS(t)

	missingHash := strings.Repeat("0", 64)
	_, err := cas.Read(missingHash, "txt")
	if !errors.Is(err, ErrBlobMissing) {
		t.Fatalf("expected ErrBlobMissing, got %v", err)
	}
}

func TestCASFilePerms0600(t *testing.T) {
	cas, _ := newTestCAS(t)

	hash, err := cas.Write([]byte("perms test"), "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(cas.Path(hash, "bin"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm: got %04o, want 0600", perm)
	}
}

func TestCASRootDirPerms0700(t *testing.T) {

	root := filepath.Join(t.TempDir(), "cas-root")
	cas, err := NewCAS(root)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("Stat root: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("root dir perm: got %04o, want 0700", perm)
	}

	hash, err := cas.Write([]byte("dir perm test"), "dat")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	prefixDir := filepath.Join(root, hash[:2])
	info, err = os.Stat(prefixDir)
	if err != nil {
		t.Fatalf("Stat prefix dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("prefix dir perm: got %04o, want 0700", perm)
	}
}

func TestCASAtomicWriteNoPartial(t *testing.T) {
	cas, root := newTestCAS(t)

	hash, err := cas.Write([]byte("atomic write"), "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	prefixDir := filepath.Join(root, hash[:2])
	entries, err := os.ReadDir(prefixDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover .tmp file found: %q", e.Name())
		}
	}
}

func TestCASReadFromReader(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("reader interface test")
	hash, err := cas.Write(body, "txt")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	rc, err := cas.Open(hash, "txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("Open round-trip: got %q, want %q", got, body)
	}
}

func TestCASRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my-cas")
	cas, err := NewCAS(root)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	if cas.Root() != root {
		t.Fatalf("Root: got %q, want %q", cas.Root(), root)
	}
}

func TestCASDelete(t *testing.T) {
	cas, _ := newTestCAS(t)

	hash, err := cas.Write([]byte("delete me"), "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := cas.Delete(hash, "bin"); err != nil {
		t.Fatalf("Delete existing: %v", err)
	}

	if _, err := os.Stat(cas.Path(hash, "bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file to be gone after Delete, stat err: %v", err)
	}

	if err := cas.Delete(hash, "bin"); err != nil {
		t.Fatalf("Delete missing (idempotent): %v", err)
	}
}

func TestCASOpenErrBlobMissing(t *testing.T) {
	cas, _ := newTestCAS(t)

	missingHash := strings.Repeat("a", 64)
	_, err := cas.Open(missingHash, "json")
	if !errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Open missing: expected ErrBlobMissing, got %v", err)
	}
}

func TestCASPathExtAutoPrefix(t *testing.T) {
	cas, root := newTestCAS(t)
	hash := strings.Repeat("b", 64)

	withDot := cas.Path(hash, ".bin")
	withoutDot := cas.Path(hash, "bin")

	expected := filepath.Join(root, hash[:2], hash+".bin")
	if withDot != expected {
		t.Fatalf("Path with dot: got %q, want %q", withDot, expected)
	}
	if withoutDot != expected {
		t.Fatalf("Path without dot: got %q, want %q", withoutDot, expected)
	}
}

func TestCASNewCASInvalidRoot(t *testing.T) {

	_, err := NewCAS("/tmp/bad\x00path")
	if err == nil {
		t.Fatal("NewCAS with null byte in path: expected error, got nil")
	}
}

func TestCASWriteDeduplicationOnExistingFile(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("fast dedup path")

	h1, err := cas.Write(body, "dat")
	if err != nil {
		t.Fatalf("Write 1: %v", err)
	}

	h2, err := cas.Write(body, "dat")
	if err != nil {
		t.Fatalf("Write 2 (dedup): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("dedup: hash mismatch %q vs %q", h1, h2)
	}
}

func TestCASReadNonErrNotExist(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission restrictions do not apply")
	}
	cas, _ := newTestCAS(t)

	body := []byte("perm restricted read")
	hash, err := cas.Write(body, "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	blobPath := cas.Path(hash, "bin")
	if err := os.Chmod(blobPath, 0o000); err != nil {
		t.Fatalf("Chmod blob 000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blobPath, 0o600) })

	_, err = cas.Read(hash, "bin")
	if err == nil {
		t.Fatal("Read from 000-perm file: expected error, got nil")
	}

	if errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Read from 000-perm file: got ErrBlobMissing, want permission error")
	}
}

func TestCASOpenNonErrNotExist(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission restrictions do not apply")
	}
	cas, _ := newTestCAS(t)

	body := []byte("perm restricted open")
	hash, err := cas.Write(body, "bin")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	blobPath := cas.Path(hash, "bin")
	if err := os.Chmod(blobPath, 0o000); err != nil {
		t.Fatalf("Chmod blob 000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blobPath, 0o600) })

	_, err = cas.Open(hash, "bin")
	if err == nil {
		t.Fatal("Open from 000-perm file: expected error, got nil")
	}
	if errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Open from 000-perm file: got ErrBlobMissing, want permission error")
	}
}

func TestCASWritePrefixDirCreation(t *testing.T) {
	cas, root := newTestCAS(t)

	body := []byte("prefix dir creation test")
	hash, err := cas.Write(body, "txt")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	prefixDir := filepath.Join(root, hash[:2])
	info, err := os.Stat(prefixDir)
	if err != nil {
		t.Fatalf("prefix dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("prefix path is not a directory")
	}
}

func TestCASWriteOEXCLRaceWin(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("excl race win test")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	prefixDir := filepath.Join(cas.Root(), hash[:2])
	if err := os.MkdirAll(prefixDir, 0o700); err != nil {
		t.Fatalf("MkdirAll prefix: %v", err)
	}
	destPath := cas.Path(hash, "bin")
	if err := os.WriteFile(destPath, body, 0o600); err != nil {
		t.Fatalf("WriteFile dest: %v", err)
	}
	tmpPath := destPath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		t.Fatalf("WriteFile .tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	if err := os.Remove(destPath); err != nil {
		t.Fatalf("Remove dest: %v", err)
	}

	if err := os.WriteFile(destPath, body, 0o600); err != nil {
		t.Fatalf("Restore dest: %v", err)
	}

	h2, err := cas.Write(body, "bin")
	if err != nil {
		t.Fatalf("Write with dest+.tmp present: %v", err)
	}
	if h2 != hash {
		t.Fatalf("race win: hash mismatch %q vs %q", h2, hash)
	}
}

func TestCASWriteOEXCLRaceLoser(t *testing.T) {
	cas, _ := newTestCAS(t)

	body := []byte("excl race loser test")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	prefixDir := filepath.Join(cas.Root(), hash[:2])
	if err := os.MkdirAll(prefixDir, 0o700); err != nil {
		t.Fatalf("MkdirAll prefix: %v", err)
	}
	tmpPath := cas.Path(hash, "bin") + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		t.Fatalf("WriteFile .tmp: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	_, err := cas.Write(body, "bin")
	if err == nil {
		t.Fatal("Write with only .tmp present (race loser): expected error, got nil")
	}

	if errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Write race loser: got ErrBlobMissing, want a write error")
	}
}

func TestCASWritePrefixDirConflict(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; cannot block directory creation with a file")
	}
	cas, root := newTestCAS(t)

	body := []byte("prefix dir conflict test")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	prefixPath := filepath.Join(root, hash[:2])
	if err := os.WriteFile(prefixPath, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}

	_, err := cas.Write(body, "txt")
	if err == nil {
		t.Fatal("Write with blocked prefix dir: expected error, got nil")
	}
}
