// go:build cgo
package link

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type fakeAuditEmitter struct {
	events []emittedEvent
}

type emittedEvent struct {
	t       federation.EventType
	payload []byte
}

func (f *fakeAuditEmitter) Emit(_ context.Context, t federation.EventType, payload []byte) error {
	f.events = append(f.events, emittedEvent{
		t:       t,
		payload: append([]byte(nil), payload...),
	})
	return nil
}

type fakeUnresolvedStore struct {
	inserted []federation.UnresolvedRow
	err      error
}

func (f *fakeUnresolvedStore) Insert(_ context.Context, row federation.UnresolvedRow) error {
	if f.err != nil {
		return f.err
	}
	f.inserted = append(f.inserted, row)
	return nil
}

func TestSurfacePolicyPersistsUnresolvedRow(t *testing.T) {
	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-1"}
	err := s.Surface(context.Background(), store.APICall{
		CallID: "c1", Repo: "client-app", BaseURLRef: "UNKNOWN_URL",
	}, yaml.PolicySurface, "no manifest entry for UNKNOWN_URL")
	if err != nil {
		t.Fatalf("Surface(PolicySurface) = %v; want nil", err)
	}
	if len(us.inserted) != 1 || us.inserted[0].CallID != "c1" {
		t.Errorf("inserted = %+v; want 1 row with CallID=c1", us.inserted)
	}
	if us.inserted[0].WorkspaceID != "ws-1" {
		t.Errorf("inserted[0].WorkspaceID = %q; want ws-1 (surfacer propagates from struct)", us.inserted[0].WorkspaceID)
	}
	if us.inserted[0].RecordedAt == 0 {
		t.Errorf("inserted[0].RecordedAt = 0; want non-zero (time.Now().UnixNano())")
	}
	if len(audit.events) != 1 {
		t.Errorf("audit events = %d; want 1", len(audit.events))
	}
	// Closed-enum check: the EventType MUST be federation.EvtUnresolvedCall
	// ;
	// a string drift here would surface as a compile error on the field
	// type, not a silent miss-route.
	if got := audit.events[0].t; got != federation.EvtUnresolvedCall {
		t.Errorf("audit EventType = %q; want %q (FIX-2 closed-enum)",
			got, federation.EvtUnresolvedCall)
	}
}

func TestFailPolicyReturnsErrAndPersistsNothing(t *testing.T) {
	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-1"}
	err := s.Surface(context.Background(), store.APICall{CallID: "c2", Repo: "client-app", BaseURLRef: "UNKNOWN_URL"}, yaml.PolicyFail, "no manifest entry")
	if err == nil {
		t.Errorf("Surface(PolicyFail) = nil; want non-nil")
	}
	if !errors.Is(err, ErrNoManifestEntry) {
		t.Errorf("err = %v; want wraps ErrNoManifestEntry", err)
	}
	if len(us.inserted) != 0 {
		t.Errorf("inserted = %d; want 0 under PolicyFail", len(us.inserted))
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d; want 0 under PolicyFail", len(audit.events))
	}
}

func TestSilentPolicyDropsQuietlyNoErrorNoAudit(t *testing.T) {
	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-1"}
	if err := s.Surface(context.Background(), store.APICall{CallID: "c3", Repo: "client-app"}, yaml.PolicySilent, "no manifest entry"); err != nil {
		t.Errorf("Surface(PolicySilent) = %v; want nil", err)
	}
	if len(us.inserted) != 0 || len(audit.events) != 0 {
		t.Errorf("PolicySilent must drop quietly; inserted=%d audit=%d", len(us.inserted), len(audit.events))
	}

}

func TestSurfaceUnknownPolicyIsError(t *testing.T) {
	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-1"}
	err := s.Surface(context.Background(), store.APICall{CallID: "c4"}, yaml.UnresolvedPolicy("garbage"), "reason")
	if err == nil {
		t.Errorf("Surface(unknown policy) = nil; want non-nil")
	}
}
