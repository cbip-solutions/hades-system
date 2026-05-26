package zenday_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeProjectConfig struct {
	optOut map[string]bool
}

func (f fakeProjectConfig) IsEODOptedIn(alias string) bool {
	if f.optOut == nil {
		return true
	}
	return !f.optOut[alias]
}

func newEODDeps(t *testing.T, tmp string, now time.Time) (zenday.EODDeps, *fakeEmitter) {
	t.Helper()
	morningDeps, emitter := newMorningDeps(t, tmp, now)
	deps := zenday.EODDeps{
		MorningDeps:   morningDeps,
		ProjectConfig: fakeProjectConfig{},
	}
	return deps, emitter
}

func canonicalHandoff(t *testing.T, alias, summary, state, next string, ts time.Time, blockers []string) []byte {
	t.Helper()
	payload := zenday.HandoffPostedPayload{
		ProjectID:       "id-" + alias,
		ProjectAlias:    alias,
		Timestamp:       ts,
		Summary:         summary,
		AutonomousState: state,
		NextSession:     next,
		Blockers:        blockers,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal handoff: %v", err)
	}
	return raw
}

func TestGenerateEODDigest_HappyPath_HandoffsRendered(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-internal-platform-x",
				ProjectAlias: "internal-platform-x",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON: canonicalHandoff(t,
					"internal-platform-x",
					"Stage 4 Build phase 12 complete",
					"paused",
					"review L4 finding",
					canonicalNow,
					[]string{"HRA L4 alert"}),
			},
			{
				ProjectID:    "id-zen-swarm",
				ProjectAlias: "zen-swarm",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON: canonicalHandoff(t,
					"zen-swarm",
					"Plan 7 Phase F-7 shipped",
					"active",
					"phase F-8 dispatch",
					canonicalNow,
					nil),
			},
		},
	}
	deps.Cost = &fakeCost{
		statuses: []zenday.CostStatus{
			{ProjectAlias: "internal-platform-x", SpendUSD: 0.42},
			{ProjectAlias: "zen-swarm", SpendUSD: 1.18},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if doc.Type != zenday.BriefTypeEOD {
		t.Errorf("doc.Type = %v, want BriefTypeEOD", doc.Type)
	}
	wantDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !doc.Date.Equal(wantDate) {
		t.Errorf("doc.Date = %v, want %v", doc.Date, wantDate)
	}
	if len(doc.PerProjectStatus) != 2 {
		t.Fatalf("len(PerProjectStatus) = %d, want 2", len(doc.PerProjectStatus))
	}

	if doc.PerProjectStatus[0].Alias != "internal-platform-x" {
		t.Errorf("section[0].Alias = %q, want internal-platform-x", doc.PerProjectStatus[0].Alias)
	}
	if doc.PerProjectStatus[1].Alias != "zen-swarm" {
		t.Errorf("section[1].Alias = %q, want zen-swarm", doc.PerProjectStatus[1].Alias)
	}
	if doc.PerProjectStatus[0].HandoffSummary != "Stage 4 Build phase 12 complete" {
		t.Errorf("section[0].HandoffSummary = %q", doc.PerProjectStatus[0].HandoffSummary)
	}
	if doc.PerProjectStatus[0].AutonomousState != "paused" {
		t.Errorf("section[0].AutonomousState = %q", doc.PerProjectStatus[0].AutonomousState)
	}
	if got := doc.CostWatchUSD; got < 1.59 || got > 1.61 {
		t.Errorf("CostWatchUSD = %f, want ~1.60", got)
	}

	wantPath := filepath.Join(tmp, "zen-day-2026-05-01-eod.md")
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected file at %s: %v", wantPath, err)
	}
	if !strings.Contains(string(body), "# zen day — 2026-05-01 EOD digest") {
		t.Errorf("digest body missing heading; body = %q", string(body))
	}
	if !strings.Contains(string(body), "### internal-platform-x") {
		t.Errorf("digest body missing internal-platform-x section; body = %q", string(body))
	}

	calls := emitter.snapshot()
	if len(calls) != 1 {
		t.Fatalf("emitter calls = %d, want 1", len(calls))
	}
	if calls[0].kind != "EODDigestReady" {
		t.Errorf("emitted kind = %q, want EODDigestReady", calls[0].kind)
	}
	var payload zenday.EODDigestReadyPayload
	if err := json.Unmarshal(calls[0].payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if !payload.Date.Equal(wantDate) {
		t.Errorf("payload.Date = %v, want %v", payload.Date, wantDate)
	}
	if payload.ProjectCount != 2 {
		t.Errorf("payload.ProjectCount = %d, want 2", payload.ProjectCount)
	}
	if got := payload.TotalCostUSD; got < 1.59 || got > 1.61 {
		t.Errorf("payload.TotalCostUSD = %f, want ~1.60", got)
	}
	if payload.FilePath != wantPath {
		t.Errorf("payload.FilePath = %q, want %q", payload.FilePath, wantPath)
	}
}

func TestGenerateEODDigest_LatestHandoffWins(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	earlier := canonicalNow.Add(-3 * time.Hour)
	later := canonicalNow.Add(-1 * time.Hour)
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-internal-platform-x",
				ProjectAlias: "internal-platform-x",
				Kind:         "HandoffPosted",
				CreatedAt:    earlier,
				PayloadJSON:  canonicalHandoff(t, "internal-platform-x", "early summary", "active", "early next", earlier, nil),
			},
			{
				ProjectID:    "id-internal-platform-x",
				ProjectAlias: "internal-platform-x",
				Kind:         "HandoffPosted",
				CreatedAt:    later,
				PayloadJSON:  canonicalHandoff(t, "internal-platform-x", "late summary (canonical)", "paused", "late next", later, []string{"L4 raised"}),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len(PerProjectStatus) = %d, want 1 (deduped)", len(doc.PerProjectStatus))
	}
	if doc.PerProjectStatus[0].HandoffSummary != "late summary (canonical)" {
		t.Errorf("HandoffSummary = %q, want %q", doc.PerProjectStatus[0].HandoffSummary, "late summary (canonical)")
	}
	if doc.PerProjectStatus[0].AutonomousState != "paused" {
		t.Errorf("AutonomousState = %q, want paused (latest)", doc.PerProjectStatus[0].AutonomousState)
	}
}

// TestGenerateEODDigest_LatestHandoffWins_OutOfOrder ensures the dedup
// is order-insensitive: the LATER timestamp wins regardless of which
// record arrives first in the slice (eventlog.QueryByType ordering is
// implementation-detail; per inv-zen-031 we MUST NOT depend on it).
func TestGenerateEODDigest_LatestHandoffWins_OutOfOrder(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	earlier := canonicalNow.Add(-3 * time.Hour)
	later := canonicalNow.Add(-1 * time.Hour)

	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-internal-platform-x",
				ProjectAlias: "internal-platform-x",
				Kind:         "HandoffPosted",
				CreatedAt:    later,
				PayloadJSON:  canonicalHandoff(t, "internal-platform-x", "later summary", "paused", "later next", later, nil),
			},
			{
				ProjectID:    "id-internal-platform-x",
				ProjectAlias: "internal-platform-x",
				Kind:         "HandoffPosted",
				CreatedAt:    earlier,
				PayloadJSON:  canonicalHandoff(t, "internal-platform-x", "earlier summary", "active", "earlier next", earlier, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len(PerProjectStatus) = %d, want 1", len(doc.PerProjectStatus))
	}
	if doc.PerProjectStatus[0].HandoffSummary != "later summary" {
		t.Errorf("HandoffSummary = %q, want %q (earlier must not overwrite later)",
			doc.PerProjectStatus[0].HandoffSummary, "later summary")
	}
}

func TestGenerateEODDigest_OptOutSkipsSection(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.ProjectConfig = fakeProjectConfig{optOut: map[string]bool{"silent-project": true}}
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-silent-project",
				ProjectAlias: "silent-project",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "silent-project", "should not render", "active", "", canonicalNow, nil),
			},
			{
				ProjectID:    "id-loud-project",
				ProjectAlias: "loud-project",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "loud-project", "this renders", "active", "", canonicalNow, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len(PerProjectStatus) = %d, want 1 (silent-project opted out)", len(doc.PerProjectStatus))
	}
	if doc.PerProjectStatus[0].Alias != "loud-project" {
		t.Errorf("kept alias = %q, want loud-project", doc.PerProjectStatus[0].Alias)
	}
	for _, s := range doc.PerProjectStatus {
		if s.Alias == "silent-project" {
			t.Error("silent-project rendered despite opt-out")
		}
	}
}

func TestGenerateEODDigest_NoHandoffsManualState(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "no-handoff", State: "active"},
		},
	}

	deps.Eventlog = &fakeEventlog{}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len(PerProjectStatus) = %d, want 1", len(doc.PerProjectStatus))
	}
	sec := doc.PerProjectStatus[0]
	if sec.Alias != "no-handoff" {
		t.Errorf("Alias = %q, want no-handoff", sec.Alias)
	}
	if sec.AutonomousState != "manual" {
		t.Errorf("AutonomousState = %q, want manual", sec.AutonomousState)
	}
	if sec.HandoffSummary != "" {
		t.Errorf("HandoffSummary = %q, want empty (no handoff)", sec.HandoffSummary)
	}

	wantPath := filepath.Join(tmp, "zen-day-2026-05-01-eod.md")
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read digest: %v", err)
	}
	if !strings.Contains(string(body), "No handoff posted today") {
		t.Errorf("expected 'No handoff posted today' line; body=%s", string(body))
	}
}

func TestGenerateEODDigest_NoProjectsAtAll(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 0 {
		t.Errorf("len(PerProjectStatus) = %d, want 0", len(doc.PerProjectStatus))
	}
	if doc.CostWatchUSD != 0 {
		t.Errorf("CostWatchUSD = %f, want 0", doc.CostWatchUSD)
	}
	if got := len(emitter.snapshot()); got != 1 {
		t.Errorf("emit count = %d, want 1 (empty digest still emits)", got)
	}
}

func TestGenerateEODDigest_Idempotent_ReturnsErrAlreadyGenerated(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)

	if _, err := zenday.GenerateEODDigest(context.Background(), deps, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err == nil {
		t.Fatal("second call: want ErrAlreadyGenerated, got nil")
	}
	if !errors.Is(err, zenday.ErrAlreadyGenerated) {
		t.Errorf("err = %v, want ErrAlreadyGenerated", err)
	}
	if got := len(emitter.snapshot()); got != 1 {
		t.Errorf("emit count after re-run = %d, want 1 (idempotent)", got)
	}
}

func TestGenerateEODDigest_ForceOverwrites(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)

	if _, err := zenday.GenerateEODDigest(context.Background(), deps, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := zenday.GenerateEODDigest(context.Background(), deps, true); err != nil {
		t.Fatalf("force second call: %v", err)
	}
	if got := len(emitter.snapshot()); got != 2 {
		t.Errorf("emit count with force = %d, want 2", got)
	}
}

func TestGenerateEODDigest_ContextCancelled(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := zenday.GenerateEODDigest(ctx, deps, false)
	if err == nil {
		t.Fatal("expected error on cancelled ctx; got nil")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "zen-day-2026-05-01-eod.md")); statErr == nil {
		t.Error("digest file written despite ctx cancel; want skipped")
	}
	if len(emitter.snapshot()) != 0 {
		t.Errorf("emit count = %d, want 0 on cancellation", len(emitter.snapshot()))
	}
}

func TestGenerateEODDigest_EventlogQueryError(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	deps.Eventlog = &fakeEventlog{err: errors.New("eventlog query down")}

	_, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err == nil {
		t.Fatal("expected eventlog query error; got nil")
	}
	if !strings.Contains(err.Error(), "eod handoff query") {
		t.Errorf("err = %v, want containing 'eod handoff query'", err)
	}
	if !strings.Contains(err.Error(), "eventlog query down") {
		t.Errorf("err = %v, want containing root cause", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "zen-day-2026-05-01-eod.md")); statErr == nil {
		t.Error("digest file written despite eventlog failure; want skipped")
	}
	if len(emitter.snapshot()) != 0 {
		t.Errorf("emit count = %d, want 0 on eventlog failure", len(emitter.snapshot()))
	}
}

func TestGenerateEODDigest_AutonomyError_PartialTolerance(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	deps.Autonomy = &fakeAutonomy{err: errors.New("autonomy down")}
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-p",
				ProjectAlias: "p",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "p", "shipped", "active", "", canonicalNow, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("autonomy failure must be soft; err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Errorf("len(PerProjectStatus) = %d, want 1 (handoff still rendered)", len(doc.PerProjectStatus))
	}
	if got := len(emitter.snapshot()); got != 1 {
		t.Errorf("emit count = %d, want 1", got)
	}
}

func TestGenerateEODDigest_CostError_PartialTolerance(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	deps.Cost = &fakeCost{err: errors.New("cost down")}
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-p",
				ProjectAlias: "p",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "p", "shipped", "active", "", canonicalNow, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("cost failure must be soft; err = %v", err)
	}
	if doc.CostWatchUSD != 0 {
		t.Errorf("CostWatchUSD = %f, want 0 on cost failure", doc.CostWatchUSD)
	}
	if got := len(emitter.snapshot()); got != 1 {
		t.Errorf("emit count = %d, want 1", got)
	}
}

func TestGenerateEODDigest_MalformedHandoffPayloadSkipped(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-bad",
				ProjectAlias: "bad",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  []byte("{not valid json"),
			},
			{
				ProjectID:    "id-good",
				ProjectAlias: "good",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "good", "shipped", "active", "", canonicalNow, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("malformed payload must not abort digest; err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len(PerProjectStatus) = %d, want 1 (only good rendered)", len(doc.PerProjectStatus))
	}
	if doc.PerProjectStatus[0].Alias != "good" {
		t.Errorf("kept alias = %q, want good", doc.PerProjectStatus[0].Alias)
	}
}

func TestGenerateEODDigest_EmitErrorDoesNotVoidDigest(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	emitter.err = errors.New("eventlog down")

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("emit failure must not void digest; err = %v", err)
	}
	if doc.Type != zenday.BriefTypeEOD {
		t.Errorf("doc.Type = %v, want BriefTypeEOD", doc.Type)
	}
	wantPath := filepath.Join(tmp, "zen-day-2026-05-01-eod.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("digest file missing after emit failure: %v", err)
	}
}

func TestGenerateEODDigest_WriteFileError(t *testing.T) {
	tmp := t.TempDir()
	deps, emitter := newEODDeps(t, tmp, canonicalNow)
	deps.WriteFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("disk full")
	}

	_, err := zenday.GenerateEODDigest(context.Background(), deps, false)
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

func TestGenerateEODDigest_MkdirEnsuresArchiveDir(t *testing.T) {
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "fresh", "nested", "archive")
	deps, _ := newEODDeps(t, subdir, canonicalNow)

	if _, err := zenday.GenerateEODDigest(context.Background(), deps, false); err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(subdir, "zen-day-2026-05-01-eod.md")); err != nil {
		t.Errorf("file missing under fresh dir: %v", err)
	}
}

func TestGenerateEODDigest_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	subdir := filepath.Join(blocker, "archive")
	deps, _ := newEODDeps(t, subdir, canonicalNow)

	_, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err == nil {
		t.Fatal("expected mkdir error; got nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("err = %v, want containing 'mkdir'", err)
	}
}

func TestEODDigestReadyPayloadJSONShape(t *testing.T) {
	payload := zenday.EODDigestReadyPayload{
		Date:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ProjectCount: 3,
		TotalCostUSD: 1.84,
		FilePath:     "/tmp/zen-day-2026-05-01-eod.md",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"date":"2026-05-01T00:00:00Z"`,
		`"project_count":3`,
		`"total_cost_usd":1.84`,
		`"file_path":"/tmp/zen-day-2026-05-01-eod.md"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload JSON missing %q; got %s", want, got)
		}
	}
}

func TestHandoffPostedPayloadJSONShape(t *testing.T) {
	payload := zenday.HandoffPostedPayload{
		ProjectID:       "id-x",
		ProjectAlias:    "x",
		Timestamp:       time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Summary:         "s",
		RecentCommits:   []string{"abc"},
		AutonomousState: "active",
		Blockers:        []string{"b"},
		NextSession:     "n",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"project_id":"id-x"`,
		`"project_alias":"x"`,
		`"timestamp":"2026-05-01T12:00:00Z"`,
		`"summary":"s"`,
		`"recent_commits":["abc"]`,
		`"autonomous_state":"active"`,
		`"blockers":["b"]`,
		`"next_session_action":"n"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload JSON missing %q; got %s", want, got)
		}
	}
}

func TestGenerateEODDigest_BlockersAndTomorrowRender(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-p",
				ProjectAlias: "p",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON: canonicalHandoff(t, "p", "shipped", "active",
					"resume tomorrow", canonicalNow, []string{"b1", "b2"}),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len = %d, want 1", len(doc.PerProjectStatus))
	}
	sec := doc.PerProjectStatus[0]
	if sec.Tomorrow != "resume tomorrow" {
		t.Errorf("Tomorrow = %q, want %q", sec.Tomorrow, "resume tomorrow")
	}
	if len(sec.Blockers) != 2 || sec.Blockers[0] != "b1" || sec.Blockers[1] != "b2" {
		t.Errorf("Blockers = %v, want [b1 b2]", sec.Blockers)
	}

	wantPath := filepath.Join(tmp, "zen-day-2026-05-01-eod.md")
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read digest: %v", err)
	}
	for _, want := range []string{"Blocker: b1", "Blocker: b2", "Tomorrow: resume tomorrow"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("digest body missing %q; body=%s", want, string(body))
		}
	}
}

func TestGenerateEODDigest_AutonomySupplementsAfterHandoff(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "p", State: "paused", PauseReason: "L4"},
		},
	}
	deps.Eventlog = &fakeEventlog{
		records: []zenday.EventRecord{
			{
				ProjectID:    "id-p",
				ProjectAlias: "p",
				Kind:         "HandoffPosted",
				CreatedAt:    canonicalNow,
				PayloadJSON:  canonicalHandoff(t, "p", "shipped", "active", "", canonicalNow, nil),
			},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len = %d, want 1", len(doc.PerProjectStatus))
	}
	sec := doc.PerProjectStatus[0]
	if sec.AutonomousState != "active" {
		t.Errorf("AutonomousState = %q, want active (HandoffPosted wins)", sec.AutonomousState)
	}
	if sec.HandoffSummary != "shipped" {
		t.Errorf("HandoffSummary = %q, want shipped", sec.HandoffSummary)
	}
}

func TestGenerateEODDigest_OptOutOfAutonomyOnlyProject(t *testing.T) {
	tmp := t.TempDir()
	deps, _ := newEODDeps(t, tmp, canonicalNow)
	deps.ProjectConfig = fakeProjectConfig{optOut: map[string]bool{"silent": true}}
	deps.Autonomy = &fakeAutonomy{
		snap: []zenday.AutonomySnapshot{
			{ProjectAlias: "silent", State: "active"},
			{ProjectAlias: "loud", State: "active"},
		},
	}

	doc, err := zenday.GenerateEODDigest(context.Background(), deps, false)
	if err != nil {
		t.Fatalf("GenerateEODDigest err = %v", err)
	}
	if len(doc.PerProjectStatus) != 1 {
		t.Fatalf("len = %d, want 1", len(doc.PerProjectStatus))
	}
	if doc.PerProjectStatus[0].Alias != "loud" {
		t.Errorf("kept alias = %q, want loud", doc.PerProjectStatus[0].Alias)
	}
}
