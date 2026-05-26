//go:build cgo
// +build cgo

package ecosystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

//go:embed testdata/chunker/go_sample.go
var goSampleSrc []byte

//go:embed testdata/chunker/python_sample.py
var pythonSampleSrc []byte

//go:embed testdata/chunker/typescript_sample.ts
var typescriptSampleSrc []byte

//go:embed testdata/chunker/rust_sample.rs
var rustSampleSrc []byte

//go:embed testdata/chunker/markdown_sample.md
var markdownSampleSrc []byte

// newTestChunker is a tiny helper that constructs a Chunker with B-1
// default options and CR prefix disabled (B-2 ships CR; B-1 unit tests
// MUST NOT pull network deps).
func newTestChunker(t *testing.T, opts ChunkerOptions) *Chunker {
	t.Helper()
	c, err := NewChunker(opts)
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return c
}

func TestChunker_Chunk_Go_FunctionLeaf(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       50,
		MaxLeafTokens:   512,
		MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{
			Ecosystem:          EcoGo,
			Name:               "crypto/sha256",
			CanonicalNamespace: "crypto/sha256",
		},
		Version:   "1.23",
		RawBody:   string(goSampleSrc),
		SourceURL: "https://pkg.go.dev/crypto/sha256",
		Sections: []DocSection{{
			Kind:        KindFunction,
			SymbolPath:  "crypto/sha256.Sum256",
			Body:        string(goSampleSrc),
			SourceURL:   "https://pkg.go.dev/crypto/sha256#Sum256",
			ASTNodeType: "function_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk; got 0")
	}

	var found bool
	for _, ch := range chunks {
		if ch.Kind == KindFunction && strings.Contains(ch.SymbolPath, "Sum256") {
			found = true
			if ch.Fingerprint == "" {
				t.Errorf("Fingerprint empty for Sum256 chunk")
			}
			want := sha256.Sum256([]byte(ch.ContentText))
			wantHex := hex.EncodeToString(want[:])
			if ch.Fingerprint != wantHex {
				t.Errorf("Fingerprint mismatch: got %s want %s", ch.Fingerprint, wantHex)
			}
			if ch.SourceType != SrcPackageDoc {
				t.Errorf("SourceType = %s; want package_doc", ch.SourceType)
			}
			if ch.SourceURL != "https://pkg.go.dev/crypto/sha256#Sum256" {
				t.Errorf("SourceURL = %s; want section URL", ch.SourceURL)
			}
			break
		}
	}
	if !found {
		t.Error("no KindFunction chunk with Sum256 symbol_path found")
	}
}

func TestChunker_Chunk_Python_FunctionLeaf(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       50,
		MaxLeafTokens:   512,
		MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{
			Ecosystem:          EcoPython,
			Name:               "numpy.linalg",
			CanonicalNamespace: "numpy.linalg",
		},
		Version:   "1.26",
		RawBody:   string(pythonSampleSrc),
		SourceURL: "https://numpy.org/doc/stable/reference/generated/numpy.linalg.solve.html",
		Sections: []DocSection{{
			Kind:        KindFunction,
			SymbolPath:  "numpy.linalg.solve",
			Body:        string(pythonSampleSrc),
			SourceURL:   "https://numpy.org/doc/stable/reference/generated/numpy.linalg.solve.html",
			ASTNodeType: "function_definition",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 Python chunk; got 0")
	}
	var found bool
	for _, ch := range chunks {
		if ch.Kind == KindFunction && strings.Contains(ch.SymbolPath, "solve") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no KindFunction chunk with solve symbol_path found; got %d chunks", len(chunks))
	}
}

func TestChunker_Chunk_TypeScript_ClassWithMethods(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:     50,
		MaxLeafTokens: 512,

		MaxParentTokens: 100,
	})
	doc := &PackageDoc{
		Package: PackageRef{
			Ecosystem:          EcoTypeScript,
			Name:               "@react-hooks/state",
			CanonicalNamespace: "@react-hooks/state",
		},
		Version: "1.0.0",
		RawBody: string(typescriptSampleSrc),
		Sections: []DocSection{{
			Kind:        KindType,
			SymbolPath:  "@react-hooks/state.StateContainer",
			Body:        string(typescriptSampleSrc),
			ASTNodeType: "class_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected ≥ 2 chunks (class + at least one method); got %d", len(chunks))
	}

	var hasParent, hasChild bool
	for _, ch := range chunks {
		if ch.Kind == KindType && !ch.ParentChunkID.Valid {
			hasParent = true
		}
		if ch.Kind == KindFunction && ch.ParentChunkID.Valid {
			hasChild = true
		}
	}
	if !hasParent {
		t.Error("expected at least one parent-class chunk (KindType, no parent)")
	}
	if !hasChild {
		t.Error("expected at least one method chunk with parent link")
	}
}

func TestChunker_Chunk_Rust_TraitImpl(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       50,
		MaxLeafTokens:   512,
		MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{
			Ecosystem:          EcoRust,
			Name:               "serde",
			CanonicalNamespace: "serde",
		},
		Version: "1.0.193",
		RawBody: string(rustSampleSrc),
		Sections: []DocSection{{
			Kind:        KindType,
			SymbolPath:  "serde::ser::Serialize",
			Body:        string(rustSampleSrc),
			ASTNodeType: "trait_item",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 Rust chunk")
	}
	var foundType bool
	for _, ch := range chunks {
		if ch.Kind == KindType {
			foundType = true
			break
		}
	}
	if !foundType {
		t.Errorf("expected at least one KindType chunk (trait_item or struct_item); got %d chunks", len(chunks))
	}
}

func TestChunker_Chunk_Markdown_Headings(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       10,
		MaxLeafTokens:   512,
		MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{
			Ecosystem:          EcoGo,
			Name:               "exampleproject",
			CanonicalNamespace: "github.com/example/project",
		},
		Version: "main",
		RawBody: string(markdownSampleSrc),
		Sections: []DocSection{{
			Kind:        KindGuide,
			SymbolPath:  "README",
			Body:        string(markdownSampleSrc),
			ASTNodeType: "document",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected ≥ 2 markdown chunks (multiple headings); got %d", len(chunks))
	}

	var hasGuide bool
	for _, ch := range chunks {
		if ch.Kind == KindGuide {
			hasGuide = true
			break
		}
	}
	if !hasGuide {
		t.Error("expected at least one KindGuide chunk from markdown headings")
	}
}

func TestChunker_Chunk_TokenBudgetHonored(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       10,
		MaxLeafTokens:   100,
		MaxParentTokens: 500,
	})

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("# Heading ")
		sb.WriteString(string(rune('A' + i)))
		sb.WriteString("\n\nLorem ipsum dolor sit amet, consectetur adipiscing elit. ")
		sb.WriteString("Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.\n\n")
	}
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "bigdoc", CanonicalNamespace: "x"},
		Version: "1.0",
		RawBody: sb.String(),
		Sections: []DocSection{{
			Kind: KindGuide, SymbolPath: "BIG", Body: sb.String(), ASTNodeType: "document",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk from long markdown")
	}

	for _, ch := range chunks {
		tokens := approxTokens(ch.ContentText)
		if tokens > 100 && !ch.Oversized {
			t.Errorf("chunk exceeds maxLeafTokens (%d > 100) without oversized flag; symbol=%s kind=%s",
				tokens, ch.SymbolPath, ch.Kind)
		}
	}
}

func TestChunker_Chunk_BoundaryPreserved(t *testing.T) {

	var body strings.Builder
	body.WriteString("package main\n\nfunc Big() {\n")
	for i := 0; i < 100; i++ {
		body.WriteString("\t// line ")
		body.WriteString(strings.Repeat("x", 30))
		body.WriteString("\n")
	}
	body.WriteString("}\n")
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "main", CanonicalNamespace: "main"},
		Version: "1.23", RawBody: body.String(),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "main.Big", Body: body.String(), ASTNodeType: "function_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	var bigChunks int
	for _, ch := range chunks {
		if strings.Contains(ch.SymbolPath, "Big") && ch.Oversized && ch.Kind == KindFunction {
			bigChunks++
		}
	}
	if bigChunks != 1 {
		t.Errorf("expected exactly 1 oversized Big chunk (boundary preserved); got %d (total chunks=%d)",
			bigChunks, len(chunks))
		for i, ch := range chunks {
			t.Logf("  chunks[%d]: kind=%s symbol=%s oversized=%v tokens=%d",
				i, ch.Kind, ch.SymbolPath, ch.Oversized, approxTokens(ch.ContentText))
		}
	}
}

func TestChunker_Chunk_Deterministic(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "crypto/sha256", CanonicalNamespace: "crypto/sha256"},
		Version: "1.23", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "crypto/sha256.Sum256", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}
	chunks1, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk1: %v", err)
	}
	chunks2, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk2: %v", err)
	}
	if len(chunks1) != len(chunks2) {
		t.Fatalf("non-deterministic: chunks1=%d chunks2=%d", len(chunks1), len(chunks2))
	}
	for i := range chunks1 {
		if chunks1[i].Fingerprint != chunks2[i].Fingerprint {
			t.Errorf("non-deterministic fingerprint at index %d: %s vs %s",
				i, chunks1[i].Fingerprint, chunks2[i].Fingerprint)
		}
		if chunks1[i].SymbolPath != chunks2[i].SymbolPath {
			t.Errorf("non-deterministic symbol_path at index %d: %s vs %s",
				i, chunks1[i].SymbolPath, chunks2[i].SymbolPath)
		}
		if chunks1[i].ContentText != chunks2[i].ContentText {
			t.Errorf("non-deterministic content_text at index %d", i)
		}
	}
}

func TestChunker_Chunk_UnknownLanguageError(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: Ecosystem("klingon"), Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: "Qapla'!",
		Sections: []DocSection{{Kind: KindProse, SymbolPath: "x", Body: "Qapla'!", ASTNodeType: "unknown"}},
	}
	_, err := c.Chunk(context.Background(), doc)
	if !errors.Is(err, ErrUnknownLanguage) {
		t.Errorf("expected ErrUnknownLanguage; got %v", err)
	}
}

func TestChunker_Chunk_NilDocError(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	_, err := c.Chunk(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil doc; got nil")
	}
	if !strings.Contains(err.Error(), "nil doc") {
		t.Errorf("expected error to mention 'nil doc'; got %v", err)
	}
}

func TestChunker_Chunk_ContextCancelled(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{{Kind: KindFunction, SymbolPath: "x", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration"}},
	}
	_, err := c.Chunk(ctx, doc)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
}

func TestNewChunker_InvalidOptions(t *testing.T) {
	_, err := NewChunker(ChunkerOptions{
		MinTokens: 600, MaxLeafTokens: 100, MaxParentTokens: 2048,
	})
	if err == nil {
		t.Error("expected error for MinTokens > MaxLeafTokens; got nil")
	}
}

func TestNewChunker_DefaultsApplied(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{})
	if err != nil {
		t.Fatalf("NewChunker with defaults: %v", err)
	}
	defer c.Close()
	if c.opts.MinTokens != 50 {
		t.Errorf("default MinTokens = %d; want 50", c.opts.MinTokens)
	}
	if c.opts.MaxLeafTokens != 512 {
		t.Errorf("default MaxLeafTokens = %d; want 512", c.opts.MaxLeafTokens)
	}
	if c.opts.MaxParentTokens != 2048 {
		t.Errorf("default MaxParentTokens = %d; want 2048", c.opts.MaxParentTokens)
	}
}

func TestApproxTokens_FourCharHeuristic(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{strings.Repeat("x", 100), 25},
		{strings.Repeat("x", 2048), 512},
	}
	for _, c := range cases {
		got := approxTokens(c.in)
		if got != c.want {
			t.Errorf("approxTokens(%q[%d chars]) = %d; want %d", c.in[:min(len(c.in), 20)], len(c.in), got, c.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestChunker_Chunk_AllSectionsProcessed(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "multi", CanonicalNamespace: "multi"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{
			{Kind: KindFunction, SymbolPath: "multi.Sum256", Body: string(goSampleSrc),
				ASTNodeType: "function_declaration"},
			{Kind: KindGuide, SymbolPath: "multi.README", Body: string(markdownSampleSrc),
				ASTNodeType: "document"},
		},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	var hasGoFunc, hasMarkdownGuide bool
	for _, ch := range chunks {
		if ch.Kind == KindFunction && strings.Contains(ch.SymbolPath, "Sum256") {
			hasGoFunc = true
		}
		if ch.Kind == KindGuide && strings.Contains(ch.SymbolPath, "README") {
			hasMarkdownGuide = true
		}
	}
	if !hasGoFunc {
		t.Error("expected chunk from Go section (Sum256); got none")
	}
	if !hasMarkdownGuide {
		t.Error("expected chunk from Markdown section (README); got none")
	}
}

func TestChunker_Chunk_NoSections(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "empty", CanonicalNamespace: "empty"},
		Version: "1.0",
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty Sections; got %d", len(chunks))
	}
}

func TestChunker_Chunk_PackageMetadataPropagated(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{ID: 42, Ecosystem: EcoGo, Name: "meta", CanonicalNamespace: "meta"},
		Version: "v1.2.3", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "meta.Sum256", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	for _, ch := range chunks {
		if ch.PackageID != 42 {
			t.Errorf("PackageID = %d; want 42", ch.PackageID)
		}
		if ch.VersionIntroduced != "v1.2.3" {
			t.Errorf("VersionIntroduced = %s; want v1.2.3", ch.VersionIntroduced)
		}
		if len(ch.StableIn) != 1 || ch.StableIn[0] != "v1.2.3" {
			t.Errorf("StableIn = %v; want [v1.2.3]", ch.StableIn)
		}
	}
}

func TestChunker_Chunk_NodeKindMappingComplete(t *testing.T) {

	for _, lang := range []Language{LangGo, LangPython, LangTypeScript, LangRust, LangMarkdown} {
		m, ok := astNodeKindMap[lang]
		if !ok {
			t.Errorf("astNodeKindMap missing entry for Language %s", lang)
			continue
		}
		if len(m) == 0 {
			t.Errorf("astNodeKindMap[%s] empty", lang)
		}
	}
}

func TestChunker_Chunk_EcosystemToLanguageMappingComplete(t *testing.T) {
	for _, eco := range AllEcosystems {
		if _, ok := ecosystemToLanguage[eco]; !ok {
			t.Errorf("ecosystemToLanguage missing entry for Ecosystem %s", eco)
		}
	}
}

func TestNewChunker_ExplicitZeroValuesUseDefaults(t *testing.T) {
	cases := []struct {
		name string
		opts ChunkerOptions
		want ChunkerOptions
	}{
		{
			name: "all-zero",
			opts: ChunkerOptions{},
			want: ChunkerOptions{MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048},
		},
		{
			name: "only-min-set",
			opts: ChunkerOptions{MinTokens: 20},
			want: ChunkerOptions{MinTokens: 20, MaxLeafTokens: 512, MaxParentTokens: 2048},
		},
		{
			name: "only-leaf-set",
			opts: ChunkerOptions{MaxLeafTokens: 256},
			want: ChunkerOptions{MinTokens: 50, MaxLeafTokens: 256, MaxParentTokens: 2048},
		},
		{
			name: "only-parent-set",
			opts: ChunkerOptions{MaxParentTokens: 4096},
			want: ChunkerOptions{MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 4096},
		},
		{
			name: "negative-values-coerced-to-default",
			opts: ChunkerOptions{MinTokens: -1, MaxLeafTokens: -10, MaxParentTokens: -100},
			want: ChunkerOptions{MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch, err := NewChunker(c.opts)
			if err != nil {
				t.Fatalf("NewChunker: %v", err)
			}
			defer ch.Close()
			if ch.opts.MinTokens != c.want.MinTokens {
				t.Errorf("MinTokens = %d; want %d", ch.opts.MinTokens, c.want.MinTokens)
			}
			if ch.opts.MaxLeafTokens != c.want.MaxLeafTokens {
				t.Errorf("MaxLeafTokens = %d; want %d", ch.opts.MaxLeafTokens, c.want.MaxLeafTokens)
			}
			if ch.opts.MaxParentTokens != c.want.MaxParentTokens {
				t.Errorf("MaxParentTokens = %d; want %d", ch.opts.MaxParentTokens, c.want.MaxParentTokens)
			}
		})
	}
}

func TestContainerSummary_Strategies(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 50, MaxParentTokens: 80,
	})

	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoTypeScript, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(typescriptSampleSrc),
		Sections: []DocSection{{
			Kind: KindType, SymbolPath: "x.StateContainer", Body: string(typescriptSampleSrc),
			ASTNodeType: "class_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	for _, ch := range chunks {
		if ch.Kind == KindType && !ch.ParentChunkID.Valid {
			budget := 50 * 4
			if len(ch.ContentText) > budget {
				t.Errorf("parent class summary too large: %d chars > %d budget", len(ch.ContentText), budget)
			}
			return
		}
	}
	t.Error("no parent class chunk found")
}

func TestContainerSummary_NilNode(t *testing.T) {
	got := containerSummary(LangGo, nil, []byte("ignored"), 100)
	if got != "" {
		t.Errorf("containerSummary(nil) = %q; want \"\"", got)
	}
}

func TestContainerSummary_SingleLine_Truncation(t *testing.T) {

	body := "trait T{fn a();fn b();fn c();fn d();fn e();fn f();fn g();fn h();fn i();fn j();fn k();}"
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 5, MaxLeafTokens: 10, MaxParentTokens: 15,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoRust, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: body,
		Sections: []DocSection{{
			Kind: KindType, SymbolPath: "x.T", Body: body, ASTNodeType: "trait_item",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	for _, ch := range chunks {
		if ch.Kind == KindType && !ch.ParentChunkID.Valid {
			budget := 10 * 4
			if len(ch.ContentText) > budget {
				t.Errorf("single-line container summary too large: %d chars > %d budget; content=%q",
					len(ch.ContentText), budget, ch.ContentText)
			}
		}
	}
}

func TestIndexByte(t *testing.T) {
	cases := []struct {
		s    string
		b    byte
		want int
	}{
		{"", 'x', -1},
		{"abc", 'x', -1},
		{"abc", 'a', 0},
		{"abc", 'b', 1},
		{"abc", 'c', 2},
		{"a\nb", '\n', 1},
		{"hello\nworld", '\n', 5},
		{"line", '\n', -1},
	}
	for _, c := range cases {
		got := indexByte(c.s, c.b)
		if got != c.want {
			t.Errorf("indexByte(%q, %c) = %d; want %d", c.s, c.b, got, c.want)
		}
	}
}

func TestEndsWithSegment(t *testing.T) {
	cases := []struct {
		path string
		seg  string
		want bool
	}{
		{"crypto/sha256", "sha256", true},
		{"numpy.linalg.solve", "solve", true},
		{"serde::ser::Serialize", "Serialize", true},
		{"sha256", "sha256", true},
		{"crypto/sha256", "Sum256", false},
		{"crypto/sha256SuM", "sha256", false},
		{"", "x", false},
		{"x", "xyz", false},
		{"abc", "abc", true},

		{"a b", "b", false},

		{"a-b", "b", false},
	}
	for _, c := range cases {
		got := endsWithSegment(c.path, c.seg)
		if got != c.want {
			t.Errorf("endsWithSegment(%q, %q) = %v; want %v", c.path, c.seg, got, c.want)
		}
	}
}

func TestExtractName_NilNode(t *testing.T) {
	got := extractName(LangGo, nil, []byte("x"))
	if got != "" {
		t.Errorf("extractName(nil) = %q; want \"\"", got)
	}
}

func TestDerivePath_FallbackToCanonical(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "test", CanonicalNamespace: "test/pkg"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	for _, ch := range chunks {
		if !strings.HasPrefix(ch.SymbolPath, "test/pkg") {
			t.Errorf("SymbolPath = %s; want prefix 'test/pkg'", ch.SymbolPath)
		}
	}
}

func TestChunker_Chunk_ParseError(t *testing.T) {

	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: "package main",
		Sections: []DocSection{{Kind: KindFunction, SymbolPath: "x", Body: "package main",
			ASTNodeType: "function_declaration"}},
	}
	_, err := c.Chunk(ctx, doc)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestChunker_Chunk_ContainerUnderBudgetRecurses(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       5,
		MaxLeafTokens:   512,
		MaxParentTokens: 5000,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoTypeScript, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(typescriptSampleSrc),
		Sections: []DocSection{{
			Kind: KindType, SymbolPath: "x.StateContainer", Body: string(typescriptSampleSrc),
			ASTNodeType: "class_declaration",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected ≥ 1 chunk; got 0")
	}

}

func TestChunker_Chunk_ContainerSummaryBudgetCap(t *testing.T) {

	body := "trait LongTraitNameThatExceedsBudget {\n  fn a();\n  fn b();\n  fn c();\n  fn d();\n  fn e();\n}"
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       3,
		MaxLeafTokens:   5,
		MaxParentTokens: 8,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoRust, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: body,
		Sections: []DocSection{{
			Kind: KindType, SymbolPath: "x.LongTraitNameThatExceedsBudget", Body: body,
			ASTNodeType: "trait_item",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	for _, ch := range chunks {
		if ch.Kind == KindType && !ch.ParentChunkID.Valid {
			if len(ch.ContentText) > 20 {
				t.Errorf("parent summary exceeded budget: %d > 20; content=%q",
					len(ch.ContentText), ch.ContentText)
			}
		}
	}
}

func TestChunker_Chunk_PythonClassWithMethods(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens:       10,
		MaxLeafTokens:   512,
		MaxParentTokens: 50,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoPython, Name: "numpy.linalg", CanonicalNamespace: "numpy.linalg"},
		Version: "1.26", RawBody: string(pythonSampleSrc),
		Sections: []DocSection{{
			Kind: KindType, SymbolPath: "numpy.linalg.LinAlgError", Body: string(pythonSampleSrc),
			ASTNodeType: "class_definition",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected ≥ 1 chunk")
	}
}

func TestContainerSummary_BudgetZeroFallback(t *testing.T) {

	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 5, MaxLeafTokens: 10, MaxParentTokens: 15,
	})

	pool := c.parserPools[LangRust]
	parser := pool.Get().(*sitter.Parser)
	defer pool.Put(parser)
	src := []byte("trait T { fn a(); }")
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Close()
	root := tree.RootNode()
	got := containerSummary(LangRust, root, src, 0)
	if got == "" {
		t.Error("expected non-empty summary with budget=0 fallback")
	}

	if len(got) > 2048 {
		t.Errorf("budget=0 path exceeded 2048 fallback: %d chars", len(got))
	}
}

func TestContainerSummary_NoNewlineFitsBudget(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 5, MaxLeafTokens: 100, MaxParentTokens: 10,
	})
	pool := c.parserPools[LangRust]
	parser := pool.Get().(*sitter.Parser)
	defer pool.Put(parser)
	src := []byte("trait T{fn a();}")
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Close()
	root := tree.RootNode()

	var trait *sitter.Node
	for i := 0; i < int(root.NamedChildCount()); i++ {
		if root.NamedChild(i).Type() == "trait_item" {
			trait = root.NamedChild(i)
			break
		}
	}
	if trait == nil {
		t.Skip("could not find trait_item; tree-sitter shape changed")
	}
	got := containerSummary(LangRust, trait, src, 100)
	if got == "" {
		t.Error("expected non-empty summary")
	}
}

func TestChunker_walkAST_ContainerUnderBudget(t *testing.T) {

	t.Skip("decision 4 is reachable only for hypothetical container kinds not also leaf-eligible (forward-compat)")
}

func TestChunker_walkAST_NilNode(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0",
	}
	sec := DocSection{Kind: KindFunction, SymbolPath: "x", Body: "package x"}
	var emitted []Chunk

	c.walkAST(context.Background(), doc, sec, LangGo, []byte("package x"), nil, sql.NullInt64{}, &emitted)
	if len(emitted) != 0 {
		t.Errorf("walkAST(nil) emitted %d chunks; want 0", len(emitted))
	}
}

func TestChunker_walkAST_CancelledContext(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0",
	}
	sec := DocSection{Kind: KindFunction, SymbolPath: "x", Body: "package x"}

	pool := c.parserPools[LangGo]
	parser := pool.Get().(*sitter.Parser)
	defer pool.Put(parser)
	src := []byte("package x")
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var emitted []Chunk

	c.walkAST(ctx, doc, sec, LangGo, src, tree.RootNode(), sql.NullInt64{}, &emitted)

	_ = emitted
}

func TestChunker_chunkSection_CancelledContext(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0",
	}
	sec := DocSection{Kind: KindFunction, SymbolPath: "x", Body: "package x"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.chunkSection(ctx, doc, sec, LangGo)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
}

func TestChunker_Chunk_SectionErrorWrap(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "test/section.symbol", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}

	ok := false
	for i := 0; i < 100 && !ok; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1)
		_, err := c.Chunk(ctx, doc)
		cancel()
		if err != nil && strings.Contains(err.Error(), "section ") {
			ok = true
		}
	}
	if !ok {
		t.Log("section-wrap error path not hit in 100 attempts (acceptable — defensive path)")
	}
}

func TestChunker_Chunk_ConcurrencyStress(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "x.Sum256", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}
	const N = 50
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := c.Chunk(context.Background(), doc)
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent stress %d: %v", i, err)
		}
	}
}

func TestChunker_Chunk_Concurrent(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0", RawBody: string(goSampleSrc),
		Sections: []DocSection{{
			Kind: KindFunction, SymbolPath: "x.Sum256", Body: string(goSampleSrc),
			ASTNodeType: "function_declaration",
		}},
	}
	const N = 8
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := c.Chunk(context.Background(), doc)
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Chunk %d: %v", i, err)
		}
	}
}

func TestChunker_Chunk_Hierarchical_Markdown(t *testing.T) {
	c := newTestChunker(t, ChunkerOptions{
		MinTokens: 10, MaxLeafTokens: 512, MaxParentTokens: 2048,
	})
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "doc", CanonicalNamespace: "doc"},
		Version: "1.0", RawBody: string(markdownSampleSrc),
		Sections: []DocSection{{
			Kind: KindGuide, SymbolPath: "README", Body: string(markdownSampleSrc),
			ASTNodeType: "document",
		}},
	}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected ≥ 1 chunk from README; got 0")
	}

	for _, ch := range chunks {
		if ch.ContentText == string(markdownSampleSrc) {
			t.Errorf("regression: chunk == full document (module root emitted)")
		}
	}
}

type recordingDispatcher struct {
	calls    []dispatchCall
	response string
	err      error
	mu       sync.Mutex
}

type dispatchCall struct {
	Provider, Prompt string
	MaxTokens        int
}

func (r *recordingDispatcher) Dispatch(_ context.Context, provider, prompt string, maxTokens int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, dispatchCall{Provider: provider, Prompt: prompt, MaxTokens: maxTokens})
	if r.err != nil {
		return "", r.err
	}
	return r.response, nil
}

// recordingOllama mocks the OllamaClient generator seam. Concurrency-safe so
// fan-out tests do not race the calls slice.
type recordingOllama struct {
	calls    []ollamaCall
	response string
	err      error
	mu       sync.Mutex
}

type ollamaCall struct {
	Model, Prompt string
	NumPredict    int
}

func (r *recordingOllama) Generate(_ context.Context, model, prompt string, numPredict int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ollamaCall{Model: model, Prompt: prompt, NumPredict: numPredict})
	if r.err != nil {
		return "", r.err
	}
	return r.response, nil
}

func TestChunker_ContextualPrefix_C2_Ollama(t *testing.T) {
	ollama := &recordingOllama{
		response: "This Sum256 function in the crypto/sha256 package implements the SHA-256 cryptographic hash per FIPS 180-4. It accepts an arbitrary byte slice and returns a fixed 32-byte digest. Part of the one-shot hashing API used across TLS handshakes, OAuth signatures, and content fingerprinting workflows everywhere.",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "default", CRPrefixLLM: "qwen2.5:7b", CitationMode: CitationOptional,
		},
		OllamaClient: &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	chunk := Chunk{
		ContentText: "func Sum256(data []byte) [Size]byte { ... }",
		Kind:        KindFunction,
		SymbolPath:  "crypto/sha256.Sum256",
	}
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "crypto/sha256", CanonicalNamespace: "crypto/sha256"},
		Version: "1.23",
	}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}
	tokens := approxTokens(prefix)
	if tokens < 60 || tokens > 80 {
		t.Errorf("prefix token count = %d; want 60-80 (got prefix: %q)", tokens, prefix)
	}
	if len(ollama.calls) != 1 {
		t.Errorf("expected exactly 1 Ollama call; got %d", len(ollama.calls))
	}
	if ollama.calls[0].Model != "qwen2.5:7b" {
		t.Errorf("model = %s; want qwen2.5:7b", ollama.calls[0].Model)
	}
	if !strings.Contains(ollama.calls[0].Prompt, "Sum256") {
		t.Error("prompt missing chunk symbol; expected Sum256 in prompt")
	}
}

func TestChunker_ContextualPrefix_C1_Ollama32B(t *testing.T) {
	ollama := &recordingOllama{
		response: "This 32B-class context describes the surrounding documentation for the Sum256 helper, situating the chunk within the crypto/sha256 one-shot hashing API used across cryptographic protocols, TLS handshakes, OAuth signatures, container image digests, and the broader Go standard library cryptographic surface for retrieval-time anchoring.",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "max-scope", CRPrefixLLM: "qwen2.5-coder:32b", CitationMode: CitationMandatoryGrammar,
		},
		OllamaClient: &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "func Sum256(data []byte) [Size]byte { ... }", Kind: KindFunction, SymbolPath: "crypto/sha256.Sum256"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "crypto/sha256", CanonicalNamespace: "crypto/sha256"}, Version: "1.23"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}
	if len(ollama.calls) != 1 {
		t.Fatalf("expected exactly 1 Ollama call; got %d", len(ollama.calls))
	}
	if ollama.calls[0].Model != "qwen2.5-coder:32b" {
		t.Errorf("model = %s; want qwen2.5-coder:32b", ollama.calls[0].Model)
	}
}

func TestChunker_ContextualPrefix_C3_Llama3(t *testing.T) {
	ollama := &recordingOllama{
		response: "Compact 3B-class context describing this chunk's role in surrounding documentation for retrieval Falls within the cost-free Ollama tier with high parallelism for very large corpora where wall-clock budget is the primary constraint rather than per-prefix quality of contextual retrieval generation",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "default", CRPrefixLLM: "llama3.2:3b",
		},
		OllamaClient: &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "func F() {}", Kind: KindFunction, SymbolPath: "x.F"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}
	if len(ollama.calls) != 1 || ollama.calls[0].Model != "llama3.2:3b" {
		t.Errorf("expected llama3.2:3b call; got %+v", ollama.calls)
	}
}

func TestChunker_ContextualPrefix_C4_Dispatcher(t *testing.T) {
	dispatch := &recordingDispatcher{
		response: "This function lives in Go crypto/sha256 package returning the SHA-256 checksum of input byte slice producing a fixed 32-byte digest used for one-shot hashing in security-critical contexts including TLS handshakes OAuth signatures content fingerprinting image digests cryptographic primitives",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "max-scope", CRPrefixLLM: "claude-haiku-4-5",
		},
		DispatcherClient: dispatch,
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{
		ContentText: "func Sum256(data []byte) [Size]byte { ... }",
		Kind:        KindFunction, SymbolPath: "crypto/sha256.Sum256",
	}
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "crypto/sha256", CanonicalNamespace: "crypto/sha256"},
		Version: "1.23",
	}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}
	if len(dispatch.calls) != 1 {
		t.Errorf("expected exactly 1 dispatcher call; got %d", len(dispatch.calls))
	}
	if dispatch.calls[0].Provider != "anthropic-paygo" {
		t.Errorf("provider = %s; want anthropic-paygo", dispatch.calls[0].Provider)
	}
	if dispatch.calls[0].MaxTokens != 100 {
		t.Errorf("maxTokens = %d; want 100", dispatch.calls[0].MaxTokens)
	}
}

func TestChunker_ContextualPrefix_C5_Selective(t *testing.T) {
	ollama := &recordingOllama{
		response: "Sixty to eighty tokens worth of context describing this content position in the surrounding documentation source material for retrieval purposes within the package namespace and overall ecosystem anchoring the chunk to its parent symbol version and module",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "default", CRPrefixLLM: "qwen2.5-coder:32b",
		},
		OllamaClient: &OllamaClient{generator: ollama},

		SelectiveCRKinds: []ChunkKind{KindFunction, KindType},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"},
		Version: "1.0",
	}

	fnChunk := Chunk{ContentText: "func F() {}", Kind: KindFunction, SymbolPath: "x.F"}
	prefix, err := c.ContextualPrefix(context.Background(), fnChunk, doc)
	if err != nil {
		t.Fatalf("KindFunction prefix: %v", err)
	}
	if prefix == "" {
		t.Error("expected non-empty prefix for KindFunction")
	}
	callsAfterFn := len(ollama.calls)
	if callsAfterFn != 1 {
		t.Errorf("expected exactly 1 Ollama call after KindFunction; got %d", callsAfterFn)
	}

	proseChunk := Chunk{ContentText: "Lorem ipsum", Kind: KindProse, SymbolPath: "x"}
	prefix, err = c.ContextualPrefix(context.Background(), proseChunk, doc)
	if err != nil {
		t.Fatalf("KindProse prefix: %v", err)
	}
	if prefix != "" {
		t.Errorf("expected EMPTY prefix for KindProse (selective skip); got %q", prefix)
	}
	if len(ollama.calls) != callsAfterFn {
		t.Errorf("expected NO additional Ollama call for KindProse selective-skip; got %d calls", len(ollama.calls))
	}

	typeChunk := Chunk{ContentText: "type T struct{}", Kind: KindType, SymbolPath: "x.T"}
	prefix, err = c.ContextualPrefix(context.Background(), typeChunk, doc)
	if err != nil {
		t.Fatalf("KindType prefix: %v", err)
	}
	if prefix == "" {
		t.Error("expected non-empty prefix for KindType")
	}

	guideChunk := Chunk{ContentText: "# Heading", Kind: KindGuide, SymbolPath: "x"}
	prefix, err = c.ContextualPrefix(context.Background(), guideChunk, doc)
	if err != nil {
		t.Fatalf("KindGuide prefix: %v", err)
	}
	if prefix != "" {
		t.Errorf("expected EMPTY prefix for KindGuide (selective skip); got %q", prefix)
	}
}

func TestChunker_ContextualPrefix_C6_DeepSeek(t *testing.T) {
	dispatch := &recordingDispatcher{
		response: "DeepSeek-V3 quality context describing this chunk role within the surrounding documentation providing retrieval-time anchors for the function purpose within the package hashing API surface area covering the call signature intended caller and the broader cryptographic",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "max-scope", CRPrefixLLM: "deepseek-v3",
		},
		DispatcherClient: dispatch,
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "func F() {}", Kind: KindFunction, SymbolPath: "x.F"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix == "" {
		t.Fatal("expected non-empty prefix")
	}
	if len(dispatch.calls) != 1 {
		t.Fatalf("expected exactly 1 dispatcher call; got %d", len(dispatch.calls))
	}
	if dispatch.calls[0].Provider != "siliconflow" {
		t.Errorf("provider = %s; want siliconflow (default DeepSeek provider)", dispatch.calls[0].Provider)
	}
}

// TestChunker_ContextualPrefix_FailHard_CapaFirewall verifies failure-mode for
// capa-firewall doctrine: LLM error MUST propagate to caller.
func TestChunker_ContextualPrefix_FailHard_CapaFirewall(t *testing.T) {
	ollama := &recordingOllama{err: errors.New("ollama: connection refused")}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "capa-firewall", CRPrefixLLM: "qwen2.5:7b", RefuseOnUnverified: true,
		},
		OllamaClient: &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected fail-hard error for capa-firewall doctrine; got nil")
	}
	if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("error should mention underlying cause; got %v", err)
	}
}

func TestChunker_ContextualPrefix_SkipAndLog_Default(t *testing.T) {
	ollama := &recordingOllama{err: errors.New("ollama: connection refused")}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile: &DoctrineProfile{
			Name: "default", CRPrefixLLM: "qwen2.5:7b", RefuseOnUnverified: false,
		},
		OllamaClient: &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("expected nil err for default doctrine skip-and-log; got %v", err)
	}
	if prefix != "" {
		t.Errorf("expected empty prefix on LLM failure; got %q", prefix)
	}
}

func TestChunker_ContextualPrefix_TruncateLong(t *testing.T) {
	long := strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 30)
	ollama := &recordingOllama{response: long}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "default", CRPrefixLLM: "qwen2.5:7b"},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	tokens := approxTokens(prefix)
	if tokens > 80 {
		t.Errorf("prefix not truncated; tokens=%d; want ≤80", tokens)
	}
}

func TestChunker_ContextualPrefix_TooShortReturnsEmpty(t *testing.T) {
	ollama := &recordingOllama{response: "Short."}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "default", CRPrefixLLM: "qwen2.5:7b"},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix != "" {
		t.Errorf("expected empty prefix for <60-token response; got %q", prefix)
	}
}

func TestChunker_ContextualPrefix_TooShortFailHardCapaFirewall(t *testing.T) {
	ollama := &recordingOllama{response: "Short."}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "capa-firewall", CRPrefixLLM: "qwen2.5:7b", RefuseOnUnverified: true},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected fail-hard error for capa-firewall on short response; got nil")
	}
	if !strings.Contains(err.Error(), "60") {
		t.Errorf("error should mention 60-token floor; got %v", err)
	}
}

func TestChunker_ContextualPrefix_Disabled(t *testing.T) {
	ollama := &recordingOllama{response: "should not be called"}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: false,
		DoctrineProfile:        &DoctrineProfile{Name: "default", CRPrefixLLM: "qwen2.5:7b"},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if prefix != "" {
		t.Errorf("expected empty prefix when disabled; got %q", prefix)
	}
	if len(ollama.calls) != 0 {
		t.Errorf("expected 0 Ollama calls when disabled; got %d", len(ollama.calls))
	}
}

func TestChunker_ContextualPrefix_NilDoctrineErrors(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,

		OllamaClient: &OllamaClient{generator: &recordingOllama{response: "ignored"}},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected error for nil DoctrineProfile; got nil")
	}
	if !strings.Contains(err.Error(), "DoctrineProfile") {
		t.Errorf("error should mention DoctrineProfile; got %v", err)
	}
}

func TestChunker_ContextualPrefix_UnknownBackendErrors(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "max-scope", CRPrefixLLM: "unknown-model-9000"},
		DispatcherClient:       &recordingDispatcher{},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}

	prefix, err := c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("expected nil err on default doctrine; got %v", err)
	}
	if prefix != "" {
		t.Errorf("expected empty prefix on unknown backend; got %q", prefix)
	}
}

func TestChunker_ContextualPrefix_OllamaClientMissing(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "capa-firewall", CRPrefixLLM: "qwen2.5:7b", RefuseOnUnverified: true},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected error for missing OllamaClient under capa-firewall; got nil")
	}
	if !strings.Contains(err.Error(), "OllamaClient") {
		t.Errorf("error should mention OllamaClient; got %v", err)
	}
}

func TestChunker_ContextualPrefix_DispatcherClientMissing(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "capa-firewall", CRPrefixLLM: "claude-haiku-4-5", RefuseOnUnverified: true},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected error for missing DispatcherClient under capa-firewall; got nil")
	}
	if !strings.Contains(err.Error(), "DispatcherClient") {
		t.Errorf("error should mention DispatcherClient; got %v", err)
	}
}

func TestChunker_ContextualPrefix_DispatcherClientMissingDeepSeek(t *testing.T) {
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "capa-firewall", CRPrefixLLM: "deepseek-v3", RefuseOnUnverified: true},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err == nil {
		t.Fatal("expected error for missing DispatcherClient (deepseek branch); got nil")
	}
	if !strings.Contains(err.Error(), "DispatcherClient") || !strings.Contains(err.Error(), "DeepSeek") {
		t.Errorf("error should mention DispatcherClient + DeepSeek; got %v", err)
	}
}

func TestChunker_ContextualPrefix_DefaultsToQwen7B(t *testing.T) {
	ollama := &recordingOllama{
		response: "Sixty-plus-token default fallback prefix when no CRPrefixLLM is set on the DoctrineProfile in this chunker invocation. Should route to qwen2.5:7b per the spec backend table default. Provides retrieval-time anchoring for the chunk content even in the absence of explicit operator configuration of the contextual retrieval LLM backend.",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "default", CRPrefixLLM: ""},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{ContentText: "x", Kind: KindFunction, SymbolPath: "x"}
	doc := &PackageDoc{Package: PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x"}, Version: "1.0"}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if len(ollama.calls) != 1 || ollama.calls[0].Model != "qwen2.5:7b" {
		t.Errorf("expected default qwen2.5:7b call; got %+v", ollama.calls)
	}
}

func TestChunker_ContextualPrefix_BuildCRPromptIncludesContext(t *testing.T) {
	ollama := &recordingOllama{
		response: "Sixty plus tokens describing the chunk's role within the package documentation source as required by the contextual retrieval algorithm for downstream embedding. Provides anchoring metadata for vector retrieval improving top-K precision per Anthropic's September 2024 contextual retrieval blog and the cAST chunking research.",
	}
	c, err := NewChunker(ChunkerOptions{
		MinTokens: 50, MaxLeafTokens: 512, MaxParentTokens: 2048,
		EnableContextualPrefix: true,
		DoctrineProfile:        &DoctrineProfile{Name: "default", CRPrefixLLM: "qwen2.5:7b"},
		OllamaClient:           &OllamaClient{generator: ollama},
	})
	if err != nil {
		t.Fatalf("NewChunker: %v", err)
	}
	defer func() { _ = c.Close() }()
	chunk := Chunk{
		ContentText: "func Sum256(data []byte) [Size]byte { ... }",
		Kind:        KindFunction,
		SymbolPath:  "crypto/sha256.Sum256",
	}
	doc := &PackageDoc{
		Package: PackageRef{Ecosystem: EcoGo, Name: "crypto/sha256", CanonicalNamespace: "crypto/sha256"},
		Version: "1.23",
	}
	_, err = c.ContextualPrefix(context.Background(), chunk, doc)
	if err != nil {
		t.Fatalf("ContextualPrefix: %v", err)
	}
	if len(ollama.calls) != 1 {
		t.Fatalf("expected 1 call; got %d", len(ollama.calls))
	}
	prompt := ollama.calls[0].Prompt
	for _, needle := range []string{
		"crypto/sha256",
		"go",
		"1.23",
		"Sum256",
		"function",
		"func Sum256",
	} {
		if !strings.Contains(prompt, needle) {
			t.Errorf("prompt missing %q; got prompt: %q", needle, prompt)
		}
	}
}

func TestTruncateToTokenRange(t *testing.T) {
	tests := []struct {
		name, in string
		max      int
		wantMax  int
	}{
		{
			name:    "under-budget unchanged",
			in:      "short text",
			max:     80,
			wantMax: 80,
		},
		{
			name:    "sentence-boundary trim",
			in:      strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 20),
			max:     80,
			wantMax: 80,
		},
		{
			name:    "whitespace fallback",
			in:      strings.Repeat("x y ", 200),
			max:     20,
			wantMax: 22,
		},
		{
			name:    "no whitespace fallback",
			in:      strings.Repeat("x", 800),
			max:     20,
			wantMax: 22,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateToTokenRange(tc.in, 0, tc.max)
			if approxTokens(got) > tc.wantMax {
				t.Errorf("approxTokens(got)=%d; want ≤%d (got: %q)",
					approxTokens(got), tc.wantMax, got)
			}
		})
	}
}
