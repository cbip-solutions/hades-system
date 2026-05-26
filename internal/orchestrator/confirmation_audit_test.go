// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// Tests for RequireOperatorIdentity middleware (Plan 5 Phase F Task F-5,
// inv-zen-099). Black-box (package orchestrator_test) so we exercise the
// public surface (NewOperatorIdentityMiddleware + Authenticate +
// OverrideKind* constants + ErrOperatorIdentityMismatch) the way Phase N
// daemon wiring will. PeerCredentials is dependency-injected so these
// tests never touch a real Unix socket — that path is exercised by
// cross-compile gates (GOOS=linux + GOOS=darwin) and live integration in
// Phase N.
//
// The middleware's audit-emission contract (per spec §7.1):
//   - on UID match: emit exactly one EvtOperatorOverrideApplied with
//     {OperatorUID, OperatorReason, OverrideKind} populated.
//   - on UID mismatch OR peer-cred error: emit NOTHING via this audit
//     channel. Failed-auth events live in daemon access logs to avoid
//     drowning real overrides in noise.
package orchestrator_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// auditTestSession + auditTestProject are the canonical SessionID +
// ProjectID the F-5 middleware stamps on every emitted
// OperatorOverrideApplied event. The Phase A eventlog.Append validation
// rejects empty session_id / project_id (lesson from F-2 fix-pass), so
// the middleware's OperatorIdentityConfig MUST carry them.
const (
	auditTestSession = "session-audit-test"
	auditTestProject = "project-audit-test"
)

type auditFakeAppender struct {
	calls       atomic.Int32
	failOn      int32
	err         error
	mu          sync.Mutex
	counts      map[eventlog.EventType]int
	lastPayload map[eventlog.EventType]map[string]any
}

func newAuditFakeAppender() *auditFakeAppender {
	return &auditFakeAppender{
		counts:      map[eventlog.EventType]int{},
		lastPayload: map[eventlog.EventType]map[string]any{},
	}
}

func (f *auditFakeAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	n := f.calls.Add(1)
	if f.failOn > 0 && n == f.failOn && f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[ev.Type]++

	cp := make(map[string]any, len(ev.Payload))
	for k, v := range ev.Payload {
		cp[k] = v
	}
	f.lastPayload[ev.Type] = cp
	return int64(n), nil
}

func (f *auditFakeAppender) Count(t eventlog.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[t]
}

func (f *auditFakeAppender) Last(t eventlog.EventType) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPayload[t]
}

func makeAuditMiddleware(t *testing.T, ap orch.AppenderAPI, processUID, peerUID int, peerErr error) *orch.OperatorIdentityMiddleware {
	t.Helper()
	return orch.NewOperatorIdentityMiddleware(orch.OperatorIdentityConfig{
		AppendEvents: ap,
		ProcessUID:   processUID,
		PeerCredentials: func(_ net.Conn) (int, error) {
			if peerErr != nil {
				return 0, peerErr
			}
			return peerUID, nil
		},
		SessionID: auditTestSession,
		ProjectID: auditTestProject,
	})
}

func TestRequireOperatorIdentity_MatchingUID_Authorizes(t *testing.T) {
	const procUID = 501
	ap := newAuditFakeAppender()
	mw := makeAuditMiddleware(t, ap, procUID, procUID, nil)

	id, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindConfirmationAck, "approved")
	if err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if id.UID != procUID {
		t.Errorf("OperatorIdentity.UID = %d, want %d", id.UID, procUID)
	}
	if id.Reason != "approved" {
		t.Errorf("OperatorIdentity.Reason = %q, want %q", id.Reason, "approved")
	}
	if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 1 {
		t.Errorf("OperatorOverrideApplied events = %d, want 1", got)
	}
}

func TestRequireOperatorIdentity_MismatchedUID_Rejects(t *testing.T) {
	ap := newAuditFakeAppender()
	mw := makeAuditMiddleware(t, ap, 501, 999, nil)

	id, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindDoctrineRevert, "rollback")
	if !errors.Is(err, orch.ErrOperatorIdentityMismatch) {
		t.Fatalf("err = %v, want ErrOperatorIdentityMismatch", err)
	}
	if id.UID != 0 || id.Reason != "" {
		t.Errorf("rejected attempt returned non-zero identity %+v; expected zero value", id)
	}
	if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 0 {
		t.Errorf("rejected attempts must NOT emit OperatorOverrideApplied (got %d)", got)
	}
}

func TestRequireOperatorIdentity_PeerCredsError_Rejects(t *testing.T) {
	ap := newAuditFakeAppender()
	credsErr := errors.New("kernel says no")
	mw := makeAuditMiddleware(t, ap, 501, 0, credsErr)

	id, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindAutonomyMode, "")
	if err == nil {
		t.Fatal("expected error from peer-cred failure")
	}
	if !errors.Is(err, credsErr) {
		t.Errorf("peer-cred error not wrapped (errors.Is == false): %v", err)
	}
	// Mismatch path MUST NOT trigger here — peer-cred failure short-circuits.
	if errors.Is(err, orch.ErrOperatorIdentityMismatch) {
		t.Error("peer-cred error must not be reported as identity mismatch")
	}
	if id.UID != 0 || id.Reason != "" {
		t.Errorf("peer-cred error returned non-zero identity %+v; expected zero value", id)
	}
	if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 0 {
		t.Errorf("peer-cred error must NOT emit OperatorOverrideApplied (got %d)", got)
	}
}

func TestRequireOperatorIdentity_AppendFails_Surfaces(t *testing.T) {
	ap := newAuditFakeAppender()
	appendErr := errors.New("eventlog disk full")
	ap.failOn = 1
	ap.err = appendErr
	mw := makeAuditMiddleware(t, ap, 501, 501, nil)

	id, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindDepthFlag, "operator override")
	if err == nil {
		t.Fatal("expected error from Append failure")
	}
	if !errors.Is(err, appendErr) {
		t.Errorf("append error not wrapped: %v", err)
	}
	if id.UID != 0 || id.Reason != "" {
		t.Errorf("append failure returned non-zero identity %+v; expected zero value", id)
	}
}

func TestRequireOperatorIdentity_AllOverrideKinds_Audited(t *testing.T) {
	kinds := []orch.OverrideKind{
		orch.OverrideKindConfirmationAck,
		orch.OverrideKindConfirmationDeny,
		orch.OverrideKindDoctrineRevert,
		orch.OverrideKindAutonomyMode,
		orch.OverrideKindDepthFlag,
	}
	const procUID = 501

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			ap := newAuditFakeAppender()
			mw := makeAuditMiddleware(t, ap, procUID, procUID, nil)

			if _, err := mw.Authenticate(context.Background(), &net.UnixConn{}, kind, "test"); err != nil {
				t.Fatalf("Authenticate(%s): %v", kind, err)
			}
			if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 1 {
				t.Fatalf("event count for %s = %d, want 1", kind, got)
			}
			payload := ap.Last(eventlog.EvtOperatorOverrideApplied)
			if got, _ := payload["override_kind"].(string); got != string(kind) {
				t.Errorf("payload override_kind = %q, want %q", got, kind)
			}
		})
	}
}

// TestNewOperatorIdentityMiddleware_DefaultPeerCredentials asserts that
// when cfg.PeerCredentials is nil the constructor wires the
// platform-specific defaultPeerCredentials. We do NOT actually invoke
// the platform syscall (that needs a real Unix-socket fd); we only
// verify that subsequent Authenticate calls use a non-nil resolver by
// passing a non-Unix conn and observing the platform stub's Unix-socket
// guard error. This keeps the test deterministic across linux/darwin
// while still exercising the "default wiring is non-nil" path.
func TestNewOperatorIdentityMiddleware_DefaultPeerCredentials(t *testing.T) {
	ap := newAuditFakeAppender()
	mw := orch.NewOperatorIdentityMiddleware(orch.OperatorIdentityConfig{
		AppendEvents: ap,
		ProcessUID:   501,

		SessionID: auditTestSession,
		ProjectID: auditTestProject,
	})

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	_, err := mw.Authenticate(context.Background(), left, orch.OverrideKindConfirmationAck, "default-test")
	if err == nil {
		t.Fatal("expected default peer-cred resolver to error on non-Unix conn")
	}

	if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 0 {
		t.Errorf("peer-cred failure must NOT emit OperatorOverrideApplied (got %d)", got)
	}
}

func TestRequireOperatorIdentity_OperatorUIDInPayload(t *testing.T) {
	const procUID = 1000
	ap := newAuditFakeAppender()
	mw := makeAuditMiddleware(t, ap, procUID, procUID, nil)

	if _, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindConfirmationDeny, "operator-said-no"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	payload := ap.Last(eventlog.EvtOperatorOverrideApplied)
	if payload == nil {
		t.Fatal("no OperatorOverrideApplied payload captured")
	}

	gotUID, ok := payload["operator_uid"].(int)
	if !ok {
		t.Fatalf("operator_uid wrong type: %T (%v)", payload["operator_uid"], payload["operator_uid"])
	}
	if gotUID != procUID {
		t.Errorf("operator_uid = %d, want %d", gotUID, procUID)
	}
	if got, _ := payload["operator_reason"].(string); got != "operator-said-no" {
		t.Errorf("operator_reason = %q, want %q", got, "operator-said-no")
	}
	if got, _ := payload["override_kind"].(string); got != string(orch.OverrideKindConfirmationDeny) {
		t.Errorf("override_kind = %q, want %q", got, orch.OverrideKindConfirmationDeny)
	}
}
