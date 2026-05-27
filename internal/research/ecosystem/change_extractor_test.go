package ecosystem_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// TestParseChangelog_KeepAChangelog verifies parsing of Keep-a-Changelog format.
// Format `## [1.2.0] - 2024-01-15` sections with `### Added` / `### Removed` /
// `### Changed` / `### Deprecated` / `### Fixed` / `### Security` subsections.
//
// Load-bearing assertion: every emitted ChangeNode carries
// SourceExtracted="explicit_changelog" — the tag E-4 (deepdiff_inferred)
// and E-5 (haiku_inferred) MUST differ from.
func TestParseChangelog_KeepAChangelog(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 1, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.1.0",
		VersionTo:      "1.2.0",
		FormatDetected: "keep-a-changelog",
		RawText: `# Changelog

## [1.2.0] - 2024-01-15

### Added
- crypto/sha256.Sum512 function for larger hashes
- crypto/sha256.New224 constructor

### Removed
- crypto/sha256.OldSum function (deprecated in 1.1.0)

### Changed
- crypto/sha256.Sum256 now returns [32]byte instead of []byte

## [1.1.0] - 2023-06-10

### Deprecated
- crypto/sha256.OldSum function
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) == 0 {
		t.Fatal("expected change nodes from Keep-a-Changelog parse, got 0")
	}

	var foundAdded, foundRemoved, foundChanged bool
	for _, n := range nodes {
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s got %q (node: %+v)", ecosystem.SourceExplicitChangelog, n.SourceExtracted, n)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
		if n.VersionFrom != "1.1.0" || n.VersionTo != "1.2.0" {
			t.Errorf("want VersionFrom=1.1.0 VersionTo=1.2.0 got %s/%s", n.VersionFrom, n.VersionTo)
		}
		switch n.ChangeType {
		case ecosystem.ChangeAdded:
			foundAdded = true
		case ecosystem.ChangeRemoved:
			foundRemoved = true
		case ecosystem.ChangeChanged:
			foundChanged = true
		}
	}
	if !foundAdded {
		t.Error("want at least one ChangeAdded node from [1.2.0] Added section")
	}
	if !foundRemoved {
		t.Error("want at least one ChangeRemoved node from [1.2.0] Removed section")
	}
	if !foundChanged {
		t.Error("want at least one ChangeChanged node from [1.2.0] Changed section")
	}
}

// TestParseChangelog_KeepAChangelog_FixedSecurityToChanged verifies that
// `### Fixed` and `### Security` subsections map to ChangeChanged (per
// plan-file spec line 1600-1601).
func TestParseChangelog_KeepAChangelog_FixedSecurityToChanged(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 1, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.1.0",
		VersionTo:      "1.2.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.2.0] - 2024-01-15

### Fixed
- crypto/sha256.Sum256 panic on nil input

### Security
- crypto/sha256.timing attack hardening
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) < 2 {
		t.Fatalf("expected ≥2 nodes from Fixed+Security sections, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeChanged {
			t.Errorf("want ChangeChanged for Fixed/Security got %s (desc=%s)", n.ChangeType, n.Description)
		}
	}
}

func TestParseChangelog_KeepAChangelog_OnlyTargetVersion(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 1, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.1.0",
		VersionTo:      "1.2.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.2.0] - 2024-01-15

### Added
- shiny new feature

## [1.1.0] - 2023-06-10

### Deprecated
- old crufty thing
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) != 1 {
		t.Fatalf("expected exactly 1 node (only [1.2.0] section), got %d: %+v", len(nodes), nodes)
	}
	if nodes[0].ChangeType != ecosystem.ChangeAdded {
		t.Errorf("want ChangeAdded got %s", nodes[0].ChangeType)
	}
	if strings.Contains(nodes[0].Description, "crufty") {
		t.Errorf("want only [1.2.0] entries, leaked [1.1.0] entry: %s", nodes[0].Description)
	}
}

func TestParseChangelog_GitHubRelease(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 2, Ecosystem: ecosystem.EcoPython, Name: "requests"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "2.31.0",
		VersionTo:      "2.32.0",
		FormatDetected: "github-release",
		RawText: `## What's Changed

* Add support for HTTP/3 by @contributor in #1234
* Fix connection pooling memory leak by @contributor in #1235
* Deprecate Session.mount() method in favor of Session.adapter() by @contributor in #1236
* Remove Python 3.7 support by @contributor in #1237

**Full Changelog**: https://github.com/psf/requests/compare/v2.31.0...v2.32.0
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) == 0 {
		t.Fatal("expected change nodes from GitHub release parse, got 0")
	}
	for _, n := range nodes {
		if n.VersionFrom != "2.31.0" || n.VersionTo != "2.32.0" {
			t.Errorf("want VersionFrom=2.31.0 VersionTo=2.32.0 got %s/%s", n.VersionFrom, n.VersionTo)
		}
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceExplicitChangelog, n.SourceExtracted)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
	}

	var sawAdded, sawChanged, sawDeprecated, sawRemoved bool
	for _, n := range nodes {
		switch n.ChangeType {
		case ecosystem.ChangeAdded:
			sawAdded = true
		case ecosystem.ChangeChanged:
			sawChanged = true
		case ecosystem.ChangeDeprecated:
			sawDeprecated = true
		case ecosystem.ChangeRemoved:
			sawRemoved = true
		}
	}
	if !sawAdded {
		t.Error("want classifier to map 'Add support...' to ChangeAdded")
	}
	if !sawChanged {
		t.Error("want classifier to map 'Fix connection...' to ChangeChanged")
	}
	if !sawDeprecated {
		t.Error("want classifier to map 'Deprecate Session.mount()...' to ChangeDeprecated")
	}
	if !sawRemoved {
		t.Error("want classifier to map 'Remove Python 3.7...' to ChangeRemoved")
	}
}

func TestParseChangelog_SemanticRelease(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 3, Ecosystem: ecosystem.EcoTypeScript, Name: "@angular/core"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "17.0.0",
		VersionTo:      "17.1.0",
		FormatDetected: "semantic-release",
		RawText: `# [17.1.0](https://github.com/angular/angular/compare/17.0.0...17.1.0) (2024-01-15)

### Bug Fixes

* **core:** fix memory leak in injector ([#53210](https://github.com/angular/angular/issues/53210)) ([abc1234](commit))

### Features

* **core:** add signal-based inputs ([#53023](https://github.com/angular/angular/issues/53023)) ([def5678](commit))

### BREAKING CHANGES

* **router:** RouterModule.forRoot now requires explicit config object
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) == 0 {
		t.Fatal("expected change nodes from semantic-release parse, got 0")
	}

	var foundFeature, foundBreaking, foundBugFix bool
	for _, n := range nodes {
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceExplicitChangelog, n.SourceExtracted)
		}
		switch {
		case n.ChangeType == ecosystem.ChangeAdded && strings.Contains(n.Description, "signal-based"):
			foundFeature = true
		case strings.Contains(n.Description, "RouterModule.forRoot"):
			foundBreaking = true
		case n.ChangeType == ecosystem.ChangeChanged && strings.Contains(n.Description, "memory leak"):
			foundBugFix = true
		}
	}
	if !foundFeature {
		t.Error("want at least one ChangeAdded node from Features section ('signal-based inputs')")
	}
	if !foundBreaking {
		t.Error("want at least one node from BREAKING CHANGES section ('RouterModule.forRoot')")
	}
	if !foundBugFix {
		t.Error("want at least one ChangeChanged node from Bug Fixes section ('memory leak')")
	}
}

func TestParseChangelog_SemanticRelease_StripsScopeAndReferences(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 3, Ecosystem: ecosystem.EcoTypeScript, Name: "@angular/core"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "17.0.0",
		VersionTo:      "17.1.0",
		FormatDetected: "semantic-release",
		RawText: `# [17.1.0](https://github.com/angular/angular/compare/17.0.0...17.1.0) (2024-01-15)

### Features

* **router:** add signal-based router state ([#1234](issue-url)) ([abcdef](commit-url))
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) != 1 {
		t.Fatalf("expected exactly 1 node, got %d", len(nodes))
	}
	desc := nodes[0].Description
	if strings.Contains(desc, "**router:**") {
		t.Errorf("want **router:** scope stripped, got: %s", desc)
	}
	if strings.Contains(desc, "[#1234]") || strings.Contains(desc, "([abcdef]") {
		t.Errorf("want issue/commit references stripped, got: %s", desc)
	}
	if !strings.Contains(desc, "add signal-based router state") {
		t.Errorf("want core description preserved, got: %s", desc)
	}
}

func TestParseChangelog_RawText(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 4, Ecosystem: ecosystem.EcoRust, Name: "tokio"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.35.0",
		VersionTo:      "1.36.0",
		FormatDetected: "raw",
		RawText:        `Added async-fn-in-trait support. Removed deprecated task::spawn_blocking_named. Changed io::BufReader buffer size default.`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) == 0 {
		t.Fatal("expected at least one change node from raw-text fallback, got 0")
	}
	for _, n := range nodes {
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceExplicitChangelog, n.SourceExtracted)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
		if n.VersionFrom != "1.35.0" || n.VersionTo != "1.36.0" {
			t.Errorf("want VersionFrom=1.35.0 VersionTo=1.36.0 got %s/%s", n.VersionFrom, n.VersionTo)
		}
	}
}

func TestParseChangelog_UnknownFormat(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 6, Ecosystem: ecosystem.EcoGo, Name: "go"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.22",
		VersionTo:      "1.23",
		FormatDetected: "go-release-notes",
		RawText:        `Added new package slog. Removed go/build internal helpers.`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) == 0 {
		t.Fatal("expected raw fallback to emit nodes for unknown format, got 0")
	}
	for _, n := range nodes {
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceExplicitChangelog, n.SourceExtracted)
		}
	}
}

func TestParseChangelog_EmptyRawText(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 5, Ecosystem: ecosystem.EcoGo, Name: "stdlib"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.22",
		VersionTo:      "1.23",
		FormatDetected: "keep-a-changelog",
		RawText:        "",
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) != 0 {
		t.Errorf("want 0 nodes from empty changelog, got %d", len(nodes))
	}
}

func TestParseChangelog_NilChangelog(t *testing.T) {
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), nil)
	if nodes != nil {
		t.Errorf("want nil from nil changelog, got %v", nodes)
	}
}

func TestParseChangelog_SymbolPathExtraction(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 7, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	changelog := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Added
- crypto/sha256.Sum512 function for larger hashes
- bare text without symbol path
`,
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), changelog)

	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}

	var withSymbol, withoutSymbol bool
	for _, n := range nodes {
		if n.SymbolPath == "crypto/sha256.Sum512" {
			withSymbol = true
		}
		if n.SymbolPath == "" && strings.Contains(n.Description, "bare text") {
			withoutSymbol = true
		}
	}
	if !withSymbol {
		t.Error("want symbol path 'crypto/sha256.Sum512' extracted from bullet text")
	}
	if !withoutSymbol {
		t.Error("want empty symbol path for bullet without recognizable Go-style ident")
	}
}

func TestParseChangelog_RawTextClassifierBranches(t *testing.T) {
	cases := []struct {
		name string
		text string
		want ecosystem.ChangeType
	}{
		{"deprecate", "Deprecate the old Session.mount API.", ecosystem.ChangeDeprecated},
		{"remove", "Remove Python 3.7 support entirely.", ecosystem.ChangeRemoved},
		{"drop-support", "Drop support for legacy clients.", ecosystem.ChangeRemoved},
		{"add-prefix", "Add new metrics endpoint for observability.", ecosystem.ChangeAdded},
		{"new-keyword", "New caching layer in the dispatcher.", ecosystem.ChangeAdded},
		{"introduc-keyword", "Introduce signal-based reactivity model.", ecosystem.ChangeAdded},
		{"move-keyword", "Move helpers from util to common package.", ecosystem.ChangeMoved},
		{"rename-keyword", "Rename internal mod to internal moddel.", ecosystem.ChangeMoved},
		{"default-changed", "Tweak default buffer size for io.Reader.", ecosystem.ChangeChanged},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := ecosystem.PackageRef{ID: 99, Ecosystem: ecosystem.EcoGo, Name: "x"}
			cl := &ecosystem.Changelog{
				Package:        pkg,
				VersionFrom:    "1.0.0",
				VersionTo:      "1.1.0",
				FormatDetected: "raw",
				RawText:        tc.text,
			}
			ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
			nodes := ce.ParseChangelog(context.Background(), cl)
			if len(nodes) == 0 {
				t.Fatalf("want ≥1 node for %q, got 0", tc.text)
			}
			if nodes[0].ChangeType != tc.want {
				t.Errorf("want %s for %q, got %s", tc.want, tc.text, nodes[0].ChangeType)
			}
		})
	}
}

func TestParseChangelog_SemanticReleaseHeadingBranches(t *testing.T) {
	cases := []struct {
		name    string
		heading string
		want    ecosystem.ChangeType
	}{
		{"features", "Features", ecosystem.ChangeAdded},
		{"bug-fixes", "Bug Fixes", ecosystem.ChangeChanged},
		{"breaking", "BREAKING CHANGES", ecosystem.ChangeChanged},
		{"deprecations", "Deprecations", ecosystem.ChangeDeprecated},
		{"removals", "Removals", ecosystem.ChangeRemoved},
		{"performance", "Performance Improvements", ecosystem.ChangeChanged},
		{"unknown-default", "Miscellaneous", ecosystem.ChangeChanged},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := ecosystem.PackageRef{ID: 88, Ecosystem: ecosystem.EcoTypeScript, Name: "@x/y"}
			cl := &ecosystem.Changelog{
				Package:        pkg,
				VersionFrom:    "1.0.0",
				VersionTo:      "1.1.0",
				FormatDetected: "semantic-release",
				RawText: `# [1.1.0](url) (2024-01-15)

### ` + tc.heading + `

* example entry
`,
			}
			ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
			nodes := ce.ParseChangelog(context.Background(), cl)
			if len(nodes) != 1 {
				t.Fatalf("want exactly 1 node for heading %q, got %d", tc.heading, len(nodes))
			}
			if nodes[0].ChangeType != tc.want {
				t.Errorf("heading %q: want %s, got %s", tc.heading, tc.want, nodes[0].ChangeType)
			}
		})
	}
}

func TestParseChangelog_KeepAChangelogUnknownSubsection(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 11, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Refactored
- some refactor work
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].ChangeType != ecosystem.ChangeChanged {
		t.Errorf("want unknown subsection 'Refactored' to default to ChangeChanged, got %s", nodes[0].ChangeType)
	}
}

func TestParseChangelog_KeepAChangelogEmptySubsection(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 12, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Added

### Removed
- crypto/sha256.OldSum function
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 {
		t.Fatalf("want exactly 1 node (only Removed has bullet), got %d", len(nodes))
	}
	if nodes[0].ChangeType != ecosystem.ChangeRemoved {
		t.Errorf("want ChangeRemoved, got %s", nodes[0].ChangeType)
	}
}

func TestParseChangelog_KeepAChangelogDeprecated(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 70, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Deprecated
- crypto/sha256.OldSum function — use Sum256 instead
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].ChangeType != ecosystem.ChangeDeprecated {
		t.Errorf("want ChangeDeprecated, got %s", nodes[0].ChangeType)
	}
	if nodes[0].SymbolPath != "crypto/sha256.OldSum" {
		t.Errorf("want SymbolPath=crypto/sha256.OldSum, got %q", nodes[0].SymbolPath)
	}
}

// TestParseChangelog_KeepAChangelog_Fixed verifies `### Fixed` subsection
// alone (without Security) routes to ChangeChanged via headingToChangeType.
// Locks the "fixed → changed" branch independent of the Security path.
func TestParseChangelog_KeepAChangelogFixedAlone(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 71, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Fixed
- nil pointer panic in cache eviction
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 || nodes[0].ChangeType != ecosystem.ChangeChanged {
		t.Fatalf("want 1 ChangeChanged node from Fixed, got %d nodes (first type=%v)", len(nodes), nodes[0].ChangeType)
	}
}

// TestParseChangelog_KeepAChangelog_Security verifies `### Security`
// subsection alone (without Fixed) routes to ChangeChanged.
func TestParseChangelog_KeepAChangelogSecurityAlone(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 72, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Security
- harden TLS cipher list
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 || nodes[0].ChangeType != ecosystem.ChangeChanged {
		t.Fatalf("want 1 ChangeChanged node from Security, got %d nodes", len(nodes))
	}
}

func TestParseChangelog_GitHubReleaseSkipsNonBullets(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 73, Ecosystem: ecosystem.EcoPython, Name: "requests"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "2.31.0",
		VersionTo:      "2.32.0",
		FormatDetected: "github-release",
		RawText: `## What's Changed

Some intro prose that should be skipped.

* Add real bullet by @x in #1

**Full Changelog**: https://example.com/compare/v1...v2
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 {
		t.Fatalf("want exactly 1 node (only the * bullet), got %d", len(nodes))
	}
	if !strings.Contains(nodes[0].Description, "Add real bullet") {
		t.Errorf("want 'Add real bullet' description, got %q", nodes[0].Description)
	}
}

func TestParseChangelog_GitHubReleaseDashBullets(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 74, Ecosystem: ecosystem.EcoPython, Name: "requests"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "2.31.0",
		VersionTo:      "2.32.0",
		FormatDetected: "github-release",
		RawText: `- Fix TLS validation bug by @x in #1
- Add new connection pool option by @y in #2
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes from dash-bullets, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.SourceExtracted != ecosystem.SourceExplicitChangelog {
			t.Errorf("want SourceExtracted=%s, got %q", ecosystem.SourceExplicitChangelog, n.SourceExtracted)
		}
	}
}

func TestParseChangelog_RawTextOnlyShortFragments(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 75, Ecosystem: ecosystem.EcoRust, Name: "tokio"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.35.0",
		VersionTo:      "1.36.0",
		FormatDetected: "raw",

		RawText: "a. b. c. d",
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 1 {
		t.Fatalf("want exactly 1 whole-text fallback node, got %d", len(nodes))
	}
	if nodes[0].Description != "a. b. c. d" {
		t.Errorf("want full RawText as description, got %q", nodes[0].Description)
	}
}

func TestParseChangelog_RawTextWhitespaceOnly(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 76, Ecosystem: ecosystem.EcoRust, Name: "tokio"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "raw",
		RawText:        "   \n\t  \n  ",
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 0 {
		t.Errorf("want 0 nodes from whitespace-only RawText, got %d", len(nodes))
	}
}

func TestParseChangelog_KeepAChangelogStarBullets(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 13, Ecosystem: ecosystem.EcoGo, Name: "mylib"}
	cl := &ecosystem.Changelog{
		Package:        pkg,
		VersionFrom:    "1.0.0",
		VersionTo:      "1.1.0",
		FormatDetected: "keep-a-changelog",
		RawText: `## [1.1.0] - 2024-01-15

### Added
* star-bullet entry one
- dash-bullet entry two
`,
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.ParseChangelog(context.Background(), cl)
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes (star + dash), got %d: %+v", len(nodes), nodes)
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeAdded {
			t.Errorf("want ChangeAdded, got %s", n.ChangeType)
		}
	}
}

// =============================================================================
// Task E-4 — ChangeExtractor.DeepDiff implicit Change node generator tests
//
// Load-bearing contract:
// - Every DeepDiff-emitted node carries SourceExtracted="implicit_deepdiff"
// (constant ecosystem.SourceImplicitDeepDiff) — the tag E-3
// "explicit_changelog" and future E-5 "haiku_inferred" MUST differ from.
// - DeepDiff is deterministic (no LLM): same inputs → same node set
// (order may vary across runs due to map iteration; downstream sorts
// by SymbolPath if total order matters).
// - Nil inputs MUST NOT panic.
// =============================================================================

func TestDeepDiff_AddedSymbols(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 1, Ecosystem: ecosystem.EcoGo, Name: "crypto/sha256"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "1.22",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.22"},
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "crypto/sha256.New", Version: "1.22"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "1.23",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "crypto/sha256.New", Version: "1.23"},
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "crypto/sha256.Sum512", Version: "1.23"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	var foundAdded bool
	for _, n := range nodes {
		if n.SymbolPath == "crypto/sha256.Sum512" && n.ChangeType == ecosystem.ChangeAdded {
			foundAdded = true
		}
		if n.SourceExtracted != ecosystem.SourceImplicitDeepDiff {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceImplicitDeepDiff, n.SourceExtracted)
		}
		// Sanity raw string match — the load-bearing tag value MUST be
		// exactly "implicit_deepdiff" (not just the constant name).
		if n.SourceExtracted != "implicit_deepdiff" {
			t.Errorf("want SourceExtracted=implicit_deepdiff (raw), got %q", n.SourceExtracted)
		}
	}
	if !foundAdded {
		t.Error("want ChangeAdded node for crypto/sha256.Sum512")
	}
	if len(nodes) != 1 {
		t.Errorf("want exactly 1 node (only Sum512 added), got %d: %+v", len(nodes), nodes)
	}
}

func TestDeepDiff_RemovedSymbols(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 2, Ecosystem: ecosystem.EcoPython, Name: "asyncio"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "3.11",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "asyncio.coroutine", Version: "3.11"},
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "asyncio.ensure_future", Version: "3.11"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "3.12",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "asyncio.ensure_future", Version: "3.12"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	var foundRemoved bool
	for _, n := range nodes {
		if n.SymbolPath == "asyncio.coroutine" && n.ChangeType == ecosystem.ChangeRemoved {
			foundRemoved = true
		}
		if n.SourceExtracted != ecosystem.SourceImplicitDeepDiff {
			t.Errorf("want SourceExtracted=%s got %q", ecosystem.SourceImplicitDeepDiff, n.SourceExtracted)
		}
	}
	if !foundRemoved {
		t.Error("want ChangeRemoved node for asyncio.coroutine")
	}
	if len(nodes) != 1 {
		t.Errorf("want exactly 1 node (only coroutine removed), got %d: %+v", len(nodes), nodes)
	}
}

// TestDeepDiff_UnchangedSymbols verifies unchanged symbols do NOT produce
// any nodes. This is load-bearing for graph storage cost — the implicit
// diff MUST NOT amplify graph density with no-op nodes.
func TestDeepDiff_UnchangedSymbols(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 3, Ecosystem: ecosystem.EcoRust, Name: "std"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.77",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "std::vec::Vec", Version: "1.77"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.78",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "std::vec::Vec", Version: "1.78"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	if len(nodes) != 0 {
		t.Errorf("want 0 nodes for unchanged symbol set, got %d: %+v", len(nodes), nodes)
	}
}

func TestDeepDiff_BothNil(t *testing.T) {
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), nil, nil)
	if nodes == nil {
		t.Error("want non-nil empty slice for both-nil inputs (caller may len-check), got nil")
	}
	if len(nodes) != 0 {
		t.Errorf("want 0 nodes from nil inputs, got %d", len(nodes))
	}
}

func TestDeepDiff_EmptyOldDoc(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 4, Ecosystem: ecosystem.EcoTypeScript, Name: "typescript"}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "5.0",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoTypeScript, SymbolPath: "typescript.createProgram", Version: "5.0"},
			{Ecosystem: ecosystem.EcoTypeScript, SymbolPath: "typescript.parseConfigFile", Version: "5.0"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), nil, newDoc)

	if len(nodes) != 2 {
		t.Fatalf("want 2 ChangeAdded nodes (one per newDoc symbol), got %d: %+v", len(nodes), nodes)
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeAdded {
			t.Errorf("want ChangeAdded, got %s (node: %+v)", n.ChangeType, n)
		}
		if n.VersionTo != "5.0" {
			t.Errorf("want VersionTo=5.0 got %q", n.VersionTo)
		}
		if n.VersionFrom != "" {
			t.Errorf("want VersionFrom=\"\" (nil oldDoc), got %q", n.VersionFrom)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
	}
}

func TestDeepDiff_EmptyOldDocStruct(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 4, Ecosystem: ecosystem.EcoTypeScript, Name: "typescript"}
	oldDoc := &ecosystem.PackageDoc{Package: pkg, Version: "4.9", Symbols: nil}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "5.0",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoTypeScript, SymbolPath: "typescript.createProgram", Version: "5.0"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	if len(nodes) == 0 {
		t.Fatal("want at least one ChangeAdded node when old symbols is empty")
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeAdded {
			t.Errorf("want ChangeAdded, got %s", n.ChangeType)
		}
		if n.VersionFrom != "4.9" {
			t.Errorf("want VersionFrom=4.9 (from non-nil oldDoc), got %q", n.VersionFrom)
		}
	}
}

func TestDeepDiff_EmptyNewDoc(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 5, Ecosystem: ecosystem.EcoGo, Name: "deprecated/pkg"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "1.0",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "deprecated/pkg.OldFunc", Version: "1.0"},
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "deprecated/pkg.OldType", Version: "1.0"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, nil)

	if len(nodes) != 2 {
		t.Fatalf("want 2 ChangeRemoved nodes, got %d: %+v", len(nodes), nodes)
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeRemoved {
			t.Errorf("want ChangeRemoved, got %s", n.ChangeType)
		}
		if n.VersionFrom != "1.0" {
			t.Errorf("want VersionFrom=1.0 got %q", n.VersionFrom)
		}
		if n.VersionTo != "" {
			t.Errorf("want VersionTo=\"\" (nil newDoc), got %q", n.VersionTo)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
	}
}

func TestDeepDiff_EmptyNewDocStruct(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 5, Ecosystem: ecosystem.EcoGo, Name: "deprecated/pkg"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg,
		Version: "1.0",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "deprecated/pkg.OldFunc", Version: "1.0"},
		},
	}
	newDoc := &ecosystem.PackageDoc{Package: pkg, Version: "2.0", Symbols: nil}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	if len(nodes) == 0 {
		t.Fatal("want at least one ChangeRemoved node when new symbols is empty")
	}
	for _, n := range nodes {
		if n.ChangeType != ecosystem.ChangeRemoved {
			t.Errorf("want ChangeRemoved, got %s", n.ChangeType)
		}
		if n.VersionTo != "2.0" {
			t.Errorf("want VersionTo=2.0 (from non-nil newDoc), got %q", n.VersionTo)
		}
	}
}

func TestDeepDiff_VersionMetadata(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 6, Ecosystem: ecosystem.EcoGo, Name: "log/slog"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.21",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "log/slog.New", Version: "1.21"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.22",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "log/slog.New", Version: "1.22"},
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "log/slog.NewLogger", Version: "1.22"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	if len(nodes) == 0 {
		t.Fatal("want at least one node for log/slog.NewLogger addition")
	}
	for _, n := range nodes {
		if n.VersionFrom != "1.21" {
			t.Errorf("want VersionFrom=1.21 got %q", n.VersionFrom)
		}
		if n.VersionTo != "1.22" {
			t.Errorf("want VersionTo=1.22 got %q", n.VersionTo)
		}
		if n.PackageID != pkg.ID {
			t.Errorf("want PackageID=%d got %d", pkg.ID, n.PackageID)
		}
	}
}

func TestDeepDiff_AddedAndRemovedCombined(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 7, Ecosystem: ecosystem.EcoPython, Name: "numpy"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.25",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "numpy.array", Version: "1.25"},
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "numpy.matrix", Version: "1.25"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "2.0",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "numpy.array", Version: "2.0"},
			{Ecosystem: ecosystem.EcoPython, SymbolPath: "numpy.strings", Version: "2.0"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes (1 added + 1 removed), got %d: %+v", len(nodes), nodes)
	}
	var sawAdded, sawRemoved bool
	for _, n := range nodes {
		switch {
		case n.SymbolPath == "numpy.strings" && n.ChangeType == ecosystem.ChangeAdded:
			sawAdded = true
		case n.SymbolPath == "numpy.matrix" && n.ChangeType == ecosystem.ChangeRemoved:
			sawRemoved = true
		default:
			t.Errorf("unexpected node: %+v", n)
		}
	}
	if !sawAdded {
		t.Error("want ChangeAdded for numpy.strings")
	}
	if !sawRemoved {
		t.Error("want ChangeRemoved for numpy.matrix")
	}
}

func TestDeepDiff_Descriptions(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 8, Ecosystem: ecosystem.EcoGo, Name: "encoding/json"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.21",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "encoding/json.OldEncoder", Version: "1.21"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.22",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "encoding/json.NewEncoder", Version: "1.22"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	for _, n := range nodes {
		if n.Description == "" {
			t.Errorf("want non-empty Description for deterministic template, got empty (node: %+v)", n)
		}
		if !strings.Contains(n.Description, n.SymbolPath) {
			t.Errorf("want Description to mention SymbolPath=%q, got %q", n.SymbolPath, n.Description)
		}
	}
}

func TestDeepDiff_Determinism(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 9, Ecosystem: ecosystem.EcoRust, Name: "tokio"}
	oldDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.35",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::spawn", Version: "1.35"},
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::select", Version: "1.35"},
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::main", Version: "1.35"},
		},
	}
	newDoc := &ecosystem.PackageDoc{
		Package: pkg, Version: "1.36",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::spawn", Version: "1.36"},
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::join", Version: "1.36"},
			{Ecosystem: ecosystem.EcoRust, SymbolPath: "tokio::pin", Version: "1.36"},
		},
	}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})

	canonicalize := func(nodes []ecosystem.ChangeNode) map[string]ecosystem.ChangeType {
		out := make(map[string]ecosystem.ChangeType, len(nodes))
		for _, n := range nodes {
			out[n.SymbolPath] = n.ChangeType
		}
		return out
	}

	first := canonicalize(ce.DeepDiff(context.Background(), oldDoc, newDoc))
	for i := 0; i < 50; i++ {
		got := canonicalize(ce.DeepDiff(context.Background(), oldDoc, newDoc))
		if len(got) != len(first) {
			t.Fatalf("non-deterministic node count: iter %d got %d, first %d", i, len(got), len(first))
		}
		for k, v := range first {
			if got[k] != v {
				t.Errorf("non-deterministic node for %q: iter %d got %s, first %s", k, i, got[k], v)
			}
		}
	}

	if len(first) != 4 {
		t.Errorf("want exactly 4 nodes (2 added + 2 removed), got %d: %+v", len(first), first)
	}
}

func TestDeepDiff_LargeDoc(t *testing.T) {
	const N = 500
	pkg := ecosystem.PackageRef{ID: 10, Ecosystem: ecosystem.EcoPython, Name: "scipy"}
	oldSymbols := make([]ecosystem.SymbolRef, N)
	newSymbols := make([]ecosystem.SymbolRef, N)
	for i := 0; i < N; i++ {
		oldSymbols[i] = ecosystem.SymbolRef{
			Ecosystem:  ecosystem.EcoPython,
			SymbolPath: "scipy.sym_" + strconv.Itoa(i),
			Version:    "1.10",
		}
	}

	for i := 0; i < N/2; i++ {
		newSymbols[i] = ecosystem.SymbolRef{
			Ecosystem:  ecosystem.EcoPython,
			SymbolPath: "scipy.sym_" + strconv.Itoa(i),
			Version:    "1.11",
		}
	}
	for i := N / 2; i < N; i++ {
		newSymbols[i] = ecosystem.SymbolRef{
			Ecosystem:  ecosystem.EcoPython,
			SymbolPath: "scipy.new_" + strconv.Itoa(i),
			Version:    "1.11",
		}
	}
	oldDoc := &ecosystem.PackageDoc{Package: pkg, Version: "1.10", Symbols: oldSymbols}
	newDoc := &ecosystem.PackageDoc{Package: pkg, Version: "1.11", Symbols: newSymbols}

	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	nodes := ce.DeepDiff(context.Background(), oldDoc, newDoc)

	wantAdded := N / 2
	wantRemoved := N - wantAdded
	wantTotal := wantAdded + wantRemoved
	if len(nodes) != wantTotal {
		t.Fatalf("want %d nodes (=%d added + %d removed), got %d", wantTotal, wantAdded, wantRemoved, len(nodes))
	}

	var addedCount, removedCount int
	for _, n := range nodes {
		switch n.ChangeType {
		case ecosystem.ChangeAdded:
			addedCount++
			if !strings.HasPrefix(n.SymbolPath, "scipy.new_") {
				t.Errorf("ChangeAdded for unexpected path %q", n.SymbolPath)
			}
		case ecosystem.ChangeRemoved:
			removedCount++
			if !strings.HasPrefix(n.SymbolPath, "scipy.sym_") {
				t.Errorf("ChangeRemoved for unexpected path %q", n.SymbolPath)
			}
		default:
			t.Errorf("unexpected ChangeType %s for %q", n.ChangeType, n.SymbolPath)
		}
	}
	if addedCount != wantAdded {
		t.Errorf("want %d added nodes, got %d", wantAdded, addedCount)
	}
	if removedCount != wantRemoved {
		t.Errorf("want %d removed nodes, got %d", wantRemoved, removedCount)
	}
}

// TestDeepDiff_SourceImplicitDeepDiffConstantValue is a guard test: the
// public constant ecosystem.SourceImplicitDeepDiff MUST equal the raw
// string "implicit_deepdiff". This contract is load-bearing for downstream
// queries that filter ecosystem_changes WHERE source_extracted = 'implicit_deepdiff'.
func TestDeepDiff_SourceImplicitDeepDiffConstantValue(t *testing.T) {
	if ecosystem.SourceImplicitDeepDiff != "implicit_deepdiff" {
		t.Errorf("want SourceImplicitDeepDiff=\"implicit_deepdiff\", got %q",
			ecosystem.SourceImplicitDeepDiff)
	}

	if ecosystem.SourceImplicitDeepDiff == ecosystem.SourceExplicitChangelog {
		t.Errorf("SourceImplicitDeepDiff must differ from SourceExplicitChangelog (both = %q)",
			ecosystem.SourceImplicitDeepDiff)
	}
}

func TestDeepDiff_EmptyVersionFallbackDescriptions(t *testing.T) {
	pkg := ecosystem.PackageRef{ID: 11, Ecosystem: ecosystem.EcoGo, Name: "vendored/pkg"}

	oldEmptyVer := &ecosystem.PackageDoc{Package: pkg, Version: "", Symbols: nil}
	newEmptyVer := &ecosystem.PackageDoc{
		Package: pkg, Version: "",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "vendored/pkg.FreshSymbol", Version: ""},
		},
	}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	addedNodes := ce.DeepDiff(context.Background(), oldEmptyVer, newEmptyVer)
	if len(addedNodes) != 1 {
		t.Fatalf("want 1 added node, got %d", len(addedNodes))
	}
	if !strings.Contains(addedNodes[0].Description, "added in this version") {
		t.Errorf("want fallback description containing 'added in this version', got %q", addedNodes[0].Description)
	}

	oldRemovedEmpty := &ecosystem.PackageDoc{
		Package: pkg, Version: "",
		Symbols: []ecosystem.SymbolRef{
			{Ecosystem: ecosystem.EcoGo, SymbolPath: "vendored/pkg.GoneSymbol", Version: ""},
		},
	}
	newRemovedEmpty := &ecosystem.PackageDoc{Package: pkg, Version: "", Symbols: nil}
	removedNodes := ce.DeepDiff(context.Background(), oldRemovedEmpty, newRemovedEmpty)
	if len(removedNodes) != 1 {
		t.Fatalf("want 1 removed node, got %d", len(removedNodes))
	}
	if !strings.Contains(removedNodes[0].Description, "removed in this version") {
		t.Errorf("want fallback description containing 'removed in this version', got %q", removedNodes[0].Description)
	}
}

type mockHaikuDescriber struct {
	calls    []haikuCall
	response string
	err      error
}

type haikuCall struct {
	symbolPath  string
	changeType  ecosystem.ChangeType
	diffSummary string
}

func (m *mockHaikuDescriber) Describe(_ context.Context, symbolPath string, ct ecosystem.ChangeType, diffSummary string) (string, error) {
	m.calls = append(m.calls, haikuCall{symbolPath, ct, diffSummary})
	if m.err != nil {
		return "", m.err
	}
	if m.response != "" {
		return m.response, nil
	}
	return "enriched description for " + symbolPath, nil
}

func TestEnrichWithHaiku_MaxScope(t *testing.T) {
	mock := &mockHaikuDescriber{response: "Sum512 computes a SHA-512/256 hash of the input data"}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			PackageID:       1,
			VersionFrom:     "1.22",
			VersionTo:       "1.23",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum512",
			Description:     "crypto/sha256.Sum512 added in this version",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("want 1 node got %d", len(enriched))
	}
	if enriched[0].Description == input[0].Description {
		t.Errorf("want enriched description, got unchanged template %q", enriched[0].Description)
	}
	if enriched[0].Description != "Sum512 computes a SHA-512/256 hash of the input data" {
		t.Errorf("want exact Haiku response, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitHaiku {
		t.Errorf("want SourceExtracted=%s for Haiku-enriched node, got %q",
			ecosystem.SourceImplicitHaiku, enriched[0].SourceExtracted)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("want 1 Haiku call, got %d", len(mock.calls))
	}
	got := mock.calls[0]
	if got.symbolPath != "crypto/sha256.Sum512" {
		t.Errorf("want symbolPath threaded through, got %q", got.symbolPath)
	}
	if got.changeType != ecosystem.ChangeAdded {
		t.Errorf("want changeType=added threaded through, got %q", got.changeType)
	}
	if got.diffSummary != "crypto/sha256.Sum512 added in this version" {
		t.Errorf("want original template as diffSummary, got %q", got.diffSummary)
	}
}

func TestEnrichWithHaiku_DefaultDoctrine(t *testing.T) {
	mock := &mockHaikuDescriber{}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: false,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "crypto/sha256.Sum512 added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum512",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("want 0 Haiku calls in default doctrine, got %d", len(mock.calls))
	}
	for i, n := range enriched {
		if n.Description != input[i].Description {
			t.Errorf("want description unchanged at i=%d, got %q", i, n.Description)
		}
		if n.SourceExtracted != input[i].SourceExtracted {
			t.Errorf("want SourceExtracted unchanged at i=%d, got %q", i, n.SourceExtracted)
		}
	}
}

// TestEnrichWithHaiku_NilDescriber_NoPanic verifies LLMJudgeEnabled=true
// with a nil HaikuDescriber does NOT panic and returns nodes unchanged.
// This is defense-in-depth: a partially-wired dispatcher (e.g.,
// provider unavailable at construction time) MUST degrade gracefully
// rather than crash the ecosystem pipeline.
func TestEnrichWithHaiku_NilDescriber_NoPanic(t *testing.T) {
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  nil,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "foo.Bar added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "foo.Bar",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error with nil describer: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("want 1 node, got %d", len(enriched))
	}
	if enriched[0].Description != input[0].Description {
		t.Errorf("want description unchanged, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitDeepDiff {
		t.Errorf("want SourceExtracted unchanged, got %q", enriched[0].SourceExtracted)
	}
}

func TestEnrichWithHaiku_CapaFirewallBehavior(t *testing.T) {
	mock := &mockHaikuDescriber{}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "tokio.spawn added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "tokio.spawn",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Errorf("want 1 Haiku call in capa-firewall doctrine, got %d", len(mock.calls))
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitHaiku {
		t.Errorf("want SourceExtracted=%s after enrichment, got %q",
			ecosystem.SourceImplicitHaiku, enriched[0].SourceExtracted)
	}
}

func TestEnrichWithHaiku_HaikuFailureRetainsTemplate(t *testing.T) {
	mock := &mockHaikuDescriber{err: errSimulatedHaikuFailure}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "foo.Bar added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "foo.Bar",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("per-node Haiku failure must NOT surface error, got %v", err)
	}
	if len(mock.calls) != 1 {
		t.Errorf("want 1 Haiku call attempt, got %d", len(mock.calls))
	}
	if enriched[0].Description != input[0].Description {
		t.Errorf("want template Description retained on Haiku failure, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitDeepDiff {
		t.Errorf("want SourceExtracted unchanged on Haiku failure, got %q", enriched[0].SourceExtracted)
	}
}

func TestEnrichWithHaiku_PreservesExplicitChangelogDescriptions(t *testing.T) {
	mock := &mockHaikuDescriber{response: "should not be called"}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "crypto/sha256.Sum512 function for larger hashes",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum512",
			SourceExtracted: ecosystem.SourceExplicitChangelog,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("want 0 Haiku calls for non-template description, got %d", len(mock.calls))
	}
	if enriched[0].Description != input[0].Description {
		t.Errorf("want explicit-changelog Description preserved, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceExplicitChangelog {
		t.Errorf("want SourceExtracted=explicit_changelog preserved, got %q", enriched[0].SourceExtracted)
	}
}

func TestEnrichWithHaiku_MixedNodes(t *testing.T) {
	mock := &mockHaikuDescriber{response: "Haiku-enriched"}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "alpha.Sym added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "alpha.Sym",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
		{
			Description:     "Explicit human-authored description from CHANGELOG",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "beta.Sym",
			SourceExtracted: ecosystem.SourceExplicitChangelog,
		},
		{
			Description:     "gamma.Sym removed in this version",
			ChangeType:      ecosystem.ChangeRemoved,
			SymbolPath:      "gamma.Sym",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Errorf("want 2 Haiku calls (template nodes only), got %d", len(mock.calls))
	}

	if enriched[0].Description != "Haiku-enriched" {
		t.Errorf("want alpha.Sym description enriched, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitHaiku {
		t.Errorf("want alpha.Sym SourceExtracted=%s, got %q", ecosystem.SourceImplicitHaiku, enriched[0].SourceExtracted)
	}

	if enriched[1].Description != "Explicit human-authored description from CHANGELOG" {
		t.Errorf("want beta.Sym description preserved, got %q", enriched[1].Description)
	}
	if enriched[1].SourceExtracted != ecosystem.SourceExplicitChangelog {
		t.Errorf("want beta.Sym SourceExtracted preserved, got %q", enriched[1].SourceExtracted)
	}

	if enriched[2].Description != "Haiku-enriched" {
		t.Errorf("want gamma.Sym description enriched, got %q", enriched[2].Description)
	}
	if enriched[2].SourceExtracted != ecosystem.SourceImplicitHaiku {
		t.Errorf("want gamma.Sym SourceExtracted=%s, got %q", ecosystem.SourceImplicitHaiku, enriched[2].SourceExtracted)
	}
}

func TestEnrichWithHaiku_EmptyResponseRetainsTemplate(t *testing.T) {
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  &emptyResponseDescriber{},
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "delta.Sym added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "delta.Sym",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched[0].Description != input[0].Description {
		t.Errorf("want template retained on empty Haiku response, got %q", enriched[0].Description)
	}
	if enriched[0].SourceExtracted != ecosystem.SourceImplicitDeepDiff {
		t.Errorf("want SourceExtracted unchanged on empty Haiku response, got %q", enriched[0].SourceExtracted)
	}
}

type emptyResponseDescriber struct{}

func (emptyResponseDescriber) Describe(_ context.Context, _ string, _ ecosystem.ChangeType, _ string) (string, error) {
	return "", nil
}

func TestEnrichWithHaiku_NilNodes(t *testing.T) {
	mock := &mockHaikuDescriber{}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	enriched, err := ce.EnrichWithHaiku(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error on nil input: %v", err)
	}
	if enriched != nil {
		t.Errorf("want nil result for nil input, got %d nodes", len(enriched))
	}
	if len(mock.calls) != 0 {
		t.Errorf("want 0 Haiku calls on nil input, got %d", len(mock.calls))
	}
}

func TestEnrichWithHaiku_EmptySlice(t *testing.T) {
	mock := &mockHaikuDescriber{}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	enriched, err := ce.EnrichWithHaiku(context.Background(), []ecosystem.ChangeNode{})
	if err != nil {
		t.Fatalf("unexpected error on empty slice: %v", err)
	}
	if enriched == nil {
		t.Error("want non-nil empty slice for empty input, got nil")
	}
	if len(enriched) != 0 {
		t.Errorf("want len(enriched)==0, got %d", len(enriched))
	}
	if len(mock.calls) != 0 {
		t.Errorf("want 0 Haiku calls on empty input, got %d", len(mock.calls))
	}
}

func TestEnrichWithHaiku_DoesNotMutateInput(t *testing.T) {
	mock := &mockHaikuDescriber{response: "mutated by Haiku"}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{
		{
			Description:     "epsilon.Sym added in this version",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "epsilon.Sym",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}
	originalDesc := input[0].Description
	originalSource := input[0].SourceExtracted

	_, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input[0].Description != originalDesc {
		t.Errorf("input slice mutated: want Description=%q, got %q", originalDesc, input[0].Description)
	}
	if input[0].SourceExtracted != originalSource {
		t.Errorf("input slice mutated: want SourceExtracted=%q, got %q", originalSource, input[0].SourceExtracted)
	}
}

// TestEnrichWithHaiku_RecognisesNonEmptyVersionTemplates covers the
// isTemplateLikeDescription branches that match deterministic templates
// emitted with a non-empty version (e.g., "foo.Bar added in version 1.2.3"
// and "foo.Bar removed (last present in version 1.2.2)"). These are the
// hot-path templates from deterministicAddedDescription /
// deterministicRemovedDescription when versionTo/versionFrom is non-empty
// — they MUST also trigger enrichment, otherwise the entire "we have
// version metadata" common case would skip Haiku.
func TestEnrichWithHaiku_RecognisesNonEmptyVersionTemplates(t *testing.T) {
	mock := &mockHaikuDescriber{response: "rich Haiku description"}
	ce := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{
		LLMJudgeEnabled: true,
		HaikuDescriber:  mock,
	})

	input := []ecosystem.ChangeNode{

		{
			Description:     "crypto/sha256.Sum512 added in version 1.23",
			ChangeType:      ecosystem.ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum512",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},

		{
			Description:     "crypto/sha256.OldHash removed (last present in version 1.22)",
			ChangeType:      ecosystem.ChangeRemoved,
			SymbolPath:      "crypto/sha256.OldHash",
			SourceExtracted: ecosystem.SourceImplicitDeepDiff,
		},
	}

	enriched, err := ce.EnrichWithHaiku(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Errorf("want 2 Haiku calls (both non-empty-version templates), got %d", len(mock.calls))
	}
	for i, n := range enriched {
		if n.Description != "rich Haiku description" {
			t.Errorf("node %d: want enriched Description, got %q", i, n.Description)
		}
		if n.SourceExtracted != ecosystem.SourceImplicitHaiku {
			t.Errorf("node %d: want SourceExtracted=%s, got %q", i, ecosystem.SourceImplicitHaiku, n.SourceExtracted)
		}
	}
}

func TestSourceImplicitHaiku_ConstantValueAndDistinct(t *testing.T) {
	if ecosystem.SourceImplicitHaiku != "haiku_inferred" {
		t.Errorf("want SourceImplicitHaiku=\"haiku_inferred\" (per header doc contract), got %q",
			ecosystem.SourceImplicitHaiku)
	}
	if ecosystem.SourceImplicitHaiku == ecosystem.SourceExplicitChangelog {
		t.Errorf("SourceImplicitHaiku must differ from SourceExplicitChangelog (both = %q)",
			ecosystem.SourceImplicitHaiku)
	}
	if ecosystem.SourceImplicitHaiku == ecosystem.SourceImplicitDeepDiff {
		t.Errorf("SourceImplicitHaiku must differ from SourceImplicitDeepDiff (both = %q)",
			ecosystem.SourceImplicitHaiku)
	}
}

var errSimulatedHaikuFailure = haikuSimulatedError("simulated haiku failure")

type haikuSimulatedError string

func (e haikuSimulatedError) Error() string { return string(e) }
