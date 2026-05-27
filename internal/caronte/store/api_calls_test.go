// go:build cgo
//go:build cgo
// +build cgo

package store

import (
	"context"
	"errors"
	"testing"
)

var apiCallConfidenceTiers = []string{"exact_proto_import", "spec_artifact", "static_path", "fuzzy_path"}

func sampleAPICall() APICall {
	return APICall{
		CallID:             "github.com/acme/ui:internal/client.GetUser:42",
		Repo:               "github.com/acme/ui",
		CallerNodeID:       "internal/client.GetUser",
		TargetMethod:       "GET",
		TargetPathTemplate: "/users/{id}",
		BaseURLRef:         "BACKEND_URL",
		Confidence:         "static_path",
		ExtractedAt:        1716480000,
		ExtractorID:        "gohttp-client@v1",
	}
}

func TestInsertAPICallRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleAPICall()
	if err := s.InsertAPICall(ctx, in); err != nil {
		t.Fatalf("InsertAPICall: %v", err)
	}
	got, err := s.GetAPICall(ctx, in.CallID)
	if err != nil {
		t.Fatalf("GetAPICall: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestInsertAPICallIsUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := sampleAPICall()
	if err := s.InsertAPICall(ctx, in); err != nil {
		t.Fatalf("InsertAPICall 1: %v", err)
	}
	in.Confidence = "spec_artifact"
	in.ExtractedAt = 1716480100
	if err := s.InsertAPICall(ctx, in); err != nil {
		t.Fatalf("InsertAPICall 2: %v", err)
	}
	got, err := s.GetAPICall(ctx, in.CallID)
	if err != nil {
		t.Fatalf("GetAPICall: %v", err)
	}
	if got.Confidence != "spec_artifact" || got.ExtractedAt != 1716480100 {
		t.Errorf("upsert did not update mutable fields: %+v", got)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_calls`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("api_calls count = %d; want 1 (upsert, not duplicate)", count)
	}
}

func TestInsertAPICallPerConfidenceTierHappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, tier := range apiCallConfidenceTiers {
		c := APICall{
			CallID:       "c-" + tier,
			Repo:         "r",
			CallerNodeID: "n",
			Confidence:   tier,
			ExtractedAt:  int64(1716480000 + i),
			ExtractorID:  "x",
		}
		if err := s.InsertAPICall(ctx, c); err != nil {
			t.Errorf("InsertAPICall(confidence=%s): %v", tier, err)
		}
	}
}

func TestInsertAPICallRefusesForgedConfidence(t *testing.T) {
	s := newTestStore(t)
	c := sampleAPICall()
	c.Confidence = "forged-tier"
	err := s.InsertAPICall(context.Background(), c)
	if err == nil {
		t.Fatal("InsertAPICall(forged confidence) returned nil; want CHECK refusal")
	}
}

func TestGetAPICallNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAPICall(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAPICall(missing) err = %v; want ErrNotFound", err)
	}
}

func TestInsertAPICallEmptyDB(t *testing.T) {
	var s Store
	err := s.InsertAPICall(context.Background(), sampleAPICall())
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("InsertAPICall(nil db) err = %v; want ErrEmptyDB", err)
	}
}

func TestGetAPICallEmptyDB(t *testing.T) {
	var s Store
	_, err := s.GetAPICall(context.Background(), "any/id")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("GetAPICall(nil db) err = %v; want ErrEmptyDB", err)
	}
}

func TestListAPICallsByCallerEmptyDB(t *testing.T) {
	var s Store
	out, err := s.ListAPICallsByCaller(context.Background(), "any/node")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("ListAPICallsByCaller(nil db) err = %v; want ErrEmptyDB", err)
	}
	if out != nil {
		t.Errorf("ListAPICallsByCaller(nil db) out = %v; want nil", out)
	}
}

func TestListAPICallsByCaller(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustInsertAPICall(t, s, APICall{CallID: "c1", Repo: "r", CallerNodeID: "n-A", Confidence: "static_path", ExtractedAt: 1, ExtractorID: "x"})
	mustInsertAPICall(t, s, APICall{CallID: "c2", Repo: "r", CallerNodeID: "n-A", Confidence: "static_path", ExtractedAt: 2, ExtractorID: "x"})
	mustInsertAPICall(t, s, APICall{CallID: "c3", Repo: "r", CallerNodeID: "n-B", Confidence: "static_path", ExtractedAt: 3, ExtractorID: "x"})
	got, err := s.ListAPICallsByCaller(ctx, "n-A")
	if err != nil {
		t.Fatalf("ListAPICallsByCaller(n-A): %v", err)
	}
	if len(got) != 2 || got[0].CallID != "c1" || got[1].CallID != "c2" {
		t.Errorf("ListAPICallsByCaller(n-A) = %v; want [c1 c2]", got)
	}
	gotEmpty, err := s.ListAPICallsByCaller(ctx, "n-missing")
	if err != nil {
		t.Fatalf("ListAPICallsByCaller(missing): %v", err)
	}
	if len(gotEmpty) != 0 {
		t.Errorf("ListAPICallsByCaller(missing) = %v; want []", gotEmpty)
	}

	if gotEmpty == nil {
		t.Errorf("ListAPICallsByCaller: expected non-nil empty slice (per the doc contract), got nil")
	}
}

func TestDeleteAPICallsByFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustUpsertNode(t, s, Node{NodeID: "n-A1", Name: "C1", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h"})
	mustUpsertNode(t, s, Node{NodeID: "n-A2", Name: "C2", Kind: "func", Language: "go", FilePath: "pkg/a.go", ContentHash: "h"})
	mustUpsertNode(t, s, Node{NodeID: "n-B1", Name: "CB", Kind: "func", Language: "go", FilePath: "pkg/b.go", ContentHash: "h"})
	mustInsertAPICall(t, s, APICall{CallID: "c-A1", Repo: "r", CallerNodeID: "n-A1", Confidence: "static_path", ExtractedAt: 1, ExtractorID: "x"})
	mustInsertAPICall(t, s, APICall{CallID: "c-A2", Repo: "r", CallerNodeID: "n-A2", Confidence: "static_path", ExtractedAt: 1, ExtractorID: "x"})
	mustInsertAPICall(t, s, APICall{CallID: "c-B1", Repo: "r", CallerNodeID: "n-B1", Confidence: "static_path", ExtractedAt: 1, ExtractorID: "x"})
	n, err := s.DeleteAPICallsByFile(ctx, "pkg/a.go")
	if err != nil {
		t.Fatalf("DeleteAPICallsByFile(a): %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2 (A1+A2)", n)
	}
	if _, err := s.GetAPICall(ctx, "c-B1"); err != nil {
		t.Errorf("c-B1 should survive file-A sweep: %v", err)
	}
	for _, id := range []string{"c-A1", "c-A2"} {
		if _, err := s.GetAPICall(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s should be gone after file-A sweep; err = %v", id, err)
		}
	}
}

func TestDeleteAPICallsByFileEmptyNoop(t *testing.T) {
	s := newTestStore(t)
	n, err := s.DeleteAPICallsByFile(context.Background(), "pkg/never-indexed.go")
	if err != nil {
		t.Fatalf("DeleteAPICallsByFile(empty): %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d; want 0 (no-op)", n)
	}
}

func TestDeleteAPICallsByFileEmptyDB(t *testing.T) {
	var s Store
	n, err := s.DeleteAPICallsByFile(context.Background(), "pkg/x.go")
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("DeleteAPICallsByFile(nil db) err = %v; want ErrEmptyDB", err)
	}
	if n != 0 {
		t.Errorf("rows = %d; want 0", n)
	}
}
