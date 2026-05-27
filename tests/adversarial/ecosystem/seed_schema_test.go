// Package ecosystem validates the Spracklen adversarial seed corpus
// shipped with Task H-2.
//
// The seed file (`spracklen_seed.json`) is a curated derived subset of
// the Spracklen et al. USENIX Security 2025 corpus of 205k fake package
// names hallucinated by LLMs across four ecosystems
// (https://arxiv.org/abs/2406.10279). The H-4 adversarial CI gate
// consumes this seed to measure the dispatcher confabulation rate per
// spec §2.7 Q7=A (<2% threshold), invariant.
//
// The schema validator here is the floor: it ensures the JSON shape
// stays stable across regenerations (full corpus or representative
// subset) so the H-4 gate can rely on it. It is intentionally
// permissive about entry count (any N >= per-ecosystem minimum
// satisfies the gate; production calibration may swap in the full
// 205k corpus) but strict about per-entry field presence, value
// constraints, and per-ecosystem minimum representation.
//
// Invariant references: invariant (adversarial-corpus floor),
// invariant (<2% confabulation gate).
package ecosystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type seedDoc struct {
	Meta    seedMeta    `json:"meta"`
	Entries []seedEntry `json:"entries"`
}

type seedMeta struct {
	Source            string         `json:"source"`
	ArtifactURL       string         `json:"artifact_url"`
	Description       string         `json:"description"`
	FilterProcedure   string         `json:"filter_procedure"`
	TotalEntries      int            `json:"total_entries"`
	PerEcosystem      map[string]int `json:"per_ecosystem"`
	ConfabulationGate float64        `json:"confabulation_gate_pct"`
	SchemaVersion     int            `json:"schema_version"`
}

type seedEntry struct {
	Ecosystem string `json:"ecosystem"`
	FakeName  string `json:"fake_name"`
	Category  string `json:"category"`
}

var expectedEcosystems = map[string]struct{}{
	"go":     {},
	"python": {},
	"npm":    {},
	"rust":   {},
}

var allowedCategories = map[string]struct{}{
	"typosquat_stdlib":      {},
	"plausible_nonexistent": {},
	"version_bump_fake":     {},
	"stdlib_extension_fake": {},
	"plausible_extension":   {},
	"subpkg_version_fake":   {},
}

const perEcosystemMin = 100

func seedPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller(0) returned !ok; cannot resolve seed path")
	}
	return filepath.Join(filepath.Dir(thisFile), "spracklen_seed.json")
}

func loadSeed(t *testing.T) *seedDoc {
	t.Helper()
	path := seedPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed file %q: %v", path, err)
	}
	var doc seedDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse seed JSON %q: %v", path, err)
	}
	return &doc
}

func TestSeedMetaPresent(t *testing.T) {
	doc := loadSeed(t)
	if doc.Meta.Source == "" {
		t.Error("meta.source empty (Spracklen attribution missing)")
	}
	if doc.Meta.ArtifactURL == "" {
		t.Error("meta.artifact_url empty")
	}
	if doc.Meta.Description == "" {
		t.Error("meta.description empty")
	}
	if doc.Meta.FilterProcedure == "" {
		t.Error("meta.filter_procedure empty")
	}
	if doc.Meta.SchemaVersion != 1 {
		t.Errorf("meta.schema_version = %d, want 1", doc.Meta.SchemaVersion)
	}
	if doc.Meta.ConfabulationGate <= 0 || doc.Meta.ConfabulationGate > 100 {
		t.Errorf("meta.confabulation_gate_pct = %g, want (0,100]", doc.Meta.ConfabulationGate)
	}

	if doc.Meta.ConfabulationGate != 2.0 {
		t.Errorf("meta.confabulation_gate_pct = %g, want 2.0 (spec §2.7 Q7=A)", doc.Meta.ConfabulationGate)
	}
}

func TestSeedTotalEntriesMatchesArray(t *testing.T) {
	doc := loadSeed(t)
	if doc.Meta.TotalEntries != len(doc.Entries) {
		t.Errorf("meta.total_entries = %d but len(entries) = %d (mismatch)",
			doc.Meta.TotalEntries, len(doc.Entries))
	}
}

func TestSeedPerEcosystemCounts(t *testing.T) {
	doc := loadSeed(t)
	got := map[string]int{}
	for _, e := range doc.Entries {
		got[e.Ecosystem]++
	}

	for eco := range expectedEcosystems {
		count, ok := got[eco]
		if !ok {
			t.Errorf("ecosystem %q missing from entries", eco)
			continue
		}
		if count < perEcosystemMin {
			t.Errorf("ecosystem %q has %d entries, want >= %d", eco, count, perEcosystemMin)
		}
	}

	if len(doc.Meta.PerEcosystem) != len(got) {
		t.Errorf("meta.per_ecosystem has %d keys, entries have %d distinct ecosystems",
			len(doc.Meta.PerEcosystem), len(got))
	}
	for eco, metaCount := range doc.Meta.PerEcosystem {
		if got[eco] != metaCount {
			t.Errorf("meta.per_ecosystem[%q] = %d but entries contain %d", eco, metaCount, got[eco])
		}
	}
}

func TestSeedEntriesShape(t *testing.T) {
	doc := loadSeed(t)
	if len(doc.Entries) == 0 {
		t.Fatal("entries array empty")
	}
	seen := map[string]int{}
	for i, e := range doc.Entries {
		if _, ok := expectedEcosystems[e.Ecosystem]; !ok {
			t.Errorf("entry %d: ecosystem %q not in closed set", i, e.Ecosystem)
		}
		if e.FakeName == "" {
			t.Errorf("entry %d: fake_name empty", i)
		}
		if _, ok := allowedCategories[e.Category]; !ok {
			t.Errorf("entry %d (%s/%s): category %q not in allowed set",
				i, e.Ecosystem, e.FakeName, e.Category)
		}
		key := e.Ecosystem + "|" + e.FakeName
		if prev, dup := seen[key]; dup {
			t.Errorf("entry %d: duplicate (ecosystem=%s, fake_name=%s); first seen at %d",
				i, e.Ecosystem, e.FakeName, prev)
		}
		seen[key] = i
	}
}

func TestSeedEntriesNonEmpty(t *testing.T) {
	doc := loadSeed(t)
	if len(doc.Entries) < perEcosystemMin*len(expectedEcosystems) {
		t.Errorf("len(entries) = %d, want >= %d (per-ecosystem min %d * %d ecosystems)",
			len(doc.Entries),
			perEcosystemMin*len(expectedEcosystems),
			perEcosystemMin,
			len(expectedEcosystems))
	}
}
