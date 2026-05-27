// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// Package daemon — server_conn_context.go (release fix-cycle
// re-review pre-existing-gap fix).
//
// connContextWithPeerCred is the http.Server.ConnContext callback wired
// in server.go:Start(). It runs ONCE per accepted connection (not once
// per request), extracts the peer-cred for UDS connections via
// auth.ExtractPeerCred, and injects the result into the connection's
// context so every HTTP request served over that connection sees the
// cred via auth.PeerCredFromContext(r.Context()).
//
// # Contract
//
// - UDS connection (*net.UnixConn) → ExtractPeerCred + WithPeerCred.
// On extraction failure, the context proceeds untouched so
// downstream handlers detect HasSet=false and reject 401
// (fail-closed).
//
// - Non-UDS connection (TCP) → return ctx unchanged. The TCP path
// is gated by the loopback predicate in sessionAuthenticated /
// auth.PeerCredOnly, not by peer-cred.
//
// # Lifecycle
//
// http.Server invokes ConnContext after net.Listener.Accept returns
// the new net.Conn and before passing it to the handler goroutine.
// The function MUST NOT block — every accept is paid by the latency
// of this hook. ExtractPeerCred is a single getsockopt syscall on
// darwin + linux (μs-scale); the non-UDS path is a type-assert.
//
// # Why a separate file
//
// Grep-trivially identifiable (mirrors the server_phase_g_defaults.go +
// server_session_doctrine.go isolation pattern). Tests live in
// server_conn_context_test.go.
package daemon

import (
	"context"
	"net"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

func connContextWithPeerCred(ctx context.Context, c net.Conn) context.Context {
	uconn, ok := c.(*net.UnixConn)
	if !ok {

		return ctx
	}
	pc, err := auth.ExtractPeerCred(uconn)
	if err != nil {
		// Fail-closed: leave ctx untouched. Downstream
		// PeerCredFromContext returns HasSet=false and the request
		// is rejected 401. We intentionally do NOT log here — a
		// successful daemon does this on every accept and the log
		// noise would drown legitimate failures.
		return ctx
	}
	return auth.WithPeerCred(ctx, pc)
}
