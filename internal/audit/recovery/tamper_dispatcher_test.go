package recovery

import (
	"context"
	"testing"
)

type stubDoctrineResolver struct {
	doctrineFor map[string]string
}

func (s *stubDoctrineResolver) DoctrineFor(ctx context.Context, projectID string) (string, error) {
	d, ok := s.doctrineFor[projectID]
	if !ok {
		return "max-scope", nil
	}
	return d, nil
}

type stubProjectLister struct {
	ids []string
}

func (s *stubProjectLister) ListProjectIDs(ctx context.Context) ([]string, error) {
	return s.ids, nil
}

type stubInboxEmitter struct {
	pushed []InboxNotification
}

func (s *stubInboxEmitter) PushURGENT(ctx context.Context, projectID, message string) error {
	s.pushed = append(s.pushed, InboxNotification{ProjectID: projectID, Severity: "URGENT", Message: message})
	return nil
}

type stubEventEmitter struct {
	events []TamperEvent
}

func (s *stubEventEmitter) EmitTamperDetected(ctx context.Context, projectID string, detection *VerifyResult) error {
	s.events = append(s.events, TamperEvent{ProjectID: projectID, Path: detection.FirstTamperPath, RecordID: detection.FirstTamperRecordID})
	return nil
}

func TestDispatchMaxScopeHaltsOnlyTheProject(t *testing.T) {
	halts := NewHalts()
	doctrine := &stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "max-scope", "beta": "max-scope"}}
	lister := &stubProjectLister{ids: []string{"alpha", "beta", "gamma"}}
	inbox := &stubInboxEmitter{}
	events := &stubEventEmitter{}

	d := NewTamperDispatcher(halts, doctrine, lister, inbox, events)
	res, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch, FirstTamperRecordID: 99})
	if err != nil {
		t.Fatalf("DispatchTamperResponse: %v", err)
	}
	if !res.Halted {
		t.Errorf("Halted = false")
	}
	if len(res.CascadedHalts) != 0 {
		t.Errorf("CascadedHalts = %v, want empty for max-scope", res.CascadedHalts)
	}
	if !halts.IsHalted("alpha") {
		t.Error("alpha not halted")
	}
	if halts.IsHalted("beta") {
		t.Error("beta halted; max-scope should not cascade")
	}
}

func TestDispatchDefaultLogsContinues(t *testing.T) {
	halts := NewHalts()
	doctrine := &stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "default"}}
	lister := &stubProjectLister{ids: []string{"alpha"}}
	inbox := &stubInboxEmitter{}
	events := &stubEventEmitter{}
	d := NewTamperDispatcher(halts, doctrine, lister, inbox, events)
	res, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathTesseraProofFail})
	if err != nil {
		t.Fatalf("DispatchTamperResponse: %v", err)
	}
	if res.Halted {
		t.Errorf("Halted = true; default doctrine should NOT halt")
	}
	if halts.IsHalted("alpha") {
		t.Error("alpha halted; default should not halt")
	}
	if len(events.events) != 1 {
		t.Errorf("events = %d, want 1 emitted", len(events.events))
	}
}

func TestDispatchCapaFirewallCascades(t *testing.T) {
	halts := NewHalts()
	doctrine := &stubDoctrineResolver{doctrineFor: map[string]string{
		"alpha": "capa-firewall",
		"beta":  "max-scope",
		"gamma": "default",
	}}
	lister := &stubProjectLister{ids: []string{"alpha", "beta", "gamma"}}
	inbox := &stubInboxEmitter{}
	events := &stubEventEmitter{}
	d := NewTamperDispatcher(halts, doctrine, lister, inbox, events)
	res, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathWitnessSignatureInvalid})
	if err != nil {
		t.Fatalf("DispatchTamperResponse: %v", err)
	}
	if !res.Halted {
		t.Error("alpha not halted under capa-firewall")
	}
	if len(res.CascadedHalts) != 2 {
		t.Errorf("CascadedHalts = %v, want 2 (beta + gamma)", res.CascadedHalts)
	}
	for _, id := range []string{"alpha", "beta", "gamma"} {
		if !halts.IsHalted(id) {
			t.Errorf("%s not halted under capa-firewall cascade", id)
		}
	}
}

func TestDispatchAllPathsEmitInbox(t *testing.T) {
	halts := NewHalts()
	doctrine := &stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "max-scope"}}
	inbox := &stubInboxEmitter{}
	events := &stubEventEmitter{}
	d := NewTamperDispatcher(halts, doctrine, &stubProjectLister{ids: []string{"alpha"}}, inbox, events)
	_, _ = d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if len(inbox.pushed) != 1 {
		t.Errorf("inbox push count = %d, want 1", len(inbox.pushed))
	}
	if inbox.pushed[0].Severity != "URGENT" {
		t.Errorf("severity = %q, want URGENT", inbox.pushed[0].Severity)
	}
}

func TestHaltsClearAndList(t *testing.T) {
	h := NewHalts()
	h.Halt("alpha")
	h.Halt("beta")
	got := h.ListHalted()
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	h.Clear("alpha")
	if h.IsHalted("alpha") {
		t.Error("alpha still halted after Clear")
	}
}

func TestDispatchRejectsCleanResult(t *testing.T) {
	d := NewTamperDispatcher(NewHalts(), &stubDoctrineResolver{}, &stubProjectLister{}, &stubInboxEmitter{}, &stubEventEmitter{})
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: true})
	if err == nil {
		t.Error("expected error when dispatching on clean result")
	}
}
