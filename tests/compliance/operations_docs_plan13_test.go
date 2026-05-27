// go:build !race
//go:build !race
// +build !race

package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPlan13OperationsHandbooksExistAndContainMandatorySections(t *testing.T) {
	repoRoot := findRepoRoot(t)
	handbooks := []struct {
		name              string
		mandatorySections []string
	}{
		{
			name: "migrate.md",
			mandatorySections: []string{
				"# zen migrate claude-code operator handbook",
				"## Overview",
				"## CLI usage",
				"## Flag reference",
				"## Mapping table",
				"## Failure modes",
				"## Recovery scenarios",
				"## Related",
			},
		},
		{
			name: "onboarding.md",
			mandatorySections: []string{
				"# zen onboarding wizard operator handbook",
				"## Overview",
				"## Path 1: Recommended defaults",
				"## Path 2: Reuse preferences",
				"## Path 3: Customize",
				"## zen recognize integration",
				"## Related",
			},
		},
		{
			name: "doctor-full.md",
			mandatorySections: []string{
				"# zen doctor full operator handbook",
				"## Overview",
				"## Checks catalog",
				"## --fix semantics",
				"## Backup + restore",
				"## EXIT CODES",
				"## Troubleshooting",
				"## Related",
			},
		},
		{
			name: "state-model.md",
			mandatorySections: []string{
				"# zen state model operator handbook",
				"## XDG-canonical paths",
				"## Retention policy",
				"## `zen state list`",
				"## `zen state cleanup`",
				"## Cross-platform notes",
				"## Related",
			},
		},
	}
	for _, h := range handbooks {
		t.Run(h.name, func(t *testing.T) {
			path := filepath.Join(repoRoot, "docs", "operations", h.name)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", h.name, err)
			}
			s := string(body)
			for _, hdr := range h.mandatorySections {
				if !strings.Contains(s, hdr) {
					t.Errorf("%s missing mandatory section %q", h.name, hdr)
				}
			}
		})
	}
}

func TestPlan13HandbooksCrossReferencesResolve(t *testing.T) {
	repoRoot := findRepoRoot(t)
	handbooks := []string{"migrate.md", "onboarding.md", "doctor-full.md", "state-model.md"}

	adrRefPattern := regexp.MustCompile(`ADR-(\d{4})`)
	invRefPattern := regexp.MustCompile(`inv-zen-(\d{3})`)

	for _, h := range handbooks {
		t.Run(h, func(t *testing.T) {
			path := filepath.Join(repoRoot, "docs", "operations", h)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", h, err)
			}
			s := string(body)

			adrMatches := adrRefPattern.FindAllStringSubmatch(s, -1)
			seen := make(map[string]bool)
			for _, m := range adrMatches {
				adrID := m[1]
				if seen[adrID] {
					continue
				}
				seen[adrID] = true
				files, _ := filepath.Glob(filepath.Join(repoRoot, "docs", "decisions", adrID+"-*.md"))
				if len(files) == 0 {
					t.Errorf("%s references ADR-%s but no file matches docs/decisions/%s-*.md", h, adrID, adrID)
				}
			}

			invMatches := invRefPattern.FindAllStringSubmatch(s, -1)
			invSeen := make(map[string]bool)
			for _, m := range invMatches {
				invID := m[1]
				if invSeen[invID] {
					continue
				}
				invSeen[invID] = true
				if !isKnownPlan13EraInvariantID(invID) {
					t.Errorf("%s references inv-zen-%s not in registered ranges", h, invID)
				}
			}
		})
	}
}

func isKnownPlan13EraInvariantID(id string) bool {

	switch {
	case id >= "001" && id <= "200":
		return true
	default:
		return false
	}
}
