// tests/replay/replay_test.go.
//
// Sample replay test that exercises the LoadJSONL + AssertEquivalentEvents
// helpers (Task O-2) against a minimal hand-built capture fixture.
// ships the test infrastructure; the recovery-driven replay-
// rebuild (which would feed captured events back through the orchestrator
// state machine) lands in a follow-up phase that exposes a
// recovery.ReplayFromFixture entry point. Until then, this test:
//
// 1. Loads the fixture (round-trips header sha + footer).
// 2. Asserts each event decodes to the expected eventlog.Event shape
// (Type + Payload-key set).
// 3. Verifies the captured baseline is bit-equivalent to a re-loaded
// copy (replay determinism floor).
// 4. Pins a 50ms hard cap on load latency so regressions surface early.
//
// Cross-worker integration paths are out of scope
// and skipped explicitly.
//
// go:build replay
//go:build replay
// +build replay

package replay_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/tests/replay"
)

func TestOrchestrator_ReplayStageBuildHappy(t *testing.T) {
	const fixturePath = "testdata/captured/stagebuild_happy_minimal.jsonl"

	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	t0 := time.Now()
	captured, err := replay.LoadJSONL(f)
	loadElapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(captured.Events) != 4 {
		t.Fatalf("captured events = %d, want 4", len(captured.Events))
	}

	if loadElapsed > 50*time.Millisecond {
		t.Errorf("LoadJSONL took %v, exceeds 50ms hard cap (regression?)", loadElapsed)
	}

	wantTypes := []eventlog.EventType{
		eventlog.EvtOrchestratorStarted,
		eventlog.EvtDepthWidthDecided,
		eventlog.EvtWorkerDispatched,
		eventlog.EvtOrchestratorStopped,
	}
	for i, want := range wantTypes {
		if got := captured.Events[i].Event.Type; got != want {
			t.Errorf("event[%d] type = %v, want %v", i, got, want)
		}
	}

	wantKeys := map[int][]string{
		0: {"session_id", "project_id", "autonomy_mode"},
		1: {"depth", "width", "rationale"},
		2: {"worker_id", "task_id", "tier"},
		3: {"outcome"},
	}
	for i, keys := range wantKeys {
		payload := captured.Events[i].Event.Payload
		if payload == nil {
			t.Errorf("event[%d] has nil payload", i)
			continue
		}
		for _, k := range keys {
			if _, ok := payload[k]; !ok {
				t.Errorf("event[%d] missing payload key %q (have keys=%v)", i, k, payloadKeys(payload))
			}
		}
	}

	f2, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("re-open fixture: %v", err)
	}
	defer f2.Close()
	captured2, err := replay.LoadJSONL(f2)
	if err != nil {
		t.Fatalf("LoadJSONL (replay): %v", err)
	}
	replay.AssertEquivalentEvents(t, captured.EventsOf(), captured2.EventsOf())
}

func TestOrchestrator_ReplayStageBuildHappy_HeaderSignature(t *testing.T) {

	f, err := os.Open("testdata/captured/stagebuild_happy_minimal.jsonl")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	captured, err := replay.LoadJSONL(f)
	if err != nil {
		t.Fatalf("LoadJSONL must verify signature: %v", err)
	}
	if captured.Header.SessionID != "sess-happy-min" {
		t.Errorf("session_id = %q, want sess-happy-min", captured.Header.SessionID)
	}
	if captured.Header.MetadataSha256 == "" {
		t.Errorf("metadata_sha256 missing from header")
	}
}

func TestOrchestrator_ReplayCrossWorker_SkippedUntilPlan6(t *testing.T) {

	t.Skip("Plan 6 MergeEngine not yet shipped — replay test for cross-worker paths deferred to Plan 6")
}

func payloadKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if strings.Compare(keys[i], keys[j]) > 0 {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
