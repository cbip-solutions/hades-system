// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// Darwin peer-credential lookup for the F-5 RequireOperatorIdentity
// middleware. Reads LOCAL_PEERCRED via golang.org/x/sys/unix.
// GetsockoptXucred — the BSD-style xucred struct carrying the peer's
// UID + group set at connect time. We use the x/sys wrapper rather than
// the stdlib `syscall` package because GetsockoptXucred is the
// cross-platform name; it is present on Darwin in golang.org/x/sys/unix
// and produces the same kernel-trusted result.
//
// This file is excluded from coverage measurement: the syscall path
// requires a real *net.UnixConn with a kernel-bound fd; unit tests
// inject the PeerCredentials function via OperatorIdentityConfig
// instead. Cross-compile is exercised by `GOOS=darwin go build./...`
// in the F-5 gate set; live behaviour lands in integration.

// go:build darwin
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
		xucred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil {
			inner = e
			return
		}
		uid = int(xucred.Uid)
	})
	if err != nil {
		return 0, err
	}
	return uid, inner
}
