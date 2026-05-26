package federation

import (
	"context"
	"testing"
)

func TestSisterClaim_NewAuditEmitter_NilAdapter_GracefulDegradation(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	emitter := NewAuditEmitter(nil, "ws-test-nil-adapter")
	if emitter == nil {
		t.Fatal("NewAuditEmitter(nil, ...) returned nil; want non-nil with degraded behaviour")
	}
	// Calling Emit on the nil-adapter emitter MUST NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("emitter.Emit(nil adapter) panicked: %v; want graceful no-op", r)
		}
	}()
	err := emitter.Emit(context.Background(), EvtCrossRepoLink, []byte(`{"test":"payload"}`))
	if err != nil {
		t.Errorf("emitter.Emit(nil adapter) returned err = %v; want nil (graceful degradation)", err)
	}
}

func TestSisterClaim_EmitAudit_InvalidEventType_Refused(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	hostileTypes := []EventType{
		EventType("forged.unknown.type"),
		EventType("plan20.invalid"),
		EventType(""),
		EventType("plan20.CROSS_REPO_LINK"),
	}
	for _, et := range hostileTypes {
		_, err := EmitAudit(context.Background(), nil, Event{
			Type:        et,
			WorkspaceID: "ws-test",
			OccurredAt:  1,
		})
		if err == nil {
			t.Errorf("EmitAudit(type=%q) returned nil err; want ErrUnknownEventType", et)
		}
	}
}

// TestSisterClaim_EmitAudit_AllValidEventTypes_PassValidation pins the
// event_types.go AllEventTypes() positive contract — every canonical
// event type in the master C-11 EventType enum MUST pass the Valid()
// check inside EmitAudit's gate. A drift between Valid() and the actual
// constants surfaces here as a hard failure.
//
// Bite-check: temporarily drop one EventType constant from
// AllEventTypes() (e.g., remove EvtWorkspacePolicySet) → this test
// fails for that type; restoring the entry turns the gate green again.
func TestSisterClaim_EmitAudit_AllValidEventTypes_PassValidation(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	// Every type in the canonical AllEventTypes() set MUST be Valid().
	for _, et := range AllEventTypes() {
		if !et.Valid() {
			t.Errorf("EventType %q is in AllEventTypes() but Valid() returns false — drift between Valid() switch and AllEventTypes() corpus", et)
		}

		_, err := EmitAudit(context.Background(), nil, Event{
			Type:        et,
			WorkspaceID: "ws-test",
			OccurredAt:  1,
		})
		if err != nil {
			t.Errorf("EmitAudit(type=%q, nil-adapter) returned err = %v; want nil (canonical type + nil adapter = graceful)", et, err)
		}
	}
}
