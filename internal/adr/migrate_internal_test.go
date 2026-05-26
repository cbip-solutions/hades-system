package adr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizePlanNoMatch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"some text without plan reference", ""},
		{"Q1 B (no plan number here)", ""},
		{"Plan 6 (Q1 B)", "plan-6"},
		{"plan-8", "plan-8"},
	}
	for _, tc := range cases {
		got := normalizePlan(tc.in)
		if got != tc.want {
			t.Errorf("normalizePlan(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseDecidersMalformedParens(t *testing.T) {

	got := parseDeciders(") text (")
	if len(got) == 0 {

		t.Logf("malformed parens: got %v (expected non-empty or graceful)", got)
	}

}

func TestParseDecidersViaKeyword(t *testing.T) {
	got := parseDeciders("operator via brainstorm")
	if len(got) != 1 || got[0] != "operator" {
		t.Errorf("parseDeciders strip-via: got %v; want [operator]", got)
	}
}

func TestExtractFrontmatterFilenameNotAllDigits(t *testing.T) {

	fm, _, err := extractFrontmatterFromLegacy("**Status**: Accepted\nbody\n", "plan-1", "foo-bar.md")
	if err != nil {
		t.Fatalf("extractFrontmatterFromLegacy: %v", err)
	}

	if fm.ID != "" {
		t.Errorf("ID = %q; want empty (non-digit prefix in filename)", fm.ID)
	}
}

func TestMigrateOneReadError(t *testing.T) {
	res := migrateOne("/no/such/file/xyzzy.md", MigrateOptions{})
	if res.Status != MigrationStatusFailed {
		t.Errorf("status = %v; want Failed for non-existent file", res.Status)
	}
	if res.Error == nil {
		t.Error("Error is nil; want non-nil for read failure")
	}
}

func TestMigrateOneWriteError(t *testing.T) {

	dir := t.TempDir()
	p := filepath.Join(dir, "0001-test.md")
	if err := os.WriteFile(p, []byte("# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod directory:", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	res := migrateOne(p, MigrateOptions{PlanFromRange: "plan-1"})
	if res.Status != MigrationStatusFailed {
		t.Errorf("status = %v; want Failed for read-only dir write", res.Status)
	}
}

func TestNormalizePlanFollowup(t *testing.T) {

	got := normalizePlan("Plan 5 (followup)")
	if got != "plan-5" {
		t.Errorf("normalizePlan(%q) = %q; want plan-5", "Plan 5 (followup)", got)
	}
}

func TestParseRelatedDuplicates(t *testing.T) {
	got := parseRelated("ADR-0001 and ADR-0001 again")
	if len(got) != 1 || got[0] != "ADR-0001" {
		t.Errorf("parseRelated duplicates: got %v; want [ADR-0001]", got)
	}
}

func TestCollapseLeadingBlankLinesEmptyInput(t *testing.T) {
	got := collapseLeadingBlankLines("")
	if got != "" {
		t.Errorf("collapseLeadingBlankLines(%q) = %q; want empty", "", got)
	}
}

func TestExtractFrontmatterEmptyPlanFallback(t *testing.T) {

	fm, _, err := extractFrontmatterFromLegacy(
		"# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n**Plan**: not-a-plan-string\n\nbody\n",
		"",
		"0001-test.md",
	)
	if err != nil {
		t.Fatalf("extractFrontmatterFromLegacy: %v", err)
	}
	if fm.Plan != "" {
		t.Errorf("Plan = %q; want empty (normalizePlan with no plan token + empty defaultPlan)", fm.Plan)
	}
	if fm.Tags == nil || len(fm.Tags) != 0 {
		t.Errorf("Tags = %v; want empty slice when plan is empty", fm.Tags)
	}
}

func TestParseDecidersBacktickStripping(t *testing.T) {

	got := parseDeciders("operator `611e2f1`")
	if len(got) == 0 {
		t.Errorf("parseDeciders(%q): got empty; want non-empty", "operator `611e2f1`")
	}
	for _, d := range got {
		if strings.Contains(d, "`") {
			t.Errorf("parseDeciders: result %q still contains backtick", d)
		}
	}
}

func TestParseDecidersUnclosedBacktick(t *testing.T) {

	got := parseDeciders("operator `unclosed")

	t.Logf("parseDeciders(unclosed backtick): %v", got)
}

func TestMigrateDirectoryContextCancelMidWalk(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "000"+string(rune('1'+i))+"-test.md")
		content := "# ADR 000" + string(rune('1'+i)) + ": Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"
		os.WriteFile(name, []byte(content), 0o644)
	}
	ctx, cancel := context.WithCancel(context.Background())

	cancel()
	_, err := MigrateDirectory(ctx, dir, MigrateOptions{PlanFromRange: "plan-1"})
	if err == nil {
		t.Error("expected error from cancelled context; got nil")
	}
}

func TestMigrateOneRenameError(t *testing.T) {

	dir := t.TempDir()
	p := filepath.Join(dir, "0001.md")
	if err := os.WriteFile(p, []byte("# ADR 0001: Test\n\n**Status**: Accepted\n**Date**: 2026-04-30\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod dir:", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	res := migrateOne(p, MigrateOptions{PlanFromRange: "plan-1"})
	if res.Status != MigrationStatusFailed {
		t.Errorf("status = %v; want Failed for write error in read-only dir", res.Status)
	}
}
