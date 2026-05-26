package adr_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func frozenClock(ts string) func() string {
	return func() string { return ts }
}

func writeStructuredMADRADR(t *testing.T, dir, filename, id, title, status string) {
	t.Helper()
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: " + status + "\n" +
		"date: \"2026-01-01\"\n" +
		"plan: \"Plan 9\"\n" +
		"tags: []\n" +
		"---\n\n" +
		"## Context\n\nSome body text.\n"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeStructuredMADRADR: %v", err)
	}
}

func TestEmitFlatManifest_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil Index")
	}
	if idx.SchemaVersion != adr.IndexSchemaVersion {
		t.Errorf("schema_version: got %d, want %d", idx.SchemaVersion, adr.IndexSchemaVersion)
	}
	if len(idx.Entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(idx.Entries))
	}
}

func TestEmitFlatManifest_SortByID(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeStructuredMADRADR(t, dir, "0003-third.md", "ADR-0003", "Third Decision", "accepted")
	writeStructuredMADRADR(t, dir, "0001-first.md", "ADR-0001", "First Decision", "accepted")
	writeStructuredMADRADR(t, dir, "0002-second.md", "ADR-0002", "Second Decision", "accepted")

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(idx.Entries) != 3 {
		t.Fatalf("entries: got %d, want 3", len(idx.Entries))
	}
	if idx.Entries[0].ID != "ADR-0001" {
		t.Errorf("entries[0].ID: got %q, want %q", idx.Entries[0].ID, "ADR-0001")
	}
	if idx.Entries[1].ID != "ADR-0002" {
		t.Errorf("entries[1].ID: got %q, want %q", idx.Entries[1].ID, "ADR-0002")
	}
	if idx.Entries[2].ID != "ADR-0003" {
		t.Errorf("entries[2].ID: got %q, want %q", idx.Entries[2].ID, "ADR-0003")
	}
}

func TestEmitFlatManifest_SkipLegacy(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeStructuredMADRADR(t, dir, "0001-structured.md", "ADR-0001", "Structured", "accepted")
	legacyContent := "# ADR-0002 Legacy Decision\n\nStatus: Accepted\n\nContext: Old format.\n"
	if err := os.WriteFile(filepath.Join(dir, "0002-legacy.md"), []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries: got %d, want 1 (legacy must be skipped)", len(idx.Entries))
	}
	if idx.Entries[0].ID != "ADR-0001" {
		t.Errorf("entries[0].ID: got %q, want %q", idx.Entries[0].ID, "ADR-0001")
	}
}

func TestEmitFlatManifest_SkipManifests(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeStructuredMADRADR(t, dir, "0001-real.md", "ADR-0001", "Real ADR", "accepted")

	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatalf("write _index.md: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "_index.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write _index.json: %v", err)
	}

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries: got %d, want 1 (manifest files must be skipped)", len(idx.Entries))
	}
}

func TestEmitFlatManifest_SkipSubdirs(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeStructuredMADRADR(t, dir, "0001-top.md", "ADR-0001", "Top-level ADR", "accepted")

	for _, sub := range []string{"proposed", "rejected", "archive"} {
		subDir := filepath.Join(dir, sub)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		writeStructuredMADRADR(t, subDir, "0099-sub.md", "ADR-0099", "Sub ADR", "proposed")
	}

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries: got %d, want 1 (subdirs must be skipped); got %v",
			len(idx.Entries), idx.Entries)
	}
	if idx.Entries[0].ID != "ADR-0001" {
		t.Errorf("entries[0].ID: got %q, want ADR-0001", idx.Entries[0].ID)
	}
}

func TestMarshalIndex_DeterministicJSON(t *testing.T) {
	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries: []adr.IndexEntry{
			{
				ID:     "ADR-0001",
				Title:  "Use Go",
				Status: "accepted",
				Path:   "docs/decisions/0001-use-go.md",
				Frontmatter: adr.Frontmatter{
					ID:     "ADR-0001",
					Title:  "Use Go",
					Status: "accepted",
					Date:   "2026-01-01",
					Plan:   "Plan 9",
					Tags:   []string{"lang"},
				},
			},
		},
	}

	b1, err := adr.MarshalIndex(idx)
	if err != nil {
		t.Fatalf("first MarshalIndex: %v", err)
	}
	b2, err := adr.MarshalIndex(idx)
	if err != nil {
		t.Fatalf("second MarshalIndex: %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output:\nfirst: %s\nsecond: %s", b1, b2)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b1, &raw); err != nil {
		t.Errorf("output is not valid JSON: %v\noutput: %s", err, b1)
	}
}

func TestMarshalIndex_CanonicalIndentation(t *testing.T) {
	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       []adr.IndexEntry{},
	}

	b, err := adr.MarshalIndex(idx)
	if err != nil {
		t.Fatalf("MarshalIndex: %v", err)
	}

	s := string(b)

	if !strings.HasSuffix(s, "\n") {
		t.Errorf("output does not end with newline: %q", s)
	}

	lines := strings.Split(s, "\n")
	foundIndented := false
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			foundIndented = true
			break
		}
	}
	if !foundIndented {
		t.Errorf("no 2-space indented line found in output:\n%s", s)
	}

	if strings.Contains(s, `<`) || strings.Contains(s, `>`) || strings.Contains(s, `&`) {
		t.Errorf("HTML escaping detected in output: %s", s)
	}
}

func TestWriteReadIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_index.json")

	original := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries: []adr.IndexEntry{
			{
				ID:     "ADR-0001",
				Title:  "Use Go",
				Status: "accepted",
				Path:   "docs/decisions/0001-use-go.md",
				Frontmatter: adr.Frontmatter{
					ID:     "ADR-0001",
					Title:  "Use Go",
					Status: "accepted",
					Date:   "2026-01-01",
					Plan:   "Plan 9",
					Tags:   []string{"lang"},
				},
			},
		},
	}

	if err := adr.WriteIndex(path, original); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	got, err := adr.ReadIndex(path)
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("schema_version: got %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if got.GeneratedAt != original.GeneratedAt {
		t.Errorf("generated_at: got %q, want %q", got.GeneratedAt, original.GeneratedAt)
	}
	if len(got.Entries) != len(original.Entries) {
		t.Fatalf("entries length: got %d, want %d", len(got.Entries), len(original.Entries))
	}
	if got.Entries[0].ID != original.Entries[0].ID {
		t.Errorf("entries[0].ID: got %q, want %q", got.Entries[0].ID, original.Entries[0].ID)
	}
}

func TestReadIndex_FileNotFound(t *testing.T) {
	_, err := adr.ReadIndex("/nonexistent/path/_index.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got: %v", err)
	}
}

func TestReadIndex_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_index.json")
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := adr.ReadIndex(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("unexpected ErrFileNotFound — got wrong error type: %v", err)
	}
}

func TestWriteIndex_DestDirMissing(t *testing.T) {
	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       []adr.IndexEntry{},
	}
	err := adr.WriteIndex("/nonexistent/dir/_index.json", idx)
	if err == nil {
		t.Fatal("expected error for missing dest dir, got nil")
	}
}

func TestWalkAndEmitIndex_ContextCancelledBefore(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adr.WalkAndEmitIndex(ctx, dir, clk)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestWalkAndEmitIndex_ContextCancelledDuring(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	for i := 1; i <= 5; i++ {
		id := "ADR-000" + string(rune('0'+i))
		title := "ADR " + string(rune('0'+i))
		writeStructuredMADRADR(t, dir, "000"+string(rune('0'+i))+"-adr.md", id, title, "accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	_, err := adr.WalkAndEmitIndex(ctx, dir, clk)

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected error (not Canceled): %v", err)
	}
}

func TestWalkAndEmitIndex_ParseError(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	badContent := "---\nid: ADR-0001\ntitle: Bad\n"
	if err := os.WriteFile(filepath.Join(dir, "0001-bad.md"), []byte(badContent), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	_, err := adr.WalkAndEmitIndex(context.Background(), dir, clk)
	if err == nil {
		t.Fatal("expected parse error for malformed frontmatter, got nil")
	}
}

func TestWalkAndEmitIndex_DirReadError(t *testing.T) {
	clk := frozenClock("2026-05-09T00:00:00Z")
	_, err := adr.WalkAndEmitIndex(context.Background(), "/nonexistent/dir/path", clk)
	if err == nil {
		t.Fatal("expected error for nonexistent dir, got nil")
	}
}

func TestWalkAndEmitIndex_NilClock(t *testing.T) {
	dir := t.TempDir()

	idx, err := adr.WalkAndEmitIndex(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil Index with nil clock")
	}
	if idx.GeneratedAt == "" {
		t.Errorf("GeneratedAt must be non-empty when nil clock is provided")
	}
}

func TestMarshalIndex_NilInput(t *testing.T) {
	_, err := adr.MarshalIndex(nil)
	if err == nil {
		t.Fatal("expected error for nil Index, got nil")
	}
}

func TestMarshalIndex_NilEntries(t *testing.T) {
	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       nil,
	}
	b, err := adr.MarshalIndex(idx)
	if err != nil {
		t.Fatalf("MarshalIndex: %v", err)
	}
	if strings.Contains(string(b), `"entries": null`) {
		t.Errorf("entries must not be null; got: %s", b)
	}
	if !strings.Contains(string(b), `"entries": []`) {
		t.Errorf("entries must be [] not null; got: %s", b)
	}
}

func TestWriteIndex_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses read-only permission; skipping")
	}
	dir := t.TempDir()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       []adr.IndexEntry{},
	}
	err := adr.WriteIndex(filepath.Join(dir, "_index.json"), idx)
	if err == nil {
		t.Fatal("expected error writing to read-only dir, got nil")
	}
}

func TestWriteIndex_NilIndex(t *testing.T) {
	dir := t.TempDir()
	err := adr.WriteIndex(filepath.Join(dir, "_index.json"), nil)
	if err == nil {
		t.Fatal("expected error for nil Index, got nil")
	}
}

func TestWriteIndex_RenameFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses some rename restrictions; skipping")
	}
	dir := t.TempDir()

	destPath := filepath.Join(dir, "_index.json")
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		t.Fatalf("mkdir destPath: %v", err)
	}

	if err := os.WriteFile(filepath.Join(destPath, "inner"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write inner: %v", err)
	}

	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       []adr.IndexEntry{},
	}
	err := adr.WriteIndex(destPath, idx)
	if err == nil {
		t.Fatal("expected rename error, got nil")
	}
}

func TestReadIndex_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions; skipping")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "_index.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"generated_at":"x","entries":[]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := adr.ReadIndex(path)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("unexpected ErrFileNotFound for permission-denied: %v", err)
	}
}

func TestMarshalIndex_Fields(t *testing.T) {
	idx := &adr.Index{
		SchemaVersion: adr.IndexSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Entries:       []adr.IndexEntry{},
	}
	b, err := adr.MarshalIndex(idx)
	if err != nil {
		t.Fatalf("MarshalIndex: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, field := range []string{"schema_version", "generated_at", "entries"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field %q in JSON output", field)
		}
	}
}
