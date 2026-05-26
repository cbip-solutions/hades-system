// SPDX-License-Identifier: MIT

//go:build !race
// +build !race

// Package compliance — Plan 15 Phase H task H-5 compliance test for
// inv-zen-317 sub-coverage (README.md component of the H-full doc
// maturity bundle).
//
// inv-zen-317 is the umbrella invariant for the H-full doc maturity
// bundle (LICENSE + NOTICE + README + CHANGELOG + SECURITY + CONTRIBUTING
// + CODE_OF_CONDUCT + handbook docs). Phase H tasks each ship one slice
// of the bundle; H-5 ships the README v1.0 curation (post-flip identity
// `hades-system`, MIT license framing, gitnexus removed per Plan 19 +
// decisión 6, badges row, Plan 15 narrative section). The umbrella test
// `tests/compliance/inv_zen_317_docs_maturity_test.go` (H-8) composes
// per-doc presence + size + sentinel checks; this file owns the
// finer-grained README content assertions.
//
// Why this test exists: README.md is the first surface every public
// visitor reads. Drift in license framing (MIT vs Apache), identity
// rename (hades-system vs zen-swarm), or removal of bypass-tier internals
// directly affects the public face of the v1.0 release. A regex-anti-leak
// gate ensures no future writer reintroduces bypass-tier mechanism
// details into the README.
//
// Acceptance criteria gated:
//
//  1. README.md exists at repo root, non-empty, size > 200 LOC (curated
//     post-v1.0 narrative is verbose; baseline is well above this floor).
//  2. License framing — contains "MIT" literal in license context;
//     does NOT carry "Apache-2.0 sovereignty" framing (superseded by
//  3. Identity — contains "hades-system" literal (public org); contains
//     "HADES" literal (brand).
//  4. Caronte mentioned (sole code-graph per Plan 19 + decisión 6).
//  5. No bypass-tier internals leak per decisión 17-c (regex anti-pattern
//     gate; see bypassTierLeakPatterns_README below).
//  6. Plan 15 v1.0 narrative section present ("What Plan 15 adds (v1.0.0").
//  7. Badges row present (license badge + at least one other badge).
//
// Composes into `make verify-invariants` via the standard compliance suite.
package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var bypassTierLeakPatterns_README = []string{
	`metadata\.user_id`,
	`fingerprint coexistence`,
	`refresh-on-429`,
	`validator schema drift`,
	`gzip\+deflate decompression`,
	`bypass-recovery-probe`,
	`refresh-protocol`,
	`Anthropic anti-abuse`,
}

func readREADME(t *testing.T) ([]byte, string) {
	t.Helper()
	root := findRepoRoot(t)
	path := filepath.Join(root, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read README.md (%s): %v", path, err)
	}
	return data, path
}

func TestInvZen317_ReadmeExists(t *testing.T) {
	t.Parallel()
	data, path := readREADME(t)
	if len(data) == 0 {
		t.Fatalf("README.md is empty: %s", path)
	}

	const minLines = 200
	lineCount := strings.Count(string(data), "\n")
	if lineCount < minLines {
		t.Fatalf("README.md has %d lines; expected ≥ %d for curated post-v1.0 narrative", lineCount, minLines)
	}
}

func TestInvZen317_ReadmeLicenseFraming(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	if !strings.Contains(text, "MIT") {
		t.Fatalf("README.md missing literal \"MIT\" — license framing not present")
	}

	forbidden := "Apache-2.0 sovereignty"
	if strings.Contains(text, forbidden) {
		t.Fatalf("README.md contains legacy framing %q — Plan 15 decisión 15 supersedes; rewrite to MIT", forbidden)
	}
}

func TestInvZen317_ReadmeIdentity(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	if !strings.Contains(text, "HADES") {
		t.Fatalf("README.md missing literal \"HADES\" — brand identity not present")
	}

	if !strings.Contains(text, "hades-system") {
		t.Fatalf("README.md missing literal \"hades-system\" — public-flip identity per decisión 4 + 11 not present")
	}
}

func TestInvZen317_ReadmeCaronteMentioned(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	if !strings.Contains(text, "Caronte") {
		t.Fatalf("README.md missing literal \"Caronte\" — Plan 19 sovereign code-graph engine not mentioned")
	}
}

func TestInvZen317_ReadmeNoBypassTierLeak(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	for _, pat := range bypassTierLeakPatterns_README {

		re, err := regexp.Compile(`(?i)` + pat)
		if err != nil {
			t.Fatalf("compile pattern %q: %v", pat, err)
		}
		if re.MatchString(text) {
			t.Fatalf("README.md contains bypass-tier internal pattern matching %q — decisión 17-c forbids in public surfaces", pat)
		}
	}
}

func TestInvZen317_ReadmePlan15Section(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	want := "What Plan 15 adds (v1.0.0"
	if !strings.Contains(text, want) {
		t.Fatalf("README.md missing Plan 15 v1.0 narrative section header %q", want)
	}
}

func TestInvZen317_ReadmeBadgesRow(t *testing.T) {
	t.Parallel()
	data, _ := readREADME(t)
	text := string(data)

	const badgeMarker = "https://img.shields.io/"
	count := strings.Count(text, badgeMarker)
	const minBadges = 3
	if count < minBadges {
		t.Fatalf("README.md contains %d img.shields.io badge URLs; expected ≥ %d (license + tests + release + go-version + brew per H-5 spec)", count, minBadges)
	}

	if !strings.Contains(text, "License-MIT") {
		t.Fatalf("README.md missing canonical MIT license badge (img.shields.io/badge/License-MIT-*)")
	}
}
