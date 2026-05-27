// go:build cgo
//go:build cgo
// +build cgo

package proto

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

// TestEndpointsReturnsErrTreeNotRegistered is the I-3 bite-check: if a caller
// hands Endpoints a *parser.Tree NOT registered via parseTree (e.g., a future
// caller that constructs the tree directly via sitter.NewParser), Endpoints
// MUST return ErrTreeNotRegistered instead of silently returning (nil, nil).
//
// Silent-empty was the original behaviour and is debug-from-hell: the caller
// sees zero endpoints and has no signal that the API was misused. The typed
// sentinel makes the failure mode loud.
//
// Sister-test for the doc-comment claim that the C-4 path requires the tree
// to have been parsed via this package's parseTree.
func TestEndpointsReturnsErrTreeNotRegistered(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()

	tree, err := e.parseTree(context.Background(), src)
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	defer tree.Close()
	parsedSources.Lock()
	delete(parsedSources.m, uintptr(unsafe.Pointer(tree)))
	parsedSources.Unlock()

	_, err = e.Endpoints(tree, "users.proto")
	if !errors.Is(err, ErrTreeNotRegistered) {
		t.Fatalf("Endpoints(untracked tree) err = %v; want errors.Is(err, ErrTreeNotRegistered)", err)
	}
}

func TestEndpointsNilTreeDegrades(t *testing.T) {
	e := New()
	eps, err := e.Endpoints(nil, "any.proto")
	if err != nil {
		t.Errorf("Endpoints(nil) err = %v; want nil", err)
	}
	if eps != nil {
		t.Errorf("Endpoints(nil) eps = %+v; want nil", eps)
	}
}
