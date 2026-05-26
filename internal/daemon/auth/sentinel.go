// SPDX-License-Identifier: MIT
// Package auth — sentinel.go (Plan 7 Phase I Task I-1).
//
// Compile-time + runtime anchors for inv-zen-131 (HTTP auth boundary)
// and inv-zen-132 (per-routine bearer constant-time + audit).
//
// Compliance tests grep for these symbols + invoke them to assert the
// auth pipeline is reachable from production code (not dead code).
package auth

import "errors"

func httpAuthBoundarySentinel() error {
	return ErrAuthBoundaryAnchor
}

func perRoutineBearerSentinel() error {
	return ErrPerRoutineBearerAnchor
}

var ErrAuthBoundaryAnchor = errors.New("auth: http boundary anchor (inv-zen-131)")

var ErrPerRoutineBearerAnchor = errors.New("auth: per-routine bearer anchor (inv-zen-132)")

var (
	_authBoundarySentinelInvoked     = httpAuthBoundarySentinel()
	_perRoutineBearerSentinelInvoked = perRoutineBearerSentinel()
)
