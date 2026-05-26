package safetynet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEmitter struct{ events []Event }

func (f *fakeEmitter) Emit(_ context.Context, e Event) error {
	f.events = append(f.events, e)
	return nil
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestPrevInstall_VerifiesSha256(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("#!/bin/sh\necho prev-zen\n")
	writeFile(t, src, body)
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}

	dst := filepath.Join(tmp, "bin", "zen-prev")
	em := &fakeEmitter{}
	p := NewPrev(dst, em)
	if err := p.Install(context.Background(), manifest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != string(body) {
		t.Fatalf("dst content drift: got %q want %q", got, body)
	}
	info, _ := os.Stat(dst)
	if info.Mode()&0o111 == 0 {
		t.Fatal("dst not executable")
	}
}

func TestPrevInstall_RejectsHashMismatch(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	writeFile(t, src, []byte("real bytes"))
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex([]byte("DIFFERENT"))}

	dst := filepath.Join(tmp, "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	err := p.Install(context.Background(), manifest)
	if !errors.Is(err, ErrPrevHashMismatch) {
		t.Fatalf("want ErrPrevHashMismatch, got %v", err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatal("dst created on hash failure (must not be)")
	}
}

func TestPrevInstall_RejectsEmptyManifestField(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	cases := []Manifest{
		{Version: "", BinaryPath: "/tmp/x", Sha256: "deadbeef"},
		{Version: "v0.4.0", BinaryPath: "", Sha256: "deadbeef"},
		{Version: "v0.4.0", BinaryPath: "/tmp/x", Sha256: ""},
	}
	for _, c := range cases {
		if err := p.Install(context.Background(), c); !errors.Is(err, ErrPrevManifest) {
			t.Errorf("manifest %+v: want ErrPrevManifest, got %v", c, err)
		}
	}
}

func TestPrevInstall_OpenSourceError(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	manifest := Manifest{Version: "v0.4.0", BinaryPath: "/no/such/path/zen", Sha256: "deadbeef"}
	err := p.Install(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "open source") {
		t.Fatalf("want open source error, got %v", err)
	}
}

func TestPrevShow_NoInstall_EmitsMissingEvent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bin", "zen-prev")
	em := &fakeEmitter{}
	p := NewPrev(dst, em)

	info, err := p.Show(context.Background())
	if !errors.Is(err, ErrPrevNotInstalled) {
		t.Fatalf("want ErrPrevNotInstalled, got %v", err)
	}
	if info.Installed {
		t.Fatal("expected Installed=false")
	}
	if len(em.events) != 1 || em.events[0].Type != EventSafetynetPrevMissing {
		t.Fatalf("expected 1 SafetynetPrevMissing event, got %+v", em.events)
	}
}

func TestPrevShow_Installed(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("#!/bin/sh\nexit 0\n")
	writeFile(t, src, body)
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}
	dst := filepath.Join(tmp, "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	if err := p.Install(context.Background(), manifest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	info, err := p.Show(context.Background())
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !info.Installed || info.Path != dst || info.Version != "v0.4.0" {
		t.Errorf("info drift: %+v", info)
	}
	if info.Sha256 != sha256hex(body) {
		t.Errorf("sha256: got %s want %s", info.Sha256, sha256hex(body))
	}
}

func TestPrevShow_RejectsDirectoryAtDst(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bin", "zen-prev")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewPrev(dst, &fakeEmitter{})
	if _, err := p.Show(context.Background()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("want dir error, got %v", err)
	}
}

func TestPrevExec_RoutesArgsToBinary(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bin", "zen-prev")
	writeFile(t, dst, []byte("#!/bin/sh\necho \"prev-args:$*\"\n"))

	p := NewPrev(dst, &fakeEmitter{})
	out, err := p.Exec(context.Background(), []string{"doctor", "--quick"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "prev-args:doctor --quick\n" {
		t.Fatalf("Exec stdout drift: %q", got)
	}
}

func TestPrevInstall_CtxCancelled(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("bytes")
	writeFile(t, src, body)
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}

	dst := filepath.Join(tmp, "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Install(ctx, manifest)
	if err == nil || !strings.Contains(err.Error(), "ctx cancelled") {
		t.Fatalf("want ctx cancelled error, got %v", err)
	}
}

func TestPrevInstall_MkdirFails(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions on Unix")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("bytes")
	writeFile(t, src, body)

	parent := filepath.Join(tmp, "ro")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	dst := filepath.Join(parent, "child", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}
	err := p.Install(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("want mkdir error, got %v", err)
	}
}

func TestPrevInstall_TmpCreateFails(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions on Unix")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("bytes")
	writeFile(t, src, body)

	parent := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	dst := filepath.Join(parent, "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}
	err := p.Install(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "create tmp") {
		t.Fatalf("want create tmp error, got %v", err)
	}
}

func TestPrevInstall_RenameFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "release", "zen")
	body := []byte("real bytes")
	writeFile(t, src, body)

	dst := filepath.Join(tmp, "bin", "zen-prev")

	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewPrev(dst, &fakeEmitter{})
	manifest := Manifest{Version: "v0.4.0", BinaryPath: src, Sha256: sha256hex(body)}
	err := p.Install(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("want rename error, got %v", err)
	}

	if _, statErr := os.Stat(dst + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatal("tmp not cleaned up after rename failure")
	}
}

func TestPrevShow_ReadFileFails(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions on Unix")
	}
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bin", "zen-prev")
	writeFile(t, dst, []byte("x"))
	if err := os.Chmod(dst, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dst, 0o755) })

	p := NewPrev(dst, &fakeEmitter{})
	if _, err := p.Show(context.Background()); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want read error, got %v", err)
	}
}

type errReader struct {
	prefix []byte
	idx    int
	err    error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.idx < len(r.prefix) {
		n := copy(p, r.prefix[r.idx:])
		r.idx += n
		return n, nil
	}
	return 0, r.err
}

func TestWriteHashedTmp_CopyFails(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "tmp.bin")
	src := &errReader{prefix: []byte("partial-"), err: errors.New("simulated read failure")}
	_, err := writeHashedTmp(tmp, src)
	if err == nil || !strings.Contains(err.Error(), "copy") {
		t.Fatalf("want copy error, got %v", err)
	}
}

func TestWriteHashedTmp_CreateFails(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions on Unix")
	}
	parent := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	_, err := writeHashedTmp(filepath.Join(parent, "x"), strings.NewReader("data"))
	if err == nil || !strings.Contains(err.Error(), "create tmp") {
		t.Fatalf("want create tmp error, got %v", err)
	}
}

func TestWriteHashedTmp_Happy(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "out.bin")
	body := []byte("hello-world")
	got, err := writeHashedTmp(tmp, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("writeHashedTmp: %v", err)
	}
	if got != sha256hex(body) {
		t.Errorf("hash drift: got %s want %s", got, sha256hex(body))
	}
	written, _ := os.ReadFile(tmp)
	if string(written) != string(body) {
		t.Errorf("body drift: got %q want %q", written, body)
	}
}

func TestPrevExec_RejectsWhenMissing(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bin", "zen-prev")
	p := NewPrev(dst, &fakeEmitter{})
	if _, err := p.Exec(context.Background(), []string{"doctor"}); !errors.Is(err, ErrPrevNotInstalled) {
		t.Fatalf("want ErrPrevNotInstalled, got %v", err)
	}
}
