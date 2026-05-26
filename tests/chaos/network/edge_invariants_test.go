//go:build chaos

// Toxiproxy-driven chaos package (inv-zen-305).
//
// edge_invariants_test.go pins the per-edge × per-category invariant
// table semantics: lookup precedence ((edge, category) over
// ("*", category)), unknown-toxic rejection, and the registered
// per-edge override coverage. The tests do NOT need a running
// Toxiproxy daemon — they synthesise a Registry pointing at a
// closed/open localhost port to drive the close/degrade/corrupt
// shape branches via the OS-level dial semantics (closed port ≈
// CLOSE-shape; open port ≈ DEGRADE/CORRUPT-shape).

package network

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func pickClosedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func pickOpenPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return l.Addr().String()
}

func TestAssertCloseShapeOnClosedPort(t *testing.T) {
	addr := pickClosedPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := assertCloseShape(ctx, reg, Scenario{Toxic: ToxicDown, Edge: "e"})
	if err != nil {
		t.Errorf("assertCloseShape on closed port: got err=%v, want nil", err)
	}
}

func TestAssertCloseShapeOnOpenPortReportsBreach(t *testing.T) {
	addr := pickOpenPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := assertCloseShape(ctx, reg, Scenario{Toxic: ToxicDown, Edge: "e"})
	if err == nil {
		t.Error("assertCloseShape on open port: got nil, want error (toxic did not engage)")
	}
}

func TestAssertDegradeShapeOnOpenPort(t *testing.T) {
	addr := pickOpenPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := assertDegradeShape(ctx, reg, Scenario{Toxic: ToxicLatency, Edge: "e"})
	if err != nil {
		t.Errorf("assertDegradeShape on open port: got err=%v, want nil", err)
	}
}

func TestAssertDegradeShapeOnClosedPortReportsBreach(t *testing.T) {
	addr := pickClosedPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := assertDegradeShape(ctx, reg, Scenario{Toxic: ToxicLatency, Edge: "e"})
	if err == nil {
		t.Error("assertDegradeShape on closed port: got nil, want error (toxic over-applied)")
	}
}

func TestAssertCorruptShapeOnOpenPort(t *testing.T) {
	addr := pickOpenPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := assertCorruptShape(ctx, reg, Scenario{Toxic: ToxicLimitData, Edge: "e"})
	if err != nil {
		t.Errorf("assertCorruptShape on open port: got err=%v, want nil", err)
	}
}

func TestAssertEdgeInvariantV2WildcardFallback(t *testing.T) {
	addr := pickClosedPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"unregistered_edge": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := AssertEdgeInvariantV2(ctx, reg, Scenario{Toxic: ToxicDown, Edge: "unregistered_edge"})
	if err != nil {
		t.Errorf("V2 fallback to wildcard: got err=%v, want nil", err)
	}
}

func TestAssertEdgeInvariantV2OverrideTakesPrecedence(t *testing.T) {

	saved := edgeInvariants["sidecar_bypass"]
	defer func() { edgeInvariants["sidecar_bypass"] = saved }()

	called := false
	edgeInvariants["sidecar_bypass"] = map[Category]EdgeInvariant{
		CategoryClose: func(_ context.Context, _ *Registry, _ Scenario) error {
			called = true
			return nil
		},
	}
	addr := pickClosedPort(t)
	reg := &Registry{Edges: map[string]EdgeConfig{"sidecar_bypass": {Listen: addr}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := AssertEdgeInvariantV2(ctx, reg, Scenario{Toxic: ToxicDown, Edge: "sidecar_bypass"}); err != nil {
		t.Fatalf("V2 override: got err=%v, want nil", err)
	}
	if !called {
		t.Error("override did not run; wildcard fallback fired instead")
	}
}

func TestAssertEdgeInvariantV2UnknownToxicReturnsError(t *testing.T) {
	reg := &Registry{Edges: map[string]EdgeConfig{"e": {Listen: "127.0.0.1:1"}}}
	err := AssertEdgeInvariantV2(context.Background(), reg, Scenario{Toxic: "synthetic_future", Edge: "e"})
	if err == nil {
		t.Fatal("V2 with unknown toxic: got nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown toxic category") {
		t.Errorf("err = %v, want 'unknown toxic category' message", err)
	}
}

func TestRegisteredPerEdgeOverridesCoverDocumentedEdges(t *testing.T) {
	cases := []struct {
		edge string
		cat  Category
	}{
		{"sidecar_bypass", CategoryClose},
		{"ctld", CategoryDegrade},
		{"hermes_plugin", CategoryClose},
	}
	for _, c := range cases {
		t.Run(c.edge+"/"+c.cat.String(), func(t *testing.T) {
			perEdge, ok := edgeInvariants[c.edge]
			if !ok {
				t.Fatalf("no per-edge table for %s", c.edge)
			}
			if _, ok := perEdge[c.cat]; !ok {
				t.Errorf("no %s override for %s", c.cat, c.edge)
			}
		})
	}
}

// TestEdgeInvariantsWildcardCoversAllCategories pins the wildcard
// fallback completeness: every Category MUST have a "*" entry so the
// V2 dispatcher cannot fall off the end of the table for an
// unregistered edge.
func TestEdgeInvariantsWildcardCoversAllCategories(t *testing.T) {
	star, ok := edgeInvariants["*"]
	if !ok {
		t.Fatal("no wildcard entry in edgeInvariants table")
	}
	for _, cat := range AllCategories() {
		if _, ok := star[cat]; !ok {
			t.Errorf("wildcard entry missing category %s", cat)
		}
	}
}
