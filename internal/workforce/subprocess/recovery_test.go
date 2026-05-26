package subprocess

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRebuildPromptEmptyHistory(t *testing.T) {
	msg, err := RebuildPrompt(ThreadID("tid-r1"), nil)
	if err != nil {
		t.Fatalf("RebuildPrompt: %v", err)
	}
	if msg.Kind != MessageKindNotification {
		t.Errorf("Kind = %v, want notification", msg.Kind)
	}
	if msg.Method != "context/restore" {
		t.Errorf("Method = %q, want context/restore", msg.Method)
	}
	if msg.ThreadID != "tid-r1" {
		t.Errorf("ThreadID = %q, want tid-r1", msg.ThreadID)
	}
	if !strings.Contains(string(msg.Payload), `"history_size":0`) {
		t.Errorf("expected history_size 0, got: %s", msg.Payload)
	}
}

func TestRebuildPromptIncludesLastNCheckpoints(t *testing.T) {
	cps := []Checkpoint{
		{ThreadID: "t", Index: 0, State: "first", CreatedAt: time.Unix(1, 0)},
		{ThreadID: "t", Index: 1, State: "second", CreatedAt: time.Unix(2, 0)},
		{ThreadID: "t", Index: 2, State: "third", CreatedAt: time.Unix(3, 0)},
	}
	msg, err := RebuildPrompt(ThreadID("t"), cps)
	if err != nil {
		t.Fatalf("RebuildPrompt: %v", err)
	}
	body := string(msg.Payload)
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(body, want) {
			t.Errorf("payload missing %q: %s", want, body)
		}
	}
	if !strings.Contains(body, `"history_size":3`) {
		t.Errorf("expected history_size 3, got: %s", body)
	}
}

func TestRebuildPromptIncludesTaskID(t *testing.T) {
	cps := []Checkpoint{
		{ThreadID: "t", TaskID: "task-007", Index: 0, State: "x", CreatedAt: time.Unix(1, 0)},
	}
	msg, err := RebuildPrompt(ThreadID("t"), cps)
	if err != nil {
		t.Fatalf("RebuildPrompt: %v", err)
	}
	if !strings.Contains(string(msg.Payload), `"task-007"`) {
		t.Errorf("task_id missing: %s", msg.Payload)
	}
}

func TestLastNDefault(t *testing.T) {
	if LastNDefault != 16 {
		t.Errorf("LastNDefault = %d, want 16", LastNDefault)
	}
}

func TestRecoveryConfigDefaultsLastN(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	cfg := mgr.RecoveryConfig()
	if cfg.LastN != LastNDefault {
		t.Errorf("LastN = %d, want %d", cfg.LastN, LastNDefault)
	}
}

func TestRecoveryConfigExplicitOverride(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory:  fakeFactory(t, "happy-path"),
		Clock:    realClock{},
		Recovery: RecoveryConfig{LastN: 32},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	cfg := mgr.RecoveryConfig()
	if cfg.LastN != 32 {
		t.Errorf("LastN = %d, want 32 (explicit override)", cfg.LastN)
	}
}

func TestRebuildPromptMarshalError(t *testing.T) {
	prev := jsonMarshal
	defer func() { jsonMarshal = prev }()
	synthetic := &marshalErr{msg: "synthetic encode err"}
	jsonMarshal = func(_ any) ([]byte, error) { return nil, synthetic }
	_, err := RebuildPrompt(ThreadID("t"), nil)
	if err == nil {
		t.Fatal("RebuildPrompt did not surface marshal error")
	}
}

type marshalErr struct{ msg string }

func (m *marshalErr) Error() string { return m.msg }

func TestCrashDetectorMarksCrashed(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "crash"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 10 * time.Second},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "crashy", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	first, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}

	if err := first.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1",
		Method: "prompt", ThreadID: first.ThreadID(),
	}); err != nil {

		_ = err
	}

	for i := 0; i < 20; i++ {
		clk.Advance(60 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			return
		}
	}
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 0 {
		t.Errorf("rows after crash = %d, want 0", len(rows))
	}
}

func TestCrashDetectorAllowsRespawnAfterCrash(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))

	scenarios := []string{"crash", "happy-path"}
	var idx int
	var mu sync.Mutex
	cyclingFactory := func(ctx context.Context, spec WorkerSpecRef) (Session, error) {
		mu.Lock()
		s := scenarios[idx%len(scenarios)]
		idx++
		mu.Unlock()
		return fakeFactory(t, s)(ctx, spec)
	}
	mgr, err := NewManager(ManagerOptions{
		Factory:         cyclingFactory,
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 10 * time.Second},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "respawn", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	a, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	_ = a.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1",
		Method: "prompt", ThreadID: a.ThreadID(),
	})

	for i := 0; i < 20; i++ {
		clk.Advance(60 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			break
		}
	}

	b, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire #2: %v", err)
	}
	if a == b {
		t.Errorf("respawn returned same Session instance")
	}
	if err := b.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "2",
		Method: "prompt", ThreadID: b.ThreadID(),
	}); err != nil {
		t.Fatalf("Send #2: %v", err)
	}
	if _, err := b.Receive(ctx); err != nil {
		t.Errorf("Receive #2: %v", err)
	}
}

func TestCrashDetectorSkipsAlreadyEvictedEntries(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory: func(_ context.Context, spec WorkerSpecRef) (Session, error) {
			return newMemSession(spec.ThreadID), nil
		},
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": time.Hour},
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "ev", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	for _, e := range mgr.persistents {
		e.evicted = true
	}
	mgr.mu.Unlock()
	clk.Advance(100 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)

}

func TestCrashDetectorSkipsMemSessions(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory: func(_ context.Context, spec WorkerSpecRef) (Session, error) {
			return newMemSession(spec.ThreadID), nil
		},
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": time.Hour},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "mem", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}
	clk.Advance(100 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Errorf("memSession entry was incorrectly removed: rows = %d", len(rows))
	}
}
