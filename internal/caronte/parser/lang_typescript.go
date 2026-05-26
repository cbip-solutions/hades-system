//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"path"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// tsCaptures maps the typescript.scm @definition.<kind> capture names to
// store.NodeKind. A TS class maps to KindStruct and a TS interface to
// KindInterface (Phase A froze no "class" kind; the language column
// disambiguates). Keys MUST match queries/typescript.scm.
var tsCaptures = map[string]store.NodeKind{
	"definition.function":  store.KindFunction,
	"definition.method":    store.KindMethod,
	"definition.struct":    store.KindStruct,
	"definition.interface": store.KindInterface,
	"definition.field":     store.KindField,
}

func tsModulePath(filePath string) string {
	slash := strings.ReplaceAll(filePath, "\\", "/")
	ext := path.Ext(slash)
	return strings.TrimSuffix(slash, ext)
}

func tsNodeID(filePath, owner, name string) string {
	mod := tsModulePath(filePath)
	if owner != "" {
		return mod + "." + owner + "." + name
	}
	return mod + "." + name
}

func tsOwnerFor(defNode *sitter.Node, src []byte) string {
	if defNode.Type() != "method_definition" {
		return ""
	}
	for n := defNode.Parent(); n != nil; n = n.Parent() {
		if n.Type() == "class_declaration" {
			if name := n.ChildByFieldName("name"); name != nil {
				return name.Content(src)
			}
			return ""
		}
	}
	return ""
}

func newTypeScriptLangSpec() (*langSpec, error) {
	return buildLangSpec("typescript", typescript.GetLanguage(), tsTagsQuery, tsCaptures, tsNodeID, tsOwnerFor)
}

func newTSXLangSpec() (*langSpec, error) {
	spec, err := buildLangSpec("typescript", tsx.GetLanguage(), tsTagsQuery, tsCaptures, tsNodeID, tsOwnerFor)
	if err != nil {
		return nil, err
	}
	return spec, nil
}
