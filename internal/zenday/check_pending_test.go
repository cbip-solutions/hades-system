package zenday_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeNextFire struct {
	next time.Time
	err  error
}

func (f *fakeNextFire) NextFire(_ context.Context, _ string) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return f.next, nil
}

var canonicalNext = time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)

func newCheckPendingDeps(now time.Time) zenday.CheckPendingDeps {
	return zenday.CheckPendingDeps{
		Inbox:          &fakeInboxStore{},
		Scheduler:      &fakeSchedulerStore{},
		NextFire:       &fakeNextFire{next: canonicalNext},
		Clock:          fakeClock{now: now},
		MorningBriefID: "morning-brief",
	}
}

func TestCheckPending_ReturnsNextFireAndZeroCounts_WhenInboxEmpty(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.Type != zenday.BriefTypeCheckPending {
		t.Errorf("doc.Type = %v, want BriefTypeCheckPending", doc.Type)
	}
	if !doc.NextScheduledAt.Equal(canonicalNext) {
		t.Errorf("doc.NextScheduledAt = %v, want %v", doc.NextScheduledAt, canonicalNext)
	}
	if doc.PendingActionNeeded != 0 {
		t.Errorf("doc.PendingActionNeeded = %d, want 0", doc.PendingActionNeeded)
	}
	if doc.PendingUrgent != 0 {
		t.Errorf("doc.PendingUrgent = %d, want 0", doc.PendingUrgent)
	}
}

func TestCheckPending_UsesLastSuccessfulFireAsCutoff(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	lastBrief := canonicalNow.Add(-3 * time.Hour)
	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{
			{ScheduleID: "morning-brief", Outcome: "success", FiredAt: lastBrief},
		},
	}
	deps.Inbox = &cutoffInboxStore{

		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, ProjectAlias: "internal-platform-x", Severity: inbox.SeverityActionNeeded, EventType: "scheduler.routine_failed", CreatedAt: canonicalNow.Add(-1 * time.Hour)},
			{NotificationID: 2, ProjectAlias: "zen-swarm", Severity: inbox.SeverityActionNeeded, EventType: "doctrine.amendment", CreatedAt: canonicalNow.Add(-2 * time.Hour)},
			{NotificationID: 3, ProjectAlias: "internal-platform-x", Severity: inbox.SeverityUrgent, EventType: "hra.l4_alert", CreatedAt: canonicalNow.Add(-30 * time.Minute)},
			{NotificationID: 4, ProjectAlias: "old", Severity: inbox.SeverityUrgent, EventType: "stale", CreatedAt: canonicalNow.Add(-12 * time.Hour)},
		},
		cutoff: lastBrief,
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.PendingActionNeeded != 2 {
		t.Errorf("PendingActionNeeded = %d, want 2", doc.PendingActionNeeded)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("PendingUrgent = %d, want 1 (older urgent excluded by cutoff)", doc.PendingUrgent)
	}
}

func TestCheckPending_FallsBackTo24hWhenNoHistory(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)

	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{
			{ScheduleID: "morning-brief", Outcome: "failed", FiredAt: canonicalNow.Add(-2 * time.Hour)},
		},
	}
	wantCutoff := canonicalNow.Add(-24 * time.Hour)
	deps.Inbox = &cutoffInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, EventType: "x", CreatedAt: canonicalNow.Add(-10 * time.Hour)},
			{NotificationID: 2, Severity: inbox.SeverityActionNeeded, EventType: "y", CreatedAt: canonicalNow.Add(-5 * time.Hour)},
		},
		cutoff: wantCutoff,
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.PendingActionNeeded != 1 || doc.PendingUrgent != 1 {
		t.Errorf("counts = (%d action-needed, %d urgent); want (1, 1)",
			doc.PendingActionNeeded, doc.PendingUrgent)
	}
}

func TestCheckPending_FallsBackTo24hWhenSchedulerErrors(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	deps.Scheduler = &fakeSchedulerStore{err: errors.New("scheduler down")}
	wantCutoff := canonicalNow.Add(-24 * time.Hour)
	deps.Inbox = &cutoffInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, EventType: "x", CreatedAt: canonicalNow.Add(-1 * time.Hour)},
		},
		cutoff: wantCutoff,
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v (scheduler failure must be soft)", err)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("PendingUrgent = %d, want 1", doc.PendingUrgent)
	}
}

func TestCheckPending_NextFireErrorRendersZeroTime(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	deps.NextFire = &fakeNextFire{err: errors.New("next-fire down")}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v (NextFire failure must be soft)", err)
	}
	if !doc.NextScheduledAt.IsZero() {
		t.Errorf("NextScheduledAt = %v, want zero time on NextFire error", doc.NextScheduledAt)
	}
}

func TestCheckPending_InboxQueryError_ReturnsError(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	deps.Inbox = &fakeInboxStore{err: errors.New("inbox down")}

	_, err := zenday.CheckPending(context.Background(), deps)
	if err == nil {
		t.Fatal("expected inbox error; got nil")
	}
	if !strings.Contains(err.Error(), "check-pending inbox") {
		t.Errorf("err = %v, want containing 'check-pending inbox'", err)
	}
	if !strings.Contains(err.Error(), "inbox down") {
		t.Errorf("err = %v, want containing root cause 'inbox down'", err)
	}
}

func TestCheckPending_SeverityClassification(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow},
			{NotificationID: 2, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow},
			{NotificationID: 3, Severity: inbox.SeverityActionNeeded, CreatedAt: canonicalNow},
			{NotificationID: 4, Severity: inbox.SeverityInfoImmediate, CreatedAt: canonicalNow},
			{NotificationID: 5, Severity: inbox.SeverityInfoDigest, CreatedAt: canonicalNow},
			{NotificationID: 6, Severity: inbox.Severity(""), CreatedAt: canonicalNow},
		},
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.PendingUrgent != 2 {
		t.Errorf("PendingUrgent = %d, want 2", doc.PendingUrgent)
	}
	if doc.PendingActionNeeded != 1 {
		t.Errorf("PendingActionNeeded = %d, want 1 (info-* + empty severity excluded)", doc.PendingActionNeeded)
	}
}

func TestCheckPending_LatestSuccessfulFireWinsAcrossHistory(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	earlier := canonicalNow.Add(-12 * time.Hour)
	later := canonicalNow.Add(-2 * time.Hour)
	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{
			{ScheduleID: "morning-brief", Outcome: "success", FiredAt: earlier},
			{ScheduleID: "morning-brief", Outcome: "failed", FiredAt: canonicalNow.Add(-1 * time.Hour)},
			{ScheduleID: "morning-brief", Outcome: "success", FiredAt: later},
		},
	}
	deps.Inbox = &cutoffInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow.Add(-30 * time.Minute)},

			{NotificationID: 2, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow.Add(-3 * time.Hour)},
		},
		cutoff: later,
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("PendingUrgent = %d, want 1 (latest cutoff excludes older row)", doc.PendingUrgent)
	}
}

func TestCheckPending_FailedFiresIgnoredForCutoff(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	wantCutoff := canonicalNow.Add(-24 * time.Hour)
	deps.Scheduler = &fakeSchedulerStore{
		history: []zenday.SchedulerHistoryEntry{

			{ScheduleID: "morning-brief", Outcome: "failed", FiredAt: canonicalNow.Add(-1 * time.Hour)},
			{ScheduleID: "morning-brief", Outcome: "rate-limited", FiredAt: canonicalNow.Add(-2 * time.Hour)},
		},
	}
	deps.Inbox = &cutoffInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow.Add(-12 * time.Hour)},
		},
		cutoff: wantCutoff,
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("PendingUrgent = %d, want 1 (cutoff = now-24h since no success)", doc.PendingUrgent)
	}
}

func TestCheckPending_RendersThroughRenderFunction(t *testing.T) {
	deps := newCheckPendingDeps(canonicalNow)
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow},
			{NotificationID: 2, Severity: inbox.SeverityActionNeeded, CreatedAt: canonicalNow},
		},
	}

	doc, err := zenday.CheckPending(context.Background(), deps)
	if err != nil {
		t.Fatalf("CheckPending err = %v", err)
	}
	body := zenday.Render(doc)
	if !strings.Contains(body, "Next morning brief: 2026-05-02 08:00:00") {
		t.Errorf("body missing next-fire line; body=%q", body)
	}
	if !strings.Contains(body, "Pending items since last brief: 1 action-needed, 1 urgent") {
		t.Errorf("body missing pending counts line; body=%q", body)
	}
}

type cutoffInboxStore struct {
	rows   []zenday.InboxCacheRow
	cutoff time.Time
	err    error
}

func (f *cutoffInboxStore) Query(_ context.Context, filter zenday.InboxListFilter) ([]zenday.InboxCacheRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	if filter.Since == nil {
		return nil, errors.New("cutoffInboxStore: Since must not be nil")
	}

	if filter.Since.Sub(f.cutoff) > time.Second || f.cutoff.Sub(*filter.Since) > time.Second {
		return nil, fmt.Errorf("cutoffInboxStore: cutoff drift; got %v want %v",
			*filter.Since, f.cutoff)
	}
	out := make([]zenday.InboxCacheRow, 0, len(f.rows))
	for _, r := range f.rows {
		if r.CreatedAt.After(f.cutoff) {
			out = append(out, r)
		}
	}
	return out, nil
}
