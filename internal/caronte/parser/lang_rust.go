// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"path"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// rustCaptures maps the rust.scm @definition.<kind> capture names to
// store.NodeKind. A Rust trait → KindInterface; a struct/enum → KindStruct
// . Keys
// MUST match queries/rust.scm.
var rustCaptures = map[string]store.NodeKind{
	"definition.function":  store.KindFunction,
	"definition.method":    store.KindMethod,
	"definition.struct":    store.KindStruct,
	"definition.interface": store.KindInterface,
	"definition.field":     store.KindField,
}

func rustModulePath(filePath string) string {
	slash := strings.ReplaceAll(filePath, "\\", "/")

	if i := strings.LastIndex(slash, "src/"); i >= 0 {
		slash = slash[i+len("src/"):]
	}
	ext := path.Ext(slash)
	noExt := strings.TrimSuffix(slash, ext)
	base := path.Base(noExt)
	dir := path.Dir(noExt)

	if dir == "." && (base == "lib" || base == "main") {
		return "crate"
	}

	if base == "mod" {
		noExt = dir
	}
	if noExt == "." || noExt == "" {
		return "crate"
	}
	segs := strings.Split(noExt, "/")
	return "crate::" + strings.Join(segs, "::")
}

func rustNodeID(filePath, owner, name string) string {
	mod := rustModulePath(filePath)
	if owner != "" {
		return mod + "::" + owner + "::" + name
	}
	return mod + "::" + name
}

func rustOwnerFor(defNode *sitter.Node, src []byte) string {
	switch defNode.Type() {
	case "function_item", "function_signature_item":
		for n := defNode.Parent(); n != nil; n = n.Parent() {
			switch n.Type() {
			case "impl_item":

				if t := n.ChildByFieldName("type"); t != nil {
					return rustTypeName(t, src)
				}
				return ""
			case "trait_item":

				if t := n.ChildByFieldName("name"); t != nil {
					return t.Content(src)
				}
				return ""
			case "function_item":

				return ""
			}
		}
		return ""
	case "field_declaration":

		for n := defNode.Parent(); n != nil; n = n.Parent() {
			if n.Type() == "struct_item" {
				if name := n.ChildByFieldName("name"); name != nil {
					return name.Content(src)
				}
				return ""
			}
		}
		return ""
	default:
		return ""
	}
}

func rustTypeName(t *sitter.Node, src []byte) string {
	switch t.Type() {
	case "type_identifier", "identifier":
		return t.Content(src)
	case "reference_type", "generic_type", "scoped_type_identifier":
		if inner := t.NamedChild(0); inner != nil {
			return rustTypeName(inner, src)
		}
		return t.Content(src)
	default:
		if inner := t.NamedChild(0); inner != nil {
			return rustTypeName(inner, src)
		}
		return t.Content(src)
	}
}

// newRustLangSpec builds the Rust langSpec: the vendored smacker Rust grammar,
// the embedded rust.scm tags query, the Rust capture→NodeKind map, and the
// Rust node_id + owner builders. The capture names MUST match
// queries/rust.scm.
func newRustLangSpec() (*langSpec, error) {
	return buildLangSpec("rust", rust.GetLanguage(), rustTagsQuery, rustCaptures, rustNodeID, rustOwnerFor)
}
