// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"os"
	"time"
)

// This file exposes a narrow public surface intended ONLY for cross-package
// compliance tests in tests/compliance/inv_zen_118_*.go. Production code
// MUST NOT import these symbols (the names carry the "ForCompliance" /
// "Compliance" suffix specifically so a code-search at review time
// surfaces any accidental production use). The boundary is documented in
// the package doc-comment of inv_zen_118_*_test.go and reviewed by the
// Stage 2 reviewer cross-phase categories (master plan §"Stage 2 reviewer
// cross-phase categories").
//
// Why this file exists (max-scope doctrine, not a stub):
//
//   - inv-zen-118 lives at THREE layers (resurrect-strategy directive,
//     pre-tar strip, post-tar scan). All three must be exercised by a
//     compliance test that lives OUTSIDE internal/tmuxlife/ so an external
//     reviewer can read it without doing package archaeology. The post-tar
//     scan and stripScratchLines are package-internal helpers; therefore
//     a sanctioned test-aux constructor is the only way to drive Save
//     end-to-end with controlled input from a sibling package.
//
//   - Adding the helpers as `_test.go` would NOT work because Go's _test.go
//     files are package-internal-only by design. The compliance suite
//     consumes tmuxlife as an external Go package and needs real exported
//     symbols.
//
//   - The alternative (refactor scratchInPayload + stripScratchLines to
//     exported functions) would broaden the production surface for one
//     test consumer; this helper keeps the production surface stable while
//     giving the compliance suite the seam it needs (max-scope doctrine
//     "build the final shape": narrow exported interface + adapter is the
//     final shape).

type ResurrectExecForCompliance interface {
	Save(ctx context.Context, sessionName string) ([]byte, error)

	// Restore extracts the tarball into the resurrect dir and invokes the
	// tmux-resurrect restore script. Compliance tests for Save do not need
	// to exercise Restore; the method exists for interface-completeness so
	// the adapter satisfies the unexported resurrectExec contract.
	Restore(ctx context.Context, sessionName string, tarball []byte) error
}

type complianceResurrectAdapter struct {
	delegate ResurrectExecForCompliance
}

func (a complianceResurrectAdapter) save(ctx context.Context, sessionName string) ([]byte, error) {
	return a.delegate.Save(ctx, sessionName)
}

func (a complianceResurrectAdapter) restore(ctx context.Context, sessionName string, tarball []byte) error {
	return a.delegate.Restore(ctx, sessionName, tarball)
}

// NewManagerForCompliance constructs a Manager wired to the given store,
// snapshot directory, and ResurrectExecForCompliance fake. Used EXCLUSIVELY
// by tests/compliance/inv_zen_118_*_test.go (the inv-zen-118 three-layer
// witness suite); production callers MUST use New(store).
//
// The deterministic clock returns 2026-05-01T14:30:45Z so SnapshotPath
// assertions stay stable across test runs. A test that needs a different
// timestamp can override after construction, but the compliance suite has
// no such requirement (the path is asserted by suffix, not exact match).
//
// Panics on nil store (mirrors New) and on nil rex (programmer error must
// surface immediately, not on first Save call). The panic-on-nil contract
// matches the zero-defer-error doctrine: a nil rex would silently produce
// nil-pointer dereferences deep in Save, obscuring the cause.
func NewManagerForCompliance(store SessionStore, snapshotDir string, rex ResurrectExecForCompliance) *Manager {
	if store == nil {
		panic("tmuxlife.NewManagerForCompliance: store is nil")
	}
	if rex == nil {
		panic("tmuxlife.NewManagerForCompliance: rex is nil")
	}
	m := New(store)
	m.resurrect = complianceResurrectAdapter{delegate: rex}
	m.snapshotDir = snapshotDir

	fixed := time.Date(2026, 5, 1, 14, 30, 45, 0, time.UTC)
	m.now = func() time.Time { return fixed }

	m.statFn = os.Stat
	return m
}
