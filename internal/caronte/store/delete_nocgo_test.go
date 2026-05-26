//go:build !cgo
// +build !cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func TestDeleteNodesByFileCGODisabled(t *testing.T) {
	var s Store
	n, err := s.DeleteNodesByFile(context.Background(), "pkg/a.go")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("DeleteNodesByFile (!cgo) err = %v; want ErrCGODisabled", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}
