//go:build chaos

// SPDX-License-Identifier: MIT

package network

import (
	"context"
	"fmt"
	"net"
	"time"
)

// EdgeInvariant is a per-edge robustness assertion. The function is
// invoked AFTER the toxic has been applied to the edge proxy and is
// responsible for asserting the daemon's documented robustness path
// engaged.
//
// Each implementation MUST be deterministic: given the same Scenario
// + Registry it MUST return the same nil / non-nil. Non-determinism
// breaks the chaos suite's ability to reproduce a failure from a
// matrix-replay log.
//
// Returns nil when the path engaged correctly, an error describing
// the breakage otherwise.
type EdgeInvariant func(ctx context.Context, reg *Registry, s Scenario) error

// edgeInvariants holds the per-edge × per-category assertion table.
// Lookup order on Run:
//
//	(1) (edge, category) — most specific
//	(2) ("*",  category) — category default
//	(3) panic — every (edge, category) MUST resolve at the registry-
//	    level (per init guard below)
//
// The "*" wildcard lets the category-default routine handle the
// 8-edge × 10-toxic baseline (80 scenarios), and per-edge overrides
// layer on edge-specific assertions (e.g. sidecar_bypass under CLOSE
// must trip the circuit-breaker; the wildcard would only check the
// dial-failure shape).
var edgeInvariants = map[string]map[Category]EdgeInvariant{
	"*": {
		CategoryClose:   assertCloseShape,
		CategoryDegrade: assertDegradeShape,
		CategoryCorrupt: assertCorruptShape,
	},
}

// assertCloseShape verifies the CLOSE-class baseline: the dial MUST
// fail because the proxy is down / resets / upstream-timed-out. A
// successful dial means the toxic did not engage at the wire level —
// a setup error, surfaced separately from an invariant breach so the
// operator can distinguish "test harness broken" from "daemon path
// broken".
func assertCloseShape(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("close-shape: unknown edge %q", s.Edge)
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("expected dial failure under %s; got success (toxic did not engage)", s.Toxic)
	}
	return nil
}

// assertDegradeShape verifies the DEGRADE-class baseline: the proxy
// stays up so the dial MUST succeed within tolerance (3s deadline,
// chosen to exceed the canonical 500ms latency + 1s slow_close + 5ms
// slicer-per-segment defaults so well-formed degrade scenarios always
// pass on a healthy daemon).
func assertDegradeShape(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("degrade-shape: unknown edge %q", s.Edge)
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err != nil {
		return fmt.Errorf("expected dial to succeed under %s; got %w (toxic over-applied)", s.Toxic, err)
	}
	_ = conn.Close()
	return nil
}

func assertCorruptShape(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("corrupt-shape: unknown edge %q", s.Edge)
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err != nil {
		return fmt.Errorf("expected dial to succeed under %s; got %w (toxic mis-classified as CLOSE?)", s.Toxic, err)
	}
	_ = conn.Close()
	return nil
}

// assertSidecarBypassClose is the per-edge override for
// sidecar_bypass + CategoryClose: tightens the close-shape assertion
// with a deadline appropriate for the bypass tier's documented
// circuit-breaker probe cadence (per Plan 2 phase G — circuit opens
// on first failure within ~150ms, half-open probe at 1s). The dial
// MUST fail strictly faster than the dispatcher's 5s tier-fallback
// budget so the dispatcher has time to fall through to the next
// tier without the test wall-clock blowing past CI budget.
func assertSidecarBypassClose(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("sidecar_bypass: unknown edge")
	}
	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("sidecar_bypass dial succeeded under %s; expected circuit-breaker trip", s.Toxic)
	}
	return nil
}

// assertCtldDegrade is the per-edge override for ctld + CategoryDegrade:
// the daemon control plane is on-host; degrade under latency MUST stay
// well under the dispatcher's 5s tier-fallback deadline, so we tighten
// the dial deadline to 2s. A 2s+ wait under canonical 500ms latency
// indicates the proxy is slower than the latency knob — surfaces a
// Toxiproxy-side regression that would otherwise mask real drift.
func assertCtldDegrade(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("ctld: unknown edge")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err != nil {
		return fmt.Errorf("ctld dial failed under %s; want success within 2s: %w", s.Toxic, err)
	}
	_ = conn.Close()
	return nil
}

// assertHermesPluginClose is the per-edge override for hermes_plugin +
// CategoryClose: the plugin-RPC boundary must not block the daemon
// indefinitely when the plugin goes down — the dial MUST fail well
// under the 5s plugin-RPC deadline so the daemon's plugin-degraded
// path engages.
func assertHermesPluginClose(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("hermes_plugin: unknown edge")
	}
	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("hermes_plugin dial succeeded under %s; expected plugin-down detection", s.Toxic)
	}
	return nil
}

func AssertEdgeInvariantV2(ctx context.Context, reg *Registry, s Scenario) error {
	cat := CategoryOf(s.Toxic)
	if cat == CategoryUnknown {
		return fmt.Errorf("unknown toxic category for %q", s.Toxic)
	}
	if perEdge, ok := edgeInvariants[s.Edge]; ok {
		if fn, ok := perEdge[cat]; ok {
			return fn(ctx, reg, s)
		}
	}
	fn, ok := edgeInvariants["*"][cat]
	if !ok {
		return fmt.Errorf("no invariant registered for category %s", cat)
	}
	return fn(ctx, reg, s)
}

func init() {

	edgeInvariants["sidecar_bypass"] = map[Category]EdgeInvariant{
		CategoryClose: assertSidecarBypassClose,
	}
	edgeInvariants["ctld"] = map[Category]EdgeInvariant{
		CategoryDegrade: assertCtldDegrade,
	}
	edgeInvariants["hermes_plugin"] = map[Category]EdgeInvariant{
		CategoryClose: assertHermesPluginClose,
	}
}
