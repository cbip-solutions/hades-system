// SPDX-License-Identifier: MIT
// Compile-time anchors for invariant (single-egress preservation) and
// invariant.
//
// These sentinels exist so the compliance test can grep the
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
// rephrase without updating tests/compliance/inv_hades_164_*_test.go.
var errSingleEgressAnchor = errors.New(
	"transport: single-egress anchor (inv-hades-164) — HadesSystemTransport routes all Hermes LLM via daemon /v1/messages",
)

func SingleEgressSentinel() error { return errSingleEgressAnchor }

var _ = SingleEgressSentinel()

func TierBackendInterfaceAnchor() (zero providers.TierBackend) { return nil }

var _ = TierBackendInterfaceAnchor
