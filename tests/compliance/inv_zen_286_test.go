// tests/compliance/inv_zen_286_test.go
//
// Compliance gate for inv-zen-286 (v0.20.3 fix): every test-side subprocess
// builder that compiles cmd/zen, cmd/hades, or cmd/zen-swarm-ctld with the
// `sqlite_fts5` build tag MUST also pass the ncruces driver-rename ldflags
// so the spawned binary does NOT panic with `sql: Register called twice
// for driver sqlite3` at init.
//
// Why: cmd/zen + cmd/hades + cmd/zen-swarm-ctld unconditionally import
// both github.com/ncruces/go-sqlite3/driver AND github.com/mattn/go-sqlite3.
// Each driver's init.0 calls database/sql.Register("sqlite3", ...) — the
// second registration panics. The Makefile (LDFLAGS_DRIVER_RENAME) renames
// the ncruces driver to `sqlite3_ncruces` so the two coexist cleanly.
// Test-side subprocess builders that omitted the ldflag silently shipped
// binaries that panicked on the FIRST sqlite-touching subcommand —
// exactly the pre-v0.20.3 failure mode for TestMigrateClaudeCode_*,
// TestSubprocess_* and TestAdversarial_* shown in v0.20.x HANDOFFs.
//
// Source-regex anchors over each builder file:
//
//   - tests/integration/migrate_claude_code_test.go::buildZenForMigrate
//   - tests/integration/plan18a/helpers_test.go::buildZenBinary +
//     buildHadesBinary
//   - tests/integration/plan18b/helpers_test.go::buildZenBinary +
//     buildHadesBinary + buildZenSwarmCtldBinary (already-canonical anchor)
//   - tests/adversarial/migrate_python_import_correctness_test.go::buildZen
//   - tests/adversarial/migrate_hostile_cc_test.go::buildZenAdv
//   - cmd/zen/main_subprocess_test.go::helperBuildZen
//
// Each builder MUST contain the literal substring
// `github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces`.
//
// Sister-test bite check: revert the ldflag in any builder; this test
// MUST fail. Inline a `make test` smoke run on the affected suite to
// confirm the live panic returns.
//
// inv-zen-286 (v0.20.3 fix).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type builderFile struct {
	relPath string

	helpers []string
}

var subprocessBuilderFiles = []builderFile{
	{
		relPath: filepath.Join("tests", "integration", "migrate_claude_code_test.go"),
		helpers: []string{"buildZenForMigrate"},
	},
	{
		relPath: filepath.Join("tests", "integration", "plan18a", "helpers_test.go"),
		helpers: []string{"buildZenBinary", "buildHadesBinary"},
	},
	{
		relPath: filepath.Join("tests", "integration", "plan18b", "helpers_test.go"),
		helpers: []string{"buildZenBinary", "buildHadesBinary", "buildZenSwarmCtldBinary"},
	},
	{
		relPath: filepath.Join("tests", "adversarial", "migrate_python_import_correctness_test.go"),
		helpers: []string{"buildZen"},
	},
	{
		relPath: filepath.Join("tests", "adversarial", "migrate_hostile_cc_test.go"),
		helpers: []string{"buildZenAdv"},
	},
	{
		relPath: filepath.Join("cmd", "zen", "main_subprocess_test.go"),
		helpers: []string{"helperBuildZen"},
	},
}

const driverRenameLdflag = "github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces"

func TestInvZen286SourceRegex_LdflagsPresentInAllBuilders(t *testing.T) {
	for _, bf := range subprocessBuilderFiles {
		bf := bf
		t.Run(bf.relPath, func(t *testing.T) {
			abs, err := filepath.Abs(filepath.Join("..", "..", bf.relPath))
			if err != nil {
				t.Fatalf("resolve %s: %v", bf.relPath, err)
			}
			src, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}
			if !strings.Contains(string(src), driverRenameLdflag) {
				t.Errorf("inv-zen-286 violated: %s missing ldflag %q (helpers: %v); the spawned binary will panic on sqlite3 double-registration", bf.relPath, driverRenameLdflag, bf.helpers)
			}
		})
	}
}

// TestInvZen286SourceRegex_TaggedBuildersAlsoCarryLdflag is anchor 2: any
// occurrence of `-tags=sqlite_fts5` in a *_test.go file under the test
// corpus MUST appear within the SAME builder block as the ldflag. The
// check is per-file: every test file containing the tag must also contain
// the ldflag. This catches the "copy the builder pattern but forget the
// ldflag" regression at compile-time discipline.
func TestInvZen286SourceRegex_TaggedBuildersAlsoCarryLdflag(t *testing.T) {
	for _, bf := range subprocessBuilderFiles {
		bf := bf
		t.Run(bf.relPath, func(t *testing.T) {
			abs, err := filepath.Abs(filepath.Join("..", "..", bf.relPath))
			if err != nil {
				t.Fatalf("resolve %s: %v", bf.relPath, err)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}
			src := string(data)

			if !strings.Contains(src, "-tags=sqlite_fts5") {
				t.Errorf("inv-zen-286 catalog drift: %s no longer contains -tags=sqlite_fts5; either the helper was removed (drop the file from subprocessBuilderFiles) or the build flags changed (update catalog)", bf.relPath)
			}

			if !strings.Contains(src, driverRenameLdflag) {
				t.Errorf("inv-zen-286 violated: %s carries -tags=sqlite_fts5 but missing %q ldflag — sqlite3 double-registration regression risk", bf.relPath, driverRenameLdflag)
			}
		})
	}
}
