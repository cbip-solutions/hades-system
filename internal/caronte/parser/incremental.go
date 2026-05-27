// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"container/list"
	"context"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

const defaultMaxLiveTrees = 150

type cacheEntry struct {
	path string
	tree *sitter.Tree
}

type treeCache struct {
	mu  sync.Mutex
	cap int
	ll  *list.List
	idx map[string]*list.Element
}

func newTreeCache(capacity int) *treeCache {
	if capacity <= 0 {
		capacity = defaultMaxLiveTrees
	}
	return &treeCache{
		cap: capacity,
		ll:  list.New(),
		idx: make(map[string]*list.Element, capacity),
	}
}

func (c *treeCache) get(path string) (*sitter.Tree, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.idx[path]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*cacheEntry).tree, true
}

func (c *treeCache) put(path string, tree *sitter.Tree) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.idx[path]; ok {
		old := el.Value.(*cacheEntry)
		if old.tree != nil && old.tree != tree {
			old.tree.Close()
		}
		old.tree = tree
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{path: path, tree: tree})
	c.idx[path] = el
	for c.ll.Len() > c.cap {
		back := c.ll.Back()
		if back == nil {
			break
		}
		ent := back.Value.(*cacheEntry)
		if ent.tree != nil {
			ent.tree.Close()
		}
		c.ll.Remove(back)
		delete(c.idx, ent.path)
	}
}

func (c *treeCache) drop(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.idx[path]
	if !ok {
		return
	}
	ent := el.Value.(*cacheEntry)
	if ent.tree != nil {
		ent.tree.Close()
	}
	c.ll.Remove(el)
	delete(c.idx, path)
}

func (c *treeCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *treeCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.ll.Front(); el != nil; el = el.Next() {
		if ent := el.Value.(*cacheEntry); ent.tree != nil {
			ent.tree.Close()
		}
	}
	c.ll.Init()
	c.idx = make(map[string]*list.Element, c.cap)
}

func editInputFor(old, neu []byte) sitter.EditInput {

	start := 0
	for start < len(old) && start < len(neu) && old[start] == neu[start] {
		start++
	}

	oldEnd := len(old)
	newEnd := len(neu)
	return sitter.EditInput{
		StartIndex:  uint32(start),
		OldEndIndex: uint32(oldEnd),
		NewEndIndex: uint32(newEnd),
		StartPoint:  countPoint(neu, start),
		OldEndPoint: countPoint(old, oldEnd),
		NewEndPoint: countPoint(neu, newEnd),
	}
}

func countPoint(buf []byte, off int) sitter.Point {
	if off > len(buf) {
		off = len(buf)
	}
	var row, col uint32
	for i := 0; i < off; i++ {
		if buf[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	return sitter.Point{Row: row, Column: col}
}

func (p *Parser) ParseFileIncremental(ctx context.Context, filePath string, oldSrc, newSrc []byte) (*ParseResult, error) {
	spec, ok := p.langForPath(filePath)
	if !ok {
		return nil, ErrUnsupportedLanguage
	}
	cached, hasCached := p.cache().get(filePath)
	if oldSrc == nil || !hasCached {
		tree, tp, err := p.parseTree(ctx, spec, nil, newSrc)
		if err != nil {
			return nil, err
		}
		p.pool.Put(tp)
		res := p.extractSymbols(spec, filePath, newSrc, tree)
		p.cache().put(filePath, tree)
		return res, nil
	}

	ei := editInputFor(oldSrc, newSrc)
	cached.Edit(ei)
	tree, tp, err := p.parseTree(ctx, spec, cached, newSrc)
	if err != nil {
		return nil, err
	}
	p.pool.Put(tp)
	res := p.extractSymbols(spec, filePath, newSrc, tree)
	p.cache().put(filePath, tree)
	return res, nil
}

func (p *Parser) cache() *treeCache {
	p.cacheOnce.Do(func() {
		if p.treeCacheCap == 0 {
			p.treeCacheCap = defaultMaxLiveTrees
		}
		p.trees = newTreeCache(p.treeCacheCap)
	})
	return p.trees
}

// SetTreeCacheCap sets the live-tree LRU capacity. MUST be called before the
// first ParseFileIncremental (it is a no-op after the cache is initialised).
func (p *Parser) SetTreeCacheCap(n int) {
	if p.trees == nil {
		p.treeCacheCap = n
	}
}

func (p *Parser) CloseTrees() {
	if p.trees != nil {
		p.trees.closeAll()
	}
}
