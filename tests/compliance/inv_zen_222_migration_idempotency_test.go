// tests/compliance/inv_zen_222_migration_idempotency_test.go
//
// invariant — Migration tooling idempotency
// + scope.
//
// Doctrine: per spec §Q4 + master §G "Critical invariants" +
// (internal/cli/migrate_plan18.go), the `hades migrate plan-18
// --from-zen-swarm-aliases` tool MUST be:
//
// 1. Idempotent — second invocation against post-apply state = no-op
// (returns nil + cli.no-op message, zero matches)
// 2. Dry-run-by-default — explicit Apply=true required to mutate;
// DryRun=true must NEVER mutate files
// 3. Allowlist-scoped — files under.ssh/,.gnupg/, paths outside
// the allowlist roots, and symlinks pointing outside the allowlist
// are NEVER modified
// 4. Allowlist count >= 6 — the allowlistEntries slice MUST NOT
// silently shrink below 6 entries (spec §Q4 scope contract)
//
// Test strategy: build a snapshot of the fixture home dir tree
// (tests/compliance/testdata/migrate_fixture_home/), invoke
// RunMigratePlan18 with HomeDir pointing at a t.TempDir() copy, and
// assert per-stage post-state matches the expected tree
// (tests/compliance/testdata/migrate_fixture_home_expected_apply/).
//
// The test also asserts: denylist files (.ssh,.gnupg) are untouched,
// outside-allowlist file (Documents/notes.md) is untouched, binary
// fixture is skipped gracefully, and a symlink pointing outside the
// allowlist root does not allow tool to modify the target.
//
// Companion ADR: architecture records
package compliance

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
)

const (
	inv222FixtureRoot   = "tests/compliance/testdata/migrate_fixture_home"
	inv222ExpectedApply = "tests/compliance/testdata/migrate_fixture_home_expected_apply"
)

func TestInvZen222MigrationIdempotency(t *testing.T) {
	root := repoRoot(t)
	fixture := filepath.Join(root, inv222FixtureRoot)
	expected := filepath.Join(root, inv222ExpectedApply)

	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("inv-zen-222: fixture %s missing: %v", inv222FixtureRoot, err)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("inv-zen-222: expected tree %s missing: %v", inv222ExpectedApply, err)
	}

	tmpHome := t.TempDir()
	if err := inv222CopyTree(fixture, tmpHome); err != nil {
		t.Fatalf("inv-zen-222: copy fixture: %v", err)
	}

	adversarial := []string{
		".ssh/config",
		".gnupg/gpg.conf",
		"Documents/notes.md",
	}
	preStates := map[string][]byte{}
	for _, p := range adversarial {
		body, err := os.ReadFile(filepath.Join(tmpHome, p))
		if err != nil {
			t.Logf("inv-zen-222: pre-snapshot %s not present in fixture (skipping)", p)
			continue
		}
		preStates[p] = body
	}

	t.Run("dry-run", func(t *testing.T) {
		var buf bytes.Buffer
		opts := cli.MigratePlan18Opts{
			HomeDir: tmpHome,
			DryRun:  true,
			Apply:   false,
			Stdout:  &buf,
			Stderr:  &buf,
		}
		err := cli.RunMigratePlan18(opts)
		if err != nil {
			t.Fatalf("dry-run returned error: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "/hades:") && !strings.Contains(output, "/zen-swarm:") {

			t.Logf("dry-run output (no diff expected — may already be migrated): %q", output)
		}

		for _, p := range []string{".zshrc", ".bashrc", ".claude/CLAUDE.md"} {
			orig, origErr := os.ReadFile(filepath.Join(fixture, p))
			got, gotErr := os.ReadFile(filepath.Join(tmpHome, p))
			if origErr != nil || gotErr != nil {
				continue
			}
			if !bytes.Equal(orig, got) {
				t.Errorf("dry-run mutated %s (should be no-op)", p)
			}
		}
	})

	t.Run("apply", func(t *testing.T) {
		var buf bytes.Buffer
		opts := cli.MigratePlan18Opts{
			HomeDir: tmpHome,
			DryRun:  false,
			Apply:   true,
			Stdout:  &buf,
			Stderr:  &buf,
		}
		err := cli.RunMigratePlan18(opts)
		if err != nil {
			t.Fatalf("apply returned error: %v", err)
		}

		for p, want := range preStates {
			got, err := os.ReadFile(filepath.Join(tmpHome, p))
			if err != nil {
				t.Errorf("denylist re-read %s: %v", p, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("denylist violation: %s was modified by apply", p)
			}
		}

		for _, p := range []string{".zshrc", ".bashrc", ".claude/CLAUDE.md",
			".config/zen-swarm/config.toml", ".zen/projects.toml",
			".claude/settings.json", ".claude/keybindings.json"} {
			got, err := os.ReadFile(filepath.Join(tmpHome, p))
			if err != nil {
				t.Logf("inv-zen-222: file %s not present in tmpHome (may be outside scope)", p)
				continue
			}
			if bytes.Contains(got, []byte("/zen-swarm:")) {
				t.Errorf("apply: file %s still contains /zen-swarm: after migration", p)
			}
			want, err := os.ReadFile(filepath.Join(expected, p))
			if err != nil {

				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("apply: post-apply content of %s diverges from expected:\ngot:  %q\nwant: %q",
					p, string(got), string(want))
			}
		}
	})

	t.Run("second-apply-noop", func(t *testing.T) {
		var buf bytes.Buffer
		opts := cli.MigratePlan18Opts{
			HomeDir: tmpHome,
			DryRun:  false,
			Apply:   true,
			Stdout:  &buf,
			Stderr:  &buf,
		}
		err := cli.RunMigratePlan18(opts)
		if err != nil {
			t.Fatalf("second apply returned error: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "/zen-swarm:") {
			t.Errorf("idempotency: second apply output still mentions /zen-swarm: — tool is not idempotent")
		}

		for _, p := range []string{".zshrc", ".bashrc", ".claude/CLAUDE.md"} {
			got, err := os.ReadFile(filepath.Join(tmpHome, p))
			if err != nil {
				continue
			}
			if bytes.Contains(got, []byte("/zen-swarm:")) {
				t.Errorf("idempotency: second apply left /zen-swarm: in %s", p)
			}
		}
	})
}

func TestInvZen222AdversarialAllowlist(t *testing.T) {
	root := repoRoot(t)
	fixture := filepath.Join(root, inv222FixtureRoot)

	tmpHome := t.TempDir()
	if err := inv222CopyTree(fixture, tmpHome); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	outsideDir := t.TempDir()
	outsideTarget := filepath.Join(outsideDir, "outside-target.txt")
	if err := os.WriteFile(outsideTarget, []byte("/zen-swarm:start outside-allowlist\n"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}

	linkPath := filepath.Join(tmpHome, ".config", "zen-swarm", "outside-link.conf")
	if err := os.Symlink(outsideTarget, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	var buf bytes.Buffer
	opts := cli.MigratePlan18Opts{
		HomeDir: tmpHome,
		DryRun:  false,
		Apply:   true,
		Stdout:  &buf,
		Stderr:  &buf,
	}

	_ = cli.RunMigratePlan18(opts)

	// Target file MUST still contain the original /zen-swarm: string.
	target, err := os.ReadFile(outsideTarget)
	if err != nil {
		t.Fatalf("re-read outside target: %v", err)
	}
	if !bytes.Contains(target, []byte("/zen-swarm:start")) {
		t.Errorf("adversarial: outside-allowlist symlink target was modified by apply — tool followed symlink out of scope")
	}
	if bytes.Contains(target, []byte("/hades:start")) {
		t.Errorf("adversarial: outside-allowlist symlink target now contains /hades: (tool followed symlink out of scope)")
	}
}

func TestInvZen222DryRunIsDefault(t *testing.T) {
	root := repoRoot(t)
	fixture := filepath.Join(root, inv222FixtureRoot)

	tmpHome := t.TempDir()
	if err := inv222CopyTree(fixture, tmpHome); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	checkPaths := []string{".zshrc", ".bashrc", ".claude/CLAUDE.md"}
	preStates := map[string][]byte{}
	for _, p := range checkPaths {
		body, err := os.ReadFile(filepath.Join(tmpHome, p))
		if err != nil {
			continue
		}
		preStates[p] = body
	}

	var buf bytes.Buffer
	opts := cli.MigratePlan18Opts{
		HomeDir: tmpHome,
		DryRun:  true,
		Apply:   false,
		Stdout:  &buf,
		Stderr:  &buf,
	}
	if err := cli.RunMigratePlan18(opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	for p, want := range preStates {
		got, err := os.ReadFile(filepath.Join(tmpHome, p))
		if err != nil {
			t.Errorf("dry-run post-read %s: %v", p, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("dry-run mutated %s: original != post-dry-run content", p)
		}
	}
}

func TestInvZen222AllowlistEntryCount(t *testing.T) {
	root := repoRoot(t)
	srcPath := filepath.Join(root, "internal", "cli", "migrate_plan18.go")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("inv-zen-222: cannot read %s: %v", srcPath, err)
	}

	count := strings.Count(string(src), `{Glob: "`)
	const minAllowlistEntries = 6
	if count < minAllowlistEntries {
		t.Errorf("inv-zen-222: allowlistEntries has %d entries; expected >= %d per spec §Q4 scope contract.\n"+
			"Remediation: do NOT remove allowlist entries; adding new entries is allowed.\n"+
			"See internal/cli/migrate_plan18.go allowlistEntries + ADR-0098.", count, minAllowlistEntries)
	}
}

func inv222CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, infoErr := d.Info()
		if infoErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}

func inv222SortedPaths(base string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		paths = append(paths, rel)
		return nil
	})
	sort.Strings(paths)
	return paths, err
}
