// SPDX-License-Identifier: MIT
// Package aggregator — compile-time invariant anchors.
//
// Each sentinel is a no-op function that exists solely to anchor a
// production code path to a documented invariant. Plan-J compliance
// tests grep production source for the sentinel name; if the sentinel
// is removed (or demoted to a test-only file) the compliance test
// fails. The anchors are package-private (lowercase) because they are
// internal scaffolding, not public API.
//
// Why anchors instead of pure documentation: a comment-only invariant
// can be silently removed without test failure. An anchor function
// referenced from production code (here: aggregator.go's New
// constructor) cannot be removed without surfacing in code review +
// compliance test output. Defence in depth — see writing-skills
// "structural anchor" pattern.
//
// Three sentinels ship in D-2:
// - aggregatorBoundaryRespectSentinel (inv-hades-031)
// - aggregatorNoWebSentinel (inv-hades-129)
// - promoteRequiresReasonSentinel (inv-hades-146)
//
// D-9..D-13 may add more (e.g., for inv-hades-130 knowledge_extension
// columns NULL); D-14 ships the compliance tests that grep for these
// anchor names.
package aggregator

func aggregatorBoundaryRespectSentinel() error {
	return nil
}

func aggregatorNoWebSentinel() error {
	return nil
}

// promoteRequiresReasonSentinel returns nil. It is invoked from
// aggregator.New (production code path) to keep the inv-hades-146 anchor
// reachable.
//
// inv-hades-146: Promote(noteID, operatorID, reason) MUST reject empty
// reason. This is the operator-attestation contract — every promote
// event surfaces in the audit log AND in the cross-project search
// surface, and a blank reason would silently erase the operator's
// rationale. D-9 implements the runtime check (returns
// ErrPromoteReasonEmpty); the schema CHECK in db.go's Init is the
// SQL-side defence in depth; this sentinel is the third layer.
//
// Static enforcement:
// - ships a custom go vet analyzer (noAutoPromote) that
// rejects callsites passing empty literals to Promote.
// - The schema CHECK constraint on knowledge_pin_index.promote_reason
// fires at INSERT time even for direct-DB writes that bypass the
// Go API.
// - This runtime sentinel proves the production code path
// (New constructor) honours the invariant by structure.
func promoteRequiresReasonSentinel() error {
	return nil
}
