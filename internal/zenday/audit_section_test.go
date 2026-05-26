package zenday_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

func TestCollectAuditSectionAllOK(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                    "zen-swarm",
				ChainLastVerifyAge:       4 * time.Hour,
				TamperEventsLast7d:       0,
				LitestreamLag:            2 * time.Second,
				S3Reachable:              true,
				ColdArchiveAge:           5 * 24 * time.Hour,
				AdrTransitionsToday:      0,
				ResearchCacheHitRate:     0.73,
				ResearchCacheHitsToday:   12,
				ResearchCacheMissesToday: 4,
				StateLastRegenerateAge:   24 * time.Hour,
				DoctrineName:             "max-scope",
			},
		},
		Now: time.Now(),
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankInfoSummary {
		t.Errorf("all-OK project rank = %v (%d), want RankInfoSummary (%d)", items[0].Rank, items[0].Rank, zenday.RankInfoSummary)
	}
}

func TestCollectAuditSectionWarnAndFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                  "zen-swarm",
				ChainLastVerifyAge:     4 * time.Hour,
				TamperEventsLast7d:     0,
				LitestreamLag:          2 * time.Second,
				S3Reachable:            true,
				ColdArchiveAge:         12 * 24 * time.Hour,
				AdrTransitionsToday:    1,
				ResearchCacheHitRate:   0.73,
				StateLastRegenerateAge: 6 * 24 * time.Hour,
				DoctrineName:           "default",
			},
			{
				Alias:                  "internal-platform-x",
				ChainLastVerifyAge:     12 * time.Hour,
				TamperEventsLast7d:     5,
				LitestreamLag:          1 * time.Second,
				S3Reachable:            true,
				ColdArchiveAge:         3 * 24 * time.Hour,
				AdrTransitionsToday:    0,
				ResearchCacheHitRate:   0.88,
				StateLastRegenerateAge: 2 * 24 * time.Hour,
				DoctrineName:           "max-scope",
			},
		},
		Now: time.Now(),
	}
	items, _ := zenday.CollectAuditSection(context.Background(), deps)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	if items[0].Rank != zenday.RankCritical {
		t.Errorf("FAIL project rank = %v (%d), want RankCritical (%d)", items[0].Rank, items[0].Rank, zenday.RankCritical)
	}
	if items[1].Rank != zenday.RankAlertNeeded {
		t.Errorf("WARN project rank = %v (%d), want RankAlertNeeded (%d)", items[1].Rank, items[1].Rank, zenday.RankAlertNeeded)
	}
}

func TestCollectAuditSectionTruncationMarker(t *testing.T) {

	deps := zenday.AuditSectionDeps{Now: time.Now()}
	for i := 0; i < 10; i++ {
		deps.Projects = append(deps.Projects, zenday.AuditProjectStatus{
			Alias:                  "test-project-" + string(rune('a'+i)),
			ChainLastVerifyAge:     4 * time.Hour,
			TamperEventsLast7d:     0,
			LitestreamLag:          1 * time.Second,
			S3Reachable:            true,
			ColdArchiveAge:         3 * 24 * time.Hour,
			ResearchCacheHitRate:   0.85,
			StateLastRegenerateAge: 2 * 24 * time.Hour,
			DoctrineName:           "default",
		})
	}
	items, _ := zenday.CollectAuditSection(context.Background(), deps)

	if len(items) != 10 {
		t.Errorf("CollectAuditSection should return ALL items uncapped; got %d, want 10", len(items))
	}
}

func TestCollectAuditSectionGoldenFormat(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                     "zen-swarm",
				ChainLastVerifyAge:        4 * time.Hour,
				TamperEventsLast7d:        0,
				LitestreamLag:             2 * time.Second,
				S3Reachable:               true,
				ColdArchiveAge:            12 * 24 * time.Hour,
				AdrTransitionsToday:       1,
				AdrTransitionDescriptions: []string{"ADR-0070 proposed"},
				ResearchCacheHitRate:      0.73,
				ResearchCacheHitsToday:    12,
				ResearchCacheMissesToday:  4,
				StateLastRegenerateAge:    6 * 24 * time.Hour,
				DoctrineName:              "default",
			},
			{
				Alias:                  "internal-platform-x",
				ChainLastVerifyAge:     12 * time.Hour,
				TamperEventsLast7d:     0,
				LitestreamLag:          1 * time.Second,
				S3Reachable:            true,
				ColdArchiveAge:         3 * 24 * time.Hour,
				AdrTransitionsToday:    0,
				ResearchCacheHitRate:   0.88,
				ResearchCacheHitsToday: 22,
				StateLastRegenerateAge: 2 * 24 * time.Hour,
				DoctrineName:           "max-scope",
			},
		},
		Now: time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	rendered := zenday.RenderAuditSection(items, deps.Now)
	expectedSubstrings := []string{
		"## [plan-9 audit + persistence] — 2026-05-07",
		"⚠ Project zen-swarm:",
		"Chain integrity: last verify 4h ago ✓",
		"Backup: Litestream lag 2s ✓; cold archive 12d ago ⚠",
		"ADRs: 1 proposed",
		"Research cache: 73% hit rate today",
		"State: last regenerate 6d ago",
		"✓ Project internal-platform-x:",
		"Research cache: 88% hit rate today",
	}
	for _, s := range expectedSubstrings {
		if !strings.Contains(rendered, s) {
			t.Errorf("rendered output missing substring %q\nGot:\n%s", s, rendered)
		}
	}
}

func TestCollectAuditSectionEmptyProjects(t *testing.T) {
	deps := zenday.AuditSectionDeps{Now: time.Now()}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestCollectAuditSectionContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{Alias: "any", S3Reachable: true, DoctrineName: "default"},
		},
	}
	_, err := zenday.CollectAuditSection(ctx, deps)
	if err == nil {
		t.Error("expected non-nil error on cancelled context, got nil")
	}
}

func TestCollectAuditSectionS3Unreachable(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:        "project-x",
				S3Reachable:  false,
				DoctrineName: "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("S3-unreachable rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestCollectAuditSectionTamperWarn(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:              "project-y",
				TamperEventsLast7d: 2,
				S3Reachable:        true,
				DoctrineName:       "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankAlertNeeded {
		t.Errorf("tamper-warn rank = %v, want RankAlertNeeded", items[0].Rank)
	}
}

func TestCollectAuditSectionChainStaleFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:              "chain-stale",
				ChainLastVerifyAge: 50 * time.Hour,
				TamperEventsLast7d: 0,
				S3Reachable:        true,
				DoctrineName:       "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("stale chain rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestCollectAuditSectionChainStaleWarn(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:              "chain-warn",
				ChainLastVerifyAge: 30 * time.Hour,
				TamperEventsLast7d: 0,
				S3Reachable:        true,
				DoctrineName:       "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankAlertNeeded {
		t.Errorf("stale chain warn rank = %v, want RankAlertNeeded", items[0].Rank)
	}
}

func TestCollectAuditSectionLitestreamLagFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:         "litestream-fail",
				LitestreamLag: 3 * time.Minute,
				S3Reachable:   true,
				DoctrineName:  "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("litestream lag fail rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestCollectAuditSectionLitestreamLagWarn(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:         "litestream-warn",
				LitestreamLag: 90 * time.Second,
				S3Reachable:   true,
				DoctrineName:  "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankAlertNeeded {
		t.Errorf("litestream lag warn rank = %v, want RankAlertNeeded", items[0].Rank)
	}
}

func TestCollectAuditSectionColdArchiveFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:          "cold-archive-fail",
				ColdArchiveAge: 20 * 24 * time.Hour,
				S3Reachable:    true,
				DoctrineName:   "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("cold archive fail rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestCollectAuditSectionCapaFirewallDoctrine(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                  "capa-project",
				S3Reachable:            true,
				StateLastRegenerateAge: 26 * time.Hour,
				DoctrineName:           "capa-firewall",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("capa-firewall state fail rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestCollectAuditSectionStateFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                  "state-fail",
				S3Reachable:            true,
				StateLastRegenerateAge: 8 * 24 * time.Hour,
				DoctrineName:           "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("CollectAuditSection error = %v", err)
	}
	if items[0].Rank != zenday.RankCritical {
		t.Errorf("state fail rank = %v, want RankCritical", items[0].Rank)
	}
}

func TestRenderAuditSectionGlyphWarn(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:          "warn-project",
				S3Reachable:    true,
				ColdArchiveAge: 12 * 24 * time.Hour,
				DoctrineName:   "default",
			},
		},
	}
	items, _ := zenday.CollectAuditSection(context.Background(), deps)
	rendered := zenday.RenderAuditSection(items, deps.Now)
	if !strings.Contains(rendered, "⚠ Project warn-project:") {
		t.Errorf("expected warn glyph in rendered output, got:\n%s", rendered)
	}
}

func TestRenderAuditSectionGlyphFail(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:              "fail-project",
				TamperEventsLast7d: 5,
				S3Reachable:        true,
				DoctrineName:       "default",
			},
		},
	}
	items, _ := zenday.CollectAuditSection(context.Background(), deps)
	rendered := zenday.RenderAuditSection(items, deps.Now)
	if !strings.Contains(rendered, "✗ Project fail-project:") {
		t.Errorf("expected fail glyph in rendered output, got:\n%s", rendered)
	}
}

func TestRenderAuditSectionNoResearchCache(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:                    "no-cache",
				S3Reachable:              true,
				ResearchCacheHitRate:     0.0,
				ResearchCacheHitsToday:   0,
				ResearchCacheMissesToday: 0,
				DoctrineName:             "default",
			},
		},
	}
	items, err := zenday.CollectAuditSection(context.Background(), deps)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	rendered := zenday.RenderAuditSection(items, deps.Now)
	if !strings.Contains(rendered, "Research cache: 0% hit rate today") {
		t.Errorf("expected zero hit rate line, got:\n%s", rendered)
	}
}

func TestCollectAuditSectionHumanDurationMinutes(t *testing.T) {
	deps := zenday.AuditSectionDeps{
		Now: time.Now(),
		Projects: []zenday.AuditProjectStatus{
			{
				Alias:              "minute-test",
				ChainLastVerifyAge: 30 * time.Minute,
				S3Reachable:        true,
				DoctrineName:       "default",
			},
		},
	}
	items, _ := zenday.CollectAuditSection(context.Background(), deps)
	rendered := zenday.RenderAuditSection(items, deps.Now)
	if !strings.Contains(rendered, "last verify 30m ago") {
		t.Errorf("expected '30m' duration, got:\n%s", rendered)
	}
}
