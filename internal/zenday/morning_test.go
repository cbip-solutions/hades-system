package zenday_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }

type fakePaths struct {
	root string
}

func (f fakePaths) MorningBriefPath(date time.Time) string {
	return filepath.Join(f.root, "zen-day-"+date.Format("2006-01-02")+".md")
}

func (f fakePaths) EODDigestPath(date time.Time) string {
	return filepath.Join(f.root, "zen-day-"+date.Format("2006-01-02")+"-eod.md")
}

type fakeEmitter struct {
	mu    sync.Mutex
	calls []emitCall
	err   error
}

type emitCall struct {
	kind    string
	payload []byte
}

func (f *fakeEmitter) Emit(_ context.Context, kind string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}

	dup := make([]byte, len(payload))
	copy(dup, payload)
	f.calls = append(f.calls, emitCall{kind: kind, payload: dup})
	return nil
}

func (f *fakeEmitter) snapshot() []emitCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]emitCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func newMorningDeps(t *testing.T, tmp string, now time.Time) (zenday.MorningDeps, *fakeEmitter) {
	t.Helper()
	emitter := &fakeEmitter{}
	deps := zenday.MorningDeps{
		CollectDeps: zenday.CollectDeps{
			Inbox:     &fakeInboxStore{},
			Scheduler: &fakeSchedulerStore{},
			Git:       &fakeGitCli{},
			Autonomy:  &fakeAutonomy{},
			Cost:      &fakeCost{},
			Eventlog:  &fakeEventlog{},
		},
		Clock:     fakeClock{now: now},
		Paths:     fakePaths{root: tmp},
		Emitter:   emitter,
		WriteFile: os.WriteFile,
	}
	return deps, emitter
}

var canonicalNow = time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)

func TestGenerateMorningBrief_HappyPath_WritesFileAndEmitsEvent(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)

	doc, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateMorningBrief err = %v", err)
	}
	if doc.Type != zenday.BriefTypeMorning {
		t.Errorf("doc.Type = %v, want BriefTypeMorning", doc.Type)
	}
	wantDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !doc.Date.Equal(wantDate) {
		t.Errorf("doc.Date = %v, want %v (UTC midnight)", doc.Date, wantDate)
	}

	wantPath := filepath.Join(tmp, "zen-day-2026-05-01.md")
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected file at %s: %v", wantPath, err)
	}
	if !strings.Contains(string(body), "# zen day — 2026-05-01 morning brief") {
		t.Errorf("brief body missing heading; body = %q", string(body))
	}

	calls := emitter.snapshot()
	if len(calls) != 1 {
		t.Fatalf("emitter calls = %d, want 1", len(calls))
	}
	if calls[0].kind != "MorningBriefReady" {
		t.Errorf("emitted kind = %q, want %q", calls[0].kind, "MorningBriefReady")
	}
	var payload zenday.MorningBriefReadyPayload
	if err := json.Unmarshal(calls[0].payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if !payload.Date.Equal(wantDate) {
		t.Errorf("payload.Date = %v, want %v", payload.Date, wantDate)
	}
	if payload.ItemCount != 0 {
		t.Errorf("payload.ItemCount = %d, want 0 (empty legs)", payload.ItemCount)
	}
	if payload.ProjectCount != 0 {
		t.Errorf("payload.ProjectCount = %d, want 0", payload.ProjectCount)
	}
	if payload.FilePath != wantPath {
		t.Errorf("payload.FilePath = %q, want %q", payload.FilePath, wantPath)
	}
}

func TestGenerateMorningBrief_Idempotent_ReturnsErrAlreadyGenerated(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)

	if _, err := zenday.GenerateMorningBrief(context.Background(), deps, false); err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err == nil {
		t.Fatal("second call: want ErrAlreadyGenerated, got nil")
	}
	if !errors.Is(err, zenday.ErrAlreadyGenerated) {
		t.Errorf("err = %v, want ErrAlreadyGenerated", err)
	}

	if len(emitter.snapshot()) != 1 {
		t.Errorf("emit count after re-run = %d, want 1 (idempotent: no re-emit)",
			len(emitter.snapshot()))
	}
}

func TestGenerateMorningBrief_ForceOverwritesExisting(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)

	if _, err := zenday.GenerateMorningBrief(context.Background(), deps, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := zenday.GenerateMorningBrief(context.Background(), deps, true); err != nil {
		t.Fatalf("force second call: %v", err)
	}
	if got := len(emitter.snapshot()); got != 2 {
		t.Errorf("emit count with force = %d, want 2", got)
	}
}

func TestGenerateMorningBrief_PartialTolerance_LegFailDoesNotVoidBrief(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)
	deps.Inbox = &fakeInboxStore{err: errors.New("inbox down")}
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "internal-platform-x", State: "paused", PauseReason: "L4"},
		},
	}

	doc, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateMorningBrief err = %v (expected nil; ≥1 leg succeeded)", err)
	}
	if len(doc.Items) != 1 {
		t.Fatalf("len(doc.Items) = %d, want 1 (autonomy paused → operator-gate)", len(doc.Items))
	}
	if len(emitter.snapshot()) != 1 {
		t.Errorf("emit count = %d, want 1", len(emitter.snapshot()))
	}
}

func TestGenerateMorningBrief_AllReadLegsFail_GracefulDegradation(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)
	deps.Inbox = &fakeInboxStore{err: errors.New("a")}
	deps.Scheduler = &fakeSchedulerStore{err: errors.New("b")}
	deps.Git = &fakeGitCli{err: errors.New("c")}
	deps.Autonomy = &fakeAutonomy{err: errors.New("d")}
	deps.Cost = &fakeCost{err: errors.New("e")}

	doc, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateMorningBrief err = %v (want graceful empty-brief)", err)
	}
	if len(doc.Items) != 0 {
		t.Errorf("len(doc.Items) = %d, want 0 (every read leg failed)", len(doc.Items))
	}

	if _, statErr := os.Stat(filepath.Join(tmp, "zen-day-2026-05-01.md")); statErr != nil {
		t.Errorf("brief file missing after partial failure: %v", statErr)
	}
	if got := len(emitter.snapshot()); got != 1 {
		t.Errorf("emit count = %d, want 1 (graceful degradation still emits)", got)
	}
}

func TestGenerateMorningBrief_ContextCancelled(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := zenday.GenerateMorningBrief(ctx, deps, false)
	if err == nil {
		t.Fatal("expected error on cancelled ctx; got nil")
	}
	if !errors.Is(err, zenday.ErrCollectCancelled) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ErrCollectCancelled or context.Canceled", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "zen-day-2026-05-01.md")); statErr == nil {
		t.Error("brief file written despite ctx cancel; want skipped")
	}
	if len(emitter.snapshot()) != 0 {
		t.Errorf("emit count = %d, want 0 on cancellation", len(emitter.snapshot()))
	}
}

func TestGenerateMorningBrief_TruncatesAboveCap(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)

	rows := make([]zenday.InboxCacheRow, 0, 9)
	for i := 0; i < 9; i++ {
		rows = append(rows, zenday.InboxCacheRow{
			NotificationID: int64(i),
			ProjectAlias:   "p" + string(rune('1'+i%9)),
			Severity:       inbox.SeverityUrgent,
			EventType:      "hra.l4_alert",
			CreatedAt:      canonicalNow.Add(-time.Duration(i) * time.Minute),
		})
	}
	deps.Inbox = &fakeInboxStore{rows: rows}

	doc, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateMorningBrief err = %v", err)
	}
	if len(doc.Items) != 7 {
		t.Errorf("len(doc.Items) = %d, want 7 (capped)", len(doc.Items))
	}
	if doc.TruncatedCount != 2 {
		t.Errorf("doc.TruncatedCount = %d, want 2", doc.TruncatedCount)
	}

	calls := emitter.snapshot()
	if len(calls) != 1 {
		t.Fatalf("emit count = %d, want 1", len(calls))
	}
	var payload zenday.MorningBriefReadyPayload
	if err := json.Unmarshal(calls[0].payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload.ItemCount != 7 {
		t.Errorf("payload.ItemCount = %d, want 7 (post-truncation)", payload.ItemCount)
	}
}

func TestGenerateMorningBrief_DistinctProjectCount(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)
	deps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, ProjectAlias: "internal-platform-x", Severity: inbox.SeverityUrgent, EventType: "hra.l4_alert", CreatedAt: canonicalNow},
			{NotificationID: 2, ProjectAlias: "internal-platform-x", Severity: inbox.SeverityActionNeeded, EventType: "scheduler.routine_failed", CreatedAt: canonicalNow},
			{NotificationID: 3, ProjectAlias: "zen-swarm", Severity: inbox.SeverityInfoImmediate, EventType: "x", CreatedAt: canonicalNow},
			{NotificationID: 4, ProjectAlias: "", Severity: inbox.SeverityInfoImmediate, EventType: "y", CreatedAt: canonicalNow},
		},
	}

	if _, err := zenday.GenerateMorningBrief(context.Background(), deps, false); err != nil {
		t.Fatalf("GenerateMorningBrief err = %v", err)
	}

	calls := emitter.snapshot()
	if len(calls) != 1 {
		t.Fatalf("emit count = %d, want 1", len(calls))
	}
	var payload zenday.MorningBriefReadyPayload
	if err := json.Unmarshal(calls[0].payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload.ProjectCount != 2 {
		t.Errorf("payload.ProjectCount = %d, want 2 (internal-platform-x + zen-swarm; empty excluded)",
			payload.ProjectCount)
	}
}

func TestGenerateMorningBrief_EmitErrorDoesNotVoidBrief(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)
	emitter.err = errors.New("eventlog down")

	doc, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateMorningBrief err = %v (emit failure must not void brief)", err)
	}
	if doc.Type != zenday.BriefTypeMorning {
		t.Errorf("doc.Type = %v, want BriefTypeMorning", doc.Type)
	}
	wantPath := filepath.Join(tmp, "zen-day-2026-05-01.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("brief file missing after emit failure: %v", err)
	}
}

func TestGenerateMorningBrief_WriteFileError(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newMorningDeps(t, tmp, canonicalNow)
	deps.WriteFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("disk full")
	}

	_, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err == nil {
		t.Fatal("expected write error; got nil")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want containing 'disk full'", err)
	}
	if len(emitter.snapshot()) != 0 {
		t.Errorf("emit count = %d, want 0 on write failure", len(emitter.snapshot()))
	}
}

func TestGenerateMorningBrief_MkdirEnsuresArchiveDir(t *testing.T) {
	tmp := t.TempDir()

	subdir := filepath.Join(tmp, "fresh", "nested", "archive")
	deps, _ := newMorningDeps(t, subdir, canonicalNow)

	if _, err := zenday.GenerateMorningBrief(context.Background(), deps, false); err != nil {
		t.Fatalf("GenerateMorningBrief err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(subdir, "zen-day-2026-05-01.md")); err != nil {
		t.Errorf("file missing under fresh dir: %v", err)
	}
}

func TestGenerateMorningBrief_MkdirError(t *testing.T) {
	tmp := t.TempDir()

	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	subdir := filepath.Join(blocker, "archive")
	deps, _ := newMorningDeps(t, subdir, canonicalNow)

	_, err := zenday.GenerateMorningBrief(context.Background(), deps, false)
	if err == nil {
		t.Fatal("expected mkdir error; got nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("err = %v, want containing 'mkdir'", err)
	}
}

func TestMorningBriefReadyPayloadJSONShape(t *testing.T) {
	payload := zenday.MorningBriefReadyPayload{
		Date:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ItemCount:    3,
		ProjectCount: 2,
		FilePath:     "/tmp/zen-day-2026-05-01.md",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"date":"2026-05-01T00:00:00Z"`,
		`"item_count":3`,
		`"project_count":2`,
		`"file_path":"/tmp/zen-day-2026-05-01.md"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload JSON missing %q; got %s", want, got)
		}
	}
}
