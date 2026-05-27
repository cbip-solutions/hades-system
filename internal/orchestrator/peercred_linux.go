// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// Linux peer-credential lookup for the F-5 RequireOperatorIdentity
// middleware. Reads SO_PEERCRED via golang.org/x/sys/unix.
// GetsockoptUcred — the kernel-trusted UID/GID/PID triple of the peer
// process at connect time. On Linux, SO_PEERCRED is the canonical way
// to authenticate a Unix-domain-socket peer; no caller-supplied UID is
// trusted.
//
// This file is excluded from coverage measurement: the syscall path
// requires a real *net.UnixConn with a kernel-bound fd; unit tests
// inject the PeerCredentials function via OperatorIdentityConfig
// instead. Cross-compile is exercised by `GOOS=linux go build./...`
// in the F-5 gate set; live behaviour lands in integration.

// go:build linux
package orchestrator

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func defaultPeerCredentials(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("orchestrator: peer-cred requires Unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid int
	var inner error
	err = raw.Control(func(fd uintptr) {
		ucred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			inner = e
			return
		}
		uid = int(ucred.Uid)
	})
	if err != nil {
		return 0, err
	}
	return uid, inner
}
