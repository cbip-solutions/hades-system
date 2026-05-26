//go:build !cgo
// +build !cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func TestInsertAPIEndpointCGODisabled(t *testing.T) {
	var s Store
	err := s.InsertAPIEndpoint(context.Background(), APIEndpoint{})
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("InsertAPIEndpoint (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestGetAPIEndpointCGODisabled(t *testing.T) {
	var s Store
	_, err := s.GetAPIEndpoint(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("GetAPIEndpoint (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestListAPIEndpointsByFileCGODisabled(t *testing.T) {
	var s Store
	_, err := s.ListAPIEndpointsByFile(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("ListAPIEndpointsByFile (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestDeleteAPIEndpointsByFileCGODisabled(t *testing.T) {
	var s Store
	n, err := s.DeleteAPIEndpointsByFile(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("DeleteAPIEndpointsByFile (!cgo) err = %v; want ErrCGODisabled", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}
