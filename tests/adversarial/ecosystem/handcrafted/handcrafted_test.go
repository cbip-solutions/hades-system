package handcrafted

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// HandcraftedEntry mirrors the schema consumed by H-4
// (tests/adversarial/ecosystem/adversarial_test.go). Kept in sync with
// that file's HandcraftedEntry type; any field rename here MUST be
// mirrored there to avoid silent corpus-test drift.
type HandcraftedEntry struct {
	Query           string `json:"query"`
	FakeSymbol      string `json:"fake_symbol"`
	ExpectedOutcome string `json:"expected_outcome"`
	Justification   string `json:"justification"`
	Category        string `json:"category"`

	VersionContext string `json:"version_context,omitempty"`
}

const minEntriesPerEcosystem = 50

var allowedOutcomes = map[string]bool{
	"abstain": true,
	"unknown": true,
	"refuse":  true,
}

var ecosystems = []string{"go", "python", "typescript", "rust"}

func loadEcosystem(t *testing.T, eco string) []HandcraftedEntry {
	t.Helper()
	data, err := os.ReadFile(eco + ".json")
	if err != nil {
		t.Fatalf("read %s.json: %v", eco, err)
	}
	var entries []HandcraftedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse %s.json: %v", eco, err)
	}
	return entries
}

func TestHandcraftedSchema(t *testing.T) {
	for _, eco := range ecosystems {
		eco := eco
		t.Run(eco, func(t *testing.T) {
			entries := loadEcosystem(t, eco)
			for i, e := range entries {
				if strings.TrimSpace(e.Query) == "" {
					t.Errorf("%s.json[%d]: empty query", eco, i)
				}
				if strings.TrimSpace(e.FakeSymbol) == "" {
					t.Errorf("%s.json[%d]: empty fake_symbol", eco, i)
				}
				if strings.TrimSpace(e.Justification) == "" {
					t.Errorf("%s.json[%d]: empty justification", eco, i)
				}
				if strings.TrimSpace(e.Category) == "" {
					t.Errorf("%s.json[%d]: empty category", eco, i)
				}
				if !allowedOutcomes[e.ExpectedOutcome] {
					t.Errorf("%s.json[%d] fake_symbol=%q: expected_outcome=%q not in {abstain,unknown,refuse}",
						eco, i, e.FakeSymbol, e.ExpectedOutcome)
				}

				if e.VersionContext != "" && strings.TrimSpace(e.VersionContext) == "" {
					t.Errorf("%s.json[%d] fake_symbol=%q: version_context present but whitespace-only",
						eco, i, e.FakeSymbol)
				}
			}
		})
	}
}

func TestHandcraftedMinCount(t *testing.T) {
	for _, eco := range ecosystems {
		eco := eco
		t.Run(eco, func(t *testing.T) {
			entries := loadEcosystem(t, eco)
			if len(entries) < minEntriesPerEcosystem {
				t.Errorf("%s.json: %d entries, minimum %d required (Plan 14 H-3 floor)",
					eco, len(entries), minEntriesPerEcosystem)
			}
		})
	}
}

func TestHandcraftedNoDuplicateFakeSymbols(t *testing.T) {
	for _, eco := range ecosystems {
		eco := eco
		t.Run(eco, func(t *testing.T) {
			entries := loadEcosystem(t, eco)
			seen := make(map[string]int, len(entries))
			for i, e := range entries {
				if prev, ok := seen[e.FakeSymbol]; ok {
					t.Errorf("%s.json[%d]: fake_symbol=%q duplicates entry[%d]",
						eco, i, e.FakeSymbol, prev)
				}
				seen[e.FakeSymbol] = i
			}
		})
	}
}

func TestHandcraftedTotalFloor(t *testing.T) {
	const totalMin = minEntriesPerEcosystem * 4
	var total int
	for _, eco := range ecosystems {
		entries := loadEcosystem(t, eco)
		total += len(entries)
	}
	if total < totalMin {
		t.Errorf("total handcrafted entries=%d, minimum %d required (4 ecosystems × %d floor)",
			total, totalMin, minEntriesPerEcosystem)
	}
}
