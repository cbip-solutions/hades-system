// SPDX-License-Identifier: MIT
// Package auth — unix_peer.go.
//
// PeerCred type + context propagation + PeerCredOnly middleware.
// Per-OS extraction lives in unix_peer_{darwin,linux,other}.go behind
// build tags.
package auth

import (
	"context"
	"net"
	"net/http"
)

type PeerCred struct {
	UID    uint32
	GID    uint32
	HasSet bool
}

type peerCredCtxKey struct{}

func WithPeerCred(ctx context.Context, pc PeerCred) context.Context {
	return context.WithValue(ctx, peerCredCtxKey{}, pc)
}

func PeerCredFromContext(ctx context.Context) PeerCred {
	pc, ok := ctx.Value(peerCredCtxKey{}).(PeerCred)
	if !ok {
		return PeerCred{}
	}
	return pc
}

func IsLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {

		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// PeerCredOnly is the middleware enforcing inv-hades-131: every
// /v1/* route (except the two bearer-only endpoints) MUST require
// either Unix-socket peer-cred OR loopback TCP.
//
// Decision tree:
//
// if r.RemoteAddr is loopback (TCP) → accept (operator-explicit
// --http 127.0.0.1:<port>).
// if r.RemoteAddr is non-loopback TCP → reject 401.
// if r.RemoteAddr is "@" or empty (UDS) → require non-zero PeerCred
// (server.go injects cred at
// connection-time so empty here
// means peer-cred extraction
// failed → reject 401).
//
// The connection-time injection is wired in server.go.Start() via
// http.Server.ConnContext.
func PeerCredOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.RemoteAddr != "" && r.RemoteAddr != "@" {
			if IsLoopbackAddr(r.RemoteAddr) {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "unauthorized: non-loopback TCP", http.StatusUnauthorized)
			return
		}

		pc := PeerCredFromContext(r.Context())
		if !pc.HasSet {
			http.Error(w, "unauthorized: no peer cred", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
