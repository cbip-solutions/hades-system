//go:build !cgo
// +build !cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func TestInsertAPICallCGODisabled(t *testing.T) {
	var s Store
	err := s.InsertAPICall(context.Background(), APICall{})
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("InsertAPICall (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestGetAPICallCGODisabled(t *testing.T) {
	var s Store
	_, err := s.GetAPICall(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("GetAPICall (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestListAPICallsByCallerCGODisabled(t *testing.T) {
	var s Store
	_, err := s.ListAPICallsByCaller(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("ListAPICallsByCaller (!cgo) err = %v; want ErrCGODisabled", err)
	}
}

func TestDeleteAPICallsByFileCGODisabled(t *testing.T) {
	var s Store
	n, err := s.DeleteAPICallsByFile(context.Background(), "x")
	if !errors.Is(err, ErrCGODisabled) {
		t.Fatalf("DeleteAPICallsByFile (!cgo) err = %v; want ErrCGODisabled", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}
