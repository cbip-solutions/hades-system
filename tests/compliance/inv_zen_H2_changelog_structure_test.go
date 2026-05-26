// SPDX-License-Identifier: MIT

//go:build !race
// +build !race

// Package compliance — Plan 15 Phase H task H-1 compliance test for
// inv-zen-318 (placeholder H2 → reconciled to inv-zen-318 per
// docs/superpowers/plans/2026-05-25-plan-15-phase-H-docs-cutover-with-compat-and-security.md
// inv-zen-317..H7 mapping).
//
// Asserts the v1.0.0 CHANGELOG.md entry follows the Plan 9 canonical
// per-package pattern AND complies with the Stage-0 reconciliation
// decisión 17-c (CHANGELOG curation policy) anti-leak rules:
//
//   - Has the v1.0.0 H2 header with `Plan 15:` framing.
//   - Contains ≥ 4 mandatory sub-sections (Added, Changed, Fixed,
//     Security) each with non-empty body.
//   - Carries MIT license framing per Stage-0 decisión 15 (NOT
//     Apache-2.0 in this entry; whole project is MIT).
//   - Names the `hades-system` public identity per decisiones 4 + 11.
//   - Does NOT leak bypass-tier internals per decisión 17-c (no
//     mention of `metadata.user_id`, `fingerprint coexistence`,
//     `refresh-on-429`, `validator schema drift`, `gzip+deflate
//     decompression`, `bypass-recovery-probe`, `refresh-protocol`,
//     `Anthropic anti-abuse`).
//   - Preserves the historical v0.9.0 entry (additive top-banner
//     authoring; H-1 prepends rather than rewrites).
//   - Sits in the 300-600 line size range (per spec ~300-400 LOC
//     target with headroom for the Stage-0-expanded sub-sections).
//
// Why this test exists: the v1.0.0 entry IS the portfolio shipping
// document — drift here directly affects what the public will read
// at the Phase H-9 cutover snapshot. A regex-anti-leak gate ensures
// no future writer reintroduces bypass-tier mechanism details into
// a release the public will see.
//
// Composes into `make verify-invariants` via the standard compliance
// suite. Runs in `make test` (default suite, not gated by env vars).
package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestInvZenH2_ChangelogV1EntryStructuredPerPackage asserts the
// CHANGELOG.md v1.0.0 entry follows the Plan 9 canonical pattern
// (### Added/Changed/Fixed/Security mandatory + optional sub-sections),
// carries MIT framing + `hades-system` identity, and does NOT leak
// bypass-tier internals per decisión 17-c.
//
// inv-zen-318 (H-2 placeholder).
func TestInvZenH2_ChangelogV1EntryStructuredPerPackage(t *testing.T) {
	t.Parallel()

	root := findRepoRoot(t)
	changelogPath := filepath.Join(root, "CHANGELOG.md")

	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	body := string(raw)

	v1HeaderRE := regexp.MustCompile(`(?m)^## \[v1\.0\.0\] — \d{4}-\d{2}-\d{2} — Plan 15:`)
	if !v1HeaderRE.MatchString(body) {
		t.Fatalf("CHANGELOG.md missing v1.0.0 H2 header per Plan 9 pattern; " +
			"expected shape: `## [v1.0.0] — YYYY-MM-DD — Plan 15: ...`")
	}

	v1SectionRE := regexp.MustCompile(`(?ms)^## \[v1\.0\.0\] —.*?(?:^## \[|\z)`)
	v1Section := v1SectionRE.FindString(body)
	if v1Section == "" {
		t.Fatal("CHANGELOG.md v1.0.0 section body extraction failed")
	}

	mandatoryH3 := []string{
		"### Added",
		"### Changed",
		"### Fixed",
		"### Security",
	}
	for _, h3 := range mandatoryH3 {
		if !strings.Contains(v1Section, h3) {
			t.Errorf("CHANGELOG.md v1.0.0 section missing mandatory sub-section: %q", h3)
			continue
		}

		subRE := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(h3) + `\s*\n(.*?)(?:^### |^## |\z)`)
		subMatch := subRE.FindStringSubmatch(v1Section)
		if len(subMatch) < 2 || len(strings.TrimSpace(subMatch[1])) < 50 {
			t.Errorf("CHANGELOG.md v1.0.0 sub-section %q is empty or too short (<50 chars)", h3)
		}
	}

	// (4) Decisión 17-c anti-leak: no bypass-tier mechanism substrings
	// in the public-bound v1.0.0 entry. STRIP-list per the curation
	// table in 2026-05-25-plan-15-phase-H-docs-cutover-with-compat-and-security.md.
	antiLeakPatterns := []string{
		"Anthropic anti-abuse",
		"metadata.user_id",
		"fingerprint coexistence",
		"refresh-on-429",
		"validator schema drift",
		"gzip+deflate decompression",
		"bypass-recovery-probe",
		"refresh-protocol",
	}
	for _, pattern := range antiLeakPatterns {
		if strings.Contains(v1Section, pattern) {
			t.Errorf("CHANGELOG.md v1.0.0 entry leaks bypass-tier internals "+
				"(decisión 17-c violation): contains %q", pattern)
		}
	}

	if strings.Contains(v1Section, "Apache-2.0") {
		t.Errorf("CHANGELOG.md v1.0.0 entry contains 'Apache-2.0' — " +
			"decisión 15 mandates MIT framing for v1.0; rephrase to MIT")
	}
	if !strings.Contains(v1Section, "MIT") {
		t.Errorf("CHANGELOG.md v1.0.0 entry missing 'MIT' license reference per decisión 15")
	}

	if !strings.Contains(v1Section, "hades-system") {
		t.Errorf("CHANGELOG.md v1.0.0 entry missing 'hades-system' identity " +
			"reference per decisión 4 + 11")
	}

	optionalH3 := []string{
		"### Deprecated",
		"### Removed",
		"### Migration notes",
		"### Methodology",
	}
	for _, h3 := range optionalH3 {
		if strings.Contains(v1Section, h3) {
			t.Logf("CHANGELOG.md v1.0.0 optional sub-section present: %s", h3)
		}
	}

	v09HeaderRE := regexp.MustCompile(`(?m)^## \[v0\.9\.0\] — 2026-05-11 — Plan 9:`)
	if !v09HeaderRE.MatchString(body) {
		t.Error("CHANGELOG.md v0.9.0 entry missing — H-1 must preserve the " +
			"Plan 9 historical entry (additive top-banner only)")
	}

	v1Lines := strings.Count(v1Section, "\n")
	if v1Lines < 300 {
		t.Errorf("CHANGELOG.md v1.0.0 entry too short: got %d lines, want ≥ 300", v1Lines)
	}
	if v1Lines > 600 {
		t.Errorf("CHANGELOG.md v1.0.0 entry too long: got %d lines, want ≤ 600", v1Lines)
	}
}
