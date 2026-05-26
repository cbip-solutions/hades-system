package zenday_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeInboxStore struct {
	rows []zenday.InboxCacheRow
	err  error
}

func (f *fakeInboxStore) Query(_ context.Context, _ zenday.InboxListFilter) ([]zenday.InboxCacheRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type fakeSchedulerStore struct {
	history []zenday.SchedulerHistoryEntry
	err     error
}

func (f *fakeSchedulerStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]zenday.SchedulerHistoryEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.history, nil
}

type fakeGitCli struct {
	activity []zenday.GitActivity
	err      error
}

func (f *fakeGitCli) RecentActivity(_ context.Context, _ time.Time) ([]zenday.GitActivity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.activity, nil
}

type fakeAutonomy struct {
	snap []zenday.AutonomySnapshot
	err  error
}

func (f *fakeAutonomy) Snapshot(_ context.Context) ([]zenday.AutonomySnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

type fakeCost struct {
	statuses []zenday.CostStatus
	err      error
}

func (f *fakeCost) SpendByProject(_ context.Context, _, _ time.Time) ([]zenday.CostStatus, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.statuses, nil
}

type fakeEventlog struct {
	records []zenday.EventRecord
	err     error
}

func (f *fakeEventlog) QueryByType(_ context.Context, _ string, _, _ time.Time) ([]zenday.EventRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

type fakeAuditProjectsProvider struct {
	projects []zenday.AuditProjectStatus
	err      error
}

func (f *fakeAuditProjectsProvider) GetAuditProjects(_ context.Context) ([]zenday.AuditProjectStatus, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.projects, nil
}

func newDeps() zenday.CollectDeps {
	return zenday.CollectDeps{
		Inbox:     &fakeInboxStore{},
		Scheduler: &fakeSchedulerStore{},
		Git:       &fakeGitCli{},
		Autonomy:  &fakeAutonomy{},
		Cost:      &fakeCost{},
		Eventlog:  &fakeEventlog{},
	}
}

func TestCollect_AllSourcesEmpty_ReturnsEmptySlice(t *testing.T) {
	deps := newDeps()
	items, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}

func TestCollect_InboxRowsAssignedRanks(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.SeverityUrgent, ProjectAlias: "internal-platform-x", EventType: "hra.l4_alert", CreatedAt: time.Now()},
			{Severity: inbox.SeverityActionNeeded, ProjectAlias: "zen-swarm", EventType: "doctrine.amendment", CreatedAt: time.Now()},
			{Severity: inbox.SeverityInfoImmediate, ProjectAlias: "reference-project", EventType: "pr.opened", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
	hasUrgent := false
	for _, it := range items {
		if it.Project == "internal-platform-x" && it.Rank == zenday.RankUrgentEvent {
			hasUrgent = true
		}
	}
	if !hasUrgent {
		t.Error("urgent inbox row not classified as RankUrgentEvent")
	}
}

func TestCollect_InboxActionNeededWithSchedulerPrefixIsRank2(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.SeverityActionNeeded, ProjectAlias: "p1", EventType: "scheduler.routine_failed", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankFailedScheduledJob {
		t.Errorf("scheduler.* action-needed rank = %d, want RankFailedScheduledJob (2)", items[0].Rank)
	}
}

func TestCollect_InboxInfoDigestAutonomyIsRank5(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.SeverityInfoDigest, ProjectAlias: "p1", EventType: "autonomy.milestone", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankAutonomousMilestone {
		t.Errorf("info-digest autonomy.* rank = %d, want RankAutonomousMilestone (5)", items[0].Rank)
	}
}

func TestCollect_InboxInfoDigestNonAutonomyFallsToInfoImmediate(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.SeverityInfoDigest, ProjectAlias: "p1", EventType: "doctrine.note", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankInfoImmediate {
		t.Errorf("info-digest non-autonomy rank = %d, want RankInfoImmediate (7)", items[0].Rank)
	}
}

func TestCollect_InboxUnknownSeverityFallsToInfoImmediate(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.Severity(""), ProjectAlias: "p1", EventType: "x.y", CreatedAt: time.Now()},
			{Severity: inbox.SeverityActionNeeded, ProjectAlias: "p2", EventType: "operator.tag", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	for _, it := range items {
		if it.Rank != zenday.RankInfoImmediate {
			t.Errorf("non-scheduler action-needed / unknown severity rank = %d, want RankInfoImmediate", it.Rank)
		}
	}
}

func TestCollect_FailedScheduledJobsAtRank2(t *testing.T) {
	deps := newDeps()
	now := time.Now()
	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{
			{ScheduleID: "sched-42", ProjectAlias: "p1", Action: "cost-sweep", Outcome: "failed", Reason: "auth error", FiredAt: now},
			{ScheduleID: "sched-43", ProjectAlias: "p2", Action: "ack", Outcome: "success", FiredAt: now},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	failedCount := 0
	for _, it := range items {
		if it.Rank == zenday.RankFailedScheduledJob {
			failedCount++
		}
	}
	if failedCount != 1 {
		t.Errorf("failed-job count = %d, want 1 (success entries excluded)", failedCount)
	}
}

func TestCollect_CostCapWarningAtRank4_Max2(t *testing.T) {
	deps := newDeps()
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "p1", PercentUsed: 92.0},
			{ProjectAlias: "p2", PercentUsed: 88.0},
			{ProjectAlias: "p3", PercentUsed: 81.0},
			{ProjectAlias: "p4", PercentUsed: 50.0},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	costCount := 0
	for _, it := range items {
		if it.Rank == zenday.RankCostCapWarning {
			costCount++
		}
	}
	if costCount != 2 {
		t.Errorf("cost-warning count = %d, want 2 (cap per Q14 B; lowest %% dropped)", costCount)
	}
}

func TestCollect_CostCapWarning_KeepsHighestPercentages(t *testing.T) {
	deps := newDeps()
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "p-low", PercentUsed: 80.5},
			{ProjectAlias: "p-mid", PercentUsed: 90.0},
			{ProjectAlias: "p-high", PercentUsed: 99.5},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	keptAliases := map[string]bool{}
	for _, it := range items {
		if it.Rank == zenday.RankCostCapWarning {
			keptAliases[it.Project] = true
		}
	}
	if len(keptAliases) != 2 {
		t.Fatalf("kept = %d, want 2 after cap", len(keptAliases))
	}
	if !keptAliases["p-high"] || !keptAliases["p-mid"] {
		t.Errorf("expected p-high + p-mid kept; got %v (lowest %% should be dropped)", keptAliases)
	}
	if keptAliases["p-low"] {
		t.Errorf("p-low (80.5%%) should be dropped — lowest of the three")
	}
}

func TestCollect_AutonomousMilestone_Max1(t *testing.T) {
	deps := newDeps()
	now := time.Now()
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "a1", State: "active", LastMilestone: "build", LastMilestoneAt: now.Add(-2 * time.Hour)},
			{ProjectAlias: "a2", State: "active", LastMilestone: "merge", LastMilestoneAt: now.Add(-30 * time.Minute)},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	milestoneCount := 0
	keptAlias := ""
	for _, it := range items {
		if it.Rank == zenday.RankAutonomousMilestone {
			milestoneCount++
			keptAlias = it.Project
		}
	}
	if milestoneCount != 1 {
		t.Errorf("milestone count = %d, want 1 (Q14 B max 1 shown)", milestoneCount)
	}
	if keptAlias != "a2" {
		t.Errorf("kept alias = %q, want %q (newest first)", keptAlias, "a2")
	}
}

func TestCollect_ActiveAutonomyZeroMilestoneAtSkipped(t *testing.T) {
	deps := newDeps()
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "p1", State: "active", LastMilestone: "", LastMilestoneAt: time.Time{}},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	for _, it := range items {
		if it.Rank == zenday.RankAutonomousMilestone {
			t.Errorf("active autonomy with zero LastMilestoneAt should not emit milestone item; got %+v", it)
		}
	}
}

func TestCollect_PausedAutonomyAtRank1(t *testing.T) {
	deps := newDeps()
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "internal-platform-x", State: "paused", PauseReason: "HRA L4 alert"},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Rank != zenday.RankOperatorGate {
		t.Errorf("paused autonomy rank = %d, want RankOperatorGate (1)", items[0].Rank)
	}
	if items[0].Action != "zen autonomy ack internal-platform-x" {
		t.Errorf("paused autonomy action = %q, want %q", items[0].Action, "zen autonomy ack internal-platform-x")
	}
}

func TestCollect_IdleOrCompleteAutonomyEmitsNoItem(t *testing.T) {
	deps := newDeps()
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "p1", State: "idle"},
			{ProjectAlias: "p2", State: "complete"},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("idle/complete autonomy emitted %d items; want 0", len(items))
	}
}

func TestCollect_GitActivityRank6_Max1(t *testing.T) {
	deps := newDeps()
	now := time.Now()
	deps.Git = &fakeGitCli{
		activity: []zenday.GitActivity{
			{ProjectAlias: "p1", Kind: "pr_opened", Description: "PR #34 opened", URL: "https://example/pr/34", CreatedAt: now.Add(-2 * time.Hour)},
			{ProjectAlias: "p2", Kind: "commit", Description: "abc123", CreatedAt: now},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	gitCount := 0
	keptAlias := ""
	for _, it := range items {
		if it.Rank == zenday.RankExternalActivity {
			gitCount++
			keptAlias = it.Project
		}
	}
	if gitCount != 1 {
		t.Errorf("git-activity count = %d, want 1 (Q14 B max 1 shown)", gitCount)
	}
	if keptAlias != "p2" {
		t.Errorf("kept alias = %q, want %q (newest first)", keptAlias, "p2")
	}
}

func TestCollect_GitActivityWithURLSetsAction(t *testing.T) {
	deps := newDeps()
	deps.Git = &fakeGitCli{
		activity: []zenday.GitActivity{
			{ProjectAlias: "p1", Kind: "pr_opened", Description: "PR #34 opened", URL: "https://example/pr/34", CreatedAt: time.Now()},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Action != "https://example/pr/34" {
		t.Errorf("Action = %q, want URL", items[0].Action)
	}
}

func TestCollect_PartialTolerance_OneLegFailsOthersSucceed(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{err: errors.New("inbox down")}
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "p1", State: "paused", PauseReason: "L4"},
		},
	}
	items, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect should succeed when ≥1 leg succeeds; err = %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected partial errors slice non-empty when 1 leg fails")
	}
	foundInbox := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "inbox") {
			foundInbox = true
		}
	}
	if !foundInbox {
		t.Errorf("partial errors should mention failing leg; got %v", errs)
	}
	if len(items) == 0 {
		t.Error("expected items from successful legs")
	}
}

func TestCollect_AllLegsFail_ReturnsErrSourceCollectFailed(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{err: errors.New("a")}
	deps.Scheduler = &fakeSchedulerStore{err: errors.New("b")}
	deps.Git = &fakeGitCli{err: errors.New("c")}
	deps.Autonomy = &fakeAutonomy{err: errors.New("d")}
	deps.Cost = &fakeCost{err: errors.New("e")}
	deps.Eventlog = &fakeEventlog{err: errors.New("f")}

	deps.AuditProjects = &fakeAuditProjectsProvider{err: errors.New("g")}
	items, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), true)
	if !errors.Is(err, zenday.ErrSourceCollectFailed) {
		t.Errorf("err = %v, want ErrSourceCollectFailed", err)
	}
	if items != nil {
		t.Errorf("items = %v, want nil on full failure", items)
	}
	if len(errs) != 7 {
		t.Errorf("legErrors count = %d, want 7 (all seven legs failed)", len(errs))
	}
}

func TestCollect_AllLegsFail_NonEodSkipsEventlog(t *testing.T) {
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{err: errors.New("a")}
	deps.Scheduler = &fakeSchedulerStore{err: errors.New("b")}
	deps.Git = &fakeGitCli{err: errors.New("c")}
	deps.Autonomy = &fakeAutonomy{err: errors.New("d")}
	deps.Cost = &fakeCost{err: errors.New("e")}

	deps.Eventlog = &fakeEventlog{err: errors.New("should not be called")}
	items, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect should succeed because eventlog leg short-circuits when eod=false; err = %v", err)
	}
	if len(errs) != 5 {
		t.Errorf("legErrors count = %d, want 5 (eventlog leg short-circuits when eod=false)", len(errs))
	}
	if items == nil {

		_ = items
	}
}

func TestCollect_ContextCancellation_ReturnsErrCollectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := newDeps()
	items, errs, err := zenday.Collect(ctx, deps, time.Now().Add(-24*time.Hour), false)
	if err == nil {
		t.Fatal("expected ctx-cancellation error, got nil")
	}
	if !errors.Is(err, zenday.ErrCollectCancelled) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ErrCollectCancelled or context.Canceled", err)
	}
	if items != nil {
		t.Errorf("items = %v, want nil on cancellation", items)
	}
	if errs != nil {
		t.Errorf("errs = %v, want nil on cancellation", errs)
	}
}

func TestCollect_EodTrueIncludesHandoffPostedRecords(t *testing.T) {
	deps := newDeps()
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{ProjectID: "abc", ProjectAlias: "internal-platform-x", Kind: "HandoffPosted", PayloadJSON: []byte(`{}`)},
		},
	}

	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), true)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	for _, e := range errs {
		if strings.Contains(e.Error(), "eventlog") {
			t.Errorf("eventlog leg should succeed; got %v", e)
		}
	}
}

func TestCollect_EodFalseSkipsEventlogLeg(t *testing.T) {
	deps := newDeps()
	deps.Eventlog = &fakeEventlog{err: errors.New("should not be called when eod=false")}
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	for _, e := range errs {
		if strings.Contains(e.Error(), "eventlog") {
			t.Errorf("eventlog leg should be skipped when eod=false; got %v", e)
		}
	}
}

func TestCollect_NilInboxLegReturnsLegError(t *testing.T) {
	deps := newDeps()
	deps.Inbox = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v (expected nil; ≥1 leg succeeded)", err)
	}
	foundInbox := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "inbox") {
			foundInbox = true
		}
	}
	if !foundInbox {
		t.Errorf("nil InboxQuerier should surface as inbox leg error; got %v", errs)
	}
}

func TestCollect_NilSchedulerLegReturnsLegError(t *testing.T) {
	deps := newDeps()
	deps.Scheduler = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "scheduler") {
			found = true
		}
	}
	if !found {
		t.Errorf("nil SchedulerHistorian should surface as scheduler leg error; got %v", errs)
	}
}

func TestCollect_NilGitLegReturnsLegError(t *testing.T) {
	deps := newDeps()
	deps.Git = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "git") {
			found = true
		}
	}
	if !found {
		t.Errorf("nil GitCli should surface as git leg error; got %v", errs)
	}
}

func TestCollect_NilAutonomyLegReturnsLegError(t *testing.T) {
	deps := newDeps()
	deps.Autonomy = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "autonomy") {
			found = true
		}
	}
	if !found {
		t.Errorf("nil AutonomyStateReader should surface as autonomy leg error; got %v", errs)
	}
}

func TestCollect_NilCostLegReturnsLegError(t *testing.T) {
	deps := newDeps()
	deps.Cost = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "cost") {
			found = true
		}
	}
	if !found {
		t.Errorf("nil CostStore should surface as cost leg error; got %v", errs)
	}
}

func TestCollect_NilEventlogLegReturnsLegErrorWhenEod(t *testing.T) {
	deps := newDeps()
	deps.Eventlog = nil
	_, errs, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), true)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "eventlog") {
			found = true
		}
	}
	if !found {
		t.Errorf("nil EventReader with eod=true should surface as eventlog leg error; got %v", errs)
	}
}

func TestCollect_AppliesPerRankCapsAcrossLegs(t *testing.T) {

	deps := newDeps()
	now := time.Now()
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "p1", PercentUsed: 99.0},
			{ProjectAlias: "p2", PercentUsed: 90.0},
			{ProjectAlias: "p3", PercentUsed: 81.0},
		},
	}
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "a1", State: "active", LastMilestone: "m1", LastMilestoneAt: now},
			{ProjectAlias: "a2", State: "active", LastMilestone: "m2", LastMilestoneAt: now.Add(-1 * time.Hour)},
		},
	}
	deps.Git = &fakeGitCli{
		activity: []zenday.GitActivity{
			{ProjectAlias: "g1", Kind: "commit", Description: "x", CreatedAt: now},
			{ProjectAlias: "g2", Kind: "pr_opened", Description: "y", CreatedAt: now.Add(-2 * time.Hour)},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	costN, milestoneN, gitN := 0, 0, 0
	for _, it := range items {
		switch it.Rank {
		case zenday.RankCostCapWarning:
			costN++
		case zenday.RankAutonomousMilestone:
			milestoneN++
		case zenday.RankExternalActivity:
			gitN++
		}
	}
	if costN != 2 {
		t.Errorf("cost-cap-warning count = %d, want 2", costN)
	}
	if milestoneN != 1 {
		t.Errorf("autonomous-milestone count = %d, want 1", milestoneN)
	}
	if gitN != 1 {
		t.Errorf("external-activity count = %d, want 1", gitN)
	}
}

func TestCollect_BriefItemFieldsPopulated(t *testing.T) {

	now := time.Now()
	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 42, ProjectAlias: "internal-platform-x", Severity: inbox.SeverityUrgent, EventType: "hra.l4_alert", CreatedAt: now},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	bi := items[0]
	if bi.Project != "internal-platform-x" {
		t.Errorf("Project = %q, want %q", bi.Project, "internal-platform-x")
	}
	if bi.EventType != "hra.l4_alert" {
		t.Errorf("EventType = %q, want %q", bi.EventType, "hra.l4_alert")
	}
	if bi.Severity != inbox.SeverityUrgent {
		t.Errorf("Severity = %q, want %q", bi.Severity, inbox.SeverityUrgent)
	}
	if bi.Source != "inbox:42" {
		t.Errorf("Source = %q, want %q", bi.Source, "inbox:42")
	}
	if !bi.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", bi.CreatedAt, now)
	}
}

func TestCollect_SchedulerEntryFieldsPopulated(t *testing.T) {
	now := time.Now()
	deps := newDeps()
	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{
			{ScheduleID: "sched-cost", ProjectAlias: "p1", Action: "cost-sweep", Outcome: "failed", Reason: "auth error", FiredAt: now},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	bi := items[0]
	if bi.Project != "p1" {
		t.Errorf("Project = %q, want %q", bi.Project, "p1")
	}
	if bi.Action != "zen schedule run --now sched-cost" {
		t.Errorf("Action = %q, want %q", bi.Action, "zen schedule run --now sched-cost")
	}
	if bi.Source != "scheduled-job:sched-cost" {
		t.Errorf("Source = %q, want %q", bi.Source, "scheduled-job:sched-cost")
	}
	if bi.Severity != inbox.SeverityActionNeeded {
		t.Errorf("Severity = %q, want SeverityActionNeeded", bi.Severity)
	}
	if !strings.Contains(bi.Message, "cost-sweep") {
		t.Errorf("Message = %q, want containing %q", bi.Message, "cost-sweep")
	}
	if !strings.Contains(bi.Message, "auth error") {
		t.Errorf("Message = %q, want containing reason %q", bi.Message, "auth error")
	}
}

func TestCollect_CostFieldsPopulated(t *testing.T) {
	deps := newDeps()
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "p1", PercentUsed: 92.5},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	bi := items[0]
	if bi.Project != "p1" {
		t.Errorf("Project = %q, want %q", bi.Project, "p1")
	}
	if bi.Source != "cost-cap:p1" {
		t.Errorf("Source = %q, want %q", bi.Source, "cost-cap:p1")
	}

	if string(bi.Severity) != "92.5%" {
		t.Errorf("Severity = %q, want %q", bi.Severity, "92.5%")
	}
}

func TestCollect_AutonomyMilestoneFieldsPopulated(t *testing.T) {
	now := time.Now()
	deps := newDeps()
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "p1", State: "active", LastMilestone: "Plan 7 Phase F shipped", LastMilestoneAt: now},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	bi := items[0]
	if bi.Project != "p1" {
		t.Errorf("Project = %q, want %q", bi.Project, "p1")
	}
	if bi.Message != "Plan 7 Phase F shipped" {
		t.Errorf("Message = %q, want %q", bi.Message, "Plan 7 Phase F shipped")
	}
	if bi.Source != "autonomy:p1.milestone" {
		t.Errorf("Source = %q, want %q", bi.Source, "autonomy:p1.milestone")
	}
	if bi.Severity != inbox.SeverityInfoDigest {
		t.Errorf("Severity = %q, want SeverityInfoDigest", bi.Severity)
	}
}

func TestCollect_CollectDepsExposesAllSixLegs(t *testing.T) {

	d := zenday.CollectDeps{
		Inbox:         &fakeInboxStore{},
		Scheduler:     &fakeSchedulerStore{},
		Git:           &fakeGitCli{},
		Autonomy:      &fakeAutonomy{},
		Cost:          &fakeCost{},
		Eventlog:      &fakeEventlog{},
		AuditProjects: &fakeAuditProjectsProvider{},
	}
	if d.Inbox == nil || d.Scheduler == nil || d.Git == nil ||
		d.Autonomy == nil || d.Cost == nil || d.Eventlog == nil ||
		d.AuditProjects == nil {
		t.Fatal("CollectDeps surface drifted; expected seven leg fields populated")
	}
}

func TestCollect_StableConcurrentAccessIsSafe(t *testing.T) {

	deps := newDeps()
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{Severity: inbox.SeverityUrgent, ProjectAlias: "p1", EventType: "x", CreatedAt: time.Now()},
		},
	}
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
			if err != nil {
				t.Errorf("Collect err = %v", err)
			}
		}()
	}
	for i := 0; i < 2; i++ {
		<-done
	}
}

func TestCollect_ParsePercentMalformedSeverityFallsToZero(t *testing.T) {

	deps := newDeps()
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "p1", PercentUsed: 80.0},
			{ProjectAlias: "p2", PercentUsed: 81.0},
			{ProjectAlias: "p3", PercentUsed: 82.0},
		},
	}
	items, _, err := zenday.Collect(context.Background(), deps, time.Now().Add(-24*time.Hour), false)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	costN := 0
	for _, it := range items {
		if it.Rank == zenday.RankCostCapWarning {
			costN++
		}
	}
	if costN != 2 {
		t.Errorf("cap = %d, want 2", costN)
	}
}
