// tests/replay/tmux_state_replay_test.go (Plan 7 Phase K Task K-13).
//
// Replay-tier validation that the tmuxlife.SessionStore active-session
// set is reconstructed deterministically from a captured stream of
// session lifecycle ops (Upsert/SetStatus/Delete) — spec §4.7
// replay-recovery contract + spec §4.1 tmux-server-crash recovery
// (lazy-respawn on next activation, idle-reaper paused, daemon.db row
// reconciles on next sweep).
//
// Coverage:
//
//  1. TestReplay_TmuxState_DeterministicReconstruction — given a
//     captured stream of lifecycle ops, two independent replays into
//     fresh InMemorySessionStore instances produce identical
//     active-session sets (filtered by StatusActive).
//
//  2. TestReplay_TmuxState_OrphanStatePreserved — sessions that
//     transition Active → Orphaned during the captured stream are
//     reconstructed as Orphaned (NOT Active) by replay. The lazy-
//     respawn contract from §4.1 depends on this distinction:
//     reattach-after-tmux-restart spawns a fresh session row, never
//     reuses an orphan.
//
//  3. TestReplay_TmuxState_IdempotentReplay — applying the same
//     captured stream twice produces the same final session set
//     (no duplicates from re-Upsert; no leaks from re-Delete).
//     Idempotency guard against daemon-reboot loops where boot-time
//     replay may run more than once.
//
// Drift from spec heredoc (K-13 Step 1+2): the spec referenced
// fictional surfaces (tmuxlife.Replayer, tmuxlife.NewReplayer,
// tmuxlife.SessionSpec/IDSha8 fields, Session.GetByID/.WhereStatus,
// eventlog.NewRecorder/.Emit, EvtTmuxSpawned/EvtTmuxTeardown/
// EvtTmuxServerLost event types, tmuxlife.StatusRunning). None exist;
// the actual Plan 7 tmuxlife API (internal/tmuxlife/{session.go,
// api_p7.go, lifecycle.go}) ships:
//
//   - tmuxlife.Session (canonical row: Alias, Sha8, Name, CreatedAt,
//     LastAttachAt, Status)
//   - tmuxlife.SessionStatus enum {Active, Idle, Orphaned, Archived}
//     (no "Running" — Active is the canonical alive state)
//   - tmuxlife.SessionStore interface (Upsert/Get/List/Delete/
//     SetLastAttach/SetStatus/ExpectedPanesFor)
//   - tmuxlife.NewInMemorySessionStore (the canonical chaos/replay
//     substrate)
//   - tmuxlife.SessionName(alias, sha8) builds canonical names
//   - No EvtTmux* in the closed-set EventType taxonomy (Plan 7 Phase F
//     extended only F-1/F-2 events; tmux state lives in the
//     SessionStore directly).
//
// We adapt to the real surfaces and uphold the same load-bearing
// contract: capture lifecycle ops → replay reconstructs identical
// active-session set; orphan status survives replay; idempotent under
// repeated apply.
//
//go:build replay
// +build replay

package replay_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

type tmuxOpKind int

const (
	opUpsert tmuxOpKind = iota
	opSetStatus
	opSetLastAttach
	opDelete
)

type tmuxOp struct {
	OpKind     tmuxOpKind
	Session    tmuxlife.Session
	Name       string
	NewStatus  tmuxlife.SessionStatus
	AttachAt   time.Time
	OccurredAt time.Time
}

func applyTmuxOps(t *testing.T, store tmuxlife.SessionStore, ops []tmuxOp) {
	t.Helper()
	for i, op := range ops {
		switch op.OpKind {
		case opUpsert:
			if err := store.UpsertSession(op.Session); err != nil {
				t.Fatalf("ops[%d] UpsertSession(%s): %v", i, op.Session.Name, err)
			}
		case opSetStatus:
			if err := store.SetStatus(op.Name, op.NewStatus); err != nil {
				t.Fatalf("ops[%d] SetStatus(%s, %v): %v", i, op.Name, op.NewStatus, err)
			}
		case opSetLastAttach:
			if err := store.SetLastAttach(op.Name, op.AttachAt); err != nil {
				t.Fatalf("ops[%d] SetLastAttach(%s, %v): %v", i, op.Name, op.AttachAt, err)
			}
		case opDelete:
			if err := store.DeleteSession(op.Name); err != nil {
				t.Fatalf("ops[%d] DeleteSession(%s): %v", i, op.Name, err)
			}
		default:
			t.Fatalf("ops[%d] unknown OpKind %v", i, op.OpKind)
		}
	}
}

func activeSessionNames(t *testing.T, store tmuxlife.SessionStore) []string {
	t.Helper()
	all, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	out := make([]string, 0, len(all))
	for _, s := range all {
		if s.Status == tmuxlife.StatusActive {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

func allSessionsByName(t *testing.T, store tmuxlife.SessionStore) map[string]tmuxlife.Session {
	t.Helper()
	all, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	out := make(map[string]tmuxlife.Session, len(all))
	for _, s := range all {
		out[s.Name] = s
	}
	return out
}

func buildBaselineOps(t *testing.T, base time.Time) []tmuxOp {
	t.Helper()
	mk := func(alias, sha8 string) tmuxlife.Session {
		return tmuxlife.Session{
			Alias:        alias,
			Sha8:         sha8,
			Name:         tmuxlife.SessionName(alias, sha8),
			CreatedAt:    base,
			LastAttachAt: time.Time{},
			Status:       tmuxlife.StatusActive,
		}
	}
	return []tmuxOp{
		{OpKind: opUpsert, Session: mk("internal-platform-x", "11111111"), OccurredAt: base},
		{OpKind: opUpsert, Session: mk("zen-swarm", "22222222"), OccurredAt: base.Add(1 * time.Second)},
		{OpKind: opUpsert, Session: mk("nexus", "33333333"), OccurredAt: base.Add(2 * time.Second)},
		{OpKind: opUpsert, Session: mk("reference-project", "44444444"), OccurredAt: base.Add(3 * time.Second)},
		{OpKind: opUpsert, Session: mk("reference-project", "55555555"), OccurredAt: base.Add(4 * time.Second)},

		{
			OpKind:     opSetLastAttach,
			Name:       tmuxlife.SessionName("internal-platform-x", "11111111"),
			AttachAt:   base.Add(5 * time.Second),
			OccurredAt: base.Add(5 * time.Second),
		},

		{
			OpKind:     opSetStatus,
			Name:       tmuxlife.SessionName("zen-swarm", "22222222"),
			NewStatus:  tmuxlife.StatusOrphaned,
			OccurredAt: base.Add(6 * time.Second),
		},

		{
			OpKind:     opDelete,
			Name:       tmuxlife.SessionName("reference-project", "55555555"),
			OccurredAt: base.Add(7 * time.Second),
		},
	}
}

func TestReplay_TmuxState_DeterministicReconstruction(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ops := buildBaselineOps(t, base)

	storeLive := tmuxlife.NewInMemorySessionStore()
	applyTmuxOps(t, storeLive, ops)
	liveActive := activeSessionNames(t, storeLive)

	storeReplay := tmuxlife.NewInMemorySessionStore()
	applyTmuxOps(t, storeReplay, ops)
	replayActive := activeSessionNames(t, storeReplay)

	if !reflect.DeepEqual(liveActive, replayActive) {
		t.Fatalf("inv-zen-105/§4.7 VIOLATION: replay diverged from live;"+
			" live=%v replay=%v", liveActive, replayActive)
	}

	wantActive := []string{
		tmuxlife.SessionName("internal-platform-x", "11111111"),
		tmuxlife.SessionName("reference-project", "44444444"),
		tmuxlife.SessionName("nexus", "33333333"),
	}
	sort.Strings(wantActive)
	if !reflect.DeepEqual(replayActive, wantActive) {
		t.Fatalf("active-session set after replay: got %v, want %v",
			replayActive, wantActive)
	}

	all := allSessionsByName(t, storeReplay)
	internalPlatformX, ok := all[tmuxlife.SessionName("internal-platform-x", "11111111")]
	if !ok {
		t.Fatalf("internal-platform-x row missing after replay")
	}
	wantAttach := base.Add(5 * time.Second)
	if !internalPlatformX.LastAttachAt.Equal(wantAttach) {
		t.Fatalf("internal-platform-x LastAttachAt: got %v, want %v", internalPlatformX.LastAttachAt, wantAttach)
	}
}

func TestReplay_TmuxState_OrphanStatePreserved(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	demoName := tmuxlife.SessionName("demo", "abcdef12")

	ops := []tmuxOp{
		{
			OpKind: opUpsert,
			Session: tmuxlife.Session{
				Alias:     "demo",
				Sha8:      "abcdef12",
				Name:      demoName,
				CreatedAt: base,
				Status:    tmuxlife.StatusActive,
			},
			OccurredAt: base,
		},
		{
			OpKind:     opSetStatus,
			Name:       demoName,
			NewStatus:  tmuxlife.StatusOrphaned,
			OccurredAt: base.Add(time.Second),
		},
	}

	storeReplay := tmuxlife.NewInMemorySessionStore()
	applyTmuxOps(t, storeReplay, ops)

	all := allSessionsByName(t, storeReplay)
	demo, ok := all[demoName]
	if !ok {
		t.Fatalf("demo row missing from replay store")
	}
	if demo.Status != tmuxlife.StatusOrphaned {
		t.Fatalf("orphan status not preserved through replay; got %v want %v",
			demo.Status, tmuxlife.StatusOrphaned)
	}

	active := activeSessionNames(t, storeReplay)
	for _, name := range active {
		if name == demoName {
			t.Fatalf("orphan %s leaked into active set: %v (lazy-respawn invariant violated)",
				name, active)
		}
	}

	respawnOps := []tmuxOp{
		{
			OpKind: opUpsert,
			Session: tmuxlife.Session{
				Alias:        "demo",
				Sha8:         "abcdef12",
				Name:         demoName,
				CreatedAt:    base.Add(2 * time.Second),
				LastAttachAt: base.Add(2 * time.Second),
				Status:       tmuxlife.StatusActive,
			},
			OccurredAt: base.Add(2 * time.Second),
		},
	}
	applyTmuxOps(t, storeReplay, respawnOps)

	respawned := allSessionsByName(t, storeReplay)
	if got, want := respawned[demoName].Status, tmuxlife.StatusActive; got != want {
		t.Fatalf("post-respawn status: got %v want %v", got, want)
	}
	postActive := activeSessionNames(t, storeReplay)
	found := false
	for _, n := range postActive {
		if n == demoName {
			found = true
		}
	}
	if !found {
		t.Fatalf("post-respawn: %s missing from active set %v", demoName, postActive)
	}
}

func TestReplay_TmuxState_IdempotentReplay(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	allOps := buildBaselineOps(t, base)

	idempotentOps := make([]tmuxOp, 0, len(allOps))
	for _, op := range allOps {
		if op.OpKind != opDelete {
			idempotentOps = append(idempotentOps, op)
		}
	}

	store := tmuxlife.NewInMemorySessionStore()

	applyTmuxOps(t, store, idempotentOps)
	firstActive := activeSessionNames(t, store)
	firstAll := allSessionsByName(t, store)

	applyTmuxOps(t, store, idempotentOps)
	secondActive := activeSessionNames(t, store)
	secondAll := allSessionsByName(t, store)

	if !reflect.DeepEqual(firstActive, secondActive) {
		t.Fatalf("idempotency VIOLATION (active set):\n  first =%v\n  second=%v",
			firstActive, secondActive)
	}

	if len(firstAll) != len(secondAll) {
		t.Fatalf("idempotency VIOLATION (row count): first=%d second=%d",
			len(firstAll), len(secondAll))
	}
	for name, firstRow := range firstAll {
		secondRow, ok := secondAll[name]
		if !ok {
			t.Fatalf("row %s missing from second-apply store", name)
		}
		if firstRow.Status != secondRow.Status {
			t.Fatalf("row %s status diverged: first=%v second=%v",
				name, firstRow.Status, secondRow.Status)
		}
		if !firstRow.LastAttachAt.Equal(secondRow.LastAttachAt) {
			t.Fatalf("row %s LastAttachAt diverged: first=%v second=%v",
				name, firstRow.LastAttachAt, secondRow.LastAttachAt)
		}
	}

	independent := tmuxlife.NewInMemorySessionStore()
	applyTmuxOps(t, independent, idempotentOps)
	independentActive := activeSessionNames(t, independent)
	if !reflect.DeepEqual(secondActive, independentActive) {
		t.Fatalf("cross-store divergence: double-applied=%v, fresh-once=%v",
			secondActive, independentActive)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
}
