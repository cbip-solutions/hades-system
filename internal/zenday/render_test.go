package zenday_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(b)
}

func TestRender_MorningBriefMatchesGolden(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{
			{
				Rank:    zenday.RankOperatorGate,
				Project: "internal-platform-x",
				Message: "autonomous-mode paused: HRA L4 alert",
				Action:  "zen autonomy ack internal-platform-x",
			},
			{
				Rank:     zenday.RankCostCapWarning,
				Project:  "nexus",
				Severity: "87.0%",
				Message:  "at 87.0% daily cap — approaches threshold",
			},
		},
		TruncatedCount: 1,
	}
	got := zenday.Render(doc)
	want := loadGolden(t, "morning_golden.md")
	if got != want {
		t.Errorf("Render morning mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRender_EODDigestMatchesGolden(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeEOD,
		PerProjectStatus: []zenday.ProjectStatusSection{
			{
				Alias:           "internal-platform-x",
				AutonomousState: "paused — operator-gate",
				HandoffSummary:  "Stage 4 Build phase 12 complete (47 commits, 0 critical)",
				Tomorrow:        "review L4 finding + resume autonomous",
				Blockers:        []string{"HRA L4 alert raised"},
			},
			{
				Alias:           "zen-swarm",
				AutonomousState: "active",
				HandoffSummary:  "Plan 7 brainstorm Path F Q15 complete; spec write next",
				Tomorrow:        "run /write-plan + dispatch phase writers",
			},
			{
				Alias:           "nexus",
				AutonomousState: "manual",
				HandoffSummary:  "",
			},
		},
		CostWatchUSD: 0.84,
	}
	got := zenday.Render(doc)
	want := loadGolden(t, "eod_golden.md")
	if got != want {
		t.Errorf("Render eod mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRender_CheckPendingMatchesGolden(t *testing.T) {
	doc := zenday.BriefDoc{
		Type:                zenday.BriefTypeCheckPending,
		NextScheduledAt:     time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC),
		PendingActionNeeded: 3,
		PendingUrgent:       1,
	}
	got := zenday.Render(doc)
	want := loadGolden(t, "check_pending_golden.md")
	if got != want {
		t.Errorf("Render check-pending mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRender_PanicsWhenItemsExceedCap(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		var msg string
		switch v := r.(type) {
		case string:
			msg = v
		case error:
			msg = v.Error()
		default:
			t.Fatalf("recovered non-string non-error: %T %v", r, r)
		}
		if !strings.Contains(msg, "MaxBriefItems") && !strings.Contains(msg, "inv-zen-126") {
			t.Errorf("panic msg = %q, want containing MaxBriefItems or inv-zen-126", msg)
		}
	}()
	doc := zenday.BriefDoc{
		Type:  zenday.BriefTypeMorning,
		Items: make([]zenday.BriefItem, zenday.MaxBriefItems+1),
	}
	for i := range doc.Items {
		doc.Items[i] = zenday.BriefItem{Rank: zenday.RankOperatorGate, Message: "x"}
	}
	_ = zenday.Render(doc)
}

func TestRender_PanicsWhenItemsNotSorted(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		var msg string
		switch v := r.(type) {
		case string:
			msg = v
		case error:
			msg = v.Error()
		default:
			t.Fatalf("recovered non-string non-error: %T %v", r, r)
		}
		if !strings.Contains(msg, "IsSorted") && !strings.Contains(msg, "inv-zen-127") {
			t.Errorf("panic msg = %q, want containing IsSorted or inv-zen-127", msg)
		}
	}()
	doc := zenday.BriefDoc{
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{
			{Rank: zenday.RankUrgentEvent, Message: "u"},
			{Rank: zenday.RankOperatorGate, Message: "g"},
		},
	}
	_ = zenday.Render(doc)
}

func TestRender_TruncationMarkerOnlyWhenTruncated(t *testing.T) {
	doc := zenday.BriefDoc{
		Type:           zenday.BriefTypeMorning,
		Items:          []zenday.BriefItem{{Rank: zenday.RankOperatorGate, Message: "x"}},
		TruncatedCount: 0,
	}
	out := zenday.Render(doc)
	if strings.Contains(out, "+ ") && strings.Contains(out, "more in") {
		t.Errorf("truncation marker rendered when TruncatedCount=0; got:\n%s", out)
	}
	doc.TruncatedCount = 5
	out = zenday.Render(doc)
	if !strings.Contains(out, "+ 5 more in `zen inbox") {
		t.Errorf("expected truncation marker '+ 5 more in zen inbox …'; got:\n%s", out)
	}
}

func TestRender_InvalidBriefTypePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on invalid BriefType")
		}
		var msg string
		switch v := r.(type) {
		case string:
			msg = v
		case error:
			msg = v.Error()
		}
		if !strings.Contains(msg, "BriefType") {
			t.Errorf("panic msg = %q, want containing 'BriefType'", msg)
		}
	}()
	doc := zenday.BriefDoc{Type: zenday.BriefType(99)}
	_ = zenday.Render(doc)
}

func TestRender_EmptyMorningBriefStillRenders(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
	}
	out := zenday.Render(doc)
	if !strings.Contains(out, "zen day — 2026-05-01") {
		t.Errorf("missing date heading; got: %s", out)
	}
}

func TestRender_SingleItemMorning(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{
			{Rank: zenday.RankOperatorGate, Project: "internal-platform-x", Message: "x", Action: "zen autonomy ack internal-platform-x"},
		},
	}
	out := zenday.Render(doc)
	if !strings.Contains(out, "## Pending operator action (1)") {
		t.Errorf("expected 'Pending operator action (1)' section; got:\n%s", out)
	}
	if !strings.Contains(out, "**[internal-platform-x]** x") {
		t.Errorf("expected bullet line with project/message; got:\n%s", out)
	}
	if !strings.Contains(out, "`zen autonomy ack internal-platform-x`") {
		t.Errorf("expected action sub-line; got:\n%s", out)
	}
}

func TestRender_SevenItemMorningAtCap(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{
			{Rank: zenday.RankOperatorGate, Project: "p1", Message: "g"},
			{Rank: zenday.RankFailedScheduledJob, Project: "p2", Message: "f"},
			{Rank: zenday.RankUrgentEvent, Project: "p3", Message: "u"},
			{Rank: zenday.RankCostCapWarning, Project: "p4", Message: "c"},
			{Rank: zenday.RankAutonomousMilestone, Project: "p5", Message: "m"},
			{Rank: zenday.RankExternalActivity, Project: "p6", Message: "e"},
			{Rank: zenday.RankInfoImmediate, Project: "p7", Message: "i"},
		},
	}
	out := zenday.Render(doc)

	if !strings.Contains(out, "## Pending operator action (3)") {
		t.Errorf("expected 'Pending operator action (3)'; got:\n%s", out)
	}
	if !strings.Contains(out, "## Cost watch (1)") {
		t.Errorf("expected 'Cost watch (1)'; got:\n%s", out)
	}
	if !strings.Contains(out, "## Activity (3)") {
		t.Errorf("expected 'Activity (3)'; got:\n%s", out)
	}
}

func TestRender_GroupFilterEmptyShortCircuit(t *testing.T) {
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
	}
	out := zenday.Render(doc)

	if strings.Contains(out, "## Pending operator action") {
		t.Errorf("empty doc rendered Pending section; got:\n%s", out)
	}
	if strings.Contains(out, "## Cost watch") {
		t.Errorf("empty doc rendered Cost watch; got:\n%s", out)
	}
	if strings.Contains(out, "## Activity") {
		t.Errorf("empty doc rendered Activity; got:\n%s", out)
	}
}
