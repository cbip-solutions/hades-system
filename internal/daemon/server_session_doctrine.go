// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package daemon

import (
	"net/http"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

// sessionDoctrine returns the active doctrine NAME for a request,
// authenticated via peer-cred (UDS) or loopback (TCP). Returns "" on
// auth failure → handler renders 401.
//
// Authentication decision tree (mirrors auth.PeerCredOnly):
//
// r.RemoteAddr is non-loopback TCP → "" (reject)
// r.RemoteAddr is loopback TCP → trust + read active
// r.RemoteAddr is "@" or empty (UDS) → require peer-cred set
// then read active
//
// Doctrine resolution: s.DoctrineActive("") returns the daemon-wide
// active doctrine name from active.Active() via
// doctrineNameForSchema. On registry init-order failure (daemon
// startup window), returns "" → handler renders 401 (degraded mode;
// fail-closed — better to reject than to silently expose a wrong
// doctrine).
//
// invariant corollary: doctrine MUST NOT be derived from a
// client-controlled signal. The prior implementation read
// X-Zen-Doctrine header; this is removed entirely from the security
// path. Tests inject doctrine via the active.Accessor (via
// active.SetUserDefault) or pass a synthetic sessionDoctrine
// function to the handler (the handlers.SessionDoctrineFunc seam
// is preserved precisely so tests can inject without going through
// the production auth path).
func (s *Server) sessionDoctrine(r *http.Request) string {
	if !s.sessionAuthenticated(r) {
		return ""
	}
	name, _, _, err := s.DoctrineActive("")
	if err != nil {

		return ""
	}
	return name
}

// sessionAuthenticated returns true iff the request comes from a
// trusted local source: either a UDS connection carrying a valid
// peer-cred, or a loopback TCP connection (the operator-explicit
// `--http 127.0.0.1:<port>` path). Mirrors auth.PeerCredOnly's
// decision tree but returns a bool instead of writing a 401 — the
// caller (sessionDoctrine) reports auth failure via empty doctrine
// string, which audit_event.AuditEventByIDHandler renders as 401.
//
// Loopback TCP is treated as authenticated-by-deployment-shape: the
// operator deliberately exposed the daemon on a local interface and
// is responsible for not running on a shared host. Same posture as
// PeerCredOnly.
//
// Non-loopback TCP MUST be rejected regardless of any HTTP-layer
// headers — exposing UDS-mode endpoints over a public interface is
// always wrong.
func (s *Server) sessionAuthenticated(r *http.Request) bool {
	if r.RemoteAddr != "" && r.RemoteAddr != "@" {

		return isLoopbackRequest(r)
	}

	pc := auth.PeerCredFromContext(r.Context())
	return pc.HasSet
}

func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return auth.IsLoopbackAddr(r.RemoteAddr)
}
