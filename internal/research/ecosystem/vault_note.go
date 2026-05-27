// SPDX-License-Identifier: MIT
// internal/research/ecosystem/vault_note.go — narrow release vault.db notes
// row shape needed by ingester.ProcessVaultNote.
//
// amendment 2026-05-15 (master §3.14 relocation):
// Note struct declared HERE (NOT in types.go) — earlier plan drafts placed
// it in types.go with Title + UpdatedAt fields that are NOT part of master
// §3.14's canonical shape; the authoritative declaration is the narrow form
// below (release owns the full vault.db schema; release reads via narrowed
// shape through a daemon-init adapter).
//
// Fields beyond release's base shape:
// - EcosystemJoinKeys: populated ingester
// (this file's contract: ProcessVaultNote writes the JSON array column
// vault.db.notes.ecosystem_join_keys per resolved symbol candidate).
// - AuditChainAnchor: populated chain (post- invariant
// inv-hades-074 binding vault notes to the audit chain).
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
