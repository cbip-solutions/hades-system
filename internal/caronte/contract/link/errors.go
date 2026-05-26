// SPDX-License-Identifier: MIT
// Package link is Plan 20's per-workspace contract linker: for every api_calls
// row in scope, it tries the precision-ordered tier chain (artifact → spec →
// static → fuzzy → unresolved) and either persists a contract_links row
// (Confidence + LinkMethod per master C-5) through Phase A's federation
// LinkStore (capa-firewall gated; inv-zen-264) + emits a Plan 14 Tessera
// audit row via Phase A's federation.AuditEmitter (inv-zen-269), or records
// an `unresolved` row (per caronte.yaml unresolved_policy; doctrine-default
// surface; inv-zen-265).
//
// Boundary (inv-zen-031 mirror inv-zen-271): this package NEVER imports
// internal/store; it imports only internal/caronte/store (Phase 19 + Plan
// 20 Phase A/B), internal/caronte/contract/extract (Phase C/D/E), and
// internal/caronte/contract/yaml (Plan 20 Phase F yaml subpkg).
package link

import "errors"

var ErrCGODisabled = errors.New("caronte/link: linker requires CGO_ENABLED=1; degraded_mode active")

var ErrNoManifestEntry = errors.New("caronte/link: no manifest entry for base_url_ref")

var ErrAmbiguousResolution = errors.New("caronte/link: ambiguous base_url_ref resolution (multiple manifest entries match)")

var ErrConfidenceTierDowngrade = errors.New("caronte/link: confidence tier inconsistent with link_method (forged-row guard)")
