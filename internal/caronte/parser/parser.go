// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"context"
	"fmt"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Parser struct {
	specs map[string]*langSpec
	pool  sync.Pool

	cacheOnce    sync.Once
	treeCacheCap int
	trees        *treeCache
}

type ParseResult struct {
	Nodes   []store.Node
	Partial bool
}

func NewParser() (*Parser, error) {
	specs, err := buildRegistry()
	if err != nil {
		return nil, err
	}
	p := &Parser{specs: specs}
	p.pool = sync.Pool{New: func() any { return sitter.NewParser() }}
	return p, nil
}

// parseTree parses src into a *sitter.Tree using a pooled parser configured for
// spec's grammar, optionally against oldTree for incremental re-parse. The
// caller owns the returned tree and MUST Close it, and returns the borrowed
// parser to the pool after the tree is consumed. The pooled parser's language
// is set per borrow (a Parser serves multiple languages), so a parser reused
// from a prior Go parse is re-pointed at, e.g., the Python grammar here.
func (p *Parser) parseTree(ctx context.Context, spec *langSpec, oldTree *sitter.Tree, src []byte) (*sitter.Tree, *sitter.Parser, error) {
	tp, _ := p.pool.Get().(*sitter.Parser)
	tp.SetLanguage(spec.lang)
	tree, err := tp.ParseCtx(ctx, oldTree, src)
	if err != nil {
		p.pool.Put(tp)
		return nil, nil, fmt.Errorf("caronte/parser: tree-sitter parse: %w", err)
	}
	return tree, tp, nil
}

// ParseFile parses a whole file's bytes and extracts its symbols, selecting the
// grammar by file extension. Returns ErrUnsupportedLanguage if no grammar is
// registered for the extension. Error-tolerant: tree-sitter always returns a
// tree, and embedded ERROR/MISSING nodes do not abort extraction — the valid
// definitions around the error are still emitted, and Partial is set.
//
// filePath is the repo-relative path used to qualify node_ids and stamp
// FilePath; src is the file content. Returns a *ParseResult; the only error
// path is a tree-sitter operation-limit / context cancellation (a true parse
// failure, distinct from a syntax error in well-formed-but-incomplete source).
func (p *Parser) ParseFile(ctx context.Context, filePath string, src []byte) (*ParseResult, error) {
	spec, ok := p.langForPath(filePath)
	if !ok {
		return nil, ErrUnsupportedLanguage
	}
	tree, tp, err := p.parseTree(ctx, spec, nil, src)
	if err != nil {
		return nil, err
	}
	defer func() {
		tree.Close()
		p.pool.Put(tp)
	}()
	return p.extractSymbols(spec, filePath, src, tree), nil
}

func (p *Parser) extractSymbols(spec *langSpec, filePath string, src []byte, tree *sitter.Tree) *ParseResult {
	root := tree.RootNode()
	res := &ParseResult{Nodes: make([]store.Node, 0, 16), Partial: root.HasError()}

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(spec.query, root)

	type pending struct {
		node     store.Node
		priority int
	}
	byID := make(map[string]pending)
	order := make([]string, 0, 16)

	kindPriority := func(k store.NodeKind) int {
		switch k {
		case store.KindFunction, store.KindMethod, store.KindStruct, store.KindInterface:
			return 0
		case store.KindField:
			return 1
		case store.KindType:
			return 2
		default:
			return 3
		}
	}

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		m = qc.FilterPredicates(m, src)

		var defNode, nameNode *sitter.Node
		var kind store.NodeKind
		for _, c := range m.Captures {
			capName := spec.query.CaptureNameForId(c.Index)
			if k, isDef := spec.captures[capName]; isDef {
				defNode = c.Node
				kind = k
				continue
			}
			if strings.HasPrefix(capName, "name") {
				nameNode = c.Node
			}
		}
		if defNode == nil || nameNode == nil {
			continue
		}

		if defNode.IsError() {
			continue
		}
		name := nameNode.Content(src)
		if name == "" {
			continue
		}
		owner := spec.ownerFor(defNode, src)
		nodeID := spec.nodeID(filePath, owner, name)
		pri := kindPriority(kind)
		node := store.Node{
			NodeID:    nodeID,
			Name:      name,
			Kind:      string(kind),
			Language:  spec.language,
			FilePath:  filePath,
			StartLine: int(defNode.StartPoint().Row) + 1,
			EndLine:   int(defNode.EndPoint().Row) + 1,
			Signature: syntacticSignature(defNode, src),
			Doc:       leadingDoc(defNode, src),
		}
		node.ContentHash = ContentHash(node.Kind + "\x00" + node.Signature + "\x00" + defNode.Content(src))
		if existing, seen := byID[nodeID]; !seen {
			order = append(order, nodeID)
			byID[nodeID] = pending{node: node, priority: pri}
		} else if pri < existing.priority {

			byID[nodeID] = pending{node: node, priority: pri}
		}
	}

	for _, id := range order {
		res.Nodes = append(res.Nodes, byID[id].node)
	}
	return res
}

func fieldOwnerType(fieldDef *sitter.Node, src []byte) string {

	cur := fieldDef.Parent()
	for cur != nil {
		switch cur.Type() {
		case "struct_type", "interface_type":

			typeSpec := cur.Parent()
			if typeSpec == nil {
				return ""
			}
			nameNode := typeSpec.ChildByFieldName("name")
			if nameNode == nil {
				return ""
			}
			return nameNode.Content(src)
		}
		cur = cur.Parent()
	}
	return ""
}

func methodReceiverType(method *sitter.Node, src []byte) string {
	recv := method.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}

	for i := 0; i < int(recv.NamedChildCount()); i++ {
		param := recv.NamedChild(i)
		if param == nil {
			continue
		}
		t := param.ChildByFieldName("type")
		if t == nil {
			continue
		}
		return typeIdentName(t, src)
	}
	return ""
}

func typeIdentName(t *sitter.Node, src []byte) string {
	switch t.Type() {
	case "pointer_type":
		if inner := t.NamedChild(0); inner != nil {
			return typeIdentName(inner, src)
		}
		return ""
	case "type_identifier", "identifier":
		return t.Content(src)
	default:

		if inner := t.NamedChild(0); inner != nil {
			return typeIdentName(inner, src)
		}
		return t.Content(src)
	}
}

func syntacticSignature(def *sitter.Node, src []byte) string {
	full := def.Content(src)
	if i := strings.IndexByte(full, '\n'); i >= 0 {
		full = full[:i]
	}
	return strings.TrimSpace(full)
}

func leadingDoc(def *sitter.Node, src []byte) string {
	prev := def.PrevNamedSibling()
	if prev == nil || prev.Type() != "comment" {
		return ""
	}

	if def.StartPoint().Row > prev.EndPoint().Row+1 {
		return ""
	}
	return strings.TrimSpace(prev.Content(src))
}
