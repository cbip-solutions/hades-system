// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// Fallback peer-credential lookup for unsupported platforms (anything
// that is neither linux nor darwin: e.g. windows, freebsd, openbsd,
// netbsd, dragonfly, solaris). The middleware's peer-cred contract
// requires kernel-trusted UID extraction from a Unix-domain-socket
// peer; on platforms where the canonical syscall is unavailable we
// fail closed rather than trust caller-supplied data.
//
// This file lets `go build./...` succeed on any GOOS without forcing
// a //go:build linux || darwin guard at every consumer of the
// orchestrator package. daemon wiring SHOULD refuse to start
// on a platform where peer-cred is not supported; the error returned
// here surfaces that decision through the normal Authenticate path.

// go:build !linux && !darwin
package orchestrator

import (
	"errors"
	"net"
)

func defaultPeerCredentials(_ net.Conn) (int, error) {
	return 0, errors.New("orchestrator: peer-cred not supported on this platform")
}
