// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT
//
// RequireOperatorIdentity middleware (HADES design task,
// invariant).
//
// The daemon accepts operator overrides (confirmation ack/deny,
// doctrine revert, autonomy mode change, depth flag) over a Unix domain
// socket. This middleware is the SINGLE audit channel through which every
// authorized override flows: it
//
// 1. Pulls peer credentials from the underlying *net.UnixConn — no
// `--user-id` flag, no header, no body field is ever trusted (the
// daemon process trusts the kernel, not the wire).
// 2. Verifies the peer UID matches the daemon's ProcessUID. Mismatch
// surfaces ErrOperatorIdentityMismatch and EMITS NOTHING via this
// audit channel. per design contract, failed-auth events live in daemon
// access logs — putting them here would drown real overrides in
// impostor noise. (The threat model treats peer-cred mismatch as
// an external authn failure, not an internal authz event.)
// 3. On match: appends EvtOperatorOverrideApplied with payload
// {OperatorUID, OperatorReason, OverrideKind} so audit consumers and
// override to (UID, reason, kind) without lossy fall-through.
//
// Platform-specific peer-cred lookups live in peercred_{linux,darwin,
// other}.go — Linux uses SO_PEERCRED via golang.org/x/sys/unix.
// GetsockoptUcred; Darwin uses LOCAL_PEERCRED via unix.GetsockoptXucred
// (the cross-platform wrapper from x/sys/unix on Darwin). The syscall is
// isolated behind the OperatorIdentityConfig.PeerCredentials function
// variable so unit tests inject a fake; cross-platform compilation is
// guarded by `GOOS=linux go build./...` + `GOOS=darwin go build./...`
// in the F-5 gate set.
//
// ships the middleware + audit emission only. wires the
// HTTP handler chain so /v1/confirmations/{ack,deny}, /v1/doctrine/revert,
// /v1/autonomy, /v1/depth all funnel through Authenticate before
// mutating orchestrator state. Adding a new operator-override surface
// in a future stage therefore reduces to (a) wiring the handler through
// this middleware and (b) extending the OverrideKind constant set.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrOperatorIdentityMismatch = errors.New("orchestrator: operator UID mismatch")

// OverrideKind enumerates the audited override surfaces. Every surface
// in + future (autonomy mode) + (depth flag)
// MUST go through Authenticate before mutating orchestrator state; the
// kind value lands verbatim in the EvtOperatorOverrideApplied audit
// row's override_kind field.
//
// The set is intentionally CLOSED: new overrides must add a new
// constant + a new test row in TestRequireOperatorIdentity_AllOverrideKinds_Audited.
// This is the same closed-taxonomy discipline applied to EventType
// and DecisionClass.
type OverrideKind string

const (
	OverrideKindConfirmationAck OverrideKind = "confirmation_ack"

	OverrideKindConfirmationDeny OverrideKind = "confirmation_deny"

	OverrideKindDoctrineRevert OverrideKind = "doctrine_revert"

	OverrideKindAutonomyMode OverrideKind = "autonomy_mode"

	OverrideKindDepthFlag OverrideKind = "depth_flag"
)

type OperatorIdentityConfig struct {
	AppendEvents    AppenderAPI
	ProcessUID      int
	PeerCredentials func(net.Conn) (int, error)
	SessionID       string
	ProjectID       string
}

type OperatorIdentityMiddleware struct {
	cfg OperatorIdentityConfig
}

func NewOperatorIdentityMiddleware(cfg OperatorIdentityConfig) *OperatorIdentityMiddleware {
	if cfg.PeerCredentials == nil {
		cfg.PeerCredentials = defaultPeerCredentials
	}
	return &OperatorIdentityMiddleware{cfg: cfg}
}

// Authenticate verifies the peer UID of conn matches the configured
// ProcessUID. On match, emits a single EvtOperatorOverrideApplied audit
// row with payload {OperatorUID, OperatorReason, OverrideKind} and
// returns the authenticated OperatorIdentity.
//
// Failure modes (each surfaces a wrapped error and an empty
// OperatorIdentity, and emits NO audit event):
// - peer-cred lookup error: wrapped as "peer-cred lookup:..." (the
// underlying syscall error survives errors.Is)
// - UID mismatch: wrapped ErrOperatorIdentityMismatch with the
// observed UIDs in the message; errors.Is(err,
// ErrOperatorIdentityMismatch) is the load-bearing test
// - Append failure: wrapped as "append OperatorOverrideApplied:..."
//
// Failed-auth attempts are NOT logged via this audit channel — daemon
// access logs handle them separately to avoid masking real overrides
// under impostor noise (per design contract).
//
// ctx is caller-controlled. Peer-cred lookup is fast (one syscall, no
// I/O), and Append is bounded by the eventlog emitter (RawEmitter at
// ); cancellation propagates naturally. We do NOT use
// context.WithoutCancel here — F-2/F-3's cleanup paths use it for
// Resume on the rollback path because cleanup must outlive cancellation,
// but Authenticate has no cleanup obligation (no state was committed
// upstream).
func (m *OperatorIdentityMiddleware) Authenticate(ctx context.Context, conn net.Conn, kind OverrideKind, reason string) (OperatorIdentity, error) {
	uid, err := m.cfg.PeerCredentials(conn)
	if err != nil {
		return OperatorIdentity{}, fmt.Errorf("orchestrator: peer-cred lookup: %w", err)
	}
	if uid != m.cfg.ProcessUID {
		return OperatorIdentity{}, fmt.Errorf("orchestrator: uid=%d processUID=%d: %w", uid, m.cfg.ProcessUID, ErrOperatorIdentityMismatch)
	}
	if _, err := m.cfg.AppendEvents.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtOperatorOverrideApplied,
		SessionID: m.cfg.SessionID,
		ProjectID: m.cfg.ProjectID,
		Payload: map[string]any{
			"operator_uid":    uid,
			"operator_reason": reason,
			"override_kind":   string(kind),
		},
	}); err != nil {
		return OperatorIdentity{}, fmt.Errorf("orchestrator: append OperatorOverrideApplied: %w", err)
	}
	return OperatorIdentity{UID: uid, Reason: reason}, nil
}
