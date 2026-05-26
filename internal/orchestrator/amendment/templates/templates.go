// SPDX-License-Identifier: MIT
// Package templates exposes the embedded Markdown / text templates used
// by the amendment package. Keeping the //go:embed in its own subpackage
// avoids the proposer / drafter Go files declaring //go:embed directives
// alongside business logic and lets the template be unit-tested in
// isolation.
package templates

import _ "embed"

// ADRProposal is the Plan 5 doctrine-amendment ADR markdown template
// (Go text/template). Loaded once at init via //go:embed; consumed by
// amendment.TemplateDrafter (K-2).
//
//go:embed adr_proposal.md.tmpl
var ADRProposal string
