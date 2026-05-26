//go:build adversarial
// +build adversarial

package adversarial

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestRecognize_HostileGitattributesVendorsAllGo(t *testing.T) {
	dir := t.TempDir()
	mustWriteRecognizeAdv(t, dir, ".gitattributes", "*.go linguist-vendored=true\n")
	mustWriteRecognizeAdv(t, dir, "main.go", "package main\nfunc main(){}\n")
	mustWriteRecognizeAdv(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if res.PrimaryLanguage != "Go" {
		t.Errorf("PrimaryLanguage = %q; want Go (Tier 1 should override hostile gitattributes)", res.PrimaryLanguage)
	}
}

func TestRecognize_HugeBinaryFileNotOOM(t *testing.T) {
	dir := t.TempDir()

	huge := make([]byte, 5*1024*1024)
	for i := range huge {
		huge[i] = byte(i)
	}
	huge[0] = 0x00
	mustWriteRecognizeAdv(t, dir, "blob.go", string(huge))
	mustWriteRecognizeAdv(t, dir, "ok.go", "package main\nfunc main(){}\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})

	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	_ = res
}

func TestRecognize_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if res.PrimaryLanguage != "" {
		t.Errorf("PrimaryLanguage = %q; want \"\" for empty repo", res.PrimaryLanguage)
	}
}

func mustWriteRecognizeAdv(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRecognize_SymlinkLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows; skipped")
	}
	dir := t.TempDir()

	mustWriteRecognizeAdv(t, dir, "go.mod", "module x\n\ngo 1.22\n")
	mustWriteRecognizeAdv(t, dir, "main.go", "package main\nfunc main(){}\n")

	loopDir := filepath.Join(dir, "loop")
	if err := os.MkdirAll(loopDir, 0o755); err != nil {
		t.Fatalf("mkdir loop: %v", err)
	}
	a := filepath.Join(loopDir, "a")
	b := filepath.Join(loopDir, "b")

	if err := os.Symlink("b", a); err != nil {
		t.Fatalf("symlink a → b: %v", err)
	}

	if err := os.Symlink("a", b); err != nil {
		t.Fatalf("symlink b → a: %v", err)
	}

	done := make(chan struct{})
	var res recognize.Result
	var recErr error
	go func() {
		r := recognize.New(recognize.Options{
			RootAbsPath: dir,
			NoAudit:     true,
		})
		res, recErr = r.Recognize(context.Background(), os.DirFS(dir))
		close(done)
	}()

	select {
	case <-done:
		if recErr != nil {
			t.Fatalf("Recognize err on symlink loop: %v", recErr)
		}

		if res.PrimaryLanguage != "Go" {
			t.Errorf("PrimaryLanguage = %q; want Go (Tier 1 short-circuit must survive symlink loop)", res.PrimaryLanguage)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Recognize did not complete within 30s on symlink loop (probable infinite-loop regression)")
	}
}

func TestRecognize_NestedSubmodules(t *testing.T) {
	dir := t.TempDir()

	mustWriteRecognizeAdv(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	mustWriteRecognizeAdv(t, dir, "pnpm-workspace.yaml", "packages: ['packages/*']\n")
	mustWriteRecognizeAdv(t, dir, "go.mod", "module outer\n\ngo 1.22\n")

	mustWriteRecognizeAdv(t, dir, "submodules/foo/.git/HEAD", "ref: refs/heads/main\n")
	mustWriteRecognizeAdv(t, dir, "submodules/foo/go.mod", "module foo\n\ngo 1.22\n")
	mustWriteRecognizeAdv(t, dir, "submodules/foo/main.go", "package main\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("Recognize err on nested submodules: %v", err)
	}

	if res.PrimaryLanguage != "Go" {
		t.Errorf("PrimaryLanguage = %q; want Go (outer go.mod must short-circuit)", res.PrimaryLanguage)
	}

	if res.Monorepo == nil {
		t.Fatal("Monorepo nil; want pnpm at outer root")
	}
	if res.Monorepo.Tool != "pnpm" {
		t.Errorf("Monorepo.Tool = %q; want pnpm", res.Monorepo.Tool)
	}

	if res.Monorepo.Root != dir {
		t.Errorf("Monorepo.Root = %q; want outer dir %q (walk-UP must respect nested .git boundary)", res.Monorepo.Root, dir)
	}
}
