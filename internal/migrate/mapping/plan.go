// SPDX-License-Identifier: MIT
package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type Plan struct {
	SchemaVersion string      `json:"schemaVersion"`
	Source        string      `json:"source"`
	Preset        Preset      `json:"preset"`
	Entries       []PlanEntry `json:"entries"`
	Warnings      []string    `json:"warnings,omitempty"`
	CreatedAt     time.Time   `json:"createdAt,omitempty"`
	MerkleRoot    string      `json:"merkleRoot,omitempty"`
}

type EntryKind string

const (
	EntryKindSkill EntryKind = "skill"

	EntryKindCommand EntryKind = "command"

	EntryKindHook EntryKind = "hook"

	EntryKindDoctrine EntryKind = "doctrine"

	EntryKindMemory EntryKind = "memory"

	EntryKindMCPServer EntryKind = "mcp_server"

	EntryKindHermesConfig EntryKind = "hermes_config"
)

type PlanEntry struct {
	Kind         EntryKind         `json:"kind"`
	SourcePath   string            `json:"sourcePath"`
	TargetPath   string            `json:"targetPath"`
	Frontmatter  map[string]string `json:"frontmatter,omitempty"`
	HookEvent    string            `json:"hookEvent,omitempty"`
	BodyBytes    []byte            `json:"-"`
	SHA256       string            `json:"sha256,omitempty"`
	RegisterCall string            `json:"registerCall,omitempty"`
	Notes        []string          `json:"notes,omitempty"`
}

func (p *Plan) MarshalJSON() ([]byte, error) {
	type alias Plan
	return json.Marshal((*alias)(p))
}

func (p *Plan) ComputeHashes() {
	for i := range p.Entries {
		p.Entries[i].SHA256 = hashHex(p.Entries[i].BodyBytes)
	}
	p.MerkleRoot = computeMerkleRoot(p.Entries)
}

func hashHex(b []byte) string {
	if len(b) == 0 {

		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func computeMerkleRoot(entries []PlanEntry) string {
	leaves := make([]string, len(entries))
	for i, e := range entries {
		leaves[i] = string(e.Kind) + "\x00" + e.SourcePath + "\x00" + e.SHA256
	}
	sort.Strings(leaves)
	h := sha256.New()
	for _, l := range leaves {
		_, _ = h.Write([]byte(l))
		_, _ = h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))
}
