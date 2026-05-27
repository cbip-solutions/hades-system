// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package nextjs

import (
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const ExtractorID = "typescript.nextjs-v1"

const extractorName = "typescript.nextjs"

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func init() {
	if err := extract.Default().Register(extractorName, New()); err != nil {
		panic("caronte/contract/extract/typescript/nextjs: Register failed: " + err.Error())
	}
}

func (e *Extractor) Language() extract.Language { return extract.LangTypeScript }

func (e *Extractor) Frameworks() []string { return []string{"nextjs"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if file == "" {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(file))
	ext := filepath.Ext(clean)
	if !isJSorTSExt(ext) {
		return false
	}
	base := filepath.Base(clean)

	if isStem(base, "middleware") {
		return true
	}

	if isStem(base, "route") && hasPrefixSegment(clean, "app") {
		return true
	}

	if hasPrefixSegments(clean, "pages", "api") {
		return true
	}
	return false
}

func (e *Extractor) Endpoints(tree *parser.Tree, file string) ([]store.APIEndpoint, error) {
	return nil, nil
}

func (e *Extractor) Calls(tree *parser.Tree, file string) ([]store.APICall, error) {
	return nil, nil
}

func (e *Extractor) StubArtifacts(file string, content []byte) []extract.StubReference {
	return []extract.StubReference{}
}

func isJSorTSExt(ext string) bool {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return true
	}
	return false
}

func isStem(base, stem string) bool {
	dot := strings.LastIndex(base, ".")
	if dot < 0 {
		return base == stem
	}
	return base[:dot] == stem
}

func hasPrefixSegment(path, seg string) bool {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i] == seg
	}
	return path == seg
}

func hasPrefixSegments(path, a, b string) bool {
	parts := strings.SplitN(path, "/", 3)
	return len(parts) >= 2 && parts[0] == a && parts[1] == b
}
