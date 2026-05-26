package adr_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestWalkAndEmitGraph_ContextCancelledBeforeEntries(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-adr.md", "ADR-0001", "Title 1", "accepted", "", nil)
	writeADRWithRelations(t, dir, "0002-adr.md", "ADR-0002", "Title 2", "accepted", "", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adr.WalkAndEmitGraph(ctx, dir, clk)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got: %v", err)
	}
}

func TestWalkAndEmitIndex_ContextCancelledBeforeEntries(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeStructuredMADRADR(t, dir, "0001-adr.md", "ADR-0001", "Title 1", "accepted")
	writeStructuredMADRADR(t, dir, "0002-adr.md", "ADR-0002", "Title 2", "accepted")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adr.WalkAndEmitIndex(ctx, dir, clk)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got: %v", err)
	}
}

func TestIndexerWalkAndParse_SkipsSubdirEntries(t *testing.T) {
	dir := t.TempDir()
	writeSchemaValidADR(t, dir, "0001-first.md", "ADR-0001", "First Decision", "accepted")

	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}

	indexer := newIndexerForTest(t, dir)
	idx, _, err := indexer.Generate(context.Background(), dir)
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Errorf("Index.Entries: got %d, want 1 (subdir must be skipped)", len(idx.Entries))
	}
}

func TestIndexerWalkAndParse_SkipsLegacyADRs(t *testing.T) {
	dir := t.TempDir()
	writeSchemaValidADR(t, dir, "0001-first.md", "ADR-0001", "First Decision", "accepted")
	legacyContent := "# ADR-0002 Legacy Decision\n\nStatus: Accepted\n\nContext: Old format.\n"
	if err := os.WriteFile(filepath.Join(dir, "0002-legacy.md"), []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy ADR: %v", err)
	}

	indexer := newIndexerForTest(t, dir)
	idx, _, err := indexer.Generate(context.Background(), dir)
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Errorf("Index.Entries: got %d, want 1 (legacy must be skipped)", len(idx.Entries))
	}
	if len(idx.Entries) > 0 && idx.Entries[0].ID != "ADR-0001" {
		t.Errorf("Index.Entries[0].ID: got %q, want ADR-0001", idx.Entries[0].ID)
	}
}

func TestIndexerWalkAndParse_ParseError(t *testing.T) {
	dir := t.TempDir()

	badContent := "---\nid: ADR-0001\ntitle: Bad\n"
	if err := os.WriteFile(filepath.Join(dir, "0001-bad.md"), []byte(badContent), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	indexer := newIndexerForTest(t, dir)
	_, _, err := indexer.Generate(context.Background(), dir)
	if err == nil {
		t.Fatal("Generate: expected parse error for malformed file, got nil")
	}
}

func TestMigrateDirectory_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()

	content := "# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "0001-test.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}

	rep, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
	if err != nil {
		t.Fatalf("MigrateDirectory: unexpected error: %v", err)
	}

	if len(rep.Files) != 1 {
		t.Errorf("Files: got %d, want 1 (subdir must be skipped)", len(rep.Files))
	}
}

func TestApplyTransition_WriteToTempFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions; skipping")
	}

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "0001-test.md")
	content := "---\n" +
		"id: ADR-0001\n" +
		"title: Test Decision\n" +
		"status: proposed\n" +
		"date: \"2026-01-01\"\n" +
		"plan: \"plan-9\"\n" +
		"tags: []\n" +
		"---\n\n## Context\n\nSome context.\n"
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod dir:", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := adr.Accept(context.Background(), srcPath, "op-test", "testing write-fail path", nil, nil)
	if err == nil {
		t.Fatal("Accept: expected error when tmp write fails in read-only dir, got nil")
	}
}

func TestParseAndValidate_HappyPath(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	body := []byte("---\n" +
		"id: ADR-0099\n" +
		"title: ParseAndValidate Coverage\n" +
		"status: accepted\n" +
		"date: \"2026-05-11\"\n" +
		"plan: \"plan-9\"\n" +
		"tags: []\n" +
		"---\n\n## Context\n\nCoverage target for ParseAndValidate.\n")
	const path = "docs/decisions/0099-parse-validate.md"
	a, err := v.ParseAndValidate(path, body)
	if err != nil {
		t.Fatalf("ParseAndValidate() = %v; want nil", err)
	}
	if a == nil {
		t.Fatal("ParseAndValidate() returned nil ADR; want non-nil")
	}
	if a.Path != path {
		t.Errorf("ADR.Path = %q; want %q", a.Path, path)
	}
	if a.Frontmatter.ID != "ADR-0099" {
		t.Errorf("ADR.Frontmatter.ID = %q; want ADR-0099", a.Frontmatter.ID)
	}
}

func TestParseAndValidate_ParseError(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	body := []byte("---\nid: ADR-0099\ntitle: Broken\n")
	_, err = v.ParseAndValidate("docs/decisions/0099-broken.md", body)
	if err == nil {
		t.Fatal("ParseAndValidate() = nil; want parse error")
	}
}
