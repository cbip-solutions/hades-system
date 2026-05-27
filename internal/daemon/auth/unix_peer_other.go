// go:build !darwin && !linux

// SPDX-License-Identifier: MIT

package auth

import "net"

func ExtractPeerCred(_ net.Conn) (PeerCred, error) {
	return PeerCred{}, ErrPeerCredUnsupported
}
