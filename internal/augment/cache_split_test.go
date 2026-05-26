package augment_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestCacheSplit_BasicSplit(t *testing.T) {
	cs := &augment.CacheSplit{}
	summaries := []augment.CommunitySummary{
		{
			ClusterID:  "internal/orchestrator/merge",
			Topic:      "function",
			Files:      []string{"internal/orchestrator/merge/engine.go"},
			Symbols:    []string{"Engine.SelectWinner"},
			NoteIDs:    []string{"n1", "n2"},
			TokenCount: 120,
		},
	}
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Engine.SelectWinner", Score: 2.5, ProjectID: "internal-platform-x", LaneIDs: []int{1, 2}, Source: "kg/fts"},
		{NoteID: "n2", Title: "Engine.diff", Score: 2.0, ProjectID: "internal-platform-x", LaneIDs: []int{2}, Source: "fts"},
	}
	meta := augment.ProjectMeta{
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		Stage:     "design",
	}
	staticCtx, volatileCtx := cs.Split(summaries, fused, meta, nil, nil)

	if !reflect.DeepEqual(staticCtx.ProjectMeta, meta) {
		t.Errorf("StaticContext.ProjectMeta: want %+v, got %+v", meta, staticCtx.ProjectMeta)
	}
	if len(staticCtx.CommunitySummaries) != 1 {
		t.Errorf("StaticContext.CommunitySummaries: want 1, got %d", len(staticCtx.CommunitySummaries))
	}
	if staticCtx.EstimatedTokens <= 0 {
		t.Errorf("StaticContext.EstimatedTokens: want > 0, got %d", staticCtx.EstimatedTokens)
	}

	if len(volatileCtx.FusedResults) != 2 {
		t.Errorf("VolatileContext.FusedResults: want 2, got %d", len(volatileCtx.FusedResults))
	}
	if volatileCtx.EstimatedTokens <= 0 {
		t.Errorf("VolatileContext.EstimatedTokens: want > 0, got %d", volatileCtx.EstimatedTokens)
	}
}

func TestCacheSplit_EmptyInputProducesValidEmptyOutput(t *testing.T) {
	cs := &augment.CacheSplit{}
	staticCtx, volatileCtx := cs.Split(nil, nil, augment.ProjectMeta{ProjectID: "x", Doctrine: "default"}, nil, nil)

	if staticCtx.ProjectMeta.ProjectID != "x" {
		t.Errorf("project_id should still be set in static, got %+v", staticCtx.ProjectMeta)
	}
	if len(staticCtx.CommunitySummaries) != 0 {
		t.Errorf("empty summaries: want 0, got %d", len(staticCtx.CommunitySummaries))
	}
	if len(volatileCtx.FusedResults) != 0 {
		t.Errorf("empty fused: want 0, got %d", len(volatileCtx.FusedResults))
	}
	if staticCtx.EstimatedTokens <= 0 {
		t.Errorf("static tokens should account for meta overhead, got %d", staticCtx.EstimatedTokens)
	}
}

func TestCacheSplit_EstimatedTokensReasonable(t *testing.T) {
	cs := &augment.CacheSplit{}
	summaries := []augment.CommunitySummary{
		{ClusterID: "x", Topic: "function", Files: []string{"a.go"}, Symbols: []string{"f"}, NoteIDs: []string{"n1"}, TokenCount: 100},
	}
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: strings.Repeat("a", 200), Score: 1.0, LaneIDs: []int{1}},
	}
	meta := augment.ProjectMeta{ProjectID: "p", Doctrine: "default"}
	staticCtx, volatileCtx := cs.Split(summaries, fused, meta, nil, nil)

	if staticCtx.EstimatedTokens < 100 {
		t.Errorf("static estimated tokens: want >= 100, got %d", staticCtx.EstimatedTokens)
	}
	if volatileCtx.EstimatedTokens < 40 {
		t.Errorf("volatile estimated tokens: want >= 40, got %d", volatileCtx.EstimatedTokens)
	}
}

func TestCacheSplit_VolatileIncludesCallersCalleesIfPresent(t *testing.T) {
	cs := augment.NewCacheSplit()
	summaries := []augment.CommunitySummary{}
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "X", Score: 1.0},
	}
	meta := augment.ProjectMeta{ProjectID: "p", Doctrine: "default"}
	_, volatileCtx := cs.Split(summaries, fused, meta, []string{"orchestrator.Run"}, []string{"engine.diff"})

	if len(volatileCtx.Callers) != 1 || volatileCtx.Callers[0] != "orchestrator.Run" {
		t.Errorf("Callers: want [orchestrator.Run], got %v", volatileCtx.Callers)
	}
	if len(volatileCtx.Callees) != 1 || volatileCtx.Callees[0] != "engine.diff" {
		t.Errorf("Callees: want [engine.diff], got %v", volatileCtx.Callees)
	}
}

// TestCacheSplit_NoCarryoverBetweenCalls pins Plan 11 Phase C fix-cycle
// Minor-10 closure: per-call callers + callees do NOT leak across
// concurrent Splits because they are NEVER stored on the struct. The
// pre-fix WithCallersCallees / one-shot clear pattern had a window
// between the Split body running and the clear running where a
// concurrent Run could observe the prior call's state.
func TestCacheSplit_NoCarryoverBetweenCalls(t *testing.T) {
	cs := augment.NewCacheSplit()
	_, _ = cs.Split(nil, nil, augment.ProjectMeta{ProjectID: "p"}, []string{"a"}, []string{"b"})

	_, vol := cs.Split(nil, nil, augment.ProjectMeta{ProjectID: "p"}, nil, nil)
	if len(vol.Callers) != 0 {
		t.Errorf("expected callers empty in second call, got %v", vol.Callers)
	}
	if len(vol.Callees) != 0 {
		t.Errorf("expected callees empty in second call, got %v", vol.Callees)
	}
}

func TestCacheSplit_NonEmptyVolatileFloors(t *testing.T) {
	cs := &augment.CacheSplit{}

	_, vol := cs.Split(nil, nil, augment.ProjectMeta{ProjectID: "p"}, []string{""}, nil)
	if vol.EstimatedTokens <= 0 {
		t.Errorf("expected positive volatile tokens with caller present, got %d", vol.EstimatedTokens)
	}
}

func TestCacheSplit_VolatileZeroWhenAllEmpty(t *testing.T) {
	cs := &augment.CacheSplit{}
	_, vol := cs.Split(nil, nil, augment.ProjectMeta{ProjectID: "p"}, nil, nil)
	if vol.EstimatedTokens != 0 {
		t.Errorf("expected 0 volatile tokens when no fused/callers/callees, got %d", vol.EstimatedTokens)
	}
}
