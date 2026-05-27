// SPDX-License-Identifier: MIT
package manifest

import (
	"fmt"
	"time"
)

type Manifest struct {
	HadesSystem    HadesSystemSection    `toml:"hades-system"`
	Plans          PlansSection          `toml:"plans"`
	Invariants     InvariantsSection     `toml:"invariants"`
	Doctrines      DoctrinesSection      `toml:"doctrines"`
	MCPs           MCPsSection           `toml:"mcps"`
	ADR            ADRSection            `toml:"adr"`
	AutonomousMode AutonomousModeSection `toml:"autonomous-mode"`
	Provenance     Provenance            `toml:"provenance"`
}

type HadesSystemSection struct {
	Version string `toml:"version"`

	Substrate string `toml:"substrate"`

	SubstrateMinVersion string `toml:"substrate_min_version"`
}

type PlansSection struct {
	Released []string `toml:"released"`

	InProgress []string `toml:"in-progress"`

	BrainstormPending []string `toml:"brainstorm-pending"`
}

type InvariantsSection struct {
	Count int `toml:"count"`

	VerifyCmd string `toml:"verify-cmd"`
}

type DoctrinesSection struct {
	Declared []string `toml:"declared"`

	Default string `toml:"default"`
}

type MCPsSection struct {
	Entries map[string]MCPEntry `toml:"entries,omitempty"`
}

type MCPEntry struct {
	Plan int `toml:"plan"`

	Status string `toml:"status"`

	MinVersion string `toml:"min_version,omitempty"`
}

type ADRSection struct {
	Count int `toml:"count"`

	Location string `toml:"location"`
}

type AutonomousModeSection struct {
	Status string `toml:"status"`

	PrerequisitesMet bool `toml:"prerequisites-met"`

	LastCheck time.Time `toml:"last-check"`
}

type Provenance struct {
	LastRegenerate time.Time `toml:"last-regenerate"`

	MissingSources []string `toml:"missing-sources,omitempty"`
}

type ManualField struct {
	Path string

	CurrentValue any

	LastChangedAt time.Time

	LastChangedBy string

	LastReason string
}

func (m ManualField) String() string {
	if m.LastChangedAt.IsZero() {
		return fmt.Sprintf("%s=%v (never set)", m.Path, m.CurrentValue)
	}
	return fmt.Sprintf("%s=%v (by %s @ %s: %s)",
		m.Path,
		m.CurrentValue,
		m.LastChangedBy,
		m.LastChangedAt.UTC().Format(time.RFC3339),
		m.LastReason,
	)
}

type SectionResult struct {
	Data any

	MissingSources []string
}

func (r SectionResult) HasMissingSources() bool {
	return len(r.MissingSources) > 0
}

func (r SectionResult) IsPartial() bool { return r.HasMissingSources() }

type ManualFieldPath struct {
	Path string
}

type AutoSourceMapping struct {
	Path string

	Source string
}
