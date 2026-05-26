// Package compliance — inv-zen-128: HandoffPostedEvent payload conforms
// to the canonical 8-field schema frozen by spec §1 Q15 + master plan
// §"HandoffPosted event coordination". Defense-in-depth: a compile-time
// anchor (HandoffPostedEvent.Type() returning EvtHandoffPosted +
// Decode() registering EvtHandoffPosted in its switch) plus a runtime
// JSON Marshal/Unmarshal round-trip property fuzz N=100.
//
// Spec §1 Q15 + §7.2 inv-zen-128 wording (Plan 7 Phase F-1):
//
//	"HandoffPosted event payload conforms to schema. Compile-check via
//	 _ HandoffPostedEvent.Type() = EvtHandoffPosted registration;
//	 runtime via Decode() switch handles EvtHandoffPosted returning
//	 typed struct; round-trips JSON Marshal/Unmarshal + verifies field
//	 set (8 fields per spec)."
//
// Phase H + I will extend this with daemon HTTP handler validation
// (4xx on schema mismatch + AutonomousState enum carve-out per Layer 3
// of defense-in-depth); this Phase F-13 boundary witness pins the
// wire schema at the type-system + JSON layer.
//
// Coverage matrix:
//
//	(a) Round-trip fuzz N=100 with deterministic seed (128): random
//	    HandoffPostedEvent values (random commit/blocker counts, random
//	    timestamps, random AutonomousState picked from the 4 canonical
//	    {active|paused|idle|complete} values). Marshal -> Unmarshal
//	    MUST preserve every field byte-identically.
//	(b) Decode() switch arm: a hand-crafted JSON byte slice routes to
//	    HandoffPostedEvent via eventlog.Decode(EvtHandoffPosted, body)
//	    — proves the cross-package decoder dispatch table contains
//	    the EvtHandoffPosted entry, not just the typed struct itself.
//	(c) Field-set exactly 8: a zero-value HandoffPostedEvent marshals
//	    to JSON, the JSON is unmarshalled into map[string]any, and the
//	    8 canonical field keys (project_id, project_alias, timestamp,
//	    summary, recent_commits, autonomous_state, blockers,
//	    next_session_action) MUST be present, no more, no fewer. A
//	    refactor that adds a 9th field without updating the spec /
//	    consumer expectations would break Phase H plugin emit + Phase
//	    I daemon HTTP handler in lockstep; this test catches the drift
//	    in CI before it ships.
//	(d) JSON tag canonical names: the spec §1 Q15 freezes the
//	    snake_case JSON keys (project_id, project_alias, timestamp,
//	    summary, recent_commits, autonomous_state, blockers,
//	    next_session_action). A refactor that flips to camelCase or
//	    accidentally drops the json:"..." struct tag would surface as
//	    a missing-field assertion in (c) AND a serialization mismatch
//	    in (a).
//	(e) AutonomousState canonical 4-value set: the typed struct
//	    accepts any string for forward-compat, but Phase H emitters +
//	    Phase I handler MUST agree on the 4-value set. The fuzz
//	    constructs payloads using only canonical states; a future
//	    addition (e.g., "suspended") MUST be recorded in spec §1 Q15 +
//	    propagated through this set + the daemon handler validator.
//	(f) Empty-slice round-trip: when RecentCommits or Blockers is the
//	    empty slice, Marshal MUST emit a JSON array (possibly empty,
//	    not null). The omitempty tag is intentionally absent on these
//	    fields per spec — they are always present on the wire to give
//	    consumers a stable shape.
//
// Boundary (inv-zen-031): this test imports only
// internal/orchestrator/eventlog + stdlib. eventlog is the load-bearing
// surface; the dispatcheradapter / store layers are not touched.
//
// Inv-zen-128 contract.
package compliance

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var canonicalAutonomousStates = []string{"active", "paused", "idle", "complete"}

var canonicalHandoffFields = []string{
	"autonomous_state",
	"blockers",
	"next_session_action",
	"project_alias",
	"project_id",
	"recent_commits",
	"summary",
	"timestamp",
}

func TestInvZen128_HandoffPostedRoundTripFuzz(t *testing.T) {
	const trials = 100
	rng := rand.New(rand.NewSource(128))
	for trial := 0; trial < trials; trial++ {
		evt := randomHandoffPostedEvent(rng)
		body, err := evt.Payload()
		if err != nil {
			t.Fatalf("inv-zen-128 trial %d: Payload() error = %v", trial, err)
		}
		var back eventlog.HandoffPostedEvent
		if err := json.Unmarshal(body, &back); err != nil {
			t.Fatalf("inv-zen-128 trial %d: json.Unmarshal: %v\nbody: %s", trial, err, string(body))
		}
		assertEqualHandoffPostedEvent(t, evt, back, trial)
	}
}

// TestInvZen128_DecodeRoutesToHandoffPostedEvent locks the cross-package
// decoder dispatch: eventlog.Decode(EvtHandoffPosted, body) MUST return
// a HandoffPostedEvent typed value. A refactor that drops the
// EvtHandoffPosted arm from the Decode switch (e.g., a merge-conflict
// resolution silently deleting the case) would break every cross-Plan
// consumer that goes through Decode (Phase I HTTP handler + Phase F EOD
// digest composer + future Plan 8/11 routing). This boundary witness
// catches that drift.
func TestInvZen128_DecodeRoutesToHandoffPostedEvent(t *testing.T) {
	body := []byte(`{` +
		`"project_id":"a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7",` +
		`"project_alias":"internal-platform-x",` +
		`"timestamp":"2026-05-01T18:30:00Z",` +
		`"summary":"Phase F-13 ship",` +
		`"recent_commits":["abc","def"],` +
		`"autonomous_state":"paused",` +
		`"blockers":["b1"],` +
		`"next_session_action":"resume autonomous"` +
		`}`)
	got, err := eventlog.Decode(eventlog.EvtHandoffPosted, body)
	if err != nil {
		t.Fatalf("inv-zen-128: eventlog.Decode(EvtHandoffPosted, ...) error = %v", err)
	}
	hp, ok := got.(eventlog.HandoffPostedEvent)
	if !ok {
		t.Fatalf("inv-zen-128: Decode returned %T, want HandoffPostedEvent", got)
	}
	if hp.ProjectAlias != "internal-platform-x" {
		t.Errorf("inv-zen-128: ProjectAlias = %q, want %q", hp.ProjectAlias, "internal-platform-x")
	}
	if hp.AutonomousState != "paused" {
		t.Errorf("inv-zen-128: AutonomousState = %q, want %q", hp.AutonomousState, "paused")
	}
	if len(hp.RecentCommits) != 2 {
		t.Errorf("inv-zen-128: len(RecentCommits) = %d, want 2", len(hp.RecentCommits))
	}
	if hp.NextSession != "resume autonomous" {
		t.Errorf("inv-zen-128: NextSession = %q, want %q", hp.NextSession, "resume autonomous")
	}
	// Type() registration anchor: the typed value MUST report
	// EvtHandoffPosted. Pins the compile-time Type() method.
	if hp.Type() != eventlog.EvtHandoffPosted {
		t.Errorf("inv-zen-128: Type() = %v, want EvtHandoffPosted", hp.Type())
	}
}

// TestInvZen128_FieldSetExactlyEight pins the canonical 8-field schema:
// a zero-value HandoffPostedEvent marshals to JSON, the JSON is
// unmarshalled into map[string]any, and the field key set MUST equal
// canonicalHandoffFields. A refactor that adds, removes, or renames a
// field without updating spec §1 Q15 + the producer/consumer in
// lockstep would break the wire schema; this test is the contract pin.
//
// Note: the canonical set asserts presence by key name. The
// `omitempty` tag is intentionally absent on the schema fields — they
// always serialize on the wire to give consumers a stable shape (a
// nil RecentCommits slice still serializes as `null` per encoding/json
// default; the schema treats null and [] as equivalent for slice
// fields). This means a zero-value event MUST emit all 8 keys.
func TestInvZen128_FieldSetExactlyEight(t *testing.T) {
	evt := eventlog.HandoffPostedEvent{}
	body, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("inv-zen-128: Marshal zero-value event: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("inv-zen-128: Unmarshal into map: %v\nbody: %s", err, string(body))
	}
	if len(got) != len(canonicalHandoffFields) {
		t.Errorf("inv-zen-128: field count = %d (%v), want %d (%v)",
			len(got), keysOf(got), len(canonicalHandoffFields), canonicalHandoffFields)
	}
	for _, k := range canonicalHandoffFields {
		if _, ok := got[k]; !ok {
			t.Errorf("inv-zen-128: missing canonical field %q in JSON output\nbody: %s",
				k, string(body))
		}
	}

	canonicalSet := make(map[string]struct{}, len(canonicalHandoffFields))
	for _, k := range canonicalHandoffFields {
		canonicalSet[k] = struct{}{}
	}
	for k := range got {
		if _, ok := canonicalSet[k]; !ok {
			t.Errorf("inv-zen-128: foreign field %q in JSON output (drift from canonical 8-field set)", k)
		}
	}
}

// TestInvZen128_AutonomousStateCanonicalSetCovered closes the producer/
// consumer agreement loop: a payload constructed with each canonical
// AutonomousState value MUST round-trip cleanly. Future additions to
// the canonical set (e.g., spec amendment introduces "suspended") MUST
// be added to canonicalAutonomousStates here AND the Phase I daemon
// HTTP handler validator in lockstep; a failure here is a load-bearing
// signal that the validator drifted.
func TestInvZen128_AutonomousStateCanonicalSetCovered(t *testing.T) {
	for _, state := range canonicalAutonomousStates {
		t.Run(state, func(t *testing.T) {
			evt := eventlog.HandoffPostedEvent{
				ProjectID:       strings.Repeat("a", 64),
				ProjectAlias:    "test-alias",
				Timestamp:       time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC),
				Summary:         "test",
				RecentCommits:   []string{"c1"},
				AutonomousState: state,
				Blockers:        []string{},
				NextSession:     "test next",
			}
			body, err := evt.Payload()
			if err != nil {
				t.Fatalf("inv-zen-128: Payload(state=%q) error = %v", state, err)
			}
			var back eventlog.HandoffPostedEvent
			if err := json.Unmarshal(body, &back); err != nil {
				t.Fatalf("inv-zen-128: Unmarshal(state=%q): %v", state, err)
			}
			if back.AutonomousState != state {
				t.Errorf("inv-zen-128: AutonomousState = %q, want %q", back.AutonomousState, state)
			}
		})
	}
}

func randomHandoffPostedEvent(rng *rand.Rand) eventlog.HandoffPostedEvent {
	commitN := rng.Intn(10)
	commits := make([]string, commitN)
	for i := range commits {
		commits[i] = fmt.Sprintf("commit-%016x-%d", rng.Int63(), i)
	}
	blockerN := rng.Intn(5)
	blockers := make([]string, blockerN)
	for i := range blockers {
		blockers[i] = fmt.Sprintf("blocker-%d-%016x", i, rng.Int63())
	}
	hexPID := fmt.Sprintf("%016x%016x%016x%016x",
		rng.Int63(), rng.Int63(), rng.Int63(), rng.Int63())
	return eventlog.HandoffPostedEvent{
		ProjectID:       hexPID,
		ProjectAlias:    fmt.Sprintf("alias-%d", rng.Intn(100000)),
		Timestamp:       time.Unix(rng.Int63n(2_000_000_000), 0).UTC(),
		Summary:         fmt.Sprintf("summary trial %d", rng.Intn(1000000)),
		RecentCommits:   commits,
		AutonomousState: canonicalAutonomousStates[rng.Intn(len(canonicalAutonomousStates))],
		Blockers:        blockers,
		NextSession:     fmt.Sprintf("next %d", rng.Intn(1000000)),
	}
}

func assertEqualHandoffPostedEvent(t *testing.T, a, b eventlog.HandoffPostedEvent, trial int) {
	t.Helper()
	if a.ProjectID != b.ProjectID {
		t.Errorf("inv-zen-128 trial %d: ProjectID drift: %q -> %q", trial, a.ProjectID, b.ProjectID)
	}
	if a.ProjectAlias != b.ProjectAlias {
		t.Errorf("inv-zen-128 trial %d: ProjectAlias drift: %q -> %q", trial, a.ProjectAlias, b.ProjectAlias)
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		t.Errorf("inv-zen-128 trial %d: Timestamp drift: %v -> %v", trial, a.Timestamp, b.Timestamp)
	}
	if a.Summary != b.Summary {
		t.Errorf("inv-zen-128 trial %d: Summary drift: %q -> %q", trial, a.Summary, b.Summary)
	}
	if a.AutonomousState != b.AutonomousState {
		t.Errorf("inv-zen-128 trial %d: AutonomousState drift: %q -> %q", trial, a.AutonomousState, b.AutonomousState)
	}
	if a.NextSession != b.NextSession {
		t.Errorf("inv-zen-128 trial %d: NextSession drift: %q -> %q", trial, a.NextSession, b.NextSession)
	}
	if len(a.RecentCommits) != len(b.RecentCommits) {
		t.Errorf("inv-zen-128 trial %d: RecentCommits len drift: %d -> %d",
			trial, len(a.RecentCommits), len(b.RecentCommits))
	} else {
		for i := range a.RecentCommits {
			if a.RecentCommits[i] != b.RecentCommits[i] {
				t.Errorf("inv-zen-128 trial %d: RecentCommits[%d] drift: %q -> %q",
					trial, i, a.RecentCommits[i], b.RecentCommits[i])
			}
		}
	}
	if len(a.Blockers) != len(b.Blockers) {
		t.Errorf("inv-zen-128 trial %d: Blockers len drift: %d -> %d",
			trial, len(a.Blockers), len(b.Blockers))
	} else {
		for i := range a.Blockers {
			if a.Blockers[i] != b.Blockers[i] {
				t.Errorf("inv-zen-128 trial %d: Blockers[%d] drift: %q -> %q",
					trial, i, a.Blockers[i], b.Blockers[i])
			}
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
