package adr_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestParseWellFormedStructuredMADR(t *testing.T) {
	const content = `---
id: "ADR-0042"
title: "Adopt structured MADR format"
status: "proposed"
date: "2026-05-10"
plan: "Plan 9"
tags: ["adr", "format"]
---

## Context

We are adopting a structured MADR format.
`
	r := strings.NewReader(content)
	got, err := adr.Parse(r)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	if got.Frontmatter.ID != "ADR-0042" {
		t.Errorf("Frontmatter.ID = %q; want %q", got.Frontmatter.ID, "ADR-0042")
	}
	if got.Frontmatter.Title != "Adopt structured MADR format" {
		t.Errorf("Frontmatter.Title = %q; want %q", got.Frontmatter.Title, "Adopt structured MADR format")
	}
	if got.Frontmatter.Status != adr.StatusProposed {
		t.Errorf("Frontmatter.Status = %q; want %q", got.Frontmatter.Status, adr.StatusProposed)
	}
	if got.Frontmatter.Plan != "Plan 9" {
		t.Errorf("Frontmatter.Plan = %q; want %q", got.Frontmatter.Plan, "Plan 9")
	}
	if len(got.Frontmatter.Tags) != 2 {
		t.Errorf("Frontmatter.Tags len = %d; want 2", len(got.Frontmatter.Tags))
	}
	if !strings.Contains(got.Body, "## Context") {
		t.Errorf("Body does not contain expected markdown section; Body = %q", got.Body)
	}
	if !got.HasFrontmatter() {
		t.Error("HasFrontmatter() = false; want true for well-formed structured MADR")
	}
}

func TestParseMarkdownHeadersOnlyLegacy(t *testing.T) {
	const content = `# ADR 0001: Substrate pivot from OpenCode to OpenClaude

**Status**: Accepted
**Date**: 2026-04-30

## Context

Some context here.
`
	r := strings.NewReader(content)
	got, err := adr.Parse(r)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error for legacy format: %v", err)
	}
	if got.Frontmatter.ID != "" {
		t.Errorf("Frontmatter.ID = %q; want empty string for legacy", got.Frontmatter.ID)
	}
	if !strings.Contains(got.Body, "# ADR 0001") {
		t.Errorf("Body does not contain full content; Body = %q", got.Body)
	}
	if got.HasFrontmatter() {
		t.Error("HasFrontmatter() = true; want false for legacy markdown-headers-only ADR")
	}
}

func TestParseMalformedYAMLReturnsErrInvalidFrontmatter(t *testing.T) {
	const content = `---
id: "ADR-0042"
title: [unclosed bracket
status: proposed
---
`
	r := strings.NewReader(content)
	_, err := adr.Parse(r)
	if err == nil {
		t.Fatal("Parse() returned nil error; want ErrInvalidFrontmatter")
	}
	if !errors.Is(err, adr.ErrInvalidFrontmatter) {
		t.Errorf("Parse() error = %v; want errors.Is(..., ErrInvalidFrontmatter)", err)
	}
}

func TestParseMissingFileReturnsErrFileNotFound(t *testing.T) {
	_, err := adr.ParseFile("/nonexistent/path/that/does/not/exist.md")
	if err == nil {
		t.Fatal("ParseFile() returned nil error; want ErrFileNotFound")
	}
	if !errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("ParseFile() error = %v; want errors.Is(..., ErrFileNotFound)", err)
	}
}

func TestParseEmptyFile(t *testing.T) {
	r := strings.NewReader("")
	got, err := adr.Parse(r)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error for empty file: %v", err)
	}
	if got == nil {
		t.Fatal("Parse() returned nil ADR; want non-nil zero ADR")
	}
	if got.Frontmatter.ID != "" {
		t.Errorf("Frontmatter.ID = %q; want empty", got.Frontmatter.ID)
	}
	if got.Body != "" {
		t.Errorf("Body = %q; want empty", got.Body)
	}
	if got.HasFrontmatter() {
		t.Error("HasFrontmatter() = true; want false for empty file")
	}
}

func TestParseFrontmatterClosedNoBody(t *testing.T) {
	const content = `---
id: "ADR-0099"
title: "Placeholder decision"
status: "reserved"
date: "2026-05-10"
plan: "Plan 9"
tags: []
---
`
	r := strings.NewReader(content)
	got, err := adr.Parse(r)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	if got.Frontmatter.ID != "ADR-0099" {
		t.Errorf("Frontmatter.ID = %q; want %q", got.Frontmatter.ID, "ADR-0099")
	}

	trimmedBody := strings.TrimSpace(got.Body)
	if trimmedBody != "" {
		t.Errorf("Body (trimmed) = %q; want empty", trimmedBody)
	}
	if !got.HasFrontmatter() {
		t.Error("HasFrontmatter() = false; want true for closed frontmatter with valid ID")
	}
}

func TestParseFrontmatterUnclosedReturnsErr(t *testing.T) {
	const content = `---
id: "ADR-0042"
title: "Decision with missing closing delimiter"
status: "proposed"
date: "2026-05-10"
plan: "Plan 9"
tags: []
`
	r := strings.NewReader(content)
	_, err := adr.Parse(r)
	if err == nil {
		t.Fatal("Parse() returned nil error; want ErrInvalidFrontmatter for unclosed frontmatter")
	}
	if !errors.Is(err, adr.ErrInvalidFrontmatter) {
		t.Errorf("Parse() error = %v; want errors.Is(..., ErrInvalidFrontmatter)", err)
	}
}

func TestParseFileWithMalformedYAMLPropagatesError(t *testing.T) {

	f, err := os.CreateTemp(t.TempDir(), "adr-*.md")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	_, _ = f.WriteString("---\nid: [unclosed\n---\n")
	f.Close()

	_, err = adr.ParseFile(f.Name())
	if err == nil {
		t.Fatal("ParseFile() returned nil error; want ErrInvalidFrontmatter")
	}
	if !errors.Is(err, adr.ErrInvalidFrontmatter) {
		t.Errorf("ParseFile() error = %v; want errors.Is(..., ErrInvalidFrontmatter)", err)
	}
}

func TestParseFilePermissionDeniedReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}
	f, err := os.CreateTemp(t.TempDir(), "adr-noperm-*.md")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	_, _ = f.WriteString("# some content\n")
	path := f.Name()
	f.Close()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(path, 0o644)

	_, err = adr.ParseFile(path)
	if err == nil {
		t.Fatal("ParseFile() returned nil error; want permission-denied error")
	}
	if errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("ParseFile() error = %v; should not wrap ErrFileNotFound (file exists, is permission-denied)", err)
	}
}

func TestParseAllExistingADRsReturnLegacyFormat(t *testing.T) {
	repoRoot := repoRootForTest(t)
	decisionsDir := filepath.Join(repoRoot, "docs", "decisions")

	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", decisionsDir, err)
	}

	var mdFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {

			name := e.Name()
			if name == "_index.md" || name == "_schema.md" {
				continue
			}

			path := filepath.Join(decisionsDir, name)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}
			if strings.HasPrefix(string(body), "---") {
				continue
			}
			mdFiles = append(mdFiles, path)
		}
	}

	const minFiles = 17
	if len(mdFiles) < minFiles {
		t.Fatalf("found %d ADR .md files in %s; want ≥%d (corpus shrank unexpectedly)",
			len(mdFiles), decisionsDir, minFiles)
	}

	for _, path := range mdFiles {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			got, err := adr.ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile(%s) error: %v", path, err)
			}

			if got.HasFrontmatter() {
				t.Errorf("HasFrontmatter() = true for pre-migration ADR %s; expected legacy format",
					filepath.Base(path))
			}
		})
	}
}
