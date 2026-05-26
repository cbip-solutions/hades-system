// tests/compliance/inv_zen_110_anomaly_typed_test.go
//
// Compliance gate for inv-zen-110: anomaly events MUST be typed via the
// Go enum AnomalyType (int kind) — NO string-typed Type field. The Plan
// 5 amendment.proposer dispatches by switch on AnomalyType so a typo or
// string concat would be a compile error, not a runtime ADR misroute.
//
// The same dispatch-table-must-be-compile-time-resolvable reasoning
// applies to EventType: subscribers (Plan 5 amendment.proposer,
// observability) decode payloads statically against the EventType
// discriminator. A string-typed event taxonomy would push that
// dispatch to runtime where typos drift silently. Hence the sibling
// guards (TestInvZen110EventTypeIsIntKind / AllEventTypesCovered) —
// they apply the same principle to the broader event surface.
//
// Two scans per type:
//  1. Reflection scan: AnomalyType / EventType underlying Kind() must
//     be reflect.Int. Catches the silent refactor "let's just make it
//     a string for cleaner JSON" — which we explicitly forbid by spec
//     §2.6 Q10 C / Q11 D.
//  2. Closed-set scan: AllAnomalyTypes() and AllEventTypes() must
//     enumerate the Phase A frozen lists (5 anomaly subtypes; 16
//     event types per spec §2.6). Adding a value without updating the
//     frozen list (or vice versa) trips the test, forcing the spec/
//     code/test triplet to stay in lockstep.
//
// String() values are the load-bearing mapping the Plan 5 amendment.
// proposer uses to select per-type templates; the closed-set scan
// asserts every expected String() name appears exactly once.
package compliance

import (
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestInvZen110AnomalyTypeIsIntKind(t *testing.T) {
	at := merge.AnomalyType(0)
	if k := reflect.TypeOf(at).Kind(); k != reflect.Int {
		t.Fatalf("inv-zen-110 VIOLATED: merge.AnomalyType.Kind() = %v want reflect.Int (spec §2.6 Q11 D — string-typed anomaly fields would defeat compile-time dispatch)", k)
	}
}

func TestInvZen110AllAnomalyTypesCovered(t *testing.T) {
	all := merge.AllAnomalyTypes()
	if len(all) != 5 {
		t.Fatalf("inv-zen-110 VIOLATED: AllAnomalyTypes() len = %d want 5 (Phase A frozen, spec §2.6)", len(all))
	}
	want := map[string]bool{
		"ScoringFormulaWinnerVetoed":       false,
		"BaselineUnstableAcrossSessions":   false,
		"FlakeRateAboveThreshold":          false,
		"TextualMergeUnresolvableRateHigh": false,
		"ModeDegradationPersistent":        false,
	}
	for _, at := range all {
		s := at.String()
		if _, ok := want[s]; !ok {
			t.Errorf("inv-zen-110 VIOLATED: unexpected AnomalyType %q (spec §2.6 frozen list mismatch — update spec or revert addition)", s)
			continue
		}
		want[s] = true
	}
	for s, found := range want {
		if !found {
			t.Errorf("inv-zen-110 VIOLATED: AllAnomalyTypes missing %q (spec §2.6 frozen list)", s)
		}
	}
}

func TestInvZen110EventTypeIsIntKind(t *testing.T) {
	et := merge.EventType(0)
	if k := reflect.TypeOf(et).Kind(); k != reflect.Int {
		t.Fatalf("inv-zen-110 (sibling) VIOLATED: merge.EventType.Kind() = %v want reflect.Int (spec §2.6 Q10 C — string-typed event taxonomy would defeat compile-time decode)", k)
	}
}

// TestInvZen110AllEventTypesCovered enforces the Phase A frozen list of
// 16 EventType values (spec §2.6: 5 merge lifecycle, 3 baseline, 4
// candidate, 1 scoring, 1 cache rebuild, 1 straggler kill, 1 anomaly).
// EvtUnknown (the zero value) is excluded from AllEventTypes per spec
// — it is reserved for "uninitialized event" detection and MUST NOT be
// emitted; subscribers receiving EvtUnknown treat it as log corruption.
func TestInvZen110AllEventTypesCovered(t *testing.T) {
	all := merge.AllEventTypes()
	if len(all) != 16 {
		t.Fatalf("inv-zen-110 (sibling) VIOLATED: AllEventTypes() len = %d want 16 (Phase A frozen, spec §2.6)", len(all))
	}
	want := map[string]bool{
		"MergeStartedWithMode":     false,
		"MergeCacheHit":            false,
		"MergeCompleted":           false,
		"MergeFailed":              false,
		"MergeAllCandidatesFailed": false,
		"BaselineStarted":          false,
		"BaselineComplete":         false,
		"BaselineFailed":           false,
		"CandidateStarted":         false,
		"CandidateComplete":        false,
		"CandidateFailed":          false,
		"FlakeRerunStarted":        false,
		"ScoringComplete":          false,
		"MergeCacheRebuilt":        false,
		"MergeStragglerKilled":     false,
		"MergeAnomalyDetected":     false,
	}
	for _, et := range all {
		s := et.String()
		if _, ok := want[s]; !ok {
			t.Errorf("inv-zen-110 (sibling) VIOLATED: unexpected EventType %q (spec §2.6 frozen list mismatch)", s)
			continue
		}
		want[s] = true
	}
	for s, found := range want {
		if !found {
			t.Errorf("inv-zen-110 (sibling) VIOLATED: AllEventTypes missing %q (spec §2.6 frozen list)", s)
		}
	}
}
