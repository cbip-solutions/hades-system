package manifest

import (
	"bytes"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestManifestRoundTripTOML(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	m := &Manifest{
		ZenSwarm: ZenSwarmSection{
			Version:             "0.9.0",
			Substrate:           "openclaude",
			SubstrateMinVersion: "0.7.0",
		},
		Plans: PlansSection{
			Released:          []string{"plan-1@v0.1.0", "plan-2@v0.2.2"},
			InProgress:        []string{},
			BrainstormPending: []string{"plan-10", "plan-11"},
		},
		Invariants: InvariantsSection{
			Count:     152,
			VerifyCmd: "make verify-invariants",
		},
		Doctrines: DoctrinesSection{
			Declared: []string{"max-scope", "default", "capa-firewall"},
			Default:  "max-scope",
		},
		MCPs: MCPsSection{
			Entries: map[string]MCPEntry{
				"research": {Plan: 4, Status: "production"},
				"budget":   {Plan: 4, Status: "production"},
			},
		},
		ADR: ADRSection{
			Count:    69,
			Location: "docs/decisions/",
		},
		AutonomousMode: AutonomousModeSection{
			Status:           "disabled",
			PrerequisitesMet: false,
			LastCheck:        now,
		},
		Provenance: Provenance{
			LastRegenerate: now,
			MissingSources: nil,
		},
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got Manifest
	if _, err := toml.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.ZenSwarm.Version != m.ZenSwarm.Version {
		t.Errorf("ZenSwarm.Version round-trip mismatch: got %q, want %q",
			got.ZenSwarm.Version, m.ZenSwarm.Version)
	}
	if got.Invariants.Count != m.Invariants.Count {
		t.Errorf("Invariants.Count round-trip: got %d, want %d",
			got.Invariants.Count, m.Invariants.Count)
	}
	if len(got.Plans.Released) != len(m.Plans.Released) {
		t.Errorf("Plans.Released length: got %d, want %d",
			len(got.Plans.Released), len(m.Plans.Released))
	}
	if !got.Provenance.LastRegenerate.Equal(m.Provenance.LastRegenerate) {
		t.Errorf("Provenance.LastRegenerate round-trip: got %v, want %v",
			got.Provenance.LastRegenerate, m.Provenance.LastRegenerate)
	}
}

func TestManualFieldStringer(t *testing.T) {
	mf := ManualField{
		Path:          "zen-swarm.substrate_min_version",
		CurrentValue:  "0.7.1",
		LastChangedAt: time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC),
		LastChangedBy: "testuser",
		LastReason:    "OpenClaude 0.7.0 has CVE-2026-X",
	}
	got := mf.String()
	if got == "" {
		t.Fatal("ManualField.String() returned empty")
	}
	want := "zen-swarm.substrate_min_version=0.7.1 (by testuser @ 2026-05-05T10:00:00Z: OpenClaude 0.7.0 has CVE-2026-X)"
	if got != want {
		t.Errorf("ManualField.String():\n got  %q\n want %q", got, want)
	}
}

func TestSectionResultMissingSources(t *testing.T) {
	r := SectionResult{
		MissingSources: []string{"git-tags", "_index.json"},
	}
	if !r.HasMissingSources() {
		t.Error("HasMissingSources should be true with non-empty slice")
	}
	if r.IsPartial() != true {
		t.Error("IsPartial should be true")
	}

	empty := SectionResult{}
	if empty.HasMissingSources() {
		t.Error("HasMissingSources should be false on zero-value")
	}
}

func TestManualFieldStringerZeroTime(t *testing.T) {
	mf := ManualField{
		Path:         "doctrines.default",
		CurrentValue: "max-scope",
	}
	got := mf.String()
	want := "doctrines.default=max-scope (never set)"
	if got != want {
		t.Errorf("ManualField.String() zero-time:\n got  %q\n want %q", got, want)
	}
}

func TestSectionResultZeroValue(t *testing.T) {
	var r SectionResult
	if r.HasMissingSources() {
		t.Error("HasMissingSources on zero value should be false")
	}
	if r.IsPartial() {
		t.Error("IsPartial on zero value should be false")
	}
}
