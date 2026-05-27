// go:build cgo
//go:build cgo
// +build cgo

package store

import (
	"context"
	"errors"
	"testing"
)

func edgeStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"pkg/x.A", "pkg/x.B"} {
		n := sampleNode()
		n.NodeID = id
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seed node %s: %v", id, err)
		}
	}
	return s, ctx
}

func TestUpsertEdgeRoundTrip(t *testing.T) {
	s, ctx := edgeStore(t)
	e := Edge{
		SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeCalls),
		Confidence: ConfExactVTA, Reachable: boolPtr(true),
		SiteFile: "x.go", SiteLine: 42,
	}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	got, err := s.ListEdgesBySource(ctx, "pkg/x.A", EdgeCalls)
	if err != nil {
		t.Fatalf("ListEdgesBySource: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d edges; want 1", len(got))
	}
	if got[0].TargetID != "pkg/x.B" || got[0].Confidence != ConfExactVTA {
		t.Errorf("edge mismatch: %+v", got[0])
	}
	if got[0].Reachable == nil || *got[0].Reachable != true {
		t.Errorf("reachable not round-tripped: %+v", got[0].Reachable)
	}
}

func TestUpsertEdgeNilReachable(t *testing.T) {
	s, ctx := edgeStore(t)
	e := Edge{
		SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeImplements),
		Confidence: ConfExactCHA, Reachable: nil, SiteFile: "x.go", SiteLine: 1,
	}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	got, _ := s.ListEdgesByTarget(ctx, "pkg/x.B", EdgeImplements)
	if len(got) != 1 || got[0].Reachable != nil {
		t.Errorf("nil reachable not preserved as NULL: %+v", got)
	}
}

func TestUpsertEdgeRejectsInvalidConfidence(t *testing.T) {
	s, ctx := edgeStore(t)
	e := Edge{
		SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeCalls),
		Confidence: Confidence("made_up"), SiteFile: "x.go", SiteLine: 7,
	}
	if err := s.UpsertEdge(ctx, e); err == nil {
		t.Fatal("UpsertEdge accepted invalid confidence; inv-zen-233 requires rejection")
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM graph_edges`).Scan(&count)
	if count != 0 {
		t.Errorf("invalid edge was persisted (count=%d)", count)
	}
}

func TestListEdgesByTargetIsBlastRadiusPath(t *testing.T) {
	s, ctx := edgeStore(t)
	c := sampleNode()
	c.NodeID = "pkg/x.C"
	_ = s.UpsertNode(ctx, c)
	for _, src := range []string{"pkg/x.A", "pkg/x.C"} {
		_ = s.UpsertEdge(ctx, Edge{SourceID: src, TargetID: "pkg/x.B", Kind: string(EdgeCalls), Confidence: ConfExactStatic, SiteFile: "f", SiteLine: 1})
	}
	got, err := s.ListEdgesByTarget(ctx, "pkg/x.B", EdgeCalls)
	if err != nil {
		t.Fatalf("ListEdgesByTarget: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 callers of B, got %d", len(got))
	}
	if got[0].SourceID != "pkg/x.A" || got[1].SourceID != "pkg/x.C" {
		t.Errorf("callers not ordered by source_id: %+v", got)
	}
}

func TestUpsertCoChangeCanonicalOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertCoChange(ctx, CoChange{FileA: "z.go", FileB: "a.go", SharedRevs: 3, RevsA: 5, RevsB: 6, WindowDays: 90, UpdatedAt: 100}); err != nil {
		t.Fatalf("UpsertCoChange: %v", err)
	}

	got, err := s.GetCoChange(ctx, "a.go", "z.go", 90)
	if err != nil {
		t.Fatalf("GetCoChange: %v", err)
	}
	if got.FileA != "a.go" || got.FileB != "z.go" {
		t.Errorf("not canonicalized: %+v", got)
	}
	if got.SharedRevs != 3 {
		t.Errorf("shared_revs = %d; want 3", got.SharedRevs)
	}

	if got.RevsA != 6 || got.RevsB != 5 {
		t.Errorf("per-file counts not swapped: RevsA=%d RevsB=%d; want 6,5", got.RevsA, got.RevsB)
	}

	if err := s.UpsertCoChange(ctx, CoChange{FileA: "a.go", FileB: "z.go", SharedRevs: 4, RevsA: 7, RevsB: 8, WindowDays: 90, UpdatedAt: 200}); err != nil {
		t.Fatalf("UpsertCoChange 2: %v", err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM co_change_matrix`).Scan(&count)
	if count != 1 {
		t.Errorf("co_change_matrix count = %d; want 1 (canonical collapse)", count)
	}
}

func TestUpsertChurnRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := Churn{Path: "x.go", WindowDays: 90, TouchCount: 12, AuthorCount: 3, LastTouched: 100, UpdatedAt: 200}
	if err := s.UpsertChurn(ctx, in); err != nil {
		t.Fatalf("UpsertChurn: %v", err)
	}
	got, err := s.GetChurn(ctx, "x.go", 90)
	if err != nil {
		t.Fatalf("GetChurn: %v", err)
	}
	if got != in {
		t.Errorf("churn round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestUpsertADRLinkAndStale(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	link := ADRLink{ADRID: "docs/decisions/0100-x.md", NodeID: "pkg/x.A", PackageID: "", LinkKind: string(LinkExplicitRef), Confidence: 0.9, Stale: false}
	if err := s.UpsertADRLink(ctx, link); err != nil {
		t.Fatalf("UpsertADRLink: %v", err)
	}
	if err := s.SetADRLinkStale(ctx, "docs/decisions/0100-x.md", "pkg/x.A", LinkExplicitRef, true); err != nil {
		t.Fatalf("SetADRLinkStale: %v", err)
	}
	got, err := s.ListADRLinksForNode(ctx, "pkg/x.A")
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	if len(got) != 1 || !got[0].Stale {
		t.Errorf("stale not set: %+v", got)
	}
}

func TestUpsertLoreTrailerAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	lt := LoreTrailer{CommitSHA: "abc123", FilePath: "x.go", NodeID: "pkg/x.A", TrailerKind: string(TrailerConstraint), Body: "no net/http in this package", AuthoredAt: 1700000000}
	if err := s.UpsertLoreTrailer(ctx, lt); err != nil {
		t.Fatalf("UpsertLoreTrailer: %v", err)
	}

	if err := s.UpsertLoreTrailer(ctx, lt); err != nil {
		t.Fatalf("UpsertLoreTrailer 2: %v", err)
	}
	got, err := s.ListLoreTrailersForNode(ctx, "pkg/x.A")
	if err != nil {
		t.Fatalf("ListLoreTrailersForNode: %v", err)
	}
	if len(got) != 1 || got[0].Body != "no net/http in this package" {
		t.Errorf("lore trailer round-trip: %+v", got)
	}
}

func TestUpsertEdgeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	e := Edge{SourceID: "a", TargetID: "b", Kind: string(EdgeCalls), Confidence: ConfExactStatic, SiteFile: "f", SiteLine: 1}
	if err := s.UpsertEdge(context.Background(), e); err == nil {
		t.Error("UpsertEdge(closed db) returned nil; want error")
	}
}

func TestListEdgesByTargetClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.ListEdgesByTarget(context.Background(), "b", EdgeCalls); err == nil {
		t.Error("ListEdgesByTarget(closed db) returned nil; want error")
	}
}

func TestListEdgesBySourceClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.ListEdgesBySource(context.Background(), "a", EdgeCalls); err == nil {
		t.Error("ListEdgesBySource(closed db) returned nil; want error")
	}
}

func TestScanEdgesScanError(t *testing.T) {
	s, ctx := edgeStore(t)
	_ = s.UpsertEdge(ctx, Edge{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeCalls), Confidence: ConfExactStatic, SiteFile: "f", SiteLine: 1})

	rows, err := s.db.QueryContext(ctx, `SELECT source_id FROM graph_edges WHERE kind = ?`, string(EdgeCalls))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	_, scanErr := scanEdges(rows)
	if scanErr == nil {
		t.Error("scanEdges(1-col row) returned nil; want scan error")
	}
}

func TestUpsertCoChangeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.UpsertCoChange(context.Background(), CoChange{FileA: "a.go", FileB: "b.go", WindowDays: 30}); err == nil {
		t.Error("UpsertCoChange(closed db) returned nil; want error")
	}
}

func TestGetCoChangeNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCoChange(context.Background(), "missing.go", "other.go", 90)
	if err == nil {
		t.Fatal("GetCoChange(absent) returned nil; want ErrNotFound")
	}
	if !isErrNotFound(err) {
		t.Errorf("GetCoChange(absent) = %v; want ErrNotFound", err)
	}
}

func TestGetCoChangeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.GetCoChange(context.Background(), "a.go", "b.go", 30); err == nil {
		t.Error("GetCoChange(closed db) returned nil; want error")
	}
}

func TestUpsertChurnClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.UpsertChurn(context.Background(), Churn{Path: "x.go", WindowDays: 30}); err == nil {
		t.Error("UpsertChurn(closed db) returned nil; want error")
	}
}

func TestGetChurnNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetChurn(context.Background(), "missing.go", 90)
	if err == nil {
		t.Fatal("GetChurn(absent) returned nil; want ErrNotFound")
	}
	if !isErrNotFound(err) {
		t.Errorf("GetChurn(absent) = %v; want ErrNotFound", err)
	}
}

func TestGetChurnClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.GetChurn(context.Background(), "x.go", 30); err == nil {
		t.Error("GetChurn(closed db) returned nil; want error")
	}
}

func TestUpsertADRLinkClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.UpsertADRLink(context.Background(), ADRLink{ADRID: "a", NodeID: "b", LinkKind: string(LinkExplicitRef)}); err == nil {
		t.Error("UpsertADRLink(closed db) returned nil; want error")
	}
}

func TestSetADRLinkStaleClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.SetADRLinkStale(context.Background(), "adr", "node", LinkExplicitRef, true); err == nil {
		t.Error("SetADRLinkStale(closed db) returned nil; want error")
	}
}

func TestListADRLinksForNodeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.ListADRLinksForNode(context.Background(), "node"); err == nil {
		t.Error("ListADRLinksForNode(closed db) returned nil; want error")
	}
}

func TestListADRLinksForNodeEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListADRLinksForNode(context.Background(), "no/such.Node")
	if err != nil {
		t.Fatalf("ListADRLinksForNode(empty): %v", err)
	}
	if got == nil {
		t.Error("ListADRLinksForNode returned nil; want non-nil empty slice")
	}
}

func TestUpsertLoreTrailerClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if err := s.UpsertLoreTrailer(context.Background(), LoreTrailer{CommitSHA: "c", TrailerKind: string(TrailerConstraint), Body: "b", AuthoredAt: 1}); err == nil {
		t.Error("UpsertLoreTrailer(closed db) returned nil; want error")
	}
}

func TestListLoreTrailersForNodeClosedDB(t *testing.T) {
	s := newClosedStore(t)
	if _, err := s.ListLoreTrailersForNode(context.Background(), "node"); err == nil {
		t.Error("ListLoreTrailersForNode(closed db) returned nil; want error")
	}
}

func TestListLoreTrailersForNodeEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListLoreTrailersForNode(context.Background(), "no/such.Node")
	if err != nil {
		t.Fatalf("ListLoreTrailersForNode(empty): %v", err)
	}
	if got == nil {
		t.Error("ListLoreTrailersForNode returned nil; want non-nil empty slice")
	}
}

func TestUpsertEdgeIsUpsert(t *testing.T) {
	s, ctx := edgeStore(t)
	e := Edge{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeCalls), Confidence: ConfExactStatic, SiteFile: "f", SiteLine: 5}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge 1: %v", err)
	}
	e.Confidence = ConfExactVTA
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge 2: %v", err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM graph_edges`).Scan(&count)
	if count != 1 {
		t.Errorf("graph_edges count = %d; want 1 (upsert, not duplicate)", count)
	}
}

func TestUpsertEdgeFalseReachable(t *testing.T) {
	s, ctx := edgeStore(t)
	e := Edge{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(EdgeCalls), Confidence: ConfExactVTA, Reachable: boolPtr(false), SiteFile: "f", SiteLine: 99}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	got, _ := s.ListEdgesBySource(ctx, "pkg/x.A", EdgeCalls)
	if len(got) != 1 || got[0].Reachable == nil || *got[0].Reachable != false {
		t.Errorf("false reachable not preserved: %+v", got)
	}
}

func TestListEdgesBySourceEmpty(t *testing.T) {
	s, ctx := edgeStore(t)
	got, err := s.ListEdgesBySource(ctx, "pkg/x.A", EdgeCalls)
	if err != nil {
		t.Fatalf("ListEdgesBySource(empty): %v", err)
	}
	if got == nil {
		t.Error("ListEdgesBySource returned nil; want non-nil empty slice")
	}
}

func TestListEdgesByTargetEmpty(t *testing.T) {
	s, ctx := edgeStore(t)
	got, err := s.ListEdgesByTarget(ctx, "pkg/x.B", EdgeCalls)
	if err != nil {
		t.Fatalf("ListEdgesByTarget(empty): %v", err)
	}
	if got == nil {
		t.Error("ListEdgesByTarget returned nil; want non-nil empty slice")
	}
}

func TestUpsertChurnIsUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := Churn{Path: "y.go", WindowDays: 30, TouchCount: 5, AuthorCount: 1, LastTouched: 50, UpdatedAt: 100}
	if err := s.UpsertChurn(ctx, in); err != nil {
		t.Fatalf("UpsertChurn 1: %v", err)
	}
	in.TouchCount = 10
	in.UpdatedAt = 200
	if err := s.UpsertChurn(ctx, in); err != nil {
		t.Fatalf("UpsertChurn 2: %v", err)
	}
	got, err := s.GetChurn(ctx, "y.go", 30)
	if err != nil {
		t.Fatalf("GetChurn: %v", err)
	}
	if got.TouchCount != 10 {
		t.Errorf("UpsertChurn did not update: touch_count=%d; want 10", got.TouchCount)
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM churn_metrics`).Scan(&count)
	if count != 1 {
		t.Errorf("churn_metrics count = %d; want 1 (upsert, not duplicate)", count)
	}
}

func TestADRLinkIsUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	l1 := ADRLink{ADRID: "docs/adr/0001.md", NodeID: "pkg/x.F", LinkKind: string(LinkSemantic), Confidence: 0.7, Stale: false}
	if err := s.UpsertADRLink(ctx, l1); err != nil {
		t.Fatalf("UpsertADRLink 1: %v", err)
	}
	l1.Confidence = 0.95
	l1.Stale = true
	if err := s.UpsertADRLink(ctx, l1); err != nil {
		t.Fatalf("UpsertADRLink 2: %v", err)
	}
	got, err := s.ListADRLinksForNode(ctx, "pkg/x.F")
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 ADR link, got %d", len(got))
	}
	if got[0].Confidence != 0.95 || !got[0].Stale {
		t.Errorf("upsert did not update: %+v", got[0])
	}
}

func isErrNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrNotFound)
}

func TestGetCoChangeClosedDBNonNoRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE co_change_matrix`); err != nil {
		t.Fatalf("drop co_change_matrix: %v", err)
	}
	_, err := s.GetCoChange(ctx, "a.go", "b.go", 30)
	if err == nil {
		t.Error("GetCoChange(missing table) returned nil; want error")
	}
	if isErrNotFound(err) {
		t.Errorf("GetCoChange(missing table) returned ErrNotFound; want a real DB error: %v", err)
	}
}

func TestGetChurnClosedDBNonNoRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE churn_metrics`); err != nil {
		t.Fatalf("drop churn_metrics: %v", err)
	}
	_, err := s.GetChurn(ctx, "x.go", 30)
	if err == nil {
		t.Error("GetChurn(missing table) returned nil; want error")
	}
	if isErrNotFound(err) {
		t.Errorf("GetChurn(missing table) returned ErrNotFound; want a real DB error: %v", err)
	}
}
