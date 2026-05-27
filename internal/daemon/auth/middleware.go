// SPDX-License-Identifier: MIT
// Package auth — middleware.go.
//
// Composition helpers for the auth middleware chain. The actual
// constructors live alongside their primitives:
//
// - PeerCredOnly → unix_peer.go
// - RequireDaemonBearer → bearer.go
// - RequirePerRoutineBearer → bearer.go
//
// This file declares Chain — a small helper that composes a list of
// middlewares left-to-right (outermost first) so server.go reads
// top-down: Chain(PeerCredOnly, RequireDaemonBearer(b))(handler).
package auth

import "net/http"

type Middleware func(http.Handler) http.Handler

func Chain(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {

		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}
