//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"path"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type langSpec struct {
	language string
	lang     *sitter.Language
	query    *sitter.Query
	captures map[string]store.NodeKind

	nodeID func(filePath, owner, name string) string

	ownerFor func(defNode *sitter.Node, src []byte) string
}

func buildLangSpec(language string, lang *sitter.Language, tagsQuery string,
	captures map[string]store.NodeKind,
	nodeID func(filePath, owner, name string) string,
	ownerFor func(defNode *sitter.Node, src []byte) string,
) (*langSpec, error) {
	if lang == nil {
		return nil, ErrNoLanguage
	}
	q, err := sitter.NewQuery([]byte(tagsQuery), lang)
	if err != nil {
		return nil, err
	}
	return &langSpec{
		language: language,
		lang:     lang,
		query:    q,
		captures: captures,
		nodeID:   nodeID,
		ownerFor: ownerFor,
	}, nil
}

func buildRegistry() (map[string]*langSpec, error) {
	specs := make(map[string]*langSpec, 5)
	builders := []func() (*langSpec, error){
		newGoLangSpec,
		newTypeScriptLangSpec,
		newPythonLangSpec,
		newRustLangSpec,
	}
	for _, b := range builders {
		spec, err := b()
		if err != nil {
			return nil, err
		}
		specs[spec.language] = spec
	}

	tsxSpec, err := newTSXLangSpec()
	if err != nil {
		return nil, err
	}
	specs["typescript.tsx"] = tsxSpec
	return specs, nil
}

func extToLanguage(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".py", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

func (p *Parser) langForPath(filePath string) (*langSpec, bool) {
	ext := strings.ToLower(path.Ext(filePath))
	if ext == ".tsx" || ext == ".jsx" {
		if s, ok := p.specs["typescript.tsx"]; ok {
			return s, true
		}
	}
	lang := extToLanguage(ext)
	if lang == "" {
		return nil, false
	}
	s, ok := p.specs[lang]
	return s, ok
}
