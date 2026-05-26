// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRootForHandbook(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatalf("repo root not found from %q", wd)
	return ""
}

func handbookPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootForHandbook(t), "docs/operations/release-engineering.md")
}

func loadHandbook(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(handbookPath(t))
	if err != nil {
		t.Fatalf("read %s: %v", handbookPath(t), err)
	}
	return string(data)
}

func TestReleaseEngineeringHandbookExists(t *testing.T) {
	if _, err := os.Stat(handbookPath(t)); err != nil {
		t.Fatalf("docs/operations/release-engineering.md not found: %v", err)
	}
}

func TestReleaseEngineeringRequiredSections(t *testing.T) {
	text := loadHandbook(t)
	requiredSections := []string{
		"## Overview",
		"## Release ritual",
		"## Reproducibility baseline",
		"## Signature verification (end-user UX)",
		"## Sequoia 15 tarball friction mitigation",
		"## Apple Developer ID future ratchet",
		"## Docker / GHCR distribution channel",
		"## Linux native install",
		"## Future ratchets",
		"## Recovery procedures",
		"## Files + scripts reference",
	}
	for _, sect := range requiredSections {
		if !strings.Contains(text, sect) {
			t.Errorf("handbook missing required section: %q", sect)
		}
	}
}

func TestReleaseEngineeringCanonicalCommands(t *testing.T) {
	text := loadHandbook(t)
	canonicalCmds := []*regexp.Regexp{
		regexp.MustCompile(`git tag -a v\d+\.\d+\.\d+ -m`),
		regexp.MustCompile(`git push origin v\d+\.\d+\.\d+`),
		regexp.MustCompile(`gh attestation verify`),
		regexp.MustCompile(`cosign verify-blob`),

		regexp.MustCompile(`(?s)cosign verify\b.*ghcr\.io/cbip-solutions/hades-system`),
		regexp.MustCompile(`xattr -d com\.apple\.quarantine`),
		regexp.MustCompile(`brew install cbip-solutions/tap/hades`),
		regexp.MustCompile(`docker pull ghcr\.io/cbip-solutions/hades-system`),
		regexp.MustCompile(`sudo dpkg -i hades-`),
		regexp.MustCompile(`sudo rpm -i hades-`),
		regexp.MustCompile(`goreleaser release`),
		regexp.MustCompile(`make verify-release-artifacts`),
	}
	for _, re := range canonicalCmds {
		if !re.MatchString(text) {
			t.Errorf("handbook missing canonical command matching: %s", re.String())
		}
	}
}

func TestReleaseEngineeringFileReferencesExist(t *testing.T) {
	root := repoRootForHandbook(t)
	text := loadHandbook(t)

	pathRegex := regexp.MustCompile("`([^`]*/[^`]+\\.(yml|yaml|go|sh|json|md))`")
	matches := pathRegex.FindAllStringSubmatch(text, -1)
	missing := []string{}
	for _, m := range matches {
		path := m[1]
		if strings.ContainsAny(path, "${<>") {
			continue
		}
		if strings.HasPrefix(path, "/usr/") || strings.HasPrefix(path, "/etc/") {
			continue
		}
		if strings.HasPrefix(path, "http") {
			continue
		}

		if strings.HasPrefix(path, "~") {
			continue
		}

		clean := strings.TrimPrefix(path, "./")
		clean = strings.TrimPrefix(clean, "../../")
		abs := filepath.Join(root, clean)
		if _, err := os.Stat(abs); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("handbook references missing files (%d): %v", len(missing), missing)
	}
}

func TestReleaseEngineeringNoClaudeAttribution(t *testing.T) {
	text := loadHandbook(t)
	forbidden := []string{
		"Co-Authored-By: prohibited assistant",
		"Co-Authored-By: claude",
		"Generated with prohibited assistant",
		"generated with claude",
	}
	for _, bad := range forbidden {
		if strings.Contains(text, bad) {
			t.Errorf("handbook contains forbidden Claude attribution: %q", bad)
		}
	}
}

func TestFutureRatchetsTableComplete(t *testing.T) {
	text := loadHandbook(t)
	idx := strings.Index(text, "## Future ratchets")
	if idx < 0 {
		t.Fatal("Future ratchets section missing")
	}
	section := text[idx:]

	if endIdx := strings.Index(section[3:], "\n## "); endIdx > 0 {
		section = section[:endIdx+3]
	}

	requiredRatchets := []string{
		"darwin-amd64",
		"Apple Developer ID",
		"FreeBSD",
		"APT/RPM paid registry",
		"Snap",
		"Nix flake",
		"AUR",
		"Windows",
		"Byte-identical reproducibility",
	}
	for _, r := range requiredRatchets {
		if !strings.Contains(section, r) {
			t.Errorf("Future ratchets table missing row: %q", r)
		}
	}
}

func TestFutureRatchetsTableNoTBD(t *testing.T) {
	text := loadHandbook(t)
	forbidden := []string{"TBD", "FIXME", "XXX"}
	for _, bad := range forbidden {
		re := regexp.MustCompile(`\b` + bad + `\b`)
		if re.MatchString(text) {
			t.Errorf("release-engineering.md contains forbidden placeholder: %q", bad)
		}
	}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "TODO" || strings.HasPrefix(trimmed, "TODO:") || strings.HasPrefix(trimmed, "TODO ") {
			t.Errorf("release-engineering.md contains bare TODO line: %q", line)
		}
	}
}

func TestFutureRatchetsTriggerCriteriaPresent(t *testing.T) {
	text := loadHandbook(t)
	idx := strings.Index(text, "## Future ratchets")
	if idx < 0 {
		t.Fatal("Future ratchets section missing")
	}
	section := text[idx:]
	if endIdx := strings.Index(section[3:], "\n## "); endIdx > 0 {
		section = section[:endIdx+3]
	}
	lines := strings.Split(section, "\n")
	rowCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}

		if strings.HasPrefix(line, "|---") || strings.HasPrefix(line, "|-") {
			continue
		}

		if strings.Contains(line, "Ratchet") && strings.Contains(line, "Trigger") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 3 {
			continue
		}
		for i, c := range cells {
			c = strings.TrimSpace(c)
			if c == "" {
				t.Errorf("table row has empty cell %d: %q", i, line)
			}
		}
		rowCount++
	}
	if rowCount < 9 {
		t.Errorf("Future ratchets table has %d data rows, want >= 9", rowCount)
	}
}
