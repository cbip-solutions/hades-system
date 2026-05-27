//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package nestjs

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const ExtractorID = "typescript.nestjs-v1"

const extractorName = "typescript.nestjs"

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func init() {
	if err := extract.Default().Register(extractorName, New()); err != nil {
		panic("caronte/contract/extract/typescript/nestjs: Register failed: " + err.Error())
	}
}

func (e *Extractor) Language() extract.Language { return extract.LangTypeScript }

func (e *Extractor) Frameworks() []string { return []string{"nestjs"} }

var nestjsDecoratorPattern = regexp.MustCompile(
	`@(Controller|Get|Post|Put|Delete|Patch|Options|Head|All)\s*\(`,
)

func (e *Extractor) Detect(file string, content []byte) bool {
	ext := filepath.Ext(file)
	if ext != ".ts" && ext != ".tsx" {
		return false
	}
	s := string(content)
	if !strings.Contains(s, "@nestjs/common") {
		return false
	}
	return nestjsDecoratorPattern.MatchString(s)
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
