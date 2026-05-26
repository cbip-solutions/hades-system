// SPDX-License-Identifier: MIT
// Compile-time anchors for inv-zen-164 (single-egress preservation) and
// inv-zen-088 (Plan 3 dispatcher remains the single LLM chokepoint).
//
// These sentinels exist so the compliance test (Phase F) can grep the
// package for the canonical anchor lines and prove the wiring is in place.
// Removing any of them breaks the compliance test build — that is the
// invariant.
package transport

import (
	"errors"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

// errSingleEgressAnchor is the sentinel error returned by SingleEgressSentinel.
// Its message is stable and grep-targeted by the compliance test; do not
// rephrase without updating tests/compliance/inv_zen_164_*_test.go.
var errSingleEgressAnchor = errors.New(
	"transport: single-egress anchor (inv-zen-164) — ZenSwarmTransport routes all Hermes LLM via daemon /v1/messages",
)

func SingleEgressSentinel() error { return errSingleEgressAnchor }

var _ = SingleEgressSentinel()

func TierBackendInterfaceAnchor() (zero providers.TierBackend) { return nil }

var _ = TierBackendInterfaceAnchor
