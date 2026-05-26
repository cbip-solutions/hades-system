//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"path"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// pyCaptures maps the python.scm @definition.<kind> capture names to
// store.NodeKind. A Python class maps to KindStruct (Phase A froze no "class"
// kind; the language column disambiguates). Keys MUST match queries/python.scm.
var pyCaptures = map[string]store.NodeKind{
	"definition.function": store.KindFunction,
	"definition.method":   store.KindMethod,
	"definition.struct":   store.KindStruct,
}

func pyModulePath(filePath string) string {
	slash := strings.ReplaceAll(filePath, "\\", "/")
	ext := path.Ext(slash)
	noExt := strings.TrimSuffix(slash, ext)
	if base := path.Base(noExt); base == "__init__" {
		noExt = path.Dir(noExt)
		if noExt == "." {
			return ""
		}
	}
	return strings.ReplaceAll(noExt, "/", ".")
}

func pyNodeID(filePath, owner, name string) string {
	mod := pyModulePath(filePath)
	parts := make([]string, 0, 3)
	if mod != "" {
		parts = append(parts, mod)
	}
	if owner != "" {
		parts = append(parts, owner)
	}
	parts = append(parts, name)
	return strings.Join(parts, ".")
}

func pyOwnerFor(defNode *sitter.Node, src []byte) string {
	if defNode.Type() != "function_definition" {
		return ""
	}
	for n := defNode.Parent(); n != nil; n = n.Parent() {
		switch n.Type() {
		case "class_definition":
			if name := n.ChildByFieldName("name"); name != nil {
				return name.Content(src)
			}
			return ""
		case "function_definition":

			return ""
		}
	}
	return ""
}

// newPythonLangSpec builds the Python langSpec: the vendored Python grammar,
// the embedded python.scm tags query, the Python capture→NodeKind map, and the
// Python node_id + owner builders. The capture names MUST match
// queries/python.scm.
func newPythonLangSpec() (*langSpec, error) {
	return buildLangSpec("python", python.GetLanguage(), pyTagsQuery, pyCaptures, pyNodeID, pyOwnerFor)
}
