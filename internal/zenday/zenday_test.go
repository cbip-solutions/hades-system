package zenday_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

func TestMaxBriefItemsIs7(t *testing.T) {
	if zenday.MaxBriefItems != 7 {
		t.Errorf("MaxBriefItems = %d, want 7 (inv-zen-126)", zenday.MaxBriefItems)
	}
}

func TestLeverageRankCanonicalOrder(t *testing.T) {
	tests := []struct {
		name string
		got  zenday.LeverageRank
		want int
	}{
		{"operator-gate", zenday.RankOperatorGate, 1},
		{"failed-scheduled-job", zenday.RankFailedScheduledJob, 2},
		{"urgent-event", zenday.RankUrgentEvent, 3},
		{"cost-cap-warning", zenday.RankCostCapWarning, 4},
		{"autonomous-milestone", zenday.RankAutonomousMilestone, 5},
		{"external-activity", zenday.RankExternalActivity, 6},
		{"info-immediate", zenday.RankInfoImmediate, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.got) != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLeverageRankZeroIsInvalid(t *testing.T) {
	var zero zenday.LeverageRank
	if zero.Valid() {
		t.Error("LeverageRank(0) reported valid; want invalid (defense-in-depth Layer 1)")
	}
}

func TestLeverageRankAboveSevenIsInvalid(t *testing.T) {
	r := zenday.LeverageRank(8)
	if r.Valid() {
		t.Error("LeverageRank(8) reported valid; want invalid")
	}
}

func TestLeverageRankString(t *testing.T) {
	tests := []struct {
		r    zenday.LeverageRank
		want string
	}{
		{zenday.RankOperatorGate, "operator-gate"},
		{zenday.RankFailedScheduledJob, "failed-scheduled-job"},
		{zenday.RankUrgentEvent, "urgent-event"},
		{zenday.RankCostCapWarning, "cost-cap-warning"},
		{zenday.RankAutonomousMilestone, "autonomous-milestone"},
		{zenday.RankExternalActivity, "external-activity"},
		{zenday.RankInfoImmediate, "info-immediate"},
		{zenday.LeverageRank(0), "invalid"},
		{zenday.LeverageRank(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.r.String(); got != tt.want {
				t.Errorf("LeverageRank(%d).String() = %q, want %q", tt.r, got, tt.want)
			}
		})
	}
}

func TestBriefTypeString(t *testing.T) {
	tests := []struct {
		bt   zenday.BriefType
		want string
	}{
		{zenday.BriefTypeMorning, "morning"},
		{zenday.BriefTypeEOD, "eod"},
		{zenday.BriefTypeCheckPending, "check-pending"},
		{zenday.BriefType(0), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.bt.String(); got != tt.want {
				t.Errorf("BriefType(%d).String() = %q, want %q", tt.bt, got, tt.want)
			}
		})
	}
}

func TestBriefItemValidateHappy(t *testing.T) {
	now := time.Now().UTC()
	bi := zenday.BriefItem{
		Rank:      zenday.RankOperatorGate,
		Severity:  inbox.SeverityActionNeeded,
		Project:   "internal-platform-x",
		EventType: "operatorgate.paused",
		Message:   "autonomous-mode paused at HRA L4 alert",
		Action:    "zen autonomy ack internal-platform-x",
		Source:    "operator-gate:internal-platform-x.autonomous-paused",
		CreatedAt: now,
	}
	if err := bi.Validate(); err != nil {
		t.Errorf("Validate happy = %v", err)
	}
}

func TestBriefItemValidateRejectsInvalidRank(t *testing.T) {
	bi := zenday.BriefItem{
		Rank:    zenday.LeverageRank(99),
		Project: "internal-platform-x",
		Message: "x",
	}
	err := bi.Validate()
	if err == nil {
		t.Fatal("Validate: want error on invalid rank, got nil")
	}
	if !strings.Contains(err.Error(), "rank") {
		t.Errorf("err = %q, want containing 'rank'", err.Error())
	}
}

func TestBriefItemValidateRejectsEmptyMessage(t *testing.T) {
	bi := zenday.BriefItem{
		Rank:    zenday.RankUrgentEvent,
		Project: "internal-platform-x",
		Message: "",
	}
	err := bi.Validate()
	if err == nil {
		t.Fatal("Validate: want error on empty message, got nil")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("err = %q, want containing 'message'", err.Error())
	}
}

func TestBriefDocIsEOD(t *testing.T) {
	doc := zenday.BriefDoc{Type: zenday.BriefTypeEOD}
	if !doc.IsEOD() {
		t.Error("IsEOD on EOD doc = false; want true")
	}
	morning := zenday.BriefDoc{Type: zenday.BriefTypeMorning}
	if morning.IsEOD() {
		t.Error("IsEOD on morning doc = true; want false")
	}
}

func TestBriefDocIsMorning(t *testing.T) {
	doc := zenday.BriefDoc{Type: zenday.BriefTypeMorning}
	if !doc.IsMorning() {
		t.Error("IsMorning on morning doc = false; want true")
	}
	eod := zenday.BriefDoc{Type: zenday.BriefTypeEOD}
	if eod.IsMorning() {
		t.Error("IsMorning on EOD doc = true; want false")
	}
}

func TestBriefDocIsCheckPending(t *testing.T) {
	doc := zenday.BriefDoc{Type: zenday.BriefTypeCheckPending}
	if !doc.IsCheckPending() {
		t.Error("IsCheckPending on check-pending doc = false; want true")
	}
	morning := zenday.BriefDoc{Type: zenday.BriefTypeMorning}
	if morning.IsCheckPending() {
		t.Error("IsCheckPending on morning doc = true; want false")
	}
}

func TestZenDayCap7SentinelReturnsErr(t *testing.T) {
	if !errors.Is(zenday.Cap7SentinelForTest(), zenday.ErrZenDayCap7Anchor) {
		t.Fatal("expected ErrZenDayCap7Anchor")
	}
}

func TestZenDayLeverageSortSentinelReturnsErr(t *testing.T) {
	if !errors.Is(zenday.LeverageSortSentinelForTest(), zenday.ErrZenDayLeverageSortAnchor) {
		t.Fatal("expected ErrZenDayLeverageSortAnchor")
	}
}

func TestErrAlreadyGeneratedWraps(t *testing.T) {
	err := zenday.ErrAlreadyGenerated
	if err == nil {
		t.Fatal("ErrAlreadyGenerated nil; expected sentinel")
	}
	if !strings.Contains(err.Error(), "already generated") {
		t.Errorf("err = %q, want containing 'already generated'", err.Error())
	}
}

func TestErrSourceCollectFailedSentinel(t *testing.T) {
	if zenday.ErrSourceCollectFailed == nil {
		t.Fatal("ErrSourceCollectFailed nil; expected sentinel")
	}
	if !strings.Contains(zenday.ErrSourceCollectFailed.Error(), "every source") {
		t.Errorf("err = %q, want containing 'every source'", zenday.ErrSourceCollectFailed.Error())
	}
}

func TestErrCollectCancelledSentinel(t *testing.T) {
	if zenday.ErrCollectCancelled == nil {
		t.Fatal("ErrCollectCancelled nil; expected sentinel")
	}
	if !strings.Contains(zenday.ErrCollectCancelled.Error(), "cancelled") {
		t.Errorf("err = %q, want containing 'cancelled'", zenday.ErrCollectCancelled.Error())
	}
}

func TestSentinelAnchorMessages(t *testing.T) {
	if !strings.Contains(zenday.ErrZenDayCap7Anchor.Error(), "inv-zen-126") {
		t.Errorf("ErrZenDayCap7Anchor = %q, want containing 'inv-zen-126'",
			zenday.ErrZenDayCap7Anchor.Error())
	}
	if !strings.Contains(zenday.ErrZenDayCap7Anchor.Error(), "MaxBriefItems") {
		t.Errorf("ErrZenDayCap7Anchor = %q, want containing 'MaxBriefItems'",
			zenday.ErrZenDayCap7Anchor.Error())
	}
	if !strings.Contains(zenday.ErrZenDayLeverageSortAnchor.Error(), "inv-zen-127") {
		t.Errorf("ErrZenDayLeverageSortAnchor = %q, want containing 'inv-zen-127'",
			zenday.ErrZenDayLeverageSortAnchor.Error())
	}
	if !strings.Contains(zenday.ErrZenDayLeverageSortAnchor.Error(), "LeverageRank") {
		t.Errorf("ErrZenDayLeverageSortAnchor = %q, want containing 'LeverageRank'",
			zenday.ErrZenDayLeverageSortAnchor.Error())
	}
}
