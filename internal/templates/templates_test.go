package templates_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cbip-solutions/hades-system/internal/templates"
)

func TestRegistry_AddGetNamesRoundtrip(t *testing.T) {
	r := templates.NewRegistry()
	r.Add(stubTemplate{name: "b"})
	r.Add(stubTemplate{name: "a"})
	r.Add(stubTemplate{name: "c"})
	got := r.Names()
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("Names() len: got %d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Names()[%d]: got %q want %q", i, got[i], want[i])
		}
	}
	if _, err := r.Get("a"); err != nil {
		t.Errorf("Get(a): %v", err)
	}
	if _, err := r.Get("missing"); err == nil {
		t.Error("Get(missing): want error")
	} else if !errors.Is(err, templates.ErrUnknownTemplate) {
		t.Errorf("Get(missing): want ErrUnknownTemplate, got %v", err)
	}
}

func TestRegistry_AddOverwriteSameName(t *testing.T) {
	r := templates.NewRegistry()
	r.Add(stubTemplate{name: "x"})
	r.Add(stubTemplate{name: "x"})
	names := r.Names()
	if len(names) != 1 || names[0] != "x" {
		t.Errorf("Names after overwrite: got %v want [x]", names)
	}
}

func TestMaterializeFS_StripsTmplSuffix(t *testing.T) {
	root := fstest.MapFS{
		"file.txt.tmpl": &fstest.MapFile{Data: []byte("hello {{.ProjectName}}"), Mode: 0o644},
		"plain.txt":     &fstest.MapFile{Data: []byte("verbatim"), Mode: 0o644},
	}
	dst := t.TempDir()
	answers := templates.Answers{ProjectName: "world"}
	if err := templates.MaterializeFS(context.Background(), root, dst, answers); err != nil {
		t.Fatalf("MaterializeFS: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if string(body) != "hello world" {
		t.Errorf("rendered content: got %q want %q", string(body), "hello world")
	}
	body2, err := os.ReadFile(filepath.Join(dst, "plain.txt"))
	if err != nil {
		t.Fatalf("read verbatim: %v", err)
	}
	if string(body2) != "verbatim" {
		t.Errorf("verbatim content: got %q want %q", string(body2), "verbatim")
	}
}

func TestMaterializeFS_ShellScriptIsExecutable(t *testing.T) {
	root := fstest.MapFS{
		"run.sh": &fstest.MapFile{Data: []byte("#!/bin/sh\necho hi\n"), Mode: 0o644},
	}
	dst := t.TempDir()
	if err := templates.MaterializeFS(context.Background(), root, dst, templates.Answers{}); err != nil {
		t.Fatalf("MaterializeFS: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "run.sh"))
	if err != nil {
		t.Fatalf("stat run.sh: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("run.sh missing exec bits: mode=%v", info.Mode())
	}
}

func TestMaterializeFS_GitkeepCollapsesToDir(t *testing.T) {
	root := fstest.MapFS{
		"empty/.gitkeep": &fstest.MapFile{Data: []byte{}, Mode: 0o644},
	}
	dst := t.TempDir()
	if err := templates.MaterializeFS(context.Background(), root, dst, templates.Answers{}); err != nil {
		t.Fatalf("MaterializeFS: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dst, "empty")); err != nil {
		t.Errorf("expected empty/ dir: %v", err)
	} else if !info.IsDir() {
		t.Errorf("empty/ is not a dir")
	}
	if _, err := os.Stat(filepath.Join(dst, "empty", ".gitkeep")); err == nil {
		t.Errorf(".gitkeep should not be materialized")
	}
}

func TestMaterializeFS_BadTemplateSyntaxReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"bad.txt.tmpl": &fstest.MapFile{Data: []byte("{{.unclosed"), Mode: 0o644},
	}
	dst := t.TempDir()
	if err := templates.MaterializeFS(context.Background(), root, dst, templates.Answers{}); err == nil {
		t.Error("expected error for bad template syntax")
	}
}

func TestMaterializeFS_ContextCancel(t *testing.T) {
	root := fstest.MapFS{
		"x.txt": &fstest.MapFile{Data: []byte("y"), Mode: 0o644},
	}
	dst := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := templates.MaterializeFS(ctx, root, dst, templates.Answers{}); err == nil {
		t.Error("expected ctx error")
	}
}

func TestMaterializeFS_WriteFileAtomic_OpenErrorPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions; skip on root")
	}
	root := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("hello"), Mode: 0o644},
	}
	parent := t.TempDir()
	dst := filepath.Join(parent, "child")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dst, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dst, 0o755)
	err := templates.MaterializeFS(context.Background(), root, dst, templates.Answers{})
	if err == nil {
		t.Fatal("expected MaterializeFS to fail on read-only dst, got nil")
	}
}

func TestMaterializeFS_WriteFileAtomic_RenameErrorPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root may behave differently for rename-onto-dir; skip on root")
	}
	root := fstest.MapFS{
		"target.txt": &fstest.MapFile{Data: []byte("payload"), Mode: 0o644},
	}
	dst := t.TempDir()

	collisionDir := filepath.Join(dst, "target.txt")
	if err := os.MkdirAll(collisionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collisionDir, "block"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := templates.MaterializeFS(context.Background(), root, dst, templates.Answers{})
	if err == nil {
		t.Fatal("expected MaterializeFS to fail on rename onto non-empty dir, got nil")
	}
}

type stubTemplate struct{ name string }

func (s stubTemplate) Name() string { return s.name }
func (s stubTemplate) FS() fs.FS    { return fstest.MapFS{} }
func (s stubTemplate) Materialize(_ context.Context, _ string, _ templates.Answers) error {
	return nil
}
