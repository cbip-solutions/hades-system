package augment_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestTruncation_UnderBudgetNoOp(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{
			{ClusterID: "c1", TokenCount: 100, Symbols: []string{"FuncA"}},
		},
		EstimatedTokens: 100,
	}
	volatileCtx := augment.VolatileContext{
		FusedResults:    []augment.RRFFusedResult{{NoteID: "n1"}},
		EstimatedTokens: 50,
	}
	gotStatic, gotVolatile, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 200)

	if truncated {
		t.Error("under-budget should not truncate")
	}
	if len(gotStatic.CommunitySummaries) != 1 {
		t.Errorf("static summaries: want 1, got %d", len(gotStatic.CommunitySummaries))
	}
	if len(gotVolatile.FusedResults) != 1 {
		t.Errorf("volatile results: want 1, got %d", len(gotVolatile.FusedResults))
	}
}

func TestTruncation_OverBudgetDropsVolatileFirst(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta:        augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{{ClusterID: "c1", TokenCount: 100, Symbols: []string{"FuncA"}}},
		EstimatedTokens:    100,
	}
	volatileCtx := augment.VolatileContext{
		FusedResults: []augment.RRFFusedResult{
			{NoteID: "n1", Title: "row1", Score: 3.0},
			{NoteID: "n2", Title: "row2", Score: 2.0},
			{NoteID: "n3", Title: "row3", Score: 1.0},
		},
		EstimatedTokens: 300,
	}
	gotStatic, gotVolatile, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 200)

	if !truncated {
		t.Error("over-budget should truncate")
	}
	if len(gotStatic.CommunitySummaries) != 1 {
		t.Errorf("static unchanged: want 1 summary, got %d", len(gotStatic.CommunitySummaries))
	}
	if len(gotVolatile.FusedResults) >= 3 {
		t.Errorf("expected volatile to be trimmed, still has %d", len(gotVolatile.FusedResults))
	}
	if len(gotVolatile.FusedResults) > 0 && gotVolatile.FusedResults[0].NoteID != "n1" {
		t.Errorf("highest-scored should remain; got first NoteID %q", gotVolatile.FusedResults[0].NoteID)
	}
}

func TestTruncation_OverBudgetTrimsSummariesIfVolatileEmpty(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{
			{ClusterID: "c1", TokenCount: 200, Symbols: []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7"}},
			{ClusterID: "c2", TokenCount: 200, Symbols: []string{"S8"}},
		},
		EstimatedTokens: 400,
	}
	volatileCtx := augment.VolatileContext{
		FusedResults:    []augment.RRFFusedResult{},
		EstimatedTokens: 0,
	}
	gotStatic, gotVolatile, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 250)

	if !truncated {
		t.Error("over-budget should truncate when volatile already empty")
	}
	if len(gotVolatile.FusedResults) != 0 {
		t.Errorf("volatile unchanged: want 0, got %d", len(gotVolatile.FusedResults))
	}
	totalRemainingTokens := gotStatic.EstimatedTokens
	if totalRemainingTokens > 250 {
		t.Errorf("static still over budget post-trim: %d > 250", totalRemainingTokens)
	}
}

func TestTruncation_OverBudgetDropsSummariesAfterTrim(t *testing.T) {
	g := &augment.Truncation{}

	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{
			{ClusterID: "c1", TokenCount: 1000, Symbols: []string{"S1"}, Files: []string{"f1"}},
			{ClusterID: "c2", TokenCount: 1000, Symbols: []string{"S2"}, Files: []string{"f2"}},
			{ClusterID: "c3", TokenCount: 1000, Symbols: []string{"S3"}, Files: []string{"f3"}},
		},
		EstimatedTokens: 3000,
	}
	volatileCtx := augment.VolatileContext{}
	gotStatic, _, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 100)

	if !truncated {
		t.Error("expected truncation")
	}
	if len(gotStatic.CommunitySummaries) >= 3 {
		t.Errorf("expected summaries to be dropped, still have %d", len(gotStatic.CommunitySummaries))
	}
}

func TestTruncation_AlwaysKeepProjectMeta(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "internal-platform-x", Doctrine: "max-scope"},
		CommunitySummaries: []augment.CommunitySummary{
			{ClusterID: "c1", TokenCount: 999999, Symbols: []string{"S1"}},
		},
		EstimatedTokens: 999999,
	}
	volatileCtx := augment.VolatileContext{}
	gotStatic, _, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 10)

	if !truncated {
		t.Error("expected truncation under extreme over-budget")
	}
	if gotStatic.ProjectMeta.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectMeta lost: want internal-platform-x, got %q", gotStatic.ProjectMeta.ProjectID)
	}
	if gotStatic.ProjectMeta.Doctrine != "max-scope" {
		t.Errorf("ProjectMeta lost: want max-scope, got %q", gotStatic.ProjectMeta.Doctrine)
	}
}

func TestTruncation_ZeroBudgetEmpties(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta:        augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{{ClusterID: "c1", TokenCount: 10, Symbols: []string{"S"}}},
		EstimatedTokens:    10,
	}
	volatileCtx := augment.VolatileContext{
		FusedResults:    []augment.RRFFusedResult{{NoteID: "n1"}},
		EstimatedTokens: 5,
	}
	gotStatic, gotVolatile, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 0)

	if !truncated {
		t.Error("zero-budget should truncate")
	}
	if len(gotVolatile.FusedResults) != 0 {
		t.Errorf("volatile should be emptied at zero budget, has %d", len(gotVolatile.FusedResults))
	}
	if len(gotStatic.CommunitySummaries) != 0 {
		t.Errorf("summaries should be dropped at zero budget, has %d", len(gotStatic.CommunitySummaries))
	}
	if gotStatic.ProjectMeta.ProjectID != "p" {
		t.Error("ProjectMeta lost at zero budget")
	}
}

func TestTruncation_SymbolsTrimmedToTopN(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{
			{ClusterID: "c1", TokenCount: 200, Symbols: []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8"}},
		},
		EstimatedTokens: 200,
	}
	volatileCtx := augment.VolatileContext{}
	gotStatic, _, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 100)

	if !truncated {
		t.Error("expected truncation")
	}
	if len(gotStatic.CommunitySummaries) > 0 && len(gotStatic.CommunitySummaries[0].Symbols) > augment.MaxSymbolsPerSummary {
		t.Errorf("Symbols not trimmed to MaxSymbolsPerSummary: have %d", len(gotStatic.CommunitySummaries[0].Symbols))
	}
}

func TestTruncation_ZeroBudgetEmptyStaticAndVolatile(t *testing.T) {
	g := &augment.Truncation{}

	gotStatic, gotVolatile, truncated := g.Apply(context.Background(), augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p"},
	}, augment.VolatileContext{}, 0)
	if truncated {
		t.Error("empty inputs at zero budget should not truncate")
	}
	if gotStatic.ProjectMeta.ProjectID != "p" {
		t.Errorf("ProjectMeta should be preserved, got %v", gotStatic.ProjectMeta)
	}
	if len(gotVolatile.FusedResults) != 0 {
		t.Errorf("expected empty volatile, got %d", len(gotVolatile.FusedResults))
	}
}

func TestTruncation_DropsCallersCalleesIfStillOver(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta:        augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{{ClusterID: "c1", TokenCount: 50, Symbols: []string{"S"}}},
		EstimatedTokens:    50,
	}
	volatileCtx := augment.VolatileContext{
		FusedResults:    nil,
		Callers:         []string{"caller-a", "caller-b"},
		Callees:         []string{"callee-a"},
		EstimatedTokens: 100,
	}
	_, gotVolatile, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 30)

	if !truncated {
		t.Error("expected truncation")
	}
	if len(gotVolatile.Callers) != 0 || len(gotVolatile.Callees) != 0 {
		t.Errorf("callers/callees should be dropped, got callers=%v callees=%v", gotVolatile.Callers, gotVolatile.Callees)
	}
}

func TestTruncation_FilesAlsoTrimmed(t *testing.T) {
	g := &augment.Truncation{}
	staticCtx := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{ProjectID: "p", Doctrine: "default"},
		CommunitySummaries: []augment.CommunitySummary{
			{
				ClusterID:  "c1",
				TokenCount: 200,
				Symbols:    []string{"S1"},
				Files:      []string{"f1", "f2", "f3", "f4", "f5", "f6", "f7"},
			},
		},
		EstimatedTokens: 200,
	}
	volatileCtx := augment.VolatileContext{}
	gotStatic, _, truncated := g.Apply(context.Background(), staticCtx, volatileCtx, 100)
	if !truncated {
		t.Error("expected truncation")
	}
	if len(gotStatic.CommunitySummaries) > 0 && len(gotStatic.CommunitySummaries[0].Files) > augment.MaxSymbolsPerSummary {
		t.Errorf("Files not trimmed to MaxSymbolsPerSummary: have %d", len(gotStatic.CommunitySummaries[0].Files))
	}
}
