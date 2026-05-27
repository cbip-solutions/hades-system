// go:build cgo
//go:build cgo
// +build cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func sampleAPIEndpoint() APIEndpoint {
	return APIEndpoint{
		EndpointID:       "github.com/acme/svc:http:GET /users/{id}",
		Repo:             "github.com/acme/svc",
		Kind:             string(KindHTTP),
		Method:           "GET",
		PathTemplate:     "/users/{id}",
		HandlerNodeID:    "internal/handler.UsersGet",
		ContractArtifact: "openapi/users.yaml",
		ExtractedAt:      1716480000,
		ExtractorID:      "gohttp/chi@v1",
	}
}

func TestInsertAPIEndpointRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleAPIEndpoint()
	if err := s.InsertAPIEndpoint(ctx, in); err != nil {
		t.Fatalf("InsertAPIEndpoint: %v", err)
	}
	got, err := s.GetAPIEndpoint(ctx, in.EndpointID)
	if err != nil {
		t.Fatalf("GetAPIEndpoint: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestInsertAPIEndpointIsUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleAPIEndpoint()
	if err := s.InsertAPIEndpoint(ctx, in); err != nil {
		t.Fatalf("InsertAPIEndpoint 1: %v", err)
	}

	in.HandlerNodeID = "internal/handler.UsersGetV2"
	in.ExtractedAt = 1716480100
	in.ExtractorID = "gohttp/chi@v2"
	if err := s.InsertAPIEndpoint(ctx, in); err != nil {
		t.Fatalf("InsertAPIEndpoint 2: %v", err)
	}
	got, err := s.GetAPIEndpoint(ctx, in.EndpointID)
	if err != nil {
		t.Fatalf("GetAPIEndpoint: %v", err)
	}
	if got.HandlerNodeID != "internal/handler.UsersGetV2" || got.ExtractorID != "gohttp/chi@v2" || got.ExtractedAt != 1716480100 {
		t.Errorf("upsert did not update mutable fields: %+v", got)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_endpoints`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("api_endpoints count = %d; want 1 (upsert, not duplicate)", count)
	}
}

func TestInsertAPIEndpointPerKindHappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, k := range AllAPIEndpointKinds() {
		e := APIEndpoint{
			EndpointID:    "ep-" + string(k),
			Repo:          "r",
			Kind:          string(k),
			HandlerNodeID: "n",
			ExtractedAt:   int64(1716480000 + i),
			ExtractorID:   "x",
		}

		switch k {
		case KindHTTP:
			e.Method, e.PathTemplate = "GET", "/x"
		case KindGRPC:
			e.ProtoService, e.ProtoRPC = "svc.X", "Do"
		case KindGraphQL:
			e.GraphQLType, e.GraphQLField = "Query", "x"
		case KindMQ:
			e.Topic = "topic.x"
		case KindWS:
			e.PathTemplate = "/ws/x"
		}
		if err := s.InsertAPIEndpoint(ctx, e); err != nil {
			t.Errorf("InsertAPIEndpoint(kind=%s): %v", k, err)
		}
	}
}

func TestInsertAPIEndpointRefusesForgedKind(t *testing.T) {
	s := newTestStore(t)
	e := sampleAPIEndpoint()
	e.Kind = "forged"
	err := s.InsertAPIEndpoint(context.Background(), e)
	if err == nil {
		t.Fatal("InsertAPIEndpoint(forged kind) returned nil; want CHECK refusal")
	}
}

func TestGetAPIEndpointNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAPIEndpoint(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAPIEndpoint(missing) err = %v; want ErrNotFound", err)
	}
}

func TestInsertAPIEndpointEmptyDB(t *testing.T) {
	var s Store
	err := s.InsertAPIEndpoint(context.Background(), sampleAPIEndpoint())
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("InsertAPIEndpoint(nil db) err = %v; want ErrEmptyDB", err)
	}
}

func TestGetAPIEndpointEmptyDB(t *testing.T) {
	var s Store
	_, err := s.GetAPIEndpoint(context.Background(), "any/id")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("GetAPIEndpoint(nil db) err = %v; want ErrEmptyDB", err)
	}
}

func TestListAPIEndpointsByFileEmptyDB(t *testing.T) {
	var s Store
	out, err := s.ListAPIEndpointsByFile(context.Background(), "pkg/x.go")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("ListAPIEndpointsByFile(nil db) err = %v; want ErrEmptyDB", err)
	}
	if out != nil {
		t.Errorf("ListAPIEndpointsByFile(nil db) out = %v; want nil", out)
	}
}

func TestListAPIEndpointsByFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustUpsertNode(t, s, Node{NodeID: "n-A", Name: "HA", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h"})
	mustUpsertNode(t, s, Node{NodeID: "n-B", Name: "HB", Kind: "func", Language: "go", FilePath: "pkg/b.go", ContentHash: "h"})
	mustInsertAPIEndpoint(t, s, APIEndpoint{
		EndpointID: "ep-A", Repo: "r", Kind: string(KindHTTP), Method: "GET", PathTemplate: "/a",
		HandlerNodeID: "n-A", ExtractedAt: 1, ExtractorID: "x",
	})
	mustInsertAPIEndpoint(t, s, APIEndpoint{
		EndpointID: "ep-B", Repo: "r", Kind: string(KindHTTP), Method: "GET", PathTemplate: "/b",
		HandlerNodeID: "n-B", ExtractedAt: 1, ExtractorID: "x",
	})
	gotA, err := s.ListAPIEndpointsByFile(ctx, "pkg/a.go")
	if err != nil {
		t.Fatalf("ListAPIEndpointsByFile(a): %v", err)
	}
	if len(gotA) != 1 || gotA[0].EndpointID != "ep-A" {
		t.Errorf("ListAPIEndpointsByFile(a) = %v; want [ep-A]", gotA)
	}
	gotEmpty, err := s.ListAPIEndpointsByFile(ctx, "pkg/missing.go")
	if err != nil {
		t.Fatalf("ListAPIEndpointsByFile(missing): %v", err)
	}
	if len(gotEmpty) != 0 {
		t.Errorf("ListAPIEndpointsByFile(missing) = %v; want []", gotEmpty)
	}

	if gotEmpty == nil {
		t.Errorf("ListAPIEndpointsByFile: expected non-nil empty slice (per the doc contract), got nil")
	}
}

func TestDeleteAPIEndpointsByFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustUpsertNode(t, s, Node{NodeID: "n-A1", Name: "H1", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h"})
	mustUpsertNode(t, s, Node{NodeID: "n-A2", Name: "H2", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h"})
	mustUpsertNode(t, s, Node{NodeID: "n-B1", Name: "HB", Kind: "func", Language: "go", FilePath: "pkg/b.go", ContentHash: "h"})
	mustInsertAPIEndpoint(t, s, APIEndpoint{EndpointID: "ep-A1", Repo: "r", Kind: string(KindHTTP), HandlerNodeID: "n-A1", ExtractedAt: 1, ExtractorID: "x"})
	mustInsertAPIEndpoint(t, s, APIEndpoint{EndpointID: "ep-A2", Repo: "r", Kind: string(KindHTTP), HandlerNodeID: "n-A2", ExtractedAt: 1, ExtractorID: "x"})
	mustInsertAPIEndpoint(t, s, APIEndpoint{EndpointID: "ep-B1", Repo: "r", Kind: string(KindHTTP), HandlerNodeID: "n-B1", ExtractedAt: 1, ExtractorID: "x"})

	n, err := s.DeleteAPIEndpointsByFile(ctx, "pkg/a.go")
	if err != nil {
		t.Fatalf("DeleteAPIEndpointsByFile(a): %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2 (A1+A2)", n)
	}

	if _, err := s.GetAPIEndpoint(ctx, "ep-B1"); err != nil {
		t.Errorf("ep-B1 should survive file-A sweep: %v", err)
	}

	for _, id := range []string{"ep-A1", "ep-A2"} {
		if _, err := s.GetAPIEndpoint(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s should be gone after file-A sweep; err = %v", id, err)
		}
	}
}

func TestDeleteAPIEndpointsByFileEmptyNoop(t *testing.T) {
	s := newTestStore(t)
	n, err := s.DeleteAPIEndpointsByFile(context.Background(), "pkg/never-indexed.go")
	if err != nil {
		t.Fatalf("DeleteAPIEndpointsByFile(empty): %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d; want 0 (no-op on un-extracted file)", n)
	}
}

func TestDeleteAPIEndpointsByFileEmptyDB(t *testing.T) {
	var s Store
	n, err := s.DeleteAPIEndpointsByFile(context.Background(), "pkg/x.go")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("DeleteAPIEndpointsByFile(nil db) err = %v; want ErrEmptyDB", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}
