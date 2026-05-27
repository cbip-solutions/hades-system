// Package compliance — invariant: zen day brief output enforces a
// 7-item HARD cap. Defense-in-depth: a compile-time anchor (sentinel
// error referencing "invariant" + the literal MaxBriefItems = 7
// constant) plus a runtime panic in zenday.Render() when the slice
// length exceeds the cap.
//
// Spec §1 Q14 B + §7.2 invariant wording:
//
// "zen day brief output enforces 7-item hard cap. Compile-check via
// _ = zenDayCap7Sentinel() anchor + const MaxBriefItems = 7
// exported constant; runtime via Render() asserts
// len(doc.Items) <= MaxBriefItems BEFORE writing markdown body,
// panics otherwise (defense-in-depth Layer 2 per spec §7.3)."
//
// In-package coverage in internal/zenday/render_test.go pins the panic
// message format on n=8 (just-over-cap); this cross-package boundary
// witness sweeps adversarial counts {8, 50, 500, 7777} so any future
// refactor of Render — e.g. swapping the >cap branch to an early return,
// changing the panic message, or accidentally collapsing the cap check
// to len(items) > 6 — gets caught at the public surface.
//
// Coverage matrix:
//
// (a) Adversarial item counts {8, 50, 500, 7777} — Render MUST panic
// for every count > MaxBriefItems; the panic value MUST contain
// "invariant" so observability tooling (and the operator's logs)
// can route on the wire-stable substring per spec §7.3.
// (b) MaxBriefItems pinned to 7 — the exported constant is a
// user-observable contract surface and must NOT drift; pinning
// catches a stealth narrow-or-widen of the cap, which would
// silently change brief output without a compliance failure.
// (c) Sentinel error string contains both "invariant" and
// "MaxBriefItems = 7" — the verify-invariants grep target uses
// this string as the static-anchor witness; collapsing or
// rewording the sentinel breaks the cross-language audit trail.
// (d) Compile-time anchor reachable — exposing the sentinel via
// zenday.Cap7SentinelForTest() proves the symbol is not
// dead-code-stripped and the build retains the anchor across
// cross-package boundaries. Lipo-stripped binaries that drop the
// sentinel would fail this assertion at build time.
// (e) Boundary case n=7 (at-cap, valid) — Render MUST NOT panic on
// exactly MaxBriefItems items; closes the cap-off-by-one loop.
//
// Boundary: this test imports only internal/zenday +
// stdlib. internal/zenday is the load-bearing surface; the
// dispatcheradapter / store layers are not touched (Render is pure).
//
// Inv-zen-126 contract.
package compliance

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

var _ = zenDayCap7AnchorReference()

func zenDayCap7AnchorReference() error {
	return zenday.ErrZenDayCap7Anchor
}

// TestInvZen126_RenderPanicsAboveCap is the adversarial sweep across
// counts above MaxBriefItems = 7. For each count, an all-rank-1
// items slice (legal sort order; the only failing invariant is the cap)
// is constructed; Render MUST panic with a message containing the
// "invariant" substring so observability can route on it. The cases
// span "just-over-cap" (8), "tens" (50), "hundreds" (500), and "well
// past any plausible volume" (7777) — proving the runtime guard does
// not degrade as item count grows.
func TestInvZen126_RenderPanicsAboveCap(t *testing.T) {
	cases := []int{8, 50, 500, 7777}
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, n := range cases {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			items := make([]zenday.BriefItem, n)
			for i := range items {
				items[i] = zenday.BriefItem{
					Rank:      zenday.RankOperatorGate,
					Message:   fmt.Sprintf("item %d", i),
					CreatedAt: base.Add(-time.Duration(i) * time.Second),
				}
			}
			doc := zenday.BriefDoc{
				Date:  base,
				Type:  zenday.BriefTypeMorning,
				Items: items,
			}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("inv-zen-126 violation NOT caught for n=%d (Render returned without panic)", n)
				}
				msg := fmt.Sprintf("%v", r)
				if !strings.Contains(msg, "inv-zen-126") {
					t.Errorf("inv-zen-126 panic msg = %q, want containing %q", msg, "inv-zen-126")
				}
				if !strings.Contains(msg, "MaxBriefItems") {
					t.Errorf("inv-zen-126 panic msg = %q, want containing %q", msg, "MaxBriefItems")
				}
			}()
			_ = zenday.Render(doc)
		})
	}
}

func TestInvZen126_MaxBriefItemsConstantPinned(t *testing.T) {
	if zenday.MaxBriefItems != 7 {
		t.Fatalf("inv-zen-126: MaxBriefItems = %d, want 7 (frozen by spec §1 Q14 B)", zenday.MaxBriefItems)
	}
}

func TestInvZen126_SentinelErrorWording(t *testing.T) {
	err := zenday.Cap7SentinelForTest()
	if err == nil {
		t.Fatal("inv-zen-126: zenday.Cap7SentinelForTest() returned nil; expected sentinel anchor")
	}
	msg := err.Error()
	if !strings.Contains(msg, "inv-zen-126") {
		t.Errorf("inv-zen-126: sentinel msg = %q, want containing %q", msg, "inv-zen-126")
	}
	if !strings.Contains(msg, "MaxBriefItems = 7") {
		t.Errorf("inv-zen-126: sentinel msg = %q, want containing %q", msg, "MaxBriefItems = 7")
	}

	if !errors.Is(err, zenday.ErrZenDayCap7Anchor) {
		t.Errorf("inv-zen-126: Cap7SentinelForTest() != ErrZenDayCap7Anchor (anchor identity drift)")
	}
}

// TestInvZen126_AtCapDoesNotPanic closes the off-by-one boundary: a
// BriefDoc with exactly MaxBriefItems items in canonical sort order
// (all rank 1, time-descending) MUST render without panic — the cap is
// inclusive. A future refactor that mistakenly tightens the predicate
// to `len(items) >= MaxBriefItems` would corrupt every legitimate
// at-cap brief; this test is the negative control.
func TestInvZen126_AtCapDoesNotPanic(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	items := make([]zenday.BriefItem, zenday.MaxBriefItems)
	for i := range items {

		items[i] = zenday.BriefItem{
			Rank:      zenday.RankOperatorGate,
			Message:   fmt.Sprintf("item %d", i),
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
	}
	doc := zenday.BriefDoc{
		Date:  base,
		Type:  zenday.BriefTypeMorning,
		Items: items,
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("inv-zen-126: at-cap (n=%d) Render must NOT panic; got %v", zenday.MaxBriefItems, r)
		}
	}()
	out := zenday.Render(doc)
	if out == "" {
		t.Errorf("inv-zen-126: at-cap Render returned empty markdown; expected non-empty body")
	}
}
