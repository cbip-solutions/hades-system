package doctrine

import (
	"strings"
	"testing"
)

const additiveDiff = `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,6 +10,7 @@ type Schema struct {
 	Research ResearchAxis ` + "`toml:\"research\"`" + `
 	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
 	Reviewer ReviewerAxis ` + "`toml:\"reviewer\"`" + `
+	NewAxis NewAxisType ` + "`toml:\"new_axis\"`" + `
 	Budget BudgetAxis ` + "`toml:\"budget\"`" + `
 	Workforce WorkforceAxis ` + "`toml:\"workforce\"`" + `
 	Apply ApplyAxis ` + "`toml:\"apply\"`" + `
`

const removalDiff = `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
 	Research ResearchAxis ` + "`toml:\"research\"`" + `
 	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
 	Reviewer ReviewerAxis ` + "`toml:\"reviewer\"`" + `
-	Budget BudgetAxis ` + "`toml:\"budget\"`" + `
 	Workforce WorkforceAxis ` + "`toml:\"workforce\"`" + `
 	Apply ApplyAxis ` + "`toml:\"apply\"`" + `
`

const renameDiff = `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,7 @@ type Schema struct {
 	Research ResearchAxis ` + "`toml:\"research\"`" + `
 	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
 	Reviewer ReviewerAxis ` + "`toml:\"reviewer\"`" + `
-	Budget BudgetAxis ` + "`toml:\"budget\"`" + `
+	Budget BudgetAxis ` + "`toml:\"budget_v2\"`" + `
 	Workforce WorkforceAxis ` + "`toml:\"workforce\"`" + `
 	Apply ApplyAxis ` + "`toml:\"apply\"`" + `
`

const noSchemaDiff = `diff --git a/internal/doctrine/loader.go b/internal/doctrine/loader.go
index 0000001..0000002 100644
--- a/internal/doctrine/loader.go
+++ b/internal/doctrine/loader.go
@@ -10,1 +10,1 @@
- // old comment
+ // new comment
`

func TestValidateAdditiveOnly(t *testing.T) {
	v, err := ValidateAdditive(additiveDiff, "feat(doctrine): add new axis")
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if !v.OK {
		t.Errorf("OK = false, want true; violations=%v", v.Violations)
	}
}

func TestValidateRejectsRemoval(t *testing.T) {
	v, err := ValidateAdditive(removalDiff, "fix(doctrine): drop budget")
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if v.OK {
		t.Error("OK = true, want false (removal without ADR)")
	}
	found := false
	for _, vio := range v.Violations {
		if strings.Contains(vio, "budget") {
			found = true
		}
	}
	if !found {
		t.Errorf("violations missing 'budget': %v", v.Violations)
	}
}

func TestValidateAcceptsRemovalWithADR(t *testing.T) {
	body := "feat(doctrine): drop budget axis\n\nADR: docs/decisions/0008-doctrine-schema-budget-removal.md"
	v, err := ValidateAdditive(removalDiff, body)
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if !v.OK {
		t.Errorf("OK = false, want true; violations=%v", v.Violations)
	}
}

func TestValidateRejectsRename(t *testing.T) {
	v, err := ValidateAdditive(renameDiff, "fix(doctrine): rename")
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if v.OK {
		t.Error("OK = true, want false (rename without ADR)")
	}
}

func TestValidateAcceptsRenameWithADR(t *testing.T) {
	body := "refactor(doctrine): rename budget tag\n\nADR: docs/decisions/0009-doctrine-schema-budget-rename.md"
	v, err := ValidateAdditive(renameDiff, body)
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if !v.OK {
		t.Errorf("OK = false, want true; violations=%v", v.Violations)
	}
}

func TestValidateNoSchemaTouched(t *testing.T) {
	v, err := ValidateAdditive(noSchemaDiff, "fix(doctrine): comment typo")
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if !v.OK {
		t.Errorf("OK = false, want true (no schema touch)")
	}
}

func TestValidateMultipleADRRefs(t *testing.T) {
	multiRemoval := strings.Replace(removalDiff,
		`-	Budget BudgetAxis `+"`toml:\"budget\"`",
		"-	Budget BudgetAxis `toml:\"budget\"`\n-	Apply ApplyAxis `toml:\"apply\"`",
		1)
	body := "refactor(doctrine): consolidate axes\n\nADR: docs/decisions/0010-doctrine-schema-consolidation.md"
	v, err := ValidateAdditive(multiRemoval, body)
	if err != nil {
		t.Fatalf("ValidateAdditive: %v", err)
	}
	if !v.OK {
		t.Errorf("OK = false, want true; violations=%v", v.Violations)
	}
}

func TestValidateADRPathPattern(t *testing.T) {
	cases := []struct {
		body    string
		wantOK  bool
		message string
	}{
		{"ADR: docs/decisions/0008-doctrine-schema-foo.md", true, "canonical ADR"},
		{"ADR: docs/decisions/0008-doctrine-schema-budget-removal.md", true, "canonical with longer name"},
		{"ADR: docs/decisions/12-doctrine-schema-foo.md", false, "wrong digit count"},
		{"ADR: docs/decisions/0008-other-topic.md", false, "wrong topic"},
		{"ADR: docs/0008-doctrine-schema-foo.md", false, "wrong directory"},
		{"see docs/decisions/0008-doctrine-schema-foo.md for context", true, "ref anywhere in body"},
		{"", false, "no ADR"},
	}
	for _, c := range cases {
		v, err := ValidateAdditive(removalDiff, c.body)
		if err != nil {
			t.Fatalf("[%s] ValidateAdditive: %v", c.message, err)
		}
		if v.OK != c.wantOK {
			t.Errorf("[%s] OK = %v, want %v; violations=%v", c.message, v.OK, c.wantOK, v.Violations)
		}
	}
}

func TestExtractRemovedTags(t *testing.T) {
	tags := extractRemovedTomlTags(removalDiff)
	if len(tags) != 1 || tags[0] != "budget" {
		t.Errorf("tags = %v, want [budget]", tags)
	}
}

func TestExtractRemovedTagsNone(t *testing.T) {
	tags := extractRemovedTomlTags(additiveDiff)
	if len(tags) != 0 {
		t.Errorf("tags = %v, want empty", tags)
	}
}

func TestExtractRemovedTagsRename(t *testing.T) {
	tags := extractRemovedTomlTags(renameDiff)
	if len(tags) != 1 || tags[0] != "budget" {
		t.Errorf("tags = %v, want [budget]", tags)
	}
}

func TestExtractRemovedTagsBypassCases(t *testing.T) {
	cases := []struct {
		name    string
		diff    string
		wantTag string
	}{
		{
			name: "omitempty modifier",
			diff: `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
 	Research ResearchAxis ` + "`toml:\"research\"`" + `
-	Budget BudgetAxis ` + "`toml:\"budget,omitempty\"`" + `
 	Workforce WorkforceAxis ` + "`toml:\"workforce\"`" + `
`,
			wantTag: "budget",
		},
		{
			name: "digits in tag",
			diff: `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
 	Research ResearchAxis ` + "`toml:\"research\"`" + `
-	PlanFour Plan4Apply ` + "`toml:\"plan_4_apply\"`" + `
 	Workforce WorkforceAxis ` + "`toml:\"workforce\"`" + `
`,
			wantTag: "plan_4_apply",
		},
		{
			name: "digits and omitempty combined",
			diff: `diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
-	Plan2 Plan2Type ` + "`toml:\"plan_2_axis,omitempty\"`" + `
`,
			wantTag: "plan_2_axis",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tags := extractRemovedTomlTags(c.diff)
			if len(tags) != 1 || tags[0] != c.wantTag {
				t.Errorf("tags = %v, want [%s]", tags, c.wantTag)
			}
		})
	}
}

func TestValidateRejectsRemovalWithBypassShapes(t *testing.T) {
	cases := []string{

		`diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
-	Budget BudgetAxis ` + "`toml:\"budget,omitempty\"`" + `
`,

		`diff --git a/internal/doctrine/schema.go b/internal/doctrine/schema.go
index 0000001..0000002 100644
--- a/internal/doctrine/schema.go
+++ b/internal/doctrine/schema.go
@@ -10,7 +10,6 @@ type Schema struct {
-	PlanFour Plan4Apply ` + "`toml:\"plan_4_apply\"`" + `
`,
	}
	for i, diff := range cases {
		v, err := ValidateAdditive(diff, "fix(doctrine): silent removal")
		if err != nil {
			t.Fatalf("case %d: ValidateAdditive: %v", i, err)
		}
		if v.OK {
			t.Errorf("case %d: OK = true, want false (bypass shape leaked through)", i)
		}
	}
}

func TestRunGitDiffSucceedsOnRepo(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := RunGitDiff(dir, "HEAD", "HEAD")
	if err != nil {
		t.Fatalf("RunGitDiff: %v", err)
	}
	if out != "" {
		t.Errorf("HEAD vs HEAD diff non-empty: %q", out)
	}
}

func TestRunGitDiffFailsOnInvalidRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := RunGitDiff(dir, "HEAD", "HEAD")
	if err == nil {
		t.Error("RunGitDiff(non-repo) returned nil error")
	}
}

func TestRunGitCommitBodySucceedsOnRepo(t *testing.T) {
	dir := initTempGitRepo(t)
	body, err := RunGitCommitBody(dir, "HEAD")
	if err != nil {
		t.Fatalf("RunGitCommitBody: %v", err)
	}
	if !strings.Contains(body, "init") {
		t.Errorf("body = %q, want 'init' in message", body)
	}
}

func TestRunGitCommitBodyFailsOnInvalidRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := RunGitCommitBody(dir, "HEAD")
	if err == nil {
		t.Error("RunGitCommitBody(non-repo) returned nil error")
	}
}

func TestValidateRangeOnRepo(t *testing.T) {
	dir := initTempGitRepo(t)
	res, err := ValidateRange(dir, "HEAD", "HEAD")
	if err != nil {
		t.Fatalf("ValidateRange: %v", err)
	}
	if !res.OK {
		t.Errorf("OK = false, want true (no diff)")
	}
}

func TestValidateRangeDiffError(t *testing.T) {
	dir := initTempGitRepo(t)
	_, err := ValidateRange(dir, "no-such-ref", "HEAD")
	if err == nil {
		t.Error("ValidateRange(bad-base) returned nil error")
	}
}

func TestValidateRangeBodyError(t *testing.T) {
	dir := initTempGitRepo(t)
	_, err := ValidateRange(dir, "HEAD", "no-such-ref")
	if err == nil {
		t.Error("ValidateRange(bad-head) returned nil error")
	}
}

// TestRunGitCommitBodyRejectsFlagLikeRef (I-6 fix): a head value
// that looks like a git flag (e.g. "-p", "--all", "-x") MUST be
// rejected before exec so it cannot be interpreted as an argument
// to git log. The hard-rule is "validated against ^[A-Za-z0-9_./~^-]+$
// excluding leading dash"; defense-in-depth against caller error or
// future code-paths that thread untrusted input through.
func TestRunGitCommitBodyRejectsFlagLikeRef(t *testing.T) {
	dir := initTempGitRepo(t)
	cases := []string{"-p", "--all", "-x", "--no-color"}
	for _, ref := range cases {
		_, err := RunGitCommitBody(dir, ref)
		if err == nil {
			t.Errorf("RunGitCommitBody(%q) returned nil; want validation rejection", ref)
		}
		if err != nil && !strings.Contains(err.Error(), "invalid ref") {
			t.Errorf("RunGitCommitBody(%q) err = %v, want 'invalid ref' message", ref, err)
		}
	}
}

func TestRunGitDiffRejectsFlagLikeRef(t *testing.T) {
	dir := initTempGitRepo(t)
	cases := []struct {
		base, head string
	}{
		{"-p", "HEAD"},
		{"HEAD", "-p"},
		{"--all", "HEAD"},
	}
	for _, c := range cases {
		_, err := RunGitDiff(dir, c.base, c.head)
		if err == nil {
			t.Errorf("RunGitDiff(%q, %q) returned nil; want validation rejection", c.base, c.head)
		}
		if err != nil && !strings.Contains(err.Error(), "invalid ref") {
			t.Errorf("RunGitDiff(%q, %q) err = %v, want 'invalid ref' message", c.base, c.head, err)
		}
	}
}

func TestRunGitDiffAcceptsValidRefs(t *testing.T) {
	dir := initTempGitRepo(t)

	for _, ref := range []string{"HEAD", "HEAD~0", "main", "master"} {

		_, err := RunGitDiff(dir, "HEAD", ref)
		if err != nil && strings.Contains(err.Error(), "invalid ref") {
			t.Errorf("RunGitDiff rejected valid-shape ref %q: %v", ref, err)
		}
	}
}
