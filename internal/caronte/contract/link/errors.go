// SPDX-License-Identifier: MIT
// Package link is release's per-workspace contract linker: for every api_calls
// row in scope, it tries the precision-ordered tier chain (artifact → spec →
// static → fuzzy → unresolved) and either persists a contract_links row
// (Confidence + LinkMethod per master C-5) through federation
// LinkStore (capa-firewall gated; invariant) + emits a release Tessera
// audit row via federation.AuditEmitter (invariant), or records
// an `unresolved` row (per caronte.yaml unresolved_policy; doctrine-default
// surface; invariant).
//
// Boundary (invariant mirror invariant): this package NEVER imports
// internal/store; it imports only internal/caronte/store ( + Plan
// 20 ), internal/caronte/contract/extract, and
// internal/caronte/contract/yaml.
package link

import "errors"

var ErrCGODisabled = errors.New("caronte/link: linker requires CGO_ENABLED=1; degraded_mode active")

var ErrNoManifestEntry = errors.New("caronte/link: no manifest entry for base_url_ref")

var ErrAmbiguousResolution = errors.New("caronte/link: ambiguous base_url_ref resolution (multiple manifest entries match)")

var ErrConfidenceTierDowngrade = errors.New("caronte/link: confidence tier inconsistent with link_method (forged-row guard)")
