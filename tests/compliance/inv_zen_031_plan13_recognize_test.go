// Package compliance — inv-zen-031 Plan 13 Phase B recognize sub-tree.
//
// inv-zen-031 (boundary discipline): `internal/recognize/{,manifest,config,
// glob,monorepo,maturity}/` packages do NOT import `internal/store` directly.
// Compile-checked via grep against package source (test files allowed for
// future fixture loading; production .go files MUST be clean).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen031Plan13RecognizeNoStoreImport(t *testing.T) {
	repoRoot := findRepoRootRecognize(t)
	recognizeDirs := []string{
		filepath.Join(repoRoot, "internal", "recognize"),
		filepath.Join(repoRoot, "internal", "recognize", "manifest"),
		filepath.Join(repoRoot, "internal", "recognize", "config"),
		filepath.Join(repoRoot, "internal", "recognize", "glob"),
		filepath.Join(repoRoot, "internal", "recognize", "monorepo"),
		filepath.Join(repoRoot, "internal", "recognize", "maturity"),
	}
	forbidden := []string{
		`"github.com/cbip-solutions/hades-system/internal/store"`,
		`"github.com/cbip-solutions/hades-system/internal/store/`,
	}
	for _, dir := range recognizeDirs {
		if _, err := os.Stat(dir); err != nil {

			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		for _, f := range files {
			n := f.Name()
			if !strings.HasSuffix(n, ".go") {
				continue
			}
			if strings.HasSuffix(n, "_test.go") {
				continue
			}
			buf, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			for _, fb := range forbidden {
				if strings.Contains(string(buf), fb) {
					t.Errorf("%s imports %s; inv-zen-031 violated", filepath.Join(dir, n), fb)
				}
			}
		}
	}
}

// TestInvZen088Plan13RecognizeNoProvidersImport asserts inv-zen-088 single-
// egress: recognize packages do NOT import internal/providers or
// internal/dispatcher (no LLM traffic from inference path).
func TestInvZen088Plan13RecognizeNoProvidersImport(t *testing.T) {
	repoRoot := findRepoRootRecognize(t)
	recognizeDirs := []string{
		filepath.Join(repoRoot, "internal", "recognize"),
		filepath.Join(repoRoot, "internal", "recognize", "manifest"),
		filepath.Join(repoRoot, "internal", "recognize", "config"),
		filepath.Join(repoRoot, "internal", "recognize", "glob"),
		filepath.Join(repoRoot, "internal", "recognize", "monorepo"),
		filepath.Join(repoRoot, "internal", "recognize", "maturity"),
	}
	forbidden := []string{
		`"github.com/cbip-solutions/hades-system/internal/providers`,
		`"github.com/cbip-solutions/hades-system/internal/dispatcher`,
	}
	for _, dir := range recognizeDirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		for _, f := range files {
			n := f.Name()
			if !strings.HasSuffix(n, ".go") {
				continue
			}
			if strings.HasSuffix(n, "_test.go") {
				continue
			}
			buf, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			for _, fb := range forbidden {
				if strings.Contains(string(buf), fb) {
					t.Errorf("%s imports %s; inv-zen-088 single-egress violated", filepath.Join(dir, n), fb)
				}
			}
		}
	}
}

func findRepoRootRecognize(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	cur := cwd
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("go.mod not found ascending from %s", cwd)
		}
		cur = parent
	}
}
