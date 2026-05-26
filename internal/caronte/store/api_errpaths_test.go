//go:build cgo
// +build cgo

package store

import (
	"context"
	"testing"
)

func TestGetAPIEndpointClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.GetAPIEndpoint(context.Background(), "any/id")
	if err == nil {
		t.Error("GetAPIEndpoint(closed db) returned nil; want error")
	}
}

func TestGetAPICallClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.GetAPICall(context.Background(), "any/id")
	if err == nil {
		t.Error("GetAPICall(closed db) returned nil; want error")
	}
}

func TestListAPIEndpointsByFileClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.ListAPIEndpointsByFile(context.Background(), "any/file")
	if err == nil {
		t.Error("ListAPIEndpointsByFile(closed db) returned nil; want error")
	}
}

func TestListAPICallsByCallerClosedDB(t *testing.T) {
	s := newClosedStore(t)
	_, err := s.ListAPICallsByCaller(context.Background(), "any/node")
	if err == nil {
		t.Error("ListAPICallsByCaller(closed db) returned nil; want error")
	}
}

func TestDeleteAPIEndpointsByFileClosedDB(t *testing.T) {
	s := newClosedStore(t)
	n, err := s.DeleteAPIEndpointsByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("DeleteAPIEndpointsByFile(closed db) returned nil; want error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestDeleteAPICallsByFileClosedDB(t *testing.T) {
	s := newClosedStore(t)
	n, err := s.DeleteAPICallsByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("DeleteAPICallsByFile(closed db) returned nil; want error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestDeleteAPIEndpointsByFileMissingTable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_endpoints`); err != nil {
		t.Fatalf("drop api_endpoints: %v", err)
	}
	n, err := s.DeleteAPIEndpointsByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("DeleteAPIEndpointsByFile with api_endpoints dropped returned nil; want a wrapped Exec error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestDeleteAPICallsByFileMissingTable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_calls`); err != nil {
		t.Fatalf("drop api_calls: %v", err)
	}
	n, err := s.DeleteAPICallsByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("DeleteAPICallsByFile with api_calls dropped returned nil; want a wrapped Exec error")
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0 on error", n)
	}
}

func TestListAPIEndpointsByFileMissingTable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_endpoints`); err != nil {
		t.Fatalf("drop api_endpoints: %v", err)
	}
	_, err := s.ListAPIEndpointsByFile(context.Background(), "pkg/a.go")
	if err == nil {
		t.Error("ListAPIEndpointsByFile with api_endpoints dropped returned nil; want a wrapped QueryContext error")
	}
}

func TestListAPICallsByCallerMissingTable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_calls`); err != nil {
		t.Fatalf("drop api_calls: %v", err)
	}
	_, err := s.ListAPICallsByCaller(context.Background(), "n")
	if err == nil {
		t.Error("ListAPICallsByCaller with api_calls dropped returned nil; want a wrapped QueryContext error")
	}
}

func TestGetAPIEndpointDBError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_endpoints`); err != nil {
		t.Fatalf("drop api_endpoints: %v", err)
	}
	_, err := s.GetAPIEndpoint(context.Background(), "any/id")
	if err == nil {
		t.Error("GetAPIEndpoint with api_endpoints dropped returned nil; want a wrapped DB error")
	}
}

func TestGetAPICallDBError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`DROP TABLE api_calls`); err != nil {
		t.Fatalf("drop api_calls: %v", err)
	}
	_, err := s.GetAPICall(context.Background(), "any/id")
	if err == nil {
		t.Error("GetAPICall with api_calls dropped returned nil; want a wrapped DB error")
	}
}
