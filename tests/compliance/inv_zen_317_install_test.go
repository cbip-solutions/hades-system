// SPDX-License-Identifier: MIT

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

func readINSTALL(t *testing.T) ([]byte, string) {
	t.Helper()
	root := findRepoRoot(t)
	path := filepath.Join(root, "INSTALL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read INSTALL.md (%s): %v", path, err)
	}
	return data, path
}

func TestInvZen317_InstallExists(t *testing.T) {
	t.Parallel()
	data, path := readINSTALL(t)
	if len(data) == 0 {
		t.Fatalf("INSTALL.md is empty: %s", path)
	}
	const minLines = 50
	lineCount := strings.Count(string(data), "\n")
	if lineCount < minLines {
		t.Fatalf("INSTALL.md has %d lines; expected ≥ %d for production-quality install doc", lineCount, minLines)
	}
}

func TestInvZen317_InstallPrerequisites(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	re := regexp.MustCompile(`Go 1\.26\+?`)
	if !re.MatchString(text) {
		t.Fatalf("INSTALL.md missing Go 1.26+ prerequisite declaration")
	}
}

func TestInvZen317_InstallCaronteMentioned(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	if !strings.Contains(text, "Caronte") {
		t.Fatalf("INSTALL.md missing literal \"Caronte\" — Plan 19 in-tree code-graph engine not mentioned")
	}
}

func TestInvZen317_InstallGitnexusRetired(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	count := strings.Count(text, "gitnexus")
	if count == 0 {

		return
	}

	retirementMarkers := []string{
		"no `gitnexus`",
		"no gitnexus",
		"retired",
		"removed",
		"retirement",
	}
	hasRetirementContext := false
	for _, m := range retirementMarkers {
		if strings.Contains(text, m) {
			hasRetirementContext = true
			break
		}
	}
	if !hasRetirementContext {
		t.Fatalf("INSTALL.md mentions \"gitnexus\" %d times without a retirement-context marker; per decisión 6 gitnexus is retired (use \"no gitnexus\" / \"retired\" / \"removed\" framing)", count)
	}
}

func TestInvZen317_InstallHadesBinaryMentioned(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	if !strings.Contains(text, "bin/hades") {
		t.Fatalf("INSTALL.md missing literal \"bin/hades\" — Plan 18a brand wrapper binary not documented")
	}
}

func TestInvZen317_InstallBrewTapPath(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	if !strings.Contains(text, "cbip-solutions/tap/hades") {
		t.Fatalf("INSTALL.md missing canonical brew tap install command \"brew install cbip-solutions/tap/hades\" — public tap per Plan 15 decisión 4 + 11")
	}
}

func TestInvZen317_InstallLicenseFraming(t *testing.T) {
	t.Parallel()
	data, _ := readINSTALL(t)
	text := string(data)

	if !strings.Contains(text, "MIT") {
		t.Fatalf("INSTALL.md missing \"MIT\" license literal — Plan 15 decisión 15 framing not present")
	}
}
