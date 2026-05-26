package zenday_test

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

func TestSortByLeverage_CanonicalOrder(t *testing.T) {
	items := []zenday.BriefItem{
		{Rank: zenday.RankInfoImmediate, Message: "info"},
		{Rank: zenday.RankOperatorGate, Message: "gate"},
		{Rank: zenday.RankFailedScheduledJob, Message: "failed-job"},
		{Rank: zenday.RankUrgentEvent, Message: "urgent"},
		{Rank: zenday.RankCostCapWarning, Message: "cost"},
		{Rank: zenday.RankExternalActivity, Message: "external"},
		{Rank: zenday.RankAutonomousMilestone, Message: "milestone"},
	}
	zenday.SortByLeverage(items)
	wantOrder := []zenday.LeverageRank{
		zenday.RankOperatorGate,
		zenday.RankFailedScheduledJob,
		zenday.RankUrgentEvent,
		zenday.RankCostCapWarning,
		zenday.RankAutonomousMilestone,
		zenday.RankExternalActivity,
		zenday.RankInfoImmediate,
	}
	for i, want := range wantOrder {
		if items[i].Rank != want {
			t.Errorf("items[%d].Rank = %d (%s), want %d (%s)", i, items[i].Rank, items[i].Rank, want, want)
		}
	}
}

func TestSortByLeverage_TiebreakByCreatedAtDesc(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t1.Add(2 * time.Hour)
	items := []zenday.BriefItem{
		{Rank: zenday.RankUrgentEvent, Message: "earliest", CreatedAt: t1},
		{Rank: zenday.RankUrgentEvent, Message: "latest", CreatedAt: t3},
		{Rank: zenday.RankUrgentEvent, Message: "middle", CreatedAt: t2},
	}
	zenday.SortByLeverage(items)
	wantOrder := []string{"latest", "middle", "earliest"}
	for i, want := range wantOrder {
		if items[i].Message != want {
			t.Errorf("items[%d].Message = %q, want %q", i, items[i].Message, want)
		}
	}
}

func TestSortByLeverage_StabilityWithinTie(t *testing.T) {

	t1 := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	items := []zenday.BriefItem{
		{Rank: zenday.RankUrgentEvent, Message: "first", CreatedAt: t1},
		{Rank: zenday.RankUrgentEvent, Message: "second", CreatedAt: t1},
		{Rank: zenday.RankUrgentEvent, Message: "third", CreatedAt: t1},
	}
	zenday.SortByLeverage(items)
	wantOrder := []string{"first", "second", "third"}
	for i, want := range wantOrder {
		if items[i].Message != want {
			t.Errorf("items[%d].Message = %q, want %q (stability violation)", i, items[i].Message, want)
		}
	}
}

func TestSortByLeverage_Idempotent(t *testing.T) {
	items := []zenday.BriefItem{
		{Rank: zenday.RankUrgentEvent, Message: "u"},
		{Rank: zenday.RankOperatorGate, Message: "g"},
	}
	zenday.SortByLeverage(items)
	first := append([]zenday.BriefItem(nil), items...)
	zenday.SortByLeverage(items)
	for i := range first {
		if first[i].Message != items[i].Message {
			t.Errorf("re-sort changed slice at %d: was %q now %q", i, first[i].Message, items[i].Message)
		}
	}
}

func TestSortByLeverage_EmptySlice(t *testing.T) {
	var items []zenday.BriefItem
	zenday.SortByLeverage(items)
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}

func TestSortByLeverage_SingleItem(t *testing.T) {
	items := []zenday.BriefItem{
		{Rank: zenday.RankUrgentEvent, Message: "x"},
	}
	zenday.SortByLeverage(items)
	if len(items) != 1 || items[0].Message != "x" {
		t.Error("single-item sort altered the slice")
	}
}

func TestIsSorted_PositiveCases(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	tests := [][]zenday.BriefItem{
		{},
		{{Rank: zenday.RankOperatorGate, Message: "x"}},
		{
			{Rank: zenday.RankOperatorGate, Message: "g"},
			{Rank: zenday.RankUrgentEvent, Message: "u"},
		},
		{
			{Rank: zenday.RankUrgentEvent, Message: "later", CreatedAt: t1.Add(time.Hour)},
			{Rank: zenday.RankUrgentEvent, Message: "earlier", CreatedAt: t1},
		},
	}
	for i, items := range tests {
		if !zenday.IsSorted(items) {
			t.Errorf("case %d: IsSorted = false, want true", i)
		}
	}
}

func TestIsSorted_NegativeCases(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	tests := [][]zenday.BriefItem{
		{
			{Rank: zenday.RankUrgentEvent, Message: "u"},
			{Rank: zenday.RankOperatorGate, Message: "g"},
		},
		{

			{Rank: zenday.RankUrgentEvent, Message: "earlier", CreatedAt: t1},
			{Rank: zenday.RankUrgentEvent, Message: "later", CreatedAt: t1.Add(time.Hour)},
		},
	}
	for i, items := range tests {
		if zenday.IsSorted(items) {
			t.Errorf("case %d: IsSorted = true, want false", i)
		}
	}
}

func TestSortByLeverage_FuzzPreservesIsSortedPostcondition(t *testing.T) {

	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 1000; trial++ {
		n := 1 + rng.Intn(50)
		items := make([]zenday.BriefItem, n)
		base := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
		for i := range items {
			items[i] = zenday.BriefItem{
				Rank:      zenday.LeverageRank(1 + rng.Intn(7)),
				Message:   "x",
				CreatedAt: base.Add(time.Duration(rng.Intn(86400)) * time.Second),
			}
		}
		zenday.SortByLeverage(items)
		if !zenday.IsSorted(items) {
			t.Fatalf("trial %d: post-sort IsSorted = false; items = %v", trial, items)
		}
	}
}

func TestByLeverageImplementsSortInterface(t *testing.T) {
	var _ sort.Interface = zenday.ByLeverage{}
}
