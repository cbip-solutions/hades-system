//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func goLanguage() *sitter.Language { return golang.GetLanguage() }

// newGoLangSpec builds the Go langSpec: the vendored Go grammar, the embedded
// go.scm tags query, the Go capture→NodeKind map, and the Go node_id +
// receiver builders (relocated from Phase B's package-level forms — behaviour
// identical). The capture names MUST match queries/go.scm (queries_test.go
// gates the contract).
func newGoLangSpec() (*langSpec, error) {
	captures := map[string]store.NodeKind{
		"definition.function":  store.KindFunction,
		"definition.method":    store.KindMethod,
		"definition.struct":    store.KindStruct,
		"definition.interface": store.KindInterface,
		"definition.type":      store.KindType,
		"definition.field":     store.KindField,
	}
	nodeID := func(filePath, owner, name string) string {
		return qualifiedNodeID(goPackagePathFromFile(filePath), owner, name)
	}
	ownerFor := func(defNode *sitter.Node, src []byte) string {
		switch defNode.Type() {
		case "method_declaration":
			return methodReceiverType(defNode, src)
		case "field_declaration", "method_elem":
			return fieldOwnerType(defNode, src)
		default:
			return ""
		}
	}
	return buildLangSpec("go", goLanguage(), goTagsQuery, captures, nodeID, ownerFor)
}

func goPackagePathFromFile(filePath string) string {
	slash := strings.ReplaceAll(filePath, "\\", "/")
	idx := strings.LastIndex(slash, "/")
	if idx < 0 {
		return ""
	}
	return slash[:idx]
}

// qualifiedNodeID builds the globally-unique node_id for a symbol. This is the
// repo-relative JOIN KEY that Phase C's canonicalNodeID must match byte-for-byte
// (gated by TestNodeIDMatchesPhaseBByteForByte in Phase C).
//
// Scheme <dir>.[Receiver.]Name — module prefix is NEVER included.
// Examples
//   - func Run in internal/widget/x.go      → "internal/widget.Run"
//   - func (s Server) Serve in same dir     → "internal/widget.Server.Serve"
//   - field prefix in Server struct         → "internal/widget.Server.prefix"
//
// For methods the receiver type is included so Server.Greet and Client.Greet
// do not collide. receiver is "" for non-methods. Empty pkgPath (root file)
// drops the directory prefix.
func qualifiedNodeID(pkgPath, receiver, name string) string {
	sep := "."
	if pkgPath == "" {

		if receiver != "" {
			return receiver + sep + name
		}
		return name
	}
	if receiver != "" {
		return pkgPath + sep + receiver + sep + name
	}
	return pkgPath + sep + name
}
