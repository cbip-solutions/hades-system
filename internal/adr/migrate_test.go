package adr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestMigrateLegacyADRSimple(t *testing.T) {
	dir := t.TempDir()
	content := `# ADR 0001: Test pivot

**Status**: Accepted
**Date**: 2026-04-30
**Decision-maker**: the operator, via brainstorm Q1

## Context

This is the body.
`
	p := filepath.Join(dir, "0001-test.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{
		DryRun:        false,
		PlanFromRange: "plan-1",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	if len(rep.Files) != 1 {
		t.Fatalf("Files = %d; want 1", len(rep.Files))
	}
	r := rep.Files[0]
	if r.Status != adr.MigrationStatusSuccess {
		t.Errorf("status = %v; want Success (err=%v)", r.Status, r.Error)
	}
	a, err := adr.ParseFile(p)
	if err != nil {
		t.Fatalf("ParseFile after migrate: %v", err)
	}
	if a.Frontmatter.ID != "ADR-0001" {
		t.Errorf("ID = %q; want ADR-0001", a.Frontmatter.ID)
	}
	if a.Frontmatter.Title != "Test pivot" {
		t.Errorf("Title = %q; want 'Test pivot'", a.Frontmatter.Title)
	}
	if a.Frontmatter.Status != adr.StatusAccepted {
		t.Errorf("Status = %q; want accepted", a.Frontmatter.Status)
	}
	if a.Frontmatter.Date != "2026-04-30" {
		t.Errorf("Date = %q; want 2026-04-30", a.Frontmatter.Date)
	}
	if a.Frontmatter.Plan != "plan-1" {
		t.Errorf("Plan = %q; want plan-1 (from PlanFromRange)", a.Frontmatter.Plan)
	}
	if !strings.Contains(a.Body, "## Context") {
		t.Errorf("body missing context: %q", a.Body)
	}
	// Body MUST NOT contain the redundant **Status**: line anymore.
	if strings.Contains(a.Body, "**Status**: Accepted") {
		t.Errorf("body still contains redundant Status header: %q", a.Body)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	content := `---
id: ADR-0001
title: Already migrated
status: accepted
date: 2026-05-07
plan: plan-9
tags: [test]
---

body
`
	p := filepath.Join(dir, "0001-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	rep, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	if rep.Files[0].Status != adr.MigrationStatusSkipped {
		t.Errorf("status = %v; want Skipped (already migrated)", rep.Files[0].Status)
	}
}

func TestMigrateDryRunDoesNotModifyFiles(t *testing.T) {
	dir := t.TempDir()
	content := `# ADR 0001: Test

**Status**: Accepted
**Date**: 2026-04-30

body
`
	p := filepath.Join(dir, "0001-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	original, _ := os.ReadFile(p)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{
		DryRun:        true,
		PlanFromRange: "plan-1",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(original) {
		t.Errorf("dry-run modified file:\nwant: %q\n got: %q", original, got)
	}
}

func TestMigrateExtractsRelatedADRs(t *testing.T) {
	dir := t.TempDir()
	content := `# ADR 0030: Test

**Status**: Accepted
**Date**: 2026-05-01
**Decision-maker**: the operator
**Plan**: Plan 6 (Q1 B)
**Related**: ADR-0001 (substrate boundary), ADR-0002, Plan 5 Phase J (interface relocation)

body
`
	p := filepath.Join(dir, "0030-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{
		PlanFromRange: "plan-6",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)
	if len(a.Frontmatter.RelatesTo) != 2 ||
		a.Frontmatter.RelatesTo[0] != "ADR-0001" ||
		a.Frontmatter.RelatesTo[1] != "ADR-0002" {
		t.Errorf("RelatesTo = %v; want [ADR-0001 ADR-0002] (Plan-NN tokens filtered)",
			a.Frontmatter.RelatesTo)
	}
}

func TestMigrateHandlesReservedStatus(t *testing.T) {
	dir := t.TempDir()
	content := `# ADR-0035 — AST/structured merge revisit window (RESERVATION)

**Status:** Reserved (revisit triggered)
**Date:** 2026-05-01
**Decision-maker:** the operator

body
`
	p := filepath.Join(dir, "0035-reserved.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{
		PlanFromRange: "plan-6",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)
	if a.Frontmatter.Status != adr.StatusReserved {
		t.Errorf("Status = %q; want Reserved", a.Frontmatter.Status)
	}
}

func TestMigrateRoundTripPreservesBody(t *testing.T) {
	dir := t.TempDir()
	body := `## Context

Multi-paragraph body.

With code blocks:

` + "```go" + `
fn := func() { return }
` + "```" + `

And tables:

| col1 | col2 |
|------|------|
| a    | b    |
`
	content := `# ADR 0001: Test

**Status**: Accepted
**Date**: 2026-04-30

` + body
	p := filepath.Join(dir, "0001.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{
		PlanFromRange: "plan-1",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)
	// Body MUST contain the multi-paragraph + code block + table
	// preserved verbatim minus the headers we extracted.
	for _, frag := range []string{"## Context", "Multi-paragraph body.",
		"```go", "fn := func() { return }", "| col1 | col2 |"} {
		if !strings.Contains(a.Body, frag) {
			t.Errorf("body missing fragment %q after migrate: %s", frag, a.Body)
		}
	}
}

func TestMigrateContextCancelled(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "0001-test.md"), []byte("# ADR 0001: Test\n\n**Status**: Accepted\n\nbody\n"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adr.MigrateDirectory(ctx, dir, adr.MigrateOptions{})
	if err == nil {
		t.Error("expected error from cancelled context; got nil")
	}
}

func TestMigrateNonExistentDir(t *testing.T) {
	_, err := adr.MigrateDirectory(context.Background(), "/does-not-exist-xyzzy", adr.MigrateOptions{})
	if err == nil {
		t.Error("expected error for non-existent directory; got nil")
	}
}

func TestMigrateSkipsUnderscoreFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "_schema.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "_index.md"), []byte("skip me"), 0o644)
	os.WriteFile(filepath.Join(dir, "0001-real.md"), []byte("# ADR 0001: Real\n\n**Status**: Accepted\n\nbody\n"), 0o644)
	rep, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	if len(rep.Files) != 1 {
		t.Errorf("Files = %d; want 1 (underscore files + non-md skipped)", len(rep.Files))
	}
}

func TestMigrateStatusVariants(t *testing.T) {
	cases := []struct {
		statusLine string
		want       adr.Status
	}{
		{"**Status**: Proposed", adr.StatusProposed},
		{"**Status**: Rejected", adr.StatusRejected},
		{"**Status**: Superseded", adr.StatusSuperseded},
		{"**Status**: Deprecated", adr.StatusDeprecated},
		{"**Status:** Reserved (revisit triggered)", adr.StatusReserved},
		{"**Status**: UNKNOWN_VALUE", adr.Status("UNKNOWN_VALUE")},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			dir := t.TempDir()
			content := "# ADR 0001: Test\n\n" + tc.statusLine + "\n**Date**: 2026-04-30\n\nbody\n"
			p := filepath.Join(dir, "0001-test.md")
			os.WriteFile(p, []byte(content), 0o644)
			_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
			if err != nil {
				t.Fatalf("MigrateDirectory: %v", err)
			}
			a, err := adr.ParseFile(p)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if a.Frontmatter.Status != tc.want {
				t.Errorf("Status = %q; want %q", a.Frontmatter.Status, tc.want)
			}
		})
	}
}

func TestMigrateFallbackIDFromFilename(t *testing.T) {
	dir := t.TempDir()

	content := "**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"
	p := filepath.Join(dir, "0042-fallback.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, err := adr.ParseFile(p)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if a.Frontmatter.ID != "ADR-0042" {
		t.Errorf("ID = %q; want ADR-0042 (from filename)", a.Frontmatter.ID)
	}
}

func TestMigrateDateFormats(t *testing.T) {
	dir := t.TempDir()

	content := "# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: April 30, 2026\n\nbody\n"
	p := filepath.Join(dir, "0001-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)
	if a.Frontmatter.Date != "April 30, 2026" {
		t.Errorf("Date = %q; want verbatim 'April 30, 2026'", a.Frontmatter.Date)
	}
}

func TestMigrateDecidersWithBacktick(t *testing.T) {
	dir := t.TempDir()
	content := "# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n**Decision-maker**: the operator, via brainstorm Q1 (commit `611e2f1`)\n\nbody\n"
	p := filepath.Join(dir, "0001-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-1"})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)

	if len(a.Frontmatter.Deciders) == 0 {
		t.Errorf("Deciders is empty; expected at least one")
	}
	for _, d := range a.Frontmatter.Deciders {
		if strings.Contains(d, "`") {
			t.Errorf("Decider %q still contains backtick", d)
		}
	}
}

func TestMigrateNoPlanHeader(t *testing.T) {
	dir := t.TempDir()
	content := "# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"
	p := filepath.Join(dir, "0001-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	_, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{PlanFromRange: "plan-9"})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	a, _ := adr.ParseFile(p)
	if a.Frontmatter.Plan != "plan-9" {
		t.Errorf("Plan = %q; want plan-9 (from PlanFromRange fallback)", a.Frontmatter.Plan)
	}
}

func TestMigrateWithColonInsideBold(t *testing.T) {

	dir := t.TempDir()
	content := `# ADR-0030 — Plan 6 package boundary merge

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q1 B)
**Related:** ADR-0001 (substrate boundary), Plan 5 Phase J (interface relocation source)

## Context

body
`
	p := filepath.Join(dir, "0030-test.md")
	os.WriteFile(p, []byte(content), 0o644)
	rep, err := adr.MigrateDirectory(context.Background(), dir, adr.MigrateOptions{})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	r := rep.Files[0]
	if r.Status != adr.MigrationStatusSuccess {
		t.Errorf("status = %v (err=%v); want Success", r.Status, r.Error)
	}
	a, err := adr.ParseFile(p)
	if err != nil {
		t.Fatalf("ParseFile after migrate: %v", err)
	}
	if a.Frontmatter.ID != "ADR-0030" {
		t.Errorf("ID = %q; want ADR-0030", a.Frontmatter.ID)
	}
	if a.Frontmatter.Status != adr.StatusAccepted {
		t.Errorf("Status = %q; want accepted", a.Frontmatter.Status)
	}
	if a.Frontmatter.Plan != "plan-6" {
		t.Errorf("Plan = %q; want plan-6", a.Frontmatter.Plan)
	}
	if len(a.Frontmatter.RelatesTo) != 1 || a.Frontmatter.RelatesTo[0] != "ADR-0001" {
		t.Errorf("RelatesTo = %v; want [ADR-0001] (Plan 5 Phase J filtered)", a.Frontmatter.RelatesTo)
	}
}

func TestMigrateExistingCorpusAllSucceed(t *testing.T) {

	repoRoot := repoRootForTest(t)
	src := filepath.Join(repoRoot, "docs", "decisions")
	dst := t.TempDir()
	matches, _ := filepath.Glob(filepath.Join(src, "*.md"))
	if len(matches) < 17 {
		t.Skipf("need at least 17 existing ADRs (legacy 0001-0008 + 0030-0038); found %d", len(matches))
	}
	for _, m := range matches {
		raw, _ := os.ReadFile(m)
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(m)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := adr.MigrateDirectory(context.Background(), dst, adr.MigrateOptions{
		PlanFromRange: "plan-unknown",
	})
	if err != nil {
		t.Fatalf("MigrateDirectory: %v", err)
	}
	for _, r := range rep.Files {
		if r.Status == adr.MigrationStatusFailed {
			t.Errorf("file %s: failed: %v", r.Path, r.Error)
		}
	}
}
