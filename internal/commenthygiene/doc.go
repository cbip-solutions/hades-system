// SPDX-License-Identifier: MIT
// Package commenthygiene implements the classifier rubric +
// scanner for use by cmd/verify-comment-hygiene and CI release-gates.
//
// The rubric is:
// - KEEP load-bearing WHY (invariants, hidden constraints, workarounds
// with upstream-bug link, subtle race conditions, memory semantics)
// - DELETE redundant WHAT (restate code; task-context rot; release-marker rot)
// - REWRITE vague-to-specific (or DELETE if can't specify)
//
// Classifier exposes Classify(commentLine string) ClassifierDecision; scanner
// walks file trees applying the classifier and emitting per-line reports.
package commenthygiene
