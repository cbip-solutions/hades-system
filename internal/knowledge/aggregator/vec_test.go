// go:build cgo
//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"errors"
	"math"
	"testing"
)

func seedVec(t *testing.T) *Aggregator {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	emb := func(seed int) []float32 {
		v := make([]float32, vecDimensions)
		switch seed {
		case 1:
			v[0] = 1.0
		case 2:
			v[0] = 0.95
			v[1] = 0.31
		case 3:
			v[2] = 1.0
		}
		return v
	}

	notes := []PinNote{
		{NoteID: "n1", ProjectID: "p1", Title: "n1", Content: "n1", FrontmatterJSON: "{}",
			PromoteReason: "x", AuditChainAnchor: "a"},
		{NoteID: "n2", ProjectID: "p1", Title: "n2", Content: "n2", FrontmatterJSON: "{}",
			PromoteReason: "x", AuditChainAnchor: "a"},
		{NoteID: "n3", ProjectID: "p1", Title: "n3", Content: "n3", FrontmatterJSON: "{}",
			PromoteReason: "x", AuditChainAnchor: "a"},
	}

	for i, n := range notes {
		_, err := db.Exec(`INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json, promoted_at,
			 promoted_by, promote_reason, audit_chain_anchor)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			n.NoteID, n.ProjectID, n.Title, n.Content, n.FrontmatterJSON,
			"2026-05-07T00:00:00Z", "testuser", n.PromoteReason, n.AuditChainAnchor)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_index[%d]: %v", i, err)
		}
		v := emb(i + 1)
		_, err = db.Exec(`INSERT INTO knowledge_pin_vec (rowid, embedding)
			SELECT rowid, ? FROM knowledge_pin_index WHERE note_id = ?`,
			float32SliceBytes(v), n.NoteID)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_vec[%d]: %v", i, err)
		}
	}

	a, err := New(Options{
		DB:       db,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestQueryVecHappyPath(t *testing.T) {
	a := seedVec(t)
	q := make([]float32, vecDimensions)
	q[0] = 1.0

	results, err := a.QueryVec(context.Background(), q, 10, 0.92)
	if err != nil {
		t.Fatalf("QueryVec: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected ≥2 results above threshold 0.92; got %d", len(results))
	}

	if results[0].NoteID != "n1" {
		t.Errorf("top result NoteID = %q; want \"n1\"", results[0].NoteID)
	}
	if results[0].Source != "vec" {
		t.Errorf("top result Source = %q; want \"vec\"", results[0].Source)
	}

	for _, r := range results {
		if r.Score < 0.92 {
			t.Errorf("result %q has Score %f below threshold 0.92", r.NoteID, r.Score)
		}
	}
}

func TestQueryVecRejectsDimensionMismatch(t *testing.T) {
	a := seedVec(t)
	q := make([]float32, 100)
	_, err := a.QueryVec(context.Background(), q, 10, 0.92)
	if err == nil {
		t.Error("expected dimension mismatch error; got nil")
	}
}

func TestQueryVecEmptyEmbeddingReturnsEmpty(t *testing.T) {
	a := seedVec(t)
	results, err := a.QueryVec(context.Background(), nil, 10, 0.92)
	if err != nil {
		t.Fatalf("QueryVec(nil): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil embedding; got %d", len(results))
	}

	results2, err := a.QueryVec(context.Background(), []float32{}, 10, 0.92)
	if err != nil {
		t.Fatalf("QueryVec([]): %v", err)
	}
	if len(results2) != 0 {
		t.Errorf("expected 0 results for empty embedding; got %d", len(results2))
	}
}

func TestQueryVecThresholdFilter(t *testing.T) {
	a := seedVec(t)
	q := make([]float32, vecDimensions)
	q[0] = 1.0

	results, err := a.QueryVec(context.Background(), q, 10, 0.99)
	if err != nil {
		t.Fatalf("QueryVec: %v", err)
	}

	for _, r := range results {
		if r.Score < 0.99 {
			t.Errorf("result %q leaked through threshold 0.99: Score=%f", r.NoteID, r.Score)
		}
	}

	for _, r := range results {
		if r.NoteID == "n2" {
			t.Errorf("n2 (cos≈0.95) should be filtered at threshold 0.99 but was returned")
		}
	}
}

func TestQueryVecDefaultLimitApplied(t *testing.T) {
	a := seedVec(t)
	q := make([]float32, vecDimensions)
	q[0] = 1.0

	results, err := a.QueryVec(context.Background(), q, 0, 0.92)
	if err != nil {
		t.Fatalf("QueryVec with limit=0: %v", err)
	}

	if len(results) < 2 {
		t.Errorf("limit=0 defaulted badly; expected ≥2 results above 0.92, got %d", len(results))
	}
}

func TestQueryVecDegradedReturnsSentinelError(t *testing.T) {
	a := seedVec(t)

	a.markDegraded()

	q := make([]float32, vecDimensions)
	q[0] = 1.0
	_, err := a.QueryVec(context.Background(), q, 10, 0.92)
	if !errors.Is(err, ErrVecUnavailable) {
		t.Errorf("QueryVec in degraded mode: err = %v; want ErrVecUnavailable", err)
	}
}

func TestErrVecUnavailableExported(t *testing.T) {
	if !errors.Is(ErrVecUnavailable, ErrVecUnavailable) {
		t.Error("ErrVecUnavailable not exported as sentinel")
	}
	if ErrVecUnavailable == nil {
		t.Error("ErrVecUnavailable must not be nil")
	}
}

func TestQueryVecScoreConsistency(t *testing.T) {
	a := seedVec(t)
	q := make([]float32, vecDimensions)
	q[0] = 1.0

	results, err := a.QueryVec(context.Background(), q, 10, 0.0)
	if err != nil {
		t.Fatalf("QueryVec: %v", err)
	}
	for _, r := range results {
		if r.Score < 0 || r.Score > 1.0+1e-6 {
			t.Errorf("result %q Score=%f is outside [0,1] (floating point consistency check)", r.NoteID, r.Score)
		}
		if math.IsNaN(r.Score) {
			t.Errorf("result %q Score is NaN", r.NoteID)
		}
	}
}

func TestFloat32SliceBytesEmpty(t *testing.T) {
	if got := float32SliceBytes(nil); got != nil {
		t.Errorf("float32SliceBytes(nil) = %v; want nil", got)
	}
	if got := float32SliceBytes([]float32{}); got != nil {
		t.Errorf("float32SliceBytes([]) = %v; want nil", got)
	}
}

func TestFloat32SliceBytesSerialization(t *testing.T) {
	input := []float32{1.0, 0.5, -1.0}
	buf := float32SliceBytes(input)
	if len(buf) != 4*len(input) {
		t.Fatalf("float32SliceBytes len = %d; want %d", len(buf), 4*len(input))
	}
	for i, want := range input {

		u := uint32(buf[4*i]) |
			uint32(buf[4*i+1])<<8 |
			uint32(buf[4*i+2])<<16 |
			uint32(buf[4*i+3])<<24
		got := math.Float32frombits(u)
		if got != want {
			t.Errorf("float32SliceBytes[%d] = %f; want %f", i, got, want)
		}
	}
}

func TestIsExtensionMissing(t *testing.T) {
	cases := []struct {
		errMsg string
		want   bool
	}{
		{"no such module: vec0", true},
		{"no such function: vec_distance_cosine", true},
		{"no such table: knowledge_pin_vec", true},
		{"some other SQL error", false},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.errMsg != "" {
			err = errors.New(tc.errMsg)
		}
		if got := isExtensionMissing(err); got != tc.want {
			t.Errorf("isExtensionMissing(%q) = %v; want %v", tc.errMsg, got, tc.want)
		}
	}

	if isExtensionMissing(nil) {
		t.Error("isExtensionMissing(nil) = true; want false")
	}
}

func TestContainsHelper(t *testing.T) {
	if !contains("hello world", "world") {
		t.Error("contains(hello world, world) = false; want true")
	}
	if contains("hello", "xyz") {
		t.Error("contains(hello, xyz) = true; want false")
	}

	if !contains("anything", "") {
		t.Error("contains(anything, ) = false; want true")
	}

	if contains("hi", "hello") {
		t.Error("contains(hi, hello) = true; want false")
	}
}
