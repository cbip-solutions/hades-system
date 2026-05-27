// go:build darwin

// SPDX-License-Identifier: MIT

package auth

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func ExtractPeerCred(c net.Conn) (PeerCred, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: not a UnixConn (got %T)", c)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: syscall conn: %w", err)
	}
	var (
		x      *unix.Xucred
		ctlErr error
	)
	cerr := raw.Control(func(fd uintptr) {
		x, ctlErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	})
	if cerr != nil {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: control: %w", cerr)
	}
	if ctlErr != nil {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: getsockopt: %w", ctlErr)
	}
	return xucredToPeerCred(x), nil
}

func xucredToPeerCred(x *unix.Xucred) PeerCred {
	pc := PeerCred{UID: x.Uid, HasSet: true}

	if x.Ngroups > 0 {
		pc.GID = x.Groups[0]
	} else {
		pc.GID = uint32(os.Getgid())
	}
	return pc
}
