// SPDX-License-Identifier: MIT
// internal/research/ecosystem/vault_note.go — narrow Plan 7 vault.db notes
// row shape needed by Phase B-11 ingester.ProcessVaultNote.
//
// Stage 2 amendment 2026-05-15 (master §3.14 relocation):
// Note struct declared HERE (NOT in types.go) — earlier plan drafts placed
// it in types.go with Title + UpdatedAt fields that are NOT part of master
// §3.14's canonical shape; the authoritative declaration is the narrow form
// below (Plan 7 owns the full vault.db schema; Plan 14 reads via narrowed
// shape through a Phase F daemon-init adapter).
//
// Fields beyond Plan 7's base shape:
//   - EcosystemJoinKeys: populated by Plan 14 Phase B-11 ingester
//     (this file's contract: ProcessVaultNote writes the JSON array column
//     vault.db.notes.ecosystem_join_keys per resolved symbol candidate).
//   - AuditChainAnchor: populated by Plan 9 chain (post-Phase-H invariant
//     inv-zen-074 binding vault notes to the audit chain).
//

package ecosystem

type Note struct {
	ID int64

	ProjectID string

	Path string

	Content string

	EcosystemJoinKeys []string

	AuditChainAnchor string
}
