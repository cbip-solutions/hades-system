package projectctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindProjectRootFindsZenswarmTOML(t *testing.T) {
	root := t.TempDir()

	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonRoot, "zenswarm.toml"), []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sub := filepath.Join(canonRoot, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != canonRoot {
		t.Errorf("got %q, want %q", got, canonRoot)
	}
}

func TestFindProjectRootFindsDotGit(t *testing.T) {
	root := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(canonRoot, ".git"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sub := filepath.Join(canonRoot, "x", "y")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != canonRoot {
		t.Errorf("got %q, want %q", got, canonRoot)
	}
}

func TestFindProjectRootStartIsRoot(t *testing.T) {
	root := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonRoot, "zenswarm.toml"), []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := FindProjectRoot(canonRoot)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != canonRoot {
		t.Errorf("got %q, want %q", got, canonRoot)
	}
}

func TestFindProjectRootNotFound(t *testing.T) {

	root := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	sub := filepath.Join(canonRoot, "deep", "child")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_, err = FindProjectRoot(sub)
	if err == nil {
		t.Fatal("expected error on not-found; got nil")
	}
	if !strings.Contains(err.Error(), "no project root") {
		t.Errorf("err = %v; want substring 'no project root'", err)
	}
}

func TestFindProjectRootEmptyPath(t *testing.T) {
	_, err := FindProjectRoot("")
	if err == nil {
		t.Fatal("expected error on empty path; got nil")
	}
}

func TestFindProjectRootCanonicalisationFails(t *testing.T) {

	_, err := FindProjectRoot("/this/path/does/not/exist/anywhere/under/the/sun/probably/abc123")
	if err == nil {
		t.Fatal("expected error on non-existent path; got nil")
	}
	if !strings.Contains(err.Error(), "projectctx.FindProjectRoot") {
		t.Errorf("err = %v; want substring 'projectctx.FindProjectRoot'", err)
	}
}
