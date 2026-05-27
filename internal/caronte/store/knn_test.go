// go:build cgo
//go:build cgo
// +build cgo

package store

import (
	"context"
	"testing"
)

func knnNode(t *testing.T, s *Store, ctx context.Context, id string, hotDim int) {
	t.Helper()
	n := sampleNode()
	n.NodeID = id
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode %s: %v", id, err)
	}
	emb := make([]float32, 1536)
	emb[hotDim] = 1.0
	if err := s.UpsertNodeVector(ctx, id, emb); err != nil {
		t.Fatalf("UpsertNodeVector %s: %v", id, err)
	}
}

func TestKNNNodeIDsOrdersByDistance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	knnNode(t, s, ctx, "pkg/x.A", 0)
	knnNode(t, s, ctx, "pkg/x.B", 1)
	knnNode(t, s, ctx, "pkg/x.C", 2)

	query := make([]float32, 1536)
	query[1] = 1.0
	got, err := s.KNNNodeIDs(ctx, query, 3)
	if err != nil {
		t.Fatalf("KNNNodeIDs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d neighbours; want 3", len(got))
	}
	if got[0].NodeID != "pkg/x.B" {
		t.Errorf("nearest = %q; want pkg/x.B (aligned axis)", got[0].NodeID)
	}

	for i := 1; i < len(got); i++ {
		if got[i].Distance < got[i-1].Distance {
			t.Errorf("distances not ascending: [%d]=%v < [%d]=%v", i, got[i].Distance, i-1, got[i-1].Distance)
		}
	}
}

func TestKNNNodeIDsCapsK(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	knnNode(t, s, ctx, "pkg/x.A", 0)
	knnNode(t, s, ctx, "pkg/x.B", 1)
	knnNode(t, s, ctx, "pkg/x.C", 2)

	query := make([]float32, 1536)
	query[0] = 1.0
	got, err := s.KNNNodeIDs(ctx, query, 2)
	if err != nil {
		t.Fatalf("KNNNodeIDs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d neighbours; want k=2", len(got))
	}
}

func TestKNNNodeIDsWrongDim(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.KNNNodeIDs(context.Background(), make([]float32, 384), 3); err == nil {
		t.Error("KNNNodeIDs(384-d) returned nil; want dimension error")
	}
}

func TestKNNNodeIDsEmptyIndex(t *testing.T) {
	s := newTestStore(t)
	query := make([]float32, 1536)
	query[0] = 1.0
	got, err := s.KNNNodeIDs(context.Background(), query, 5)
	if err != nil {
		t.Fatalf("KNNNodeIDs on empty index: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d neighbours on empty index; want 0", len(got))
	}
}

func TestKNNNodeIDsNonPositiveK(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	knnNode(t, s, ctx, "pkg/x.A", 0)
	query := make([]float32, 1536)
	query[0] = 1.0
	got, err := s.KNNNodeIDs(ctx, query, 0)
	if err != nil {
		t.Fatalf("KNNNodeIDs(k=0): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("k=0 returned %d; want 0", len(got))
	}
}

func TestKNNNodeIDsClosedDB(t *testing.T) {
	s := newClosedStore(t)
	query := make([]float32, 1536)
	query[0] = 1.0
	_, err := s.KNNNodeIDs(context.Background(), query, 3)
	if err == nil {
		t.Error("KNNNodeIDs(closed db) returned nil; want error")
	}
}

func TestKNNNodeIDsQueryCancelledCtx(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := make([]float32, 1536)
	query[0] = 1.0
	_, err := s.KNNNodeIDs(ctx, query, 3)
	if err == nil {
		t.Error("KNNNodeIDs(cancelled ctx) returned nil; want error")
	}
}

func TestKNNNodeIDsNegativeK(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	knnNode(t, s, ctx, "pkg/x.A", 0)
	query := make([]float32, 1536)
	query[0] = 1.0
	got, err := s.KNNNodeIDs(ctx, query, -1)
	if err != nil {
		t.Fatalf("KNNNodeIDs(k=-1): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("k=-1 returned %d; want 0", len(got))
	}
}
