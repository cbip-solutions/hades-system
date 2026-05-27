// go:build linux

// SPDX-License-Identifier: MIT

package auth

import (
	"fmt"
	"net"

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
		uid uint32
		gid uint32
	)
	var ctlErr error
	cerr := raw.Control(func(fd uintptr) {
		ucred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			ctlErr = e
			return
		}
		uid = ucred.Uid
		gid = ucred.Gid
	})
	if cerr != nil {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: control: %w", cerr)
	}
	if ctlErr != nil {
		return PeerCred{}, fmt.Errorf("auth.ExtractPeerCred: getsockopt: %w", ctlErr)
	}
	return PeerCred{UID: uid, GID: gid, HasSet: true}, nil
}
