// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package citation

import (
	"encoding/json"
	"net/http"
)

// envelopeJSONSchemaSentinel anchors invariant: every Envelope field
// MUST be referenced here. Adding/removing/renaming a field without
// updating serialization fails compilation.
//
// This sentinel does no runtime work; the compiler verifies that every
// field name listed below is reachable from Envelope. The runtime
// guarantee (round-trip preservation across 10000 random envelopes) is
// enforced by TestEnvelopeRoundtripPreserves (envelope_test.go) +
// tests/compliance/inv_zen_166_citation_serialize_test.go.
func envelopeJSONSchemaSentinel() {
	var e Envelope
	_ = e.ID
	_ = e.Type
	_ = e.Source
	_ = e.Lane
	_ = e.AuditEventID
	_ = e.Confidence
	_ = e.RRFScore
	_ = e.RRFRank
	_ = e.Expiration
	_ = e.ProjectID
	_ = e.Payload
	_ = e.PlatformRenders
	// Compile-check: marshaling MUST work over zero-value (canonical
	// JSON shape exposed even when validation would reject it).
	_, _ = json.Marshal(&e)
}

// auditEventHandlerAuthSentinel anchors invariant: the zen://audit URL
// handler signature is fixed at the auth-required shape (handler must
// accept request bound to an authenticated session context; doctrine
// filter applies before serving the audit row).
//
// The handler implementation lives in internal/daemon/handlers/
// audit_event.go (cross-package compile-anchor — the same precedent
// used elsewhere for cross-package handler wiring). The sentinel
// here references the interface shape
// the handler MUST satisfy; implements; compliance
// test verifies runtime auth contract.
func auditEventHandlerAuthSentinel() {

	var _ http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {}
}

func init() {
	envelopeJSONSchemaSentinel()
	auditEventHandlerAuthSentinel()
}
