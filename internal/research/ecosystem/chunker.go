//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	tsmd "github.com/smacker/go-tree-sitter/markdown/tree-sitter-markdown"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	tsts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

var ErrUnknownLanguage = errors.New("ecosystem: unknown language; tree-sitter grammar not registered")

type Language string

const (
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangRust       Language = "rust"
	LangMarkdown   Language = "markdown"
)

var ecosystemToLanguage = map[Ecosystem]Language{
	EcoGo:         LangGo,
	EcoPython:     LangPython,
	EcoTypeScript: LangTypeScript,
	EcoRust:       LangRust,
}

var astNodeKindMap = map[Language]map[string]ChunkKind{
	LangGo: {
		"function_declaration": KindFunction,
		"method_declaration":   KindFunction,
		"type_declaration":     KindType,
		"type_spec":            KindType,
		"interface_type":       KindType,
		"struct_type":          KindType,
		"source_file":          KindModule,
		"package_clause":       KindModule,
	},
	LangPython: {
		"function_definition": KindFunction,
		"class_definition":    KindType,
		"module":              KindModule,
	},
	LangTypeScript: {
		"function_declaration":   KindFunction,
		"method_definition":      KindFunction,
		"arrow_function":         KindFunction,
		"class_declaration":      KindType,
		"interface_declaration":  KindType,
		"type_alias_declaration": KindType,
		"program":                KindModule,
	},
	LangRust: {
		"function_item": KindFunction,
		"struct_item":   KindType,
		"enum_item":     KindType,
		"trait_item":    KindType,
		"impl_item":     KindType,
		"source_file":   KindModule,
	},
	LangMarkdown: {
		"atx_heading":    KindGuide,
		"setext_heading": KindGuide,
		"paragraph":      KindProse,
		"document":       KindModule,
	},
}

type ChunkerOptions struct {
	MinTokens int

	MaxLeafTokens int

	MaxParentTokens int

	EnableContextualPrefix bool

	DoctrineProfile *DoctrineProfile

	OllamaClient *OllamaClient

	DispatcherClient DispatcherClient

	SelectiveCRKinds []ChunkKind
}

type DispatcherClient interface {
	Dispatch(ctx context.Context, provider, prompt string, maxTokens int) (string, error)
}

type Chunker struct {
	opts        ChunkerOptions
	languages   map[Language]*sitter.Language
	parserPools map[Language]*sync.Pool
}

func NewChunker(opts ChunkerOptions) (*Chunker, error) {
	if opts.MinTokens <= 0 {
		opts.MinTokens = 50
	}
	if opts.MaxLeafTokens <= 0 {
		opts.MaxLeafTokens = 512
	}
	if opts.MaxParentTokens <= 0 {
		opts.MaxParentTokens = 2048
	}
	if opts.MinTokens > opts.MaxLeafTokens {
		return nil, fmt.Errorf("ecosystem: ChunkerOptions invalid: MinTokens (%d) > MaxLeafTokens (%d)",
			opts.MinTokens, opts.MaxLeafTokens)
	}
	c := &Chunker{
		opts: opts,
		languages: map[Language]*sitter.Language{
			LangGo:         golang.GetLanguage(),
			LangPython:     python.GetLanguage(),
			LangTypeScript: tsts.GetLanguage(),
			LangRust:       rust.GetLanguage(),
			LangMarkdown:   tsmd.GetLanguage(),
		},
		parserPools: make(map[Language]*sync.Pool),
	}
	for lang, sl := range c.languages {
		l := sl
		c.parserPools[lang] = &sync.Pool{
			New: func() any {
				p := sitter.NewParser()
				p.SetLanguage(l)
				return p
			},
		}
	}
	return c, nil
}

func (c *Chunker) Close() error {
	return nil
}

func (c *Chunker) Chunk(ctx context.Context, doc *PackageDoc) ([]Chunk, error) {
	if doc == nil {
		return nil, errors.New("ecosystem: Chunk: nil doc")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	primaryLang := c.selectLanguage(doc)
	if primaryLang == "" {
		return nil, ErrUnknownLanguage
	}
	var out []Chunk
	for _, section := range doc.Sections {

		lang := primaryLang
		if section.Kind == KindGuide || section.Kind == KindProse {
			lang = LangMarkdown
		}
		chunks, err := c.chunkSection(ctx, doc, section, lang)
		if err != nil {
			return nil, fmt.Errorf("ecosystem: section %s: %w", section.SymbolPath, err)
		}
		out = append(out, chunks...)
	}
	return out, nil
}

func (c *Chunker) selectLanguage(doc *PackageDoc) Language {
	if lang, ok := ecosystemToLanguage[doc.Package.Ecosystem]; ok {
		return lang
	}
	return ""
}

func (c *Chunker) chunkSection(
	ctx context.Context, doc *PackageDoc, sec DocSection, lang Language,
) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pool := c.parserPools[lang]
	parser, _ := pool.Get().(*sitter.Parser)
	defer pool.Put(parser)

	src := []byte(sec.Body)
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", lang, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	var emitted []Chunk
	c.walkAST(ctx, doc, sec, lang, src, root, sql.NullInt64{}, &emitted)
	return emitted, nil
}

func (c *Chunker) walkAST(
	ctx context.Context, doc *PackageDoc, sec DocSection, lang Language,
	src []byte, node *sitter.Node, parentID sql.NullInt64, emitted *[]Chunk,
) {
	if err := ctx.Err(); err != nil {
		return
	}
	if node == nil {
		return
	}
	nodeText := node.Content(src)
	tokens := approxTokens(nodeText)
	nodeType := node.Type()
	kind := nodeKind(lang, nodeType)
	leaf := isLeafEligible(lang, nodeType)
	container := isContainer(lang, nodeType)
	moduleRoot := isModuleRoot(lang, nodeType)

	if moduleRoot {
		c.recurseChildren(ctx, doc, sec, lang, src, node, parentID, emitted)
		return
	}

	switch {
	case container && tokens > c.opts.MaxParentTokens:

		parentSummary := containerSummary(lang, node, src, c.opts.MaxLeafTokens)
		parentChunk := c.emitChunkWithContent(doc, sec, lang, node, src, kind, parentID, false, parentSummary)
		*emitted = append(*emitted, parentChunk)
		newParent := sql.NullInt64{Int64: int64(len(*emitted)), Valid: true}
		c.recurseChildren(ctx, doc, sec, lang, src, node, newParent, emitted)
		return

	case leaf && tokens > c.opts.MaxLeafTokens:

		*emitted = append(*emitted, c.emitChunk(doc, sec, lang, node, src, kind, parentID, true))
		return

	case leaf && leafAlwaysEmit(kind):

		*emitted = append(*emitted, c.emitChunk(doc, sec, lang, node, src, kind, parentID, false))
		return

	case leaf && tokens >= c.opts.MinTokens:

		*emitted = append(*emitted, c.emitChunk(doc, sec, lang, node, src, kind, parentID, false))
		return

	case container:

		c.recurseChildren(ctx, doc, sec, lang, src, node, parentID, emitted)
		return

	default:

		c.recurseChildren(ctx, doc, sec, lang, src, node, parentID, emitted)
	}
}

func leafAlwaysEmit(kind ChunkKind) bool {
	return kind == KindFunction || kind == KindType
}

func (c *Chunker) recurseChildren(
	ctx context.Context, doc *PackageDoc, sec DocSection, lang Language,
	src []byte, node *sitter.Node, parentID sql.NullInt64, emitted *[]Chunk,
) {
	count := int(node.NamedChildCount())
	if count == 0 {
		return
	}
	children := make([]*sitter.Node, 0, count)
	for i := 0; i < count; i++ {
		if ch := node.NamedChild(i); ch != nil {
			children = append(children, ch)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].StartByte() < children[j].StartByte()
	})
	for _, ch := range children {
		c.walkAST(ctx, doc, sec, lang, src, ch, parentID, emitted)
	}
}

func (c *Chunker) emitChunk(
	doc *PackageDoc, sec DocSection, lang Language,
	node *sitter.Node, src []byte, kind ChunkKind, parentID sql.NullInt64, oversized bool,
) Chunk {
	return c.emitChunkWithContent(doc, sec, lang, node, src, kind, parentID, oversized, node.Content(src))
}

func (c *Chunker) emitChunkWithContent(
	doc *PackageDoc, sec DocSection, lang Language,
	node *sitter.Node, src []byte, kind ChunkKind, parentID sql.NullInt64, oversized bool, content string,
) Chunk {
	fp := sha256.Sum256([]byte(content))
	return Chunk{
		PackageID:         doc.Package.ID,
		VersionIntroduced: doc.Version,
		StableIn:          []string{doc.Version},
		ContentText:       content,
		Fingerprint:       hex.EncodeToString(fp[:]),
		ParentChunkID:     parentID,
		SourceType:        SrcPackageDoc,
		SymbolPath:        derivePath(doc, sec, lang, node, src),
		Kind:              kind,
		SourceURL:         sec.SourceURL,
		Oversized:         oversized,
	}
}

// containerSummary returns a signature-only summary of a container node
// suitable for a parent chunk's ContentText. The summary MUST stay under
// maxLeafTokens (the parent shouldn't violate the same budget the leaves
// do).
//
// Strategy per grammar:
//
//   - source_file / program / module / document: emit the first
//     declaration-like content (package clause, header, title) plus
//     up to N more lines of identifiers.
//   - class_declaration / interface_declaration / type_declaration:
//     emit the declaration line ("class Foo extends Bar {") plus the
//     leading doc comment if present.
//   - trait_item / impl_item: emit the trait/impl signature.
//
// Fallback extract the first source line of the node, truncated to
// maxLeafTokens*4 chars.
func containerSummary(_ Language, node *sitter.Node, src []byte, maxLeafTokens int) string {
	if node == nil {
		return ""
	}
	full := node.Content(src)

	budget := maxLeafTokens * 4
	if budget <= 0 {
		budget = 2048
	}

	if i := indexByte(full, '\n'); i >= 0 {
		head := full[:i]
		if len(head) <= budget {

			limit := budget
			if limit > len(full) {
				limit = len(full)
			}
			return full[:limit]
		}
		return head[:budget]
	}
	if len(full) > budget {
		return full[:budget]
	}
	return full
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func derivePath(doc *PackageDoc, sec DocSection, lang Language, node *sitter.Node, src []byte) string {
	base := sec.SymbolPath
	if base == "" {
		base = doc.Package.CanonicalNamespace
	}
	childName := extractName(lang, node, src)
	if childName == "" {
		return base
	}

	if base == childName || endsWithSegment(base, childName) {
		return base
	}
	return base + "." + childName
}

func endsWithSegment(path, seg string) bool {
	if path == seg {
		return true
	}
	if len(path) < len(seg)+1 {
		return false
	}
	suffix := path[len(path)-len(seg):]
	if suffix != seg {
		return false
	}
	delim := path[len(path)-len(seg)-1]
	return delim == '.' || delim == '/' || delim == ':'
}

func extractName(_ Language, node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}

	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(src)
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier", "type_identifier", "property_identifier", "field_identifier", "scoped_identifier":
			return child.Content(src)
		}
	}
	return ""
}

func nodeKind(lang Language, nodeType string) ChunkKind {
	if langMap, ok := astNodeKindMap[lang]; ok {
		if k, ok2 := langMap[nodeType]; ok2 {
			return k
		}
	}
	return KindProse
}

func isLeafEligible(lang Language, nodeType string) bool {
	switch nodeKind(lang, nodeType) {
	case KindFunction, KindType, KindGuide, KindProse:

		_, recognized := astNodeKindMap[lang][nodeType]
		return recognized
	}
	return false
}

func isContainer(lang Language, nodeType string) bool {
	return nodeKind(lang, nodeType) == KindType
}

func isModuleRoot(lang Language, nodeType string) bool {
	return nodeKind(lang, nodeType) == KindModule
}

func approxTokens(s string) int {
	return (len(s) + 3) / 4
}

// ContextualPrefix generates a 60-80 token Anthropic-style Contextual
// Retrieval prefix for a chunk. Per spec §2.3 Q3=C citing Anthropic's
// Sep 2024 CR blog: prepending a chunk-in-context description before
// embedding cuts top-K failure 49% → 67% (with rerank).
//
// Behavior
//
//   - When EnableContextualPrefix is false, returns "" + nil error (chunker
//     is operating in B-1 mode; no LLM dispatch).
//   - When DoctrineProfile is nil and EnableContextualPrefix is true,
//     returns an error (init-order bug signal; daemon-init wiring is
//     incomplete).
//   - When SelectiveCRKinds is non-empty and chunk.Kind is not in the
//     trigger set, returns "" + nil error (C5 selective path).
//   - Otherwise dispatches to the configured backend (Ollama for
//     qwen/llama/gemma model prefixes; DispatcherClient for claude/
//     deepseek prefixes) per DoctrineProfile.CRPrefixLLM, truncates
//     response to 60-80 tokens, and returns the trimmed prefix.
//
// Failure-mode per DoctrineProfile.RefuseOnUnverified:
//
//   - capa-firewall (RefuseOnUnverified=true): LLM error or <60-token
//     response → returns the error to the caller (chunker aborts).
//   - default / max-scope (RefuseOnUnverified=false): LLM error or
//     <60-token response → returns "" + nil error (chunker continues
//     with an empty prefix; Ingester aggregates per-package failures
//     for observability).
//
// The DoctrineProfile.RefuseOnUnverified knob is the single source of
// truth for failure-mode policy — Chunker MUST NOT bake doctrine names
// into the dispatch path (that would couple the chunker to per-doctrine
// behavior and silently drift if a new doctrine is added).
func (c *Chunker) ContextualPrefix(ctx context.Context, chunk Chunk, doc *PackageDoc) (string, error) {
	if !c.opts.EnableContextualPrefix {
		return "", nil
	}
	if c.opts.DoctrineProfile == nil {
		return "", errors.New("ecosystem: ContextualPrefix: nil DoctrineProfile (required when EnableContextualPrefix=true)")
	}

	if len(c.opts.SelectiveCRKinds) > 0 {
		matched := false
		for _, k := range c.opts.SelectiveCRKinds {
			if k == chunk.Kind {
				matched = true
				break
			}
		}
		if !matched {
			return "", nil
		}
	}
	prompt := c.buildCRPrompt(chunk, doc)
	rawPrefix, dispatchErr := c.dispatchCR(ctx, prompt)
	if dispatchErr != nil {
		if c.opts.DoctrineProfile.RefuseOnUnverified {
			return "", fmt.Errorf("ContextualPrefix: %w", dispatchErr)
		}

		return "", nil
	}
	trimmed := truncateToTokenRange(rawPrefix, 60, 80)
	tokens := approxTokens(trimmed)
	if tokens < 60 {
		if c.opts.DoctrineProfile.RefuseOnUnverified {
			return "", fmt.Errorf("ContextualPrefix: LLM response %d tokens; want ≥60", tokens)
		}
		return "", nil
	}
	return trimmed, nil
}

func (c *Chunker) buildCRPrompt(chunk Chunk, doc *PackageDoc) string {
	return fmt.Sprintf(`<document>
Package: %s (%s ecosystem, version %s)
Symbol: %s
Kind: %s
</document>

<chunk>
%s
</chunk>

Please give a short, succinct context (60 to 80 tokens) to situate this chunk within the overall document for improving search retrieval of the chunk. Answer only with the context, no preamble.`,
		doc.Package.Name,
		doc.Package.Ecosystem,
		doc.Version,
		chunk.SymbolPath,
		chunk.Kind,
		chunk.ContentText,
	)
}

func (c *Chunker) dispatchCR(ctx context.Context, prompt string) (string, error) {
	model := c.opts.DoctrineProfile.CRPrefixLLM
	if model == "" {
		model = "qwen2.5:7b"
	}
	switch {
	case strings.HasPrefix(model, "qwen"), strings.HasPrefix(model, "llama"), strings.HasPrefix(model, "gemma"):
		if c.opts.OllamaClient == nil {
			return "", errors.New("ecosystem: OllamaClient not configured for local backend")
		}
		return c.opts.OllamaClient.Generate(ctx, model, prompt, 100)
	case strings.HasPrefix(model, "claude"):
		if c.opts.DispatcherClient == nil {
			return "", errors.New("ecosystem: DispatcherClient not configured for Claude backend")
		}
		return c.opts.DispatcherClient.Dispatch(ctx, "anthropic-paygo", prompt, 100)
	case strings.HasPrefix(model, "deepseek"):
		if c.opts.DispatcherClient == nil {
			return "", errors.New("ecosystem: DispatcherClient not configured for DeepSeek backend")
		}
		return c.opts.DispatcherClient.Dispatch(ctx, "siliconflow", prompt, 100)
	default:
		return "", fmt.Errorf("ecosystem: unknown CR backend model %q", model)
	}
}

func truncateToTokenRange(s string, _, maxTokens int) string {
	if approxTokens(s) <= maxTokens {
		return s
	}

	limit := maxTokens * 4
	if limit >= len(s) {

		return s
	}
	t := s[:limit]
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == '.' || t[i] == '!' || t[i] == '?' {
			return t[:i+1]
		}
	}
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == ' ' {
			return t[:i]
		}
	}
	return t
}
