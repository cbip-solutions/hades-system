// SPDX-License-Identifier: MIT
// Package spikes provides the spike registry + execution substrate
// A-4 (release-gate spike re-execution).
//
// Per amendment §2.4 D-4: 8 spikes verify Hermes substrate ABCs +
// hook contracts + MCP envelope shapes + renderer feasibility. Spike
// vigencia ≤14d (Q10=B Hermes evolution velocity); CATASTROPHIC severity
// blocks releases unconditionally (invariant).
//
// Spike harness files live under docs/spikes/spike_NN_*.go (build tag
// spikes; isolated from production binaries). Each harness exposes a
// Spike01...Spike08 factory returning a Result.
//
// Doctrine hard parts are where value lives — re-running spikes at
// release is the gate that earns release confidence; no defer.
package spikes
