// go:build cgo
//go:build cgo
// +build cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func TestDeleteNodesByFileEmptyDB(t *testing.T) {
	var s Store
	n, err := s.DeleteNodesByFile(context.Background(), "pkg/a.go")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("DeleteNodesByFile(nil db) err = %v; want ErrEmptyDB", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}

func TestDeleteNodesByFileClosedDB(t *testing.T) {
	s := newClosedStore(t)
	n, err := s.DeleteNodesByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("DeleteNodesByFile(closed db) returned nil; want error (BeginTx fails)")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestDeleteNodesByFileSelectErrors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec("DROP TABLE graph_nodes"); err != nil {
		t.Fatalf("drop graph_nodes: %v", err)
	}
	n, err := s.DeleteNodesByFile(context.Background(), "pkg/e.go")
	if err == nil {
		t.Error("DeleteNodesByFile with graph_nodes dropped returned nil; want a wrapped SELECT error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestDeleteNodesByFileMidTxStatementErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		dropTable string
	}{
		{"fts_delete_fails", "graph_nodes_fts"},
		{"vec_delete_fails", "code_node_vec"},
		{"edge_delete_fails", "graph_edges"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			mustUpsertNode(t, s, Node{NodeID: "e1", Name: "Err", Kind: "func", Language: "go", FilePath: "pkg/e.go", ContentHash: "h"})
			mustUpsertVector(t, s, "e1")
			mustUpsertEdge(t, s, Edge{SourceID: "e1", TargetID: "e1", Kind: string(EdgeCalls), Confidence: ConfExactStatic})

			if _, err := s.DB().Exec("DROP TABLE " + tc.dropTable); err != nil {
				t.Fatalf("drop %s: %v", tc.dropTable, err)
			}
			n, err := s.DeleteNodesByFile(ctx, "pkg/e.go")
			if err == nil {
				t.Errorf("DeleteNodesByFile with %s dropped returned nil; want a wrapped error", tc.dropTable)
			}
			if n != 0 {
				t.Errorf("rows = %d; want 0 on error", n)
			}
		})
	}
}
