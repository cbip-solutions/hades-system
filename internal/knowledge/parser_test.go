package knowledge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFileWithFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "memory.md")
	body := `---
title: "My memory"
tags: [foo, bar]
created_at: 2026-05-01
---

# Header One

Body line one.
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{
		Path: p, Kind: FileTypeMemory,
		ProjectID: "abc", ProjectAlias: "internal-platform-x",
		Size: int64(len(body)), ModTime: time.Now().UnixNano(),
	}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "My memory" {
		t.Errorf("Title = %q, want %q (frontmatter title takes precedence)", doc.Title, "My memory")
	}
	if !strings.Contains(doc.ContentText, "Body line one") {
		t.Errorf("ContentText missing body: %q", doc.ContentText)
	}
	if strings.Contains(doc.ContentText, "title:") {
		t.Errorf("ContentText leaked frontmatter: %q", doc.ContentText)
	}
	if doc.FrontmatterJSON == nil {
		t.Fatalf("FrontmatterJSON nil; expected JSON")
	}
	var fm map[string]any
	if err := json.Unmarshal(doc.FrontmatterJSON, &fm); err != nil {
		t.Fatalf("FrontmatterJSON not valid JSON: %v", err)
	}
	if fm["title"] != "My memory" {
		t.Errorf("FrontmatterJSON title = %v, want My memory", fm["title"])
	}
}

func TestParseFileWithoutFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "no-fm.md")
	body := "# Just a title\n\nbody"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory, ProjectID: "p"}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "Just a title" {
		t.Errorf("Title = %q, want first H1", doc.Title)
	}
	if doc.FrontmatterJSON != nil {
		t.Errorf("FrontmatterJSON = %s, want nil (no frontmatter present)", doc.FrontmatterJSON)
	}
	if doc.ContentText != "# Just a title\n\nbody" {
		t.Errorf("ContentText = %q, want full body", doc.ContentText)
	}
}

func TestParseFileBasenameFallbackTitle(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "no-h1-no-fm.md")
	if err := os.WriteFile(p, []byte("just body"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "no-h1-no-fm" {
		t.Errorf("Title = %q, want %q (basename fallback)", doc.Title, "no-h1-no-fm")
	}
}

func TestParseFileWithMalformedFrontmatterFallsBack(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "bad.md")
	body := `---
title: "broken
key: [unclosed
---

# H

body
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		// Per spec §4.5 partial-tolerance: we want a Doc back, plus optional warning.
		t.Fatalf("Parse should not return error on malformed frontmatter; got %v", err)
	}
	if doc.FrontmatterJSON != nil {
		t.Errorf("FrontmatterJSON = %s, want nil (malformed YAML must drop frontmatter)", doc.FrontmatterJSON)
	}

	if !strings.Contains(doc.ContentText, "body") {
		t.Errorf("ContentText missing body on partial-tolerance path: %q", doc.ContentText)
	}
	if doc.Title != "H" {
		t.Errorf("Title = %q, want %q (H1 fallback when frontmatter unparseable)", doc.Title, "H")
	}
}

// TestParseFileBinaryDataRejected — operator may drop a binary blob
// with a `.md` extension (rare but possible). Parse MUST detect the
// non-textual content via the 4KB head heuristic and return
// ErrBinaryContent rather than indexing gibberish into FTS5.
func TestParseFileBinaryDataRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "fake.md")

	bytes := make([]byte, 4096)
	for i := range bytes {
		if i%4 == 0 {
			bytes[i] = 'a'
		}
	}
	if err := os.WriteFile(p, bytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	_, err := Parse(sf)
	if err == nil {
		t.Errorf("expected error for binary file, got nil")
	}
	if !errors.Is(err, ErrBinaryContent) {
		t.Errorf("expected ErrBinaryContent, got %v", err)
	}
}

// TestParseExtensionHookFieldsZeroValue — invariant prerequisite:
// even when YAML frontmatter contains keys that COLLIDE with the three
// extension-hook field names, the parser MUST leave the corresponding
// sql.NullString fields with Valid=false. / / Caronte
// are the authoritative writers.
func TestParseExtensionHookFieldsZeroValue(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "x.md")
	if err := os.WriteFile(p, []byte(`---
audit_chain_anchor: "should-not-leak"
ecosystem_join_keys: ["http://example.com"]
caronte_symbol_refs: ["foo"]
---

body`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.AuditChainAnchor.Valid {
		t.Errorf("AuditChainAnchor.Valid = true; parser must leave it NULL (inv-zen-130)")
	}
	if doc.EcosystemJoinKeys.Valid {
		t.Errorf("EcosystemJoinKeys.Valid = true; parser must leave it NULL (inv-zen-130)")
	}
	if doc.CaronteSymbolRefs.Valid {
		t.Errorf("CaronteSymbolRefs.Valid = true; parser must leave it NULL (inv-zen-130)")
	}
}

func TestParseHandoffFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "HANDOFF.md")
	body := "# Handoff\n\n## TL;DR\n\nthings\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeHandoff, ProjectID: "p", ProjectAlias: "internal-platform-x"}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.FileType != FileTypeHandoff {
		t.Errorf("FileType = %v, want FileTypeHandoff", doc.FileType)
	}
	if doc.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want internal-platform-x", doc.ProjectAlias)
	}
	if !strings.Contains(doc.ContentText, "TL;DR") {
		t.Errorf("ContentText missing TL;DR: %q", doc.ContentText)
	}
}

func TestParseFileMissingPath(t *testing.T) {
	sf := ScannedFile{Path: "/this/path/does/not/exist.md", Kind: FileTypeMemory}
	_, err := Parse(sf)
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
	if errors.Is(err, ErrBinaryContent) {
		t.Fatalf("missing-file error must NOT be ErrBinaryContent; got %v", err)
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist; got %v", err)
	}
}

func TestParseFileEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "empty.md")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.FrontmatterJSON != nil {
		t.Errorf("FrontmatterJSON = %s, want nil", doc.FrontmatterJSON)
	}
	if doc.ContentText != "" {
		t.Errorf("ContentText = %q, want empty", doc.ContentText)
	}
	if doc.Title != "empty" {
		t.Errorf("Title = %q, want %q (basename fallback)", doc.Title, "empty")
	}
}

func TestParseFileFrontmatterTitleNonString(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "weird-title.md")
	body := `---
title: [1, 2, 3]
---

# H

body
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "H" {
		t.Errorf("Title = %q, want %q (H1 fallback when frontmatter title is non-string)", doc.Title, "H")
	}
	if doc.FrontmatterJSON == nil {
		t.Errorf("FrontmatterJSON nil; expected populated (frontmatter is valid YAML, only title key has unexpected type)")
	}
}

// TestParseFileFrontmatterEmptyTitle — frontmatter parses, but
// `title:` is the empty string. Title MUST fall through to the H1
// fallback (frontmatter title only "wins" when non-empty).
func TestParseFileFrontmatterEmptyTitle(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "empty-title.md")
	body := `---
title: ""
---

# Real Title

body
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "Real Title" {
		t.Errorf("Title = %q, want %q (H1 fallback when frontmatter title is empty)", doc.Title, "Real Title")
	}
}

func TestParseFileCRLFLineEndings(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "crlf.md")
	body := "---\r\ntitle: \"CRLF Title\"\r\n---\r\n\r\n# H\r\n\r\nbody\r\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "CRLF Title" {
		t.Errorf("Title = %q, want %q (CRLF frontmatter delimiters)", doc.Title, "CRLF Title")
	}
	if doc.FrontmatterJSON == nil {
		t.Errorf("FrontmatterJSON nil; expected populated for CRLF frontmatter")
	}
}

func TestParseFileLastModifiedFromScannedFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "ts.md")
	if err := os.WriteFile(p, []byte("# T\n\nbody"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sf := ScannedFile{
		Path:    p,
		Kind:    FileTypeMemory,
		ModTime: fixed.UnixNano(),
	}
	before := time.Now().UTC()
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	after := time.Now().UTC()
	if !doc.LastModified.Equal(fixed) {
		t.Errorf("LastModified = %v, want %v (must come from ScannedFile.ModTime)", doc.LastModified, fixed)
	}
	if doc.LastIndexed.Before(before) || doc.LastIndexed.After(after) {
		t.Errorf("LastIndexed = %v, want in [%v, %v]", doc.LastIndexed, before, after)
	}
}

func TestParseFileLargeContent(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "big.md")

	var sb strings.Builder
	sb.WriteString("# Big\n\n")
	for i := 0; i < 8000; i++ {
		sb.WriteString("This is a paragraph of moderate size for FTS indexing.\n")
	}
	body := sb.String()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypePlan}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "Big" {
		t.Errorf("Title = %q, want %q", doc.Title, "Big")
	}
	if len(doc.ContentText) < 200_000 {
		t.Errorf("ContentText len = %d, want >= 200_000 (no truncation)", len(doc.ContentText))
	}
}

func TestParseFileFrontmatterEmptyMap(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "emptyfm.md")

	body := "---\n\n---\n\n# Title\n\nbody"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.FrontmatterJSON != nil {
		t.Errorf("FrontmatterJSON = %s, want nil for empty frontmatter map", doc.FrontmatterJSON)
	}
	if doc.Title != "Title" {
		t.Errorf("Title = %q, want %q", doc.Title, "Title")
	}
	if !strings.Contains(doc.ContentText, "body") {
		t.Errorf("ContentText missing body: %q", doc.ContentText)
	}
	// Frontmatter MUST be stripped — ContentText should not include the
	// `---` delimiters when the regex matched (even on the empty-map
	// fallback). This proves the body branch returns the stripped slice.
	if strings.HasPrefix(doc.ContentText, "---") {
		t.Errorf("ContentText leaked frontmatter delimiters: %q", doc.ContentText)
	}
}

func TestParseFileBinaryDetectionInvalidUTF8(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "badutf.md")

	bad := []byte{'a', 'b', 0xC0, 0x80, 'c', 'd', 0xFF, 0xFE, 'e'}
	if err := os.WriteFile(p, bad, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	_, err := Parse(sf)
	if !errors.Is(err, ErrBinaryContent) {
		t.Errorf("expected ErrBinaryContent for invalid UTF-8 head; got %v", err)
	}
}

func TestParseFileFrontmatterJSONUnmarshalable(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "inf.md")
	body := `---
ratio: .inf
---

# H

body`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	doc, err := Parse(sf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.FrontmatterJSON != nil {
		t.Errorf("FrontmatterJSON = %s, want nil (json.Marshal must reject +Inf)", doc.FrontmatterJSON)
	}
	if doc.Title != "H" {
		t.Errorf("Title = %q, want %q (H1 fallback)", doc.Title, "H")
	}
	if !strings.Contains(doc.ContentText, "body") {
		t.Errorf("ContentText missing body: %q", doc.ContentText)
	}
}

// TestParseFileNULBelowThreshold — a file with NUL bytes BELOW the 10%
// threshold MUST be accepted (the tolerance threshold rationale is
// documented in parser.go isBinary). Single stray NUL in 4KB of ASCII
// is a borderline fixture and must not false-positive.
func TestParseFileNULBelowThreshold(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "single-nul.md")

	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = 'a'
	}
	buf[100] = 0x00
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := ScannedFile{Path: p, Kind: FileTypeMemory}
	_, err := Parse(sf)
	if errors.Is(err, ErrBinaryContent) {
		t.Errorf("single NUL in 4KB ASCII triggered ErrBinaryContent; expected accept (1/4096 < 10%% threshold)")
	}
}
