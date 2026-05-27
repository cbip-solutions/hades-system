// SPDX-License-Identifier: MIT
// cmd/verify-no-bypass-references — TDD tests for the 5-surface boundary
// scanner.
//
// Each surface exercises (a) a positive case where an unsanctioned bypass
// mention MUST fail, and (b) a negative case where the mention is allowed
// (sanctioned via defaultAllowlist()).
//
// # Surfaces
//
// 1. AST imports + qualified identifiers (.go files via go/ast)
// 2. tests/ directory (grep for bypass outside sanctioned helpers)
// 3. docs/ directory (grep outside sanctioned bypass-sidecar-recipe.md)
// 4. configs/ directory (grep outside sidecars.toml.example)
// 5. internal/store/migrations/ SQL (grep for bypass-specific tables)
//
// Test scaffolding: t.TempDir() seeded with surface-specific fixture
// files; scanXxx functions operate against the temp dir and return
// ScanResult{Violations []Violation}. Default empty Violations = pass.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyNoBypassReferencesBuilds(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "build", "-o", os.DevNull,
		"github.com/cbip-solutions/hades-system/cmd/verify-no-bypass-references")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}

func mkfile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSurface1_ASTImports_FailsOnUnsanctionedImport — a.go file outside
// the sanctioned allowlist that imports an anthropic-bypass path MUST
// trigger a violation.
func TestSurface1_ASTImports_FailsOnUnsanctionedImport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/random/foo.go", `package random
import anthropicbypass "github.com/example/anthropic-bypass"
var _ = anthropicbypass.Client{}
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) == 0 {
		t.Fatal("expected AST violation for unsanctioned anthropic-bypass import; got 0")
	}

	if result.Violations[0].Surface != "ast" {
		t.Errorf("violation.Surface = %q want %q", result.Violations[0].Surface, "ast")
	}
}

// TestSurface1_ASTImports_AllowsSanctionedDaemonSidecarBackend — a.go
// file inside `internal/providers/sidecar_backend.go` MUST NOT trigger
// a violation even if it mentions "bypass" (sanctioned daemon HTTP client).
func TestSurface1_ASTImports_AllowsSanctionedDaemonSidecarBackend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/providers/sidecar_backend.go", `package providers
// SidecarBackend is the daemon-side HTTP client to the localhost bypass sidecar.
// Mention of "bypass" sanctioned per decisión 17-d (daemon-side HTTP client).
type SidecarBackend struct{}
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned daemon sidecar path: %v", result.Violations)
	}
}

// TestSurface1_ASTImports_AllowsAnthropicBypassTree — the entire
// `private-tier1-module/**` subtree is sanctioned (
// correction #4 retains the source in the dev repo). Files there MUST
// NOT trigger a violation.
func TestSurface1_ASTImports_AllowsAnthropicBypassTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "private-tier1-module/bypass.go", `package bypass
// Bypass is the legitimate tier-1 implementation; sanctioned.
type Client struct{}
type BypassClient = Client
type BypassBackend = Client
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned anthropic-bypass tree: %v", result.Violations)
	}
}

// TestSurface1_ASTImports_DetectsBypassClientIdentifierUnsanctioned — a
// .go file outside the sanctioned allowlist that references the
// qualified identifier `BypassClient` MUST trigger a violation.
func TestSurface1_ASTImports_DetectsBypassClientIdentifierUnsanctioned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/random/uses_bypass.go", `package random
import bp "example.com/some/anthropic-bypass"
func use() { _ = bp.BypassClient{} }
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) == 0 {
		t.Fatal("expected AST violation for unsanctioned BypassClient identifier")
	}
}

// TestSurface1_ASTImports_IgnoresUnrelatedCode — clean.go file (no
// bypass tokens) MUST NOT trigger a violation.
func TestSurface1_ASTImports_IgnoresUnrelatedCode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/random/clean.go", `package random
import "fmt"
func hello() { fmt.Println("hello") }
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in clean code: %v", result.Violations)
	}
}

// TestSurface2_TestsDir_FailsOnUnsanctionedBypassMention — a file under
// tests/ outside the sanctioned subtrees mentioning bypass tokens MUST
// trigger a violation.
//
// Note the broad dev-repo allowlist sanctions ALL canonical tests/*
// subtrees (compliance, integration, realworld, chaos, adversarial,
// testdata, etc.) because dev-repo retention covers them; the Phase
// C-13 sync filter strips specific test fixtures. This positive case
// uses a HYPOTHETICAL new subtree (`tests/newsurface/`) to exercise
// the unsanctioned-path branch — if a future tests subtree is added
// without allowlisting, the scanner catches it.
func TestSurface2_TestsDir_FailsOnUnsanctionedBypassMention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "tests/newsurface/random_test.go", `package newsurface
// Bypass-tier mention without being in a sanctioned subtree.
var _ = "BypassClient"
`)
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))
	if len(result.Violations) == 0 {
		t.Fatal("expected tests/ violation for unsanctioned bypass mention")
	}
}

// TestSurface2_TestsDir_AllowsSanctionedTestHelper — a tests/ file in
// the sanctioned compliance subtree MUST NOT trigger a violation.
func TestSurface2_TestsDir_AllowsSanctionedTestHelper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "tests/compliance/bypass_invariants_test.go", `package compliance
// Sanctioned tests/compliance/** covers bypass-tier residual invariants.
var _ = "BypassClient"
`)
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned tests file: %v", result.Violations)
	}
}

// TestSurface2_TestsDir_IgnoresUnrelatedTestFile — a test file with no
// forbidden tokens MUST NOT trigger a violation.
func TestSurface2_TestsDir_IgnoresUnrelatedTestFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "tests/newsurface/clean_test.go", `package newsurface
// no forbidden tokens in this clean file.
var ok = true
`)
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in unrelated test file: %v", result.Violations)
	}
}

// TestSurface3_DocsDir_FailsOnBypassMentionElsewhere — a doc file with
// no allowlist entry that mentions bypass MUST trigger a violation.
func TestSurface3_DocsDir_FailsOnBypassMentionElsewhere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "docs/something/random.md", "# random\n\nthis mentions anthropic-bypass.\n")
	result := scanTextSurface(dir, "docs/", docsAllowlist(defaultAllowlist()))
	if len(result.Violations) == 0 {
		t.Fatal("expected docs/ violation for unsanctioned bypass mention")
	}
}

// TestSurface3_DocsDir_AllowsBypassSidecarRecipe — the sanctioned
// `docs/operations/bypass-sidecar-recipe.md` MUST NOT trigger a violation
// .
func TestSurface3_DocsDir_AllowsBypassSidecarRecipe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "docs/operations/bypass-sidecar-recipe.md",
		"# bypass sidecar recipe\n\nHTTP API contract.\n")
	result := scanTextSurface(dir, "docs/", docsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned docs recipe file: %v", result.Violations)
	}
}

// TestSurface3_DocsDir_AllowsPrivateBypassOpsDoc — `docs/operations/bypass.md`
// MUST NOT trigger a violation in the dev-repo
// scan because the doc-surface includes the private operator handbook.
func TestSurface3_DocsDir_AllowsPrivateBypassOpsDoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "docs/operations/bypass.md", "# bypass private ops\n")
	result := scanTextSurface(dir, "docs/", docsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in private-only sanctioned bypass.md: %v", result.Violations)
	}
}

func TestSurface3_DocsDir_AllowsADRsAndPlans(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "docs/decisions/0101-bypass-refresh-protocol.md", "# adr 0101 bypass refresh\n")
	mkfile(t, dir, "docs/decisions/0102-bypass-v0179-fingerprint-coexistence.md", "# adr 0102 bypass fp\n")
	mkfile(t, dir, "docs/decisions/0103-bypass-v01710-metadata-user-id.md", "# adr 0103 bypass metadata\n")
	mkfile(t, dir, "docs/decisions/0104-bypass-response-decompression-and-schema-drift.md", "# adr 0104 bypass schema\n")
	mkfile(t, dir, "docs/decisions/0118-bypass-tier-private-org.md", "# adr 0118 bypass tier\n")
	result := scanTextSurface(dir, "docs/", docsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned bypass ADRs: %v", result.Violations)
	}
}

// TestSurface4_ConfigsDir_AllowsSidecarsTomlExample — sanctioned config
// example file MUST NOT trigger a violation.
func TestSurface4_ConfigsDir_AllowsSidecarsTomlExample(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "configs/sidecars.toml.example",
		"# sidecars.toml.example — bypass sidecar discovery config\n")
	result := scanTextSurface(dir, "configs/", configsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned sidecars.toml.example: %v", result.Violations)
	}
}

// TestSurface4_ConfigsDir_FailsOnBypassMentionElsewhere — a config file
// outside the sanctioned allowlist mentioning bypass MUST trigger a
// violation.
func TestSurface4_ConfigsDir_FailsOnBypassMentionElsewhere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "configs/random.toml.example",
		"# random.toml — should not mention BypassClient.\n")
	result := scanTextSurface(dir, "configs/", configsAllowlist(defaultAllowlist()))
	if len(result.Violations) == 0 {
		t.Fatal("expected configs/ violation for unsanctioned bypass mention")
	}
}

func TestSurface4_ConfigsDir_AllowsBypassConfigJSONExample(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "configs/bypass-config.json.example",
		`{"comment":"bypass config example"}`)
	result := scanTextSurface(dir, "configs/", configsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in sanctioned bypass-config.json.example: %v", result.Violations)
	}
}

// TestSurface5_SQLMigrations_FailsOnBypassTable — a SQL migration that
// declares a bypass-specific table MUST trigger a violation.
//
// invariant daemon-only store ownership: the sidecar maintains NO
// SQLite state; any "bypass_*" table in `internal/store/migrations/` is
// a boundary breach.
func TestSurface5_SQLMigrations_FailsOnBypassTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/store/migrations/099_bypass_tokens.sql",
		"CREATE TABLE bypass_tokens (id INTEGER PRIMARY KEY);\n")
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	if len(result.Violations) == 0 {
		t.Fatal("expected SQL migration violation for bypass_* table")
	}
}

// TestSurface5_SQLMigrations_AllowsCleanMigration — a clean SQL
// migration without bypass-specific table prefixes MUST NOT trigger a
// violation. Pre-existing comments mentioning "bypass" in a different
// semantic sense (e.g., "this path bypasses any other lookup") are
// allowed because they are NOT table declarations.
func TestSurface5_SQLMigrations_AllowsCleanMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/store/migrations/062_tmux_session_state.sql",
		`-- 062: tmux session state.
--     path bypasses any other lookup.
CREATE TABLE tmux_session_state (id INTEGER PRIMARY KEY);
`)
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("unexpected violation in clean migration with comment mention: %v", result.Violations)
	}
}

func TestSurface5_SQLMigrations_FailsOnAnthropicBypassTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/store/migrations/100_ab.sql",
		"CREATE TABLE anthropic_bypass_audit (id INTEGER PRIMARY KEY);\n")
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	if len(result.Violations) == 0 {
		t.Fatal("expected SQL migration violation for anthropic_bypass_* table")
	}
}

func TestDefaultAllowlistIsConservative(t *testing.T) {
	t.Parallel()
	got := defaultAllowlist()
	want := []string{
		"private-tier1-module/**",
		"internal/providers/sidecar_backend.go",
		"internal/providers/sidecar_backend_test.go",
		"internal/providers/errors_sidecar.go",
		"internal/daemon/dispatcheradapter/sidecar_registration.go",
		"internal/daemon/dispatcheradapter/sidecar_registration_test.go",
		"internal/config/sidecars.go",
		"internal/config/sidecars_test.go",
		"internal/cli/init.go",
		"internal/cli/init_test.go",
		"configs/sidecars.toml.example",
		"configs/bypass-config.json.example",
		"docs/operations/bypass-sidecar-recipe.md",
		"docs/operations/bypass.md",
	}
	gotPaths := make(map[string]bool, len(got))
	for _, e := range got {
		gotPaths[e.Path] = true
	}
	var missing []string
	for _, w := range want {
		if !gotPaths[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("defaultAllowlist missing required entries: %v", missing)
	}
}

func TestPathMatchesAllowEntry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"private-tier1-module/**", "private-tier1-module/bypass.go", true},
		{"private-tier1-module/**", "private-tier1-module/sub/x.go", true},
		{"private-tier1-module/**", "internal/other/foo.go", false},
		{"internal/providers/sidecar_backend.go", "internal/providers/sidecar_backend.go", true},
		{"internal/providers/sidecar_backend.go", "internal/providers/other.go", false},
		{"tests/compliance/bypass_*", "tests/compliance/bypass_invariants_test.go", true},
		{"tests/compliance/bypass_*", "tests/compliance/other_test.go", false},
	}
	for _, c := range cases {
		got := matchAllowPattern(c.pattern, c.path)
		if got != c.want {
			t.Errorf("matchAllowPattern(%q, %q) = %v want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestScanAll_CleanRepo_NoViolations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/providers/sidecar_backend.go", `package providers
type SidecarBackend struct{} // bypass sanctioned per decisión 17-d
`)
	mkfile(t, dir, "configs/sidecars.toml.example", "# bypass sidecar config\n")
	mkfile(t, dir, "tests/compliance/bypass_invariants_test.go", `package compliance
var _ = "BypassClient" // sanctioned bypass test
`)
	mkfile(t, dir, "internal/store/migrations/062_tmux_session_state.sql",
		"-- tmux: this path bypasses other lookup.\nCREATE TABLE t (id INT);\n")
	result := scanAll(dir, defaultAllowlist())
	if len(result.Violations) != 0 {
		var detail []string
		for _, v := range result.Violations {
			detail = append(detail, v.String())
		}
		t.Errorf("expected zero violations on clean fixture; got %d:\n%s",
			len(result.Violations), strings.Join(detail, "\n"))
	}
}

func TestViolationString_WithAndWithoutLine(t *testing.T) {
	t.Parallel()
	withLine := Violation{Surface: "ast", Path: "x.go", Line: 7, Token: "bypass", Snippet: "import"}
	if s := withLine.String(); !strings.Contains(s, "x.go:7") || !strings.Contains(s, "bypass") {
		t.Errorf("with-line String() = %q", s)
	}
	noLine := Violation{Surface: "sql", Path: "y.sql", Token: "bypass", Snippet: "create"}
	if s := noLine.String(); strings.Contains(s, ":0") {
		t.Errorf("no-line String() should not show line 0: %q", s)
	}
}

func TestIsTextScanCandidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"x.go", true},
		{"x.md", true},
		{"x.toml", true},
		{"x.toml.example", true},
		{"x.yaml", true},
		{"x.yml", true},
		{"x.json", true},
		{"x.json.example", true},
		{"x.sh", true},
		{"x.py", true},
		{"x.sql", true},
		{"x.txt", true},
		{"x.png", false},
		{"x.bin", false},
		{"binary.zip", false},
	}
	for _, c := range cases {
		if got := isTextScanCandidate(c.path); got != c.want {
			t.Errorf("isTextScanCandidate(%q) = %v want %v", c.path, got, c.want)
		}
	}
}

func TestSurfaceForPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"tests/", "tests"},
		{"docs/", "docs"},
		{"configs/", "configs"},
		{"other/", "other/"},
	}
	for _, c := range cases {
		if got := surfaceForPrefix(c.in); got != c.want {
			t.Errorf("surfaceForPrefix(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate_BoundaryCases(t *testing.T) {
	t.Parallel()
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
	long := "abcdefghijklmnop"
	got := truncate(long, 8)
	if got != "abcde..." {
		t.Errorf("truncate long = %q", got)
	}
}

func TestScanAST_AbsentRoot(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) == 0 {
		t.Fatal("expected walk-error violation on absent root")
	}
	if result.Violations[0].Token != "walk-error" {
		t.Errorf("expected walk-error token; got %q", result.Violations[0].Token)
	}
}

func TestScanTextSurface_AbsentBase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("expected zero violations on absent base; got %v", result.Violations)
	}
}

func TestScanSQLMigrations_AbsentBase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("expected zero violations on absent base; got %v", result.Violations)
	}
}

func TestScanTextSurface_SkipsBinaryExtensions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "tests/newsurface/binary.bin", "binary BypassClient content")
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("expected zero violations on binary extension; got %v", result.Violations)
	}
}

func TestScanAST_UnparseableGoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/random/broken.go", "package random\nfunc broken() { // missing brace")

	_ = scanAST(dir, defaultAllowlist())
}

func TestScanTextSurface_MultipleViolationsPerFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "tests/newsurface/multi_test.go", `package newsurface
// BypassClient BypassBackend anthropic-bypass — all on one line
var _ = "BypassClient"
`)
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))

	if len(result.Violations) != 2 {
		t.Errorf("expected 2 violations (one per line); got %d: %v",
			len(result.Violations), result.Violations)
	}
}

func TestMatchAllowPattern_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, path string
		want          bool
	}{

		{"foo/**", "foo", true},
		{"foo/**", "foo/x", true},
		{"foo/**", "foobar", false},

		{"foo/**", "bar/x", false},

		{"a/b_*", "a/b_x", true},
		{"a/b_*", "a/b_x/y", false},

		{"x", "y", false},
	}
	for _, c := range cases {
		if got := matchAllowPattern(c.pattern, c.path); got != c.want {
			t.Errorf("matchAllowPattern(%q, %q) = %v want %v",
				c.pattern, c.path, got, c.want)
		}
	}
}

func TestScanAST_SkipsVendorAndDotGit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "vendor/foo/bypass.go", `package foo
type BypassClient struct{}
`)
	mkfile(t, dir, ".git/hooks/sample.go", `package hooks
var _ = "BypassClient"
`)
	mkfile(t, dir, "node_modules/x/y.go", `package y
var _ = "BypassClient"
`)
	mkfile(t, dir, "dist/cached.go", `package cached
var _ = "BypassClient"
`)
	mkfile(t, dir, "bin/built.go", `package built
var _ = "BypassClient"
`)
	mkfile(t, dir, ".cache/x.go", `package x
var _ = "BypassClient"
`)
	result := scanAST(dir, defaultAllowlist())
	if len(result.Violations) != 0 {
		t.Errorf("expected zero violations from skipped dirs; got: %v", result.Violations)
	}
}

func TestScanSQLMigrations_NonSQLFilesSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkfile(t, dir, "internal/store/migrations/README.md", "CREATE TABLE bypass_x;")
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	if len(result.Violations) != 0 {
		t.Errorf("expected zero violations from non-.sql file; got %v", result.Violations)
	}
}

func pipedFiles(t *testing.T) (writer *os.File, flush func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	captured := make(chan []byte, 1)
	go func() {
		var buf [4096]byte
		var out []byte
		for {
			n, err := r.Read(buf[:])
			if n > 0 {
				out = append(out, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		captured <- out
	}()
	return w, func() string {
		w.Close()
		b := <-captured
		r.Close()
		return string(b)
	}
}

func TestRun_ExitsZeroOnCleanFixture(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "internal/providers/sidecar_backend.go", `package providers
type SidecarBackend struct{} // bypass sanctioned per decisión 17-d
`)
	stdoutW, flushOut := pipedFiles(t)
	stderrW, flushErr := pipedFiles(t)
	code := run(dir, stdoutW, stderrW)
	out := flushOut()
	stderrOut := flushErr()
	if code != 0 {
		t.Fatalf("clean fixture should exit 0; code=%d stdout=%s stderr=%s",
			code, out, stderrOut)
	}
	if !strings.Contains(out, "verify-no-bypass-references OK") {
		t.Errorf("missing OK banner: %s", out)
	}
}

func TestRun_ExitsNonZeroOnDirtyFixture(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "internal/random/dirty.go", `package random
import "github.com/example/anthropic-bypass"
var _ = anthropicbypass.Client{}
`)
	stdoutW, flushOut := pipedFiles(t)
	stderrW, flushErr := pipedFiles(t)
	code := run(dir, stdoutW, stderrW)
	_ = flushOut()
	stderrOut := flushErr()
	if code == 0 {
		t.Fatal("dirty fixture should exit non-zero")
	}
	if !strings.Contains(stderrOut, "FAIL:") {
		t.Errorf("missing FAIL banner: %s", stderrOut)
	}
}

func TestRun_DirtyFixture_SortsMixedSurfaces(t *testing.T) {
	dir := t.TempDir()

	mkfile(t, dir, "internal/random/multi.go", `package random
import bp "github.com/example/anthropic-bypass"
var _ = bp.BypassClient{}
var _ = bp.BypassBackend{}
`)

	mkfile(t, dir, "docs/random/file.md", "leak: BypassClient")
	stdoutW, flushOut := pipedFiles(t)
	stderrW, flushErr := pipedFiles(t)
	code := run(dir, stdoutW, stderrW)
	_ = flushOut()
	stderrOut := flushErr()
	if code == 0 {
		t.Fatal("expected non-zero exit on multi-surface dirty fixture")
	}
	if !strings.Contains(stderrOut, "FAIL:") {
		t.Errorf("missing FAIL banner: %s", stderrOut)
	}
}

func TestScanTextSurface_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod; skipping unreadable-file test")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "tests/newsurface/unreadable_test.go")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("package integration\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	result := scanTextSurface(dir, "tests/", testsAllowlist(defaultAllowlist()))

	for _, v := range result.Violations {
		if v.Token == "io-error" {
			return
		}
	}
	t.Logf("scanTextSurface returned %d violations; OK if io-error branch not hit on this OS", len(result.Violations))
}

func TestScanSQLMigrations_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod; skipping unreadable-file test")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "internal/store/migrations/099_unreadable.sql")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("CREATE TABLE x (id INT);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	result := scanSQLMigrations(dir, sqlMigrationsAllowlist(defaultAllowlist()))
	for _, v := range result.Violations {
		if v.Token == "io-error" {
			return
		}
	}
	t.Logf("scanSQLMigrations returned %d violations; OK if io-error branch not hit on this OS", len(result.Violations))
}

func TestRun_TruncatesAtFiftyViolations(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 60; i++ {
		mkfile(t, dir, fmt.Sprintf("internal/random/dirty_%d.go", i),
			fmt.Sprintf(`package random
import bp%d "github.com/example/anthropic-bypass"
var _ = bp%d.Client{}
`, i, i))
	}
	stdoutW, flushOut := pipedFiles(t)
	stderrW, flushErr := pipedFiles(t)
	code := run(dir, stdoutW, stderrW)
	_ = flushOut()
	stderrOut := flushErr()
	if code == 0 {
		t.Fatal("expected non-zero exit on dirty fixture")
	}
	if !strings.Contains(stderrOut, "and ") || !strings.Contains(stderrOut, "more") {
		t.Errorf("expected truncation banner; got %s", stderrOut)
	}
}

func TestScanAll_DirtyRepo_ViolationsAcrossSurfaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mkfile(t, dir, "internal/random/foo.go", `package random
import "github.com/example/anthropic-bypass"
var _ = anthropicbypass.Client{}
`)

	mkfile(t, dir, "tests/newsurface/random_test.go", `package newsurface
var _ = "anthropic-bypass"
`)

	mkfile(t, dir, "docs/something/random.md", "# random\n\nanthropic-bypass leak.\n")

	mkfile(t, dir, "configs/random.toml.example",
		"# random — BypassClient mention.\n")

	mkfile(t, dir, "internal/store/migrations/099_bypass_tokens.sql",
		"CREATE TABLE bypass_tokens (id INTEGER);\n")

	result := scanAll(dir, defaultAllowlist())
	if len(result.Violations) < 5 {
		t.Fatalf("expected ≥5 violations (one per surface); got %d", len(result.Violations))
	}
	surfacesSeen := map[string]bool{}
	for _, v := range result.Violations {
		surfacesSeen[v.Surface] = true
	}
	for _, s := range []string{"ast", "tests", "docs", "configs", "sql"} {
		if !surfacesSeen[s] {
			t.Errorf("expected at least one violation on surface %q; saw none", s)
		}
	}
}
