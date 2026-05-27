// go:build cgo
//go:build cgo
// +build cgo

package parser

import (
	"context"
	"testing"
)

func TestIndexerDeleteClosedStorePropagatesError(t *testing.T) {
	s := newTestStore(t)
	s.DB().Close()
	p, _ := NewParser()
	idx := NewIndexer(p, s)

	n, err := idx.Delete(context.Background(), "pkg/x/x.go")
	if err == nil {
		t.Error("Indexer.Delete(closed store) returned nil; want the wrapped DeleteNodesByFile error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}
