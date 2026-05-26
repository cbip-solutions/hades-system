package adr

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type countingContext struct {
	context.Context
	freeN int
	calls int
}

func newCountingContext(freeN int) *countingContext {
	return &countingContext{
		Context: context.Background(),
		freeN:   freeN,
	}
}

func (c *countingContext) Err() error {
	c.calls++
	if c.calls > c.freeN {
		return context.Canceled
	}
	return nil
}

func (c *countingContext) Done() <-chan struct{} {
	ch := make(chan struct{})
	if c.calls > c.freeN {
		close(ch)
	}
	return ch
}

func (c *countingContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *countingContext) Value(key any) any {
	return c.Context.Value(key)
}

func TestWalkAndEmitGraph_PerEntryContextCancel(t *testing.T) {
	dir := t.TempDir()

	for i, id := range []string{"ADR-0001", "ADR-0002"} {
		name := "000" + string(rune('1'+i)) + "-adr.md"
		content := "---\nid: " + id + "\ntitle: T\nstatus: accepted\ndate: \"2026-01-01\"\nplan: \"Plan 9\"\ntags: []\n---\n\n## Body\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := newCountingContext(1)
	_, err := WalkAndEmitGraph(ctx, dir, func() string { return "2026-05-09T00:00:00Z" })
	if err == nil {
		t.Fatal("WalkAndEmitGraph: expected context error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("WalkAndEmitGraph: expected context.Canceled, got: %v", err)
	}
}

func TestWalkAndEmitIndex_PerEntryContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i, id := range []string{"ADR-0001", "ADR-0002"} {
		name := "000" + string(rune('1'+i)) + "-adr.md"
		content := "---\nid: " + id + "\ntitle: T\nstatus: accepted\ndate: \"2026-01-01\"\nplan: \"Plan 9\"\ntags: []\n---\n\n## Body\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := newCountingContext(1)
	_, err := WalkAndEmitIndex(ctx, dir, func() string { return "2026-05-09T00:00:00Z" })
	if err == nil {
		t.Fatal("WalkAndEmitIndex: expected context error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("WalkAndEmitIndex: expected context.Canceled, got: %v", err)
	}
}

func TestGenerate_WalkAndEmitGraphError(t *testing.T) {
	dir := t.TempDir()
	schemaPath := findSchemaPath(t)
	v, err := NewValidator(schemaPath)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	content := "---\nid: ADR-0001\ntitle: T\nstatus: accepted\ndate: \"2026-01-01\"\nplan: \"plan-9\"\ntags: []\n---\n\n## Body\n"
	if err := os.WriteFile(filepath.Join(dir, "0001-adr.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ix := NewIndexer(v, func() string { return "2026-05-09T00:00:00Z" })

	ctx := newCountingContext(2)
	_, _, genErr := ix.Generate(ctx, dir)
	if genErr == nil {
		t.Fatal("Generate: expected error from WalkAndEmitGraph cancellation, got nil")
	}

	if genErr.Error() == "" {
		t.Errorf("Generate: non-nil error has empty message: %v", genErr)
	}
}

func findSchemaPath(t *testing.T) string {
	t.Helper()

	candidates := []string{
		"../../docs/decisions/_schema.json",
		"../../../docs/decisions/_schema.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
		}
	}

	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "docs", "decisions", "_schema.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("findSchemaPath: cannot locate docs/decisions/_schema.json")
	return ""
}

func TestMigrateDirectory_PerFileContextCancel(t *testing.T) {
	dir := t.TempDir()

	for i := 1; i <= 3; i++ {
		name := filepath.Join(dir, "000"+string(rune('0'+i))+"-test.md")
		content := "# ADR 000" + string(rune('0'+i)) + ": Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	ctx := newCountingContext(1)
	_, err := MigrateDirectory(ctx, dir, MigrateOptions{PlanFromRange: "plan-1"})
	if err == nil {
		t.Fatal("MigrateDirectory: expected context error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("MigrateDirectory: expected context.Canceled, got: %v", err)
	}
}

func TestMigrateOne_RenameFailureViaDir(t *testing.T) {
	t.Skip("migrate.go:183-186 rename-failure branch requires atomic interposition between WriteFile and Rename — not testable without OS-level hooks; documented in coverage_gap_test.go")
}

func TestApplyTransition_RenameFailure_Internal(t *testing.T) {
	t.Skip("transitions.go:149-154 rename-failure branch requires atomic interposition between WriteFile and Rename — not testable without OS-level hooks; documented in coverage_gap_test.go")
}
