// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/inv_zen_099_override_audit_test.go
//
// inv-zen-099 (operator override audit, spec §7.1):
//
//	Every authorized operator override MUST emit exactly one
//	EvtOperatorOverrideApplied event carrying {OperatorUID, OverrideKind}
//	in its payload. Failed authentication (UID mismatch) MUST emit zero
//	events — the audit channel is reserved for authorized overrides only.
//
// Tests:
//  1. TestInvZen099_EveryOverrideKindEmitsAuditEvent — t.Run subtests
//     for all 5 OverrideKind constants. Each exercises Authenticate with
//     matching UID and asserts exactly 1 EvtOperatorOverrideApplied with
//     OverrideKind + OperatorUID populated correctly in the payload.
//  2. TestInvZen099_MismatchedUIDDoesNotEmit — mismatched peer UID
//     returns ErrOperatorIdentityMismatch and emits zero events.
//
// Payload format: confirmation_audit.go emits map[string]any with keys
// "operator_uid" (int), "operator_reason" (string), "override_kind"
// (string). The compliance fake's Last() returns this map directly for
// field-level assertions — no type-assertion to eventlog.OperatorOverrideApplied
// is needed (Event.Payload is map[string]any, not an interface value).
//
// No build tags: default `go test ./tests/compliance/` runs these.
package compliance_test

import (
	"context"
	"errors"
	"net"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func makeComplianceMiddleware(ap orch.AppenderAPI, processUID, peerUID int) *orch.OperatorIdentityMiddleware {
	return orch.NewOperatorIdentityMiddleware(orch.OperatorIdentityConfig{
		AppendEvents: ap,
		ProcessUID:   processUID,
		PeerCredentials: func(_ net.Conn) (int, error) {
			return peerUID, nil
		},
		SessionID: complianceTestSession,
		ProjectID: complianceTestProject,
	})
}

func TestInvZen099_EveryOverrideKindEmitsAuditEvent(t *testing.T) {
	kinds := []orch.OverrideKind{
		orch.OverrideKindConfirmationAck,
		orch.OverrideKindConfirmationDeny,
		orch.OverrideKindDoctrineRevert,
		orch.OverrideKindAutonomyMode,
		orch.OverrideKindDepthFlag,
	}
	const procUID = 501

	for _, k := range kinds {
		k := k
		t.Run(string(k), func(t *testing.T) {
			ap := newComplianceFakeAppender()
			mw := makeComplianceMiddleware(ap, procUID, procUID)

			if _, err := mw.Authenticate(context.Background(), &net.UnixConn{}, k, "test"); err != nil {
				t.Fatalf("Authenticate(%s): %v", k, err)
			}

			if got := ap.Count(eventlog.EvtOperatorOverrideApplied); got != 1 {
				t.Errorf("kind=%s: EvtOperatorOverrideApplied count = %d, want 1", k, got)
			}

			payload := ap.Last(eventlog.EvtOperatorOverrideApplied)
			if payload == nil {
				t.Fatalf("kind=%s: no payload captured for EvtOperatorOverrideApplied", k)
			}

			gotKind, _ := payload["override_kind"].(string)
			if gotKind != string(k) {
				t.Errorf("kind=%s: payload override_kind = %q, want %q", k, gotKind, string(k))
			}

			gotUID, ok := payload["operator_uid"].(int)
			if !ok {
				t.Fatalf("kind=%s: operator_uid wrong type %T (%v)", k, payload["operator_uid"], payload["operator_uid"])
			}
			if gotUID != procUID {
				t.Errorf("kind=%s: payload operator_uid = %d, want %d", k, gotUID, procUID)
			}
		})
	}
}

func TestInvZen099_MismatchedUIDDoesNotEmit(t *testing.T) {
	ap := newComplianceFakeAppender()

	mw := makeComplianceMiddleware(ap, 501, 999)

	_, err := mw.Authenticate(context.Background(), &net.UnixConn{}, orch.OverrideKindConfirmationAck, "")
	if !errors.Is(err, orch.ErrOperatorIdentityMismatch) {
		t.Fatalf("err = %v, want ErrOperatorIdentityMismatch", err)
	}
	if n := ap.Count(eventlog.EvtOperatorOverrideApplied); n != 0 {
		t.Errorf("inv-zen-099: failed auth emitted %d OperatorOverrideApplied events; want 0", n)
	}
}
