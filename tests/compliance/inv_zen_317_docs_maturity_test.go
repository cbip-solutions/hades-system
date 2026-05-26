// SPDX-License-Identifier: MIT

//go:build !race
// +build !race

// Package compliance — Plan 15 Phase H task H-8 compliance umbrella test
// for inv-zen-317 (H-full doc maturity bundle).
//
// inv-zen-317 is the umbrella invariant for the doc-maturity bundle that
// turns the v1.0 source tree into a portfolio-grade OSS surface. H-1..H-7
// each shipped a slice of the bundle; H-8 composes the umbrella that
// asserts every doc:
//
//   - is PRESENT at its canonical path,
//   - is non-empty + within a sane size range,
//   - carries at least one canonical-content sentinel string proving the
//     intended content (not just an empty file matching the path).
//
// This umbrella sits ABOVE the finer-grained per-doc tests (which own
// content-specific assertions like "Apache-2.0 sovereignty" absence in
// the README or `Contributor Covenant` literal in CODE_OF_CONDUCT). When
// the umbrella fails it surfaces a missing/empty/wrong-content file at
// the doc-bundle level; when a per-doc test fails it surfaces drift in
// the content semantics within a present file.
//
// Why this test exists: a v1.0 OSS public release MUST ship the standard
// doc-maturity bundle (CHANGELOG + LICENSE + SECURITY + CONTRIBUTING +
// CODE_OF_CONDUCT + README + INSTALL) per the GitHub community profile +
// CNCF Hosted Project requirements + sigstore/Docker/Kubernetes precedent.
// A single umbrella gate ensures no doc is deleted, renamed, or left
// empty by future refactors.
//
// Bundle composition (7 files; 1 already covered by inv-zen-219 via doc
// scan but verified here at the structural level):
//
//	docFile           Path                                  CanonicalSentinel
//	─────────────────────────────────────────────────────────────────────────
//	README.md         README.md                             "HADES system"
//	INSTALL.md        INSTALL.md                            "Homebrew" or "brew"
//	CHANGELOG.md      CHANGELOG.md                          "Keep a Changelog"
//	LICENSE           LICENSE                               "MIT License"
//	SECURITY.md       SECURITY.md                           "Reporting a vulnerability"
//	CONTRIBUTING.md   CONTRIBUTING.md                       "DCO sign-off"
//	CODE_OF_CONDUCT.md CODE_OF_CONDUCT.md                   "Contributor Covenant"
//
// (NOTICE is OPTIONAL per Plan 15 decisión 15 — operators MAY ship a
// minimal NOTICE for inbound Apache-2.0 propagation; the umbrella does
// NOT gate its presence to avoid false-positive on minimalist releases.)
//
// Composes into `make verify-invariants` via the standard compliance suite.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type docMaturityFile struct {
	relPath   string
	minLines  int
	maxLines  int
	sentinels []string
}

var docMaturityBundle = []docMaturityFile{
	{
		relPath:  "README.md",
		minLines: 200,
		maxLines: 2000,
		sentinels: []string{
			"HADES system",
			"hades-system",
		},
	},
	{
		relPath:  "INSTALL.md",
		minLines: 50,
		maxLines: 600,
		sentinels: []string{
			"brew install cbip-solutions/tap/hades",
			"Homebrew",
		},
	},
	{
		relPath:  "CHANGELOG.md",
		minLines: 100,
		maxLines: 10000,
		sentinels: []string{
			"Keep a Changelog",
			"## [v1.0.0]",
		},
	},
	{
		relPath:  "LICENSE",
		minLines: 5,
		maxLines: 200,
		sentinels: []string{
			"MIT License",
			"Permission is hereby granted",
		},
	},
	{
		relPath:  "SECURITY.md",
		minLines: 20,
		maxLines: 300,
		sentinels: []string{
			"Reporting a vulnerability",
			"GHSA",
		},
	},
	{
		relPath:  "CONTRIBUTING.md",
		minLines: 50,
		maxLines: 500,
		sentinels: []string{
			"DCO sign-off",
			"Conventional Commits",
		},
	},
	{
		relPath:  "CODE_OF_CONDUCT.md",
		minLines: 80,
		maxLines: 250,
		sentinels: []string{
			"Contributor Covenant",
		},
	},
}

func TestInvZen317_DocsMaturityUmbrellaBundleComplete(t *testing.T) {
	root := findRepoRoot(t)

	for _, doc := range docMaturityBundle {
		doc := doc
		t.Run(doc.relPath, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(root, doc.relPath)

			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				t.Fatalf("doc-maturity bundle missing file: %s does not exist", path)
			}
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.IsDir() {
				t.Fatalf("doc-maturity bundle entry %s is a directory, expected a regular file", path)
			}
			if info.Size() == 0 {
				t.Fatalf("doc-maturity bundle entry %s is empty (zero-byte file)", path)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			lineCount := strings.Count(text, "\n")

			if len(data) > 0 && data[len(data)-1] != '\n' {
				lineCount++
			}
			if lineCount < doc.minLines {
				t.Fatalf("doc-maturity bundle entry %s has %d lines; expected ≥ %d (file may be a stub/placeholder)", doc.relPath, lineCount, doc.minLines)
			}
			if lineCount > doc.maxLines {
				t.Fatalf("doc-maturity bundle entry %s has %d lines; expected ≤ %d (file may have grown unboundedly; consider splitting)", doc.relPath, lineCount, doc.maxLines)
			}

			matched := false
			for _, sent := range doc.sentinels {
				if strings.Contains(text, sent) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("doc-maturity bundle entry %s missing canonical-content sentinel (any-of: %v) — file may have wrong content or be a placeholder", doc.relPath, doc.sentinels)
			}
		})
	}
}

func TestInvZen317_DocsMaturityUmbrellaCount(t *testing.T) {
	t.Parallel()
	const minBundleSize = 7
	if len(docMaturityBundle) < minBundleSize {
		t.Fatalf("doc-maturity bundle enumeration shrunk to %d entries; expected ≥ %d (README + INSTALL + CHANGELOG + LICENSE + SECURITY + CONTRIBUTING + CODE_OF_CONDUCT)", len(docMaturityBundle), minBundleSize)
	}

	required := []string{
		"README.md",
		"INSTALL.md",
		"CHANGELOG.md",
		"LICENSE",
		"SECURITY.md",
		"CONTRIBUTING.md",
		"CODE_OF_CONDUCT.md",
	}
	present := make(map[string]bool, len(docMaturityBundle))
	for _, d := range docMaturityBundle {
		present[d.relPath] = true
	}
	for _, p := range required {
		if !present[p] {
			t.Fatalf("doc-maturity bundle enumeration missing canonical path %q; expected v1.0 H-full bundle to enumerate all of %v", p, required)
		}
	}
}
