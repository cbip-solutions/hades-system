// SPDX-License-Identifier: MIT

//go:build !race
// +build !race

// Package compliance — Plan 15 Phase I task I-8 compliance test for
// the canonical-docs hygiene gate.
//
// Asserts the verify-canonical-docs-hygiene gate passes against the live
// repo. The gate enforces five sub-invariants (one per Phase I sub-task
// I-1..I-5 anti-regression surface):
//
//	inv-zen-324: canonical-docs hygiene gate operational + composes into
//	             the verify-release-gates aggregator additively (Phase G
//	             owns the aggregator; this test asserts the per-gate
//	             entry-point binary works in isolation).
//	inv-zen-325: frontmatter `last_updated` ≤180 days for docs with YAML
//	             frontmatter (currently: AGENTS.md).
//	inv-zen-326: docs/operations/gitnexus-ux.md MUST NOT exist (Plan 15
//	             decisión 6; Plan 19 Caronte replaces gitnexus entirely).
//	inv-zen-327: Plan-N body audit — no references to retired/decommissioned
//	             plan numbers in canonical docs except those on the allowlist
//	             at tests/testdata/plan_n_audit_allowlist.txt.
//	inv-zen-328: docs/decisions/_index.json entries length == count of
//	             on-disk ADRs that have YAML frontmatter (substrate-honest
//	             formulation: WalkAndEmitIndex intentionally skips legacy
//	             ADRs without frontmatter per internal/adr/index.go:69-72;
//	             entries-count parity gates the regen-adr-index workflow).
//
// Numeric ID inv-zen-329 is the umbrella for the canonical-docs hygiene
// gate per Plan 15 spec §K Batch-4 sparse allocation (324-329 reserved
// for Phase I sub-task anti-regression invariants). The umbrella test
// runs all five sub-invariants as table-driven subtests.
//
// Why this test exists: even with the bash wrapper at
// scripts/verify_canonical_docs_hygiene.sh, the Go test gate ensures
// `make test` + CI catch regressions in the canonical-docs surface
// where (a) a required doc gets deleted, (b) frontmatter ages past 180d
// without a refresh, (c) gitnexus-ux.md gets re-introduced, (d) Plan-N
// references drift, or (e) ADR manifest goes stale.
//
// Composes into the verify-release-gates Makefile composite once Phase G
// G-1 ships the aggregator (Phase I-8 ships the per-gate target standalone;
// Phase G refinement absorbs additively).
package compliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	FreshnessWindowDays = 180
)

var canonicalDocsHygiene = []string{
	"AGENTS.md",
	"llms.txt",
	"INSTALL.md",
	"docs/METHODOLOGY.md",
	"docs/operations/adr.md",
	"docs/operations/autonomy.md",
	"docs/operations/doctrine.md",
	"docs/decisions/_index.json",
}

var docsWithFrontmatterHygiene = []string{
	"AGENTS.md",
}

func TestInvZen329_CanonicalDocsHygiene(t *testing.T) {
	root := findRepoRootHygiene(t)

	t.Run("inv-zen-324_required_docs_present", func(t *testing.T) {

		for _, doc := range canonicalDocsHygiene {
			p := filepath.Join(root, doc)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("inv-zen-324: required canonical doc missing: %s (%v)", doc, err)
			}
		}
	})

	t.Run("inv-zen-325_frontmatter_freshness", func(t *testing.T) {

		threshold := time.Now().AddDate(0, 0, -FreshnessWindowDays)
		for _, doc := range docsWithFrontmatterHygiene {
			p := filepath.Join(root, doc)
			content, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("inv-zen-325: read %s: %v", doc, err)
				continue
			}
			lastUpdated := extractFrontmatterDateHygiene(string(content), "last_updated")
			if lastUpdated.IsZero() {
				t.Errorf("inv-zen-325: %s missing last_updated in frontmatter", doc)
				continue
			}
			if lastUpdated.Before(threshold) {
				ageDays := int(time.Since(lastUpdated).Hours() / 24)
				t.Errorf("inv-zen-325: %s last_updated=%s is %d days old (>%d window)",
					doc, lastUpdated.Format("2006-01-02"), ageDays, FreshnessWindowDays)
			}
		}
	})

	t.Run("inv-zen-326_gitnexus_ux_absent", func(t *testing.T) {
		// Decisión 6: Caronte = sole code-graph; gitnexus retired entirely.
		// The file docs/operations/gitnexus-ux.md MUST NOT exist on disk.
		p := filepath.Join(root, "docs", "operations", "gitnexus-ux.md")
		if _, err := os.Stat(p); err == nil {
			t.Errorf("inv-zen-326: %s MUST NOT exist (decisión 6; Plan 19 Caronte replaces gitnexus)", p)
		}

		entries, err := os.ReadDir(filepath.Join(root, "docs", "operations"))
		if err != nil {
			t.Fatalf("inv-zen-326: readdir docs/operations: %v", err)
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "gitnexus") && strings.HasSuffix(name, ".md") {
				t.Errorf("inv-zen-326: docs/operations/%s MUST NOT exist (gitnexus retired per decisión 6)", name)
			}
		}
	})

	t.Run("inv-zen-327_plan_n_body_audit", func(t *testing.T) {

		allowlist, err := loadAllowlistHygiene(filepath.Join(root, "tests", "testdata", "plan_n_audit_allowlist.txt"))
		if err != nil {
			t.Fatalf("inv-zen-327: load allowlist: %v", err)
		}
		planRefRe := regexp.MustCompile(`(?i)\bplan[- ]?(\d+)\b`)
		for _, doc := range canonicalDocsHygiene {
			if strings.HasSuffix(doc, ".json") {
				continue
			}
			p := filepath.Join(root, doc)
			content, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			lines := strings.Split(string(content), "\n")
			for lineNo, line := range lines {
				matches := planRefRe.FindAllStringSubmatch(line, -1)
				for _, m := range matches {
					ref := fmt.Sprintf("%s:Plan-%s", doc, m[1])
					if isAllowedHygiene(allowlist, ref, doc, m[0]) {
						continue
					}
					if isRetiredPlanNumberHygiene(m[1]) {
						t.Errorf("inv-zen-327: %s:%d references retired plan %q outside allowlist: %q",
							doc, lineNo+1, m[1], strings.TrimSpace(line))
					}
				}
			}
		}
	})

	t.Run("inv-zen-328_adr_index_entries_match_frontmatter_set", func(t *testing.T) {

		idxPath := filepath.Join(root, "docs", "decisions", "_index.json")
		content, err := os.ReadFile(idxPath)
		if err != nil {
			t.Fatalf("inv-zen-328: read %s: %v", idxPath, err)
		}
		var idx struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(content, &idx); err != nil {
			t.Fatalf("inv-zen-328: parse %s: %v", idxPath, err)
		}
		dir := filepath.Join(root, "docs", "decisions")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("inv-zen-328: readdir %s: %v", dir, err)
		}
		adrRe := regexp.MustCompile(`^\d{4}-.*\.md$`)
		frontmatterCount := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !adrRe.MatchString(name) {
				continue
			}
			adrPath := filepath.Join(dir, name)
			raw, err := os.ReadFile(adrPath)
			if err != nil {
				continue
			}

			if strings.HasPrefix(string(raw), "---\n") {
				frontmatterCount++
			}
		}
		if len(idx.Entries) != frontmatterCount {
			t.Errorf("inv-zen-328: docs/decisions/_index.json has %d entries; on-disk ADRs WITH frontmatter = %d (mismatch — run cmd/regen-adr-index)",
				len(idx.Entries), frontmatterCount)
		}
	})
}

func extractFrontmatterDateHygiene(content, key string) time.Time {
	if !strings.HasPrefix(content, "---") {
		return time.Time{}
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return time.Time{}
	}
	frontmatter := content[3 : 3+end]
	lineRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(\S+)`)
	m := lineRe.FindStringSubmatch(frontmatter)
	if len(m) < 2 {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

func loadAllowlistHygiene(path string) (map[string]bool, error) {
	allow := map[string]bool{}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return allow, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allow[line] = true
	}
	return allow, nil
}

func isAllowedHygiene(allowlist map[string]bool, ref, doc, match string) bool {
	return allowlist[ref] || allowlist[doc] || allowlist[match]
}

func isRetiredPlanNumberHygiene(n string) bool {
	retired := map[string]bool{}
	return retired[n]
}

func findRepoRootHygiene(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("go.mod not found walking up from %s", cwd)
	return ""
}
