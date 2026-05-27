// go:build cgo
package store

import (
	"context"
	"testing"
)

func TestListAPICallsByRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	calls := []APICall{
		{CallID: "a:b:1", Repo: "repo-a", CallerNodeID: "n1", Confidence: "static_path", ExtractedAt: 1, ExtractorID: "x"},
		{CallID: "a:b:2", Repo: "repo-a", CallerNodeID: "n2", Confidence: "fuzzy_path", ExtractedAt: 2, ExtractorID: "x"},
		{CallID: "c:d:1", Repo: "repo-b", CallerNodeID: "n3", Confidence: "static_path", ExtractedAt: 3, ExtractorID: "x"},
	}
	for _, c := range calls {
		if err := s.InsertAPICall(ctx, c); err != nil {
			t.Fatalf("InsertAPICall(%s): %v", c.CallID, err)
		}
	}

	gotA, err := s.ListAPICallsByRepo(ctx, "repo-a")
	if err != nil {
		t.Fatalf("ListAPICallsByRepo(repo-a): %v", err)
	}
	if len(gotA) != 2 {
		t.Errorf("len(repo-a) = %d; want 2", len(gotA))
	}
	if len(gotA) >= 2 && (gotA[0].CallID != "a:b:1" || gotA[1].CallID != "a:b:2") {
		t.Errorf("order drift: %+v; want a:b:1,a:b:2 (call_id ASC)", gotA)
	}

	gotB, err := s.ListAPICallsByRepo(ctx, "repo-b")
	if err != nil {
		t.Fatalf("ListAPICallsByRepo(repo-b): %v", err)
	}
	if len(gotB) != 1 || gotB[0].CallID != "c:d:1" {
		t.Errorf("repo-b = %+v; want 1 row c:d:1", gotB)
	}

	gotMiss, err := s.ListAPICallsByRepo(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListAPICallsByRepo(miss): %v", err)
	}
	if len(gotMiss) != 0 {
		t.Errorf("miss = %d rows; want 0", len(gotMiss))
	}
}

func TestListAPIEndpointsByRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	endpoints := []APIEndpoint{
		{EndpointID: "rA:http:GET /a", Repo: "repo-a", Kind: "http", Method: "GET", PathTemplate: "/a", HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "x"},
		{EndpointID: "rA:http:GET /b", Repo: "repo-a", Kind: "http", Method: "GET", PathTemplate: "/b", HandlerNodeID: "h2", ExtractedAt: 2, ExtractorID: "x"},
		{EndpointID: "rB:http:GET /c", Repo: "repo-b", Kind: "http", Method: "GET", PathTemplate: "/c", HandlerNodeID: "h3", ExtractedAt: 3, ExtractorID: "x"},
	}
	for _, e := range endpoints {
		if err := s.InsertAPIEndpoint(ctx, e); err != nil {
			t.Fatalf("InsertAPIEndpoint(%s): %v", e.EndpointID, err)
		}
	}

	gotA, err := s.ListAPIEndpointsByRepo(ctx, "repo-a")
	if err != nil {
		t.Fatalf("ListAPIEndpointsByRepo(repo-a): %v", err)
	}
	if len(gotA) != 2 {
		t.Errorf("len(repo-a) = %d; want 2", len(gotA))
	}
	if len(gotA) >= 2 && (gotA[0].EndpointID != "rA:http:GET /a" || gotA[1].EndpointID != "rA:http:GET /b") {
		t.Errorf("order drift: %+v; want endpoint_id ASC", gotA)
	}

	gotMiss, err := s.ListAPIEndpointsByRepo(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListAPIEndpointsByRepo(miss): %v", err)
	}
	if len(gotMiss) != 0 {
		t.Errorf("miss = %d; want 0", len(gotMiss))
	}
}
