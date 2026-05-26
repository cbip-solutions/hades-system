package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestMigratePlan18_PackageCompiles(t *testing.T) {
	t.Parallel()

	_ = RunMigratePlan18

	_ = MigratePlan18Opts{}
}

func TestMigratePlan18_CobraFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		wantErr   bool
		errSubstr string
		want      migrateP18Flags
	}{
		{
			name: "happy_path_dry_run_default",
			args: []string{"plan-18", "--from-zen-swarm-aliases"},
			want: migrateP18Flags{
				fromZenSwarmAliases: true,
				dryRun:              true,
				apply:               false,
				includeAliases:      false,
			},
		},
		{
			name: "happy_path_apply",
			args: []string{"plan-18", "--from-zen-swarm-aliases", "--apply"},
			want: migrateP18Flags{
				fromZenSwarmAliases: true,
				dryRun:              false,
				apply:               true,
			},
		},
		{
			name: "happy_path_include_aliases",
			args: []string{"plan-18", "--from-zen-swarm-aliases", "--include-aliases"},
			want: migrateP18Flags{
				fromZenSwarmAliases: true,
				dryRun:              true,
				includeAliases:      true,
			},
		},
		{
			name:      "missing_required_flag",
			args:      []string{"plan-18"},
			wantErr:   true,
			errSubstr: "from-zen-swarm-aliases",
		},
		{
			name:      "apply_and_dry_run_explicit_conflict",
			args:      []string{"plan-18", "--from-zen-swarm-aliases", "--apply", "--dry-run"},
			wantErr:   true,
			errSubstr: "mutually exclusive",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := newMigratePlan18Command()
			var out, errBuf bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errBuf)

			capturedFlags := migrateP18Flags{}
			cmd.RunE = func(c *cobra.Command, _ []string) error {

				capturedFlags.fromZenSwarmAliases, _ = c.Flags().GetBool("from-zen-swarm-aliases")
				capturedFlags.dryRun, _ = c.Flags().GetBool("dry-run")
				capturedFlags.apply, _ = c.Flags().GetBool("apply")
				capturedFlags.includeAliases, _ = c.Flags().GetBool("include-aliases")

				dryRunExplicit := c.Flags().Changed("dry-run")
				return validateMigrateP18FlagsWithChanged(&capturedFlags, dryRunExplicit)
			}
			cmd.SetArgs(tc.args[1:])
			err := cmd.Execute()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error containing %q; got nil", tc.errSubstr)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("want error containing %q; got %q", tc.errSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedFlags.fromZenSwarmAliases != tc.want.fromZenSwarmAliases {
				t.Errorf("fromZenSwarmAliases: want %v; got %v", tc.want.fromZenSwarmAliases, capturedFlags.fromZenSwarmAliases)
			}
			if capturedFlags.dryRun != tc.want.dryRun {
				t.Errorf("dryRun: want %v; got %v", tc.want.dryRun, capturedFlags.dryRun)
			}
			if capturedFlags.apply != tc.want.apply {
				t.Errorf("apply: want %v; got %v", tc.want.apply, capturedFlags.apply)
			}
			if capturedFlags.includeAliases != tc.want.includeAliases {
				t.Errorf("includeAliases: want %v; got %v", tc.want.includeAliases, capturedFlags.includeAliases)
			}
		})
	}
}

func TestMigratePlan18_RegisteredUnderMigrate(t *testing.T) {
	t.Parallel()
	root := NewMigrateCmd()
	var found bool
	for _, c := range root.Commands() {
		if c.Use == "plan-18" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("plan-18 not registered under NewMigrateCmd; got children: %v",
			childNames(root))
	}
}

func childNames(cmd *cobra.Command) []string {
	out := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		out = append(out, c.Use)
	}
	return out
}

func TestMigratePlan18_Allowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		relPath   string
		want      bool
		rationale string
	}{

		{"zshrc", ".zshrc", true, "shell RC file"},
		{"bashrc", ".bashrc", true, "shell RC file"},
		{"zprofile", ".zprofile", true, "shell RC file"},
		{"bash_profile", ".bash_profile", true, "shell RC file"},
		{"config_zen_swarm_toml", ".config/zen-swarm/config.toml", true, "config dir"},
		{"config_zen_swarm_nested", ".config/zen-swarm/projects/foo.yaml", true, "config dir nested"},
		{"zen_dir", ".zen/notes.md", true, "zen state dir"},
		{"zen_dir_nested", ".zen/projects/notes/foo.md", true, "zen state dir nested"},
		{"hermes_plugins_zen_swarm", ".hermes/plugins/zen-swarm/cache.txt", true, "legacy plugin dir"},
		{"claude_md", ".claude/CLAUDE.md", true, "claude memory"},
		{"claude_settings", ".claude/settings.json", true, "claude settings"},
		{"claude_keybindings", ".claude/keybindings.json", true, "claude keybindings"},

		{"git_dir", ".git/config", false, "git config NEVER touched"},
		{"ssh_dir", ".ssh/id_ed25519", false, "ssh keys NEVER touched"},
		{"gnupg_dir", ".gnupg/secring.gpg", false, "gpg keys NEVER touched"},
		{"home_root", "secret.txt", false, "outside allowlist (home root file not in list)"},
		{"random_dir", ".random/data", false, "unknown dir"},
		{"claude_random", ".claude/random.txt", false, "claude has explicit file list; random.txt NOT included"},
		{"hermes_other_plugin", ".hermes/plugins/other-plugin/cache.txt", false, "only zen-swarm legacy plugin dir; other plugins NEVER touched"},
		{"docs_dir", ".docs/note.md", false, "unknown root"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isInAllowlist(tc.relPath)
			if got != tc.want {
				t.Errorf("isInAllowlist(%q) = %v; want %v (%s)", tc.relPath, got, tc.want, tc.rationale)
			}
		})
	}
}

func TestMigratePlan18_AllowlistViolationError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	bad := filepath.Join(home, "secret.txt")
	if err := os.WriteFile(bad, []byte("nothing to migrate"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := assertPathAllowed(home, bad)
	if err == nil {
		t.Fatalf("want migrate.allowlist-violation error; got nil")
	}
	if !isCodedError(err, "migrate.allowlist-violation") {
		t.Errorf("want migrate.allowlist-violation; got %v", err)
	}
}

func TestMigratePlan18_DenylistDefenseInDepth(t *testing.T) {
	t.Parallel()
	paths := []string{".git/config", ".ssh/known_hosts", ".gnupg/pubring.kbx"}
	for _, p := range paths {
		if isInAllowlist(p) {
			t.Errorf("isInAllowlist(%q) = true; want false (denylist defense)", p)
		}
	}
}

func TestMigratePlan18_Scanner(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.FilesScanned == 0 {
		t.Fatalf("FilesScanned = 0; expected >= 6 (fixture corpus)")
	}
	if len(result.Matches) == 0 {
		t.Fatalf("Matches = 0; expected >= 7 (fixture has 7+ /zen-swarm: references)")
	}

	seenPaths := map[string]bool{}
	for _, m := range result.Matches {
		if m.LineNo > 0 {
			rel, _ := filepath.Rel(home, m.Path)
			seenPaths[filepath.ToSlash(rel)] = true
		}
	}
	wantFiles := []string{
		".zshrc", ".bashrc",
		".config/zen-swarm/config.toml",
		".zen/notes.md",
		".hermes/plugins/zen-swarm/cache.txt",
		".claude/CLAUDE.md",
	}
	for _, want := range wantFiles {
		if !seenPaths[want] {
			t.Errorf("expected match in %q; got matches: %v", want, seenPaths)
		}
	}

	for _, m := range result.Matches {
		if m.LineNo == 0 {
			continue
		}
		if strings.Contains(m.ReplacedText, "/zen-swarm:") {
			t.Errorf("match at %s:%d still contains /zen-swarm: after replacement (replaced=%q)",
				m.Path, m.LineNo, m.ReplacedText)
		}
		if !strings.Contains(m.ReplacedText, "/hades:") {
			t.Errorf("match at %s:%d expected /hades: in replacement; got %q",
				m.Path, m.LineNo, m.ReplacedText)
		}
	}
}

func setupHappyFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	src := filepath.Join("testdata", "migrate_plan18", "happy")
	if err := copyTree(src, home); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return home
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, info.Mode().Perm())
	})
}

func TestMigratePlan18_BinaryFilesSkipped(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bin := filepath.Join(home, ".zshrc")
	binData := []byte{0x00, 0x01, 0x02, '/', 'z', 'e', 'n', '-', 's', 'w', 'a', 'r', 'm', ':', 's'}
	if err := os.WriteFile(bin, binData, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var sawSkip bool
	for _, m := range result.Matches {
		if m.Path == bin && m.LineNo == 0 && strings.Contains(m.LineText, "(skip: binary file)") {
			sawSkip = true
			break
		}
	}
	if !sawSkip {
		t.Errorf("expected (skip: binary file) entry for %s; got matches: %+v", bin, result.Matches)
	}
	for _, m := range result.Matches {
		if m.Path == bin && m.LineNo > 0 {
			t.Errorf("binary file %s should not have line-level matches; got %+v", bin, m)
		}
	}
}

func TestMigratePlan18_DryRunDiffOutput(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	var out, errBuf bytes.Buffer
	opts := MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
		Stderr:  &errBuf,
	}
	if err := RunMigratePlan18(opts); err != nil {
		t.Fatalf("RunMigratePlan18: %v", err)
	}
	got := out.String()
	mustContain := []string{
		"--- ", "+++ ",
		`-alias zs="run /zen-swarm:start"`,
		`+alias zs="run /hades:start"`,
		"-/zen-swarm:dashboard",
		"+/hades:dashboard",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q\nfull output:\n%s", want, got)
		}
	}

	data, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("read post-dryrun: %v", err)
	}
	if !strings.Contains(string(data), "/zen-swarm:") {
		t.Errorf("dry-run mutated .zshrc; expected /zen-swarm: still present")
	}
}

func TestMigratePlan18_DryRunNoMutation(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)

	preState := map[string][]byte{}
	_ = filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		preState[p] = data
		return nil
	})
	var out bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
	})
	if err != nil {
		t.Fatalf("RunMigratePlan18: %v", err)
	}
	for p, want := range preState {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s post-dryrun: %v", p, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("file mutated by dry-run: %s\npre: %q\npost: %q", p, want, got)
		}
	}
}

func TestMigratePlan18_ApplyHappyPath(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")
	var out, errBuf bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		DryRun:     false,
		BackupRoot: backupRoot,
		Stdout:     &out,
		Stderr:     &errBuf,
	})
	if err != nil {
		t.Fatalf("RunMigratePlan18 apply: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errBuf.String())
	}
	relFiles := []string{
		".zshrc", ".bashrc",
		".config/zen-swarm/config.toml",
		".zen/notes.md",
		".hermes/plugins/zen-swarm/cache.txt",
		".claude/CLAUDE.md",
	}
	for _, rel := range relFiles {
		data, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if strings.Contains(string(data), "/zen-swarm:") {
			t.Errorf("%s still contains /zen-swarm: after apply\ncontent:\n%s", rel, data)
		}
		if !strings.Contains(string(data), "/hades:") {
			t.Errorf("%s missing /hades: after apply\ncontent:\n%s", rel, data)
		}
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 timestamped backup dir; got %d: %v", len(entries), entries)
	}
	tsDir := filepath.Join(backupRoot, entries[0].Name())
	for _, rel := range relFiles {
		backup := filepath.Join(tsDir, rel)
		data, err := os.ReadFile(backup)
		if err != nil {
			t.Errorf("backup missing for %s: %v", rel, err)
			continue
		}
		if !strings.Contains(string(data), "/zen-swarm:") {
			t.Errorf("backup for %s does not contain pre-apply /zen-swarm: marker; backup got %q", rel, data)
		}
	}
}

func TestMigratePlan18_ConcurrentApplyLocked(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	lockPath := filepath.Join(backupRoot, "lock")
	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatalf("setup lock: %v", err)
	}
	f.Close()
	var out, errBuf bytes.Buffer
	err = RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		DryRun:     false,
		BackupRoot: backupRoot,
		Stdout:     &out,
		Stderr:     &errBuf,
	})
	if err == nil {
		t.Fatalf("want migrate.write-failed error; got nil")
	}
	if !isCodedError(err, "migrate.write-failed") {
		t.Errorf("want migrate.write-failed code; got %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(data), "/zen-swarm:") {
		t.Errorf(".zshrc mutated despite locked apply; expected /zen-swarm: still present")
	}
}

func TestMigratePlan18_AtomicWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")
	zenDir := filepath.Join(home, ".zen")
	if err := os.Chmod(zenDir, 0o555); err != nil {
		t.Fatalf("setup chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(zenDir, 0o755) })
	var out, errBuf bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		DryRun:     false,
		BackupRoot: backupRoot,
		Stdout:     &out,
		Stderr:     &errBuf,
	})
	if err == nil {
		t.Fatalf("want migrate.write-failed; got nil")
	}
	if !isCodedError(err, "migrate.write-failed") {
		t.Errorf("want migrate.write-failed; got %v", err)
	}
}

func TestMigratePlan18_IdempotencyApply(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")
	var out1, err1 bytes.Buffer
	if err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		BackupRoot: backupRoot,
		Stdout:     &out1,
		Stderr:     &err1,
	}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	firstState := map[string][]byte{}
	_ = filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		firstState[p] = data
		return nil
	})

	backupRoot2 := filepath.Join(t.TempDir(), "backups-2")
	var out2, err2 bytes.Buffer
	if err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		BackupRoot: backupRoot2,
		Stdout:     &out2,
		Stderr:     &err2,
	}); err != nil {
		t.Fatalf("second apply (idempotency): %v", err)
	}
	if !strings.Contains(out2.String(), "no changes") && !strings.Contains(err2.String(), "no changes") {
		t.Errorf("second apply: want <no changes; already migrated> message; got stdout=%q stderr=%q", out2.String(), err2.String())
	}
	for p, want := range firstState {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s post-second: %v", p, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("file mutated by idempotent second apply: %s", p)
		}
	}

	entries, _ := os.ReadDir(backupRoot2)
	tsDirCount := 0
	for _, e := range entries {
		if e.IsDir() {
			tsDirCount++
		}
	}
	if tsDirCount > 0 {
		t.Errorf("second apply (no-op) should not create timestamped backup dir; got %d", tsDirCount)
	}
}

func TestMigratePlan18_IdempotencyDryRun(t *testing.T) {
	t.Parallel()
	home := setupAlreadyMigratedFixture(t)
	var out bytes.Buffer
	if err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
	}); err != nil {
		t.Fatalf("dry-run on already-migrated: %v", err)
	}
	if !strings.Contains(out.String(), "no changes") {
		t.Errorf("want <no changes; already migrated> message; got %q", out.String())
	}
}

func setupAlreadyMigratedFixture(t *testing.T) string {
	t.Helper()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "pre-backups")
	if err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		BackupRoot: backupRoot,
	}); err != nil {
		t.Fatalf("pre-apply for already-migrated fixture: %v", err)
	}
	return home
}

func TestMigratePlan18_SymlinkInsideAllowlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()
	home := t.TempDir()
	target := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup zshrc: %v", err)
	}
	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Matches) == 0 {
		t.Errorf("expected at least one match; got 0\nresult: %+v", result)
	}
}

func TestMigratePlan18_SymlinkOutsideAllowlistRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside_target.txt")
	if err := os.WriteFile(outside, []byte("/zen-swarm:secret\n"), 0o644); err != nil {
		t.Fatalf("setup outside: %v", err)
	}
	sym := filepath.Join(home, ".zshrc")
	if err := os.Symlink(outside, sym); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}
	result, err := scanHomeDir(home, false)
	if err != nil {
		if !isCodedError(err, "migrate.symlink-out-of-scope") {
			t.Fatalf("want migrate.symlink-out-of-scope; got %v", err)
		}
		return
	}
	var saw bool
	for _, m := range result.Matches {
		if m.LineNo == 0 && strings.Contains(m.LineText, "symlink-out-of-scope") {
			saw = true
			break
		}
	}
	if !saw {
		t.Errorf("expected symlink-out-of-scope skip entry; got matches: %+v", result.Matches)
	}
	data, _ := os.ReadFile(outside)
	if !strings.Contains(string(data), "/zen-swarm:") {
		t.Errorf("outside file mutated by scan; expected /zen-swarm: still present")
	}
}

func TestMigratePlan18_ReplaceLineIncludeAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		line           string
		includeAliases bool
		want           string
	}{
		{
			name:           "default_slash_only",
			line:           `alias zs="run /zen-swarm:start"`,
			includeAliases: false,
			want:           `alias zs="run /hades:start"`,
		},
		{
			name:           "default_bare_alias_not_rewritten",
			line:           `alias zs="zen-swarm-cli --version"`,
			includeAliases: false,
			want:           `alias zs="zen-swarm-cli --version"`,
		},
		{
			name:           "default_text_zen_swarm_not_rewritten",
			line:           `# This is about zen-swarm system`,
			includeAliases: false,
			want:           `# This is about zen-swarm system`,
		},
		{
			name:           "aliases_slash_in_alias",
			line:           `alias zs="run /zen-swarm:start"`,
			includeAliases: true,
			want:           `alias zs="run /hades:start"`,
		},
		{
			name:           "aliases_bare_in_alias",
			line:           `alias zsd="zen-swarm-doctor --verbose"`,
			includeAliases: true,
			want:           `alias zsd="hades-doctor --verbose"`,
		},
		{
			name:           "aliases_no_alias_unchanged",
			line:           `# This is about zen-swarm system`,
			includeAliases: true,
			want:           `# This is about zen-swarm system`,
		},
		{
			name:           "aliases_mixed_slash_and_bare",
			line:           `alias x="/zen-swarm:start && zen-swarm doctor"`,
			includeAliases: true,
			want:           `alias x="/hades:start && hades doctor"`,
		},
		{
			name:           "aliases_single_quoted",
			line:           `alias y='zen-swarm:dashboard'`,
			includeAliases: true,
			want:           `alias y='hades:dashboard'`,
		},
		{
			name:           "default_alias_word_in_text",
			line:           `# fix the alias zen-swarm name`,
			includeAliases: false,
			want:           `# fix the alias zen-swarm name`,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := replaceLine(tc.line, tc.includeAliases)
			if got != tc.want {
				t.Errorf("replaceLine(%q, %v) = %q; want %q", tc.line, tc.includeAliases, got, tc.want)
			}
		})
	}
}

func TestMigratePlan18_IncludeAliasesEndToEnd(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	src := filepath.Join("testdata", "migrate_plan18", "include_aliases")
	if err := copyTree(src, home); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	backupRoot := filepath.Join(t.TempDir(), "backups")
	var out bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:        home,
		Apply:          true,
		IncludeAliases: true,
		BackupRoot:     backupRoot,
		Stdout:         &out,
	})
	if err != nil {
		t.Fatalf("apply --include-aliases: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("read post-apply: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "/zen-swarm:") {
		t.Errorf("/zen-swarm: still present after apply --include-aliases\n%s", content)
	}
	if strings.Contains(content, `alias zsd="zen-swarm-doctor`) {
		t.Errorf("zen-swarm-doctor NOT rewritten in alias declaration\n%s", content)
	}
	if !strings.Contains(content, "plain text about zen-swarm") {
		t.Errorf("plain text comment 'plain text about zen-swarm' SHOULD remain unchanged\n%s", content)
	}
}

func TestMigratePlan18_AdversarialBinaryInAllowlist(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bin := filepath.Join(home, ".config", "zen-swarm", "cache.bin")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	buf := make([]byte, 16*1024)
	for i := range buf {
		buf[i] = byte(i % 256)
	}
	buf[100] = 0x00
	buf[101] = 0x01
	copy(buf[200:], "/zen-swarm:fake")
	if err := os.WriteFile(bin, buf, 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup zshrc: %v", err)
	}
	var out bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
	})
	if err != nil {
		t.Fatalf("RunMigratePlan18: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "binary file") {
		t.Errorf("expected binary file skip; got output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "-/zen-swarm:start") {
		t.Errorf("expected .zshrc diff; got output:\n%s", outStr)
	}
}

func TestMigratePlan18_AdversarialCircularSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	a := filepath.Join(configDir, "a")
	b := filepath.Join(configDir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("setup symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("setup symlink b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "real.toml"), []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup real: %v", err)
	}
	done := make(chan struct{})
	var scanErr error
	go func() {
		defer close(done)
		var out bytes.Buffer
		scanErr = RunMigratePlan18(MigratePlan18Opts{
			HomeDir: home,
			DryRun:  true,
			Stdout:  &out,
		})
	}()
	select {
	case <-done:
		if scanErr != nil {
			t.Logf("scan returned (acceptable): %v", scanErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("scan stuck in circular-symlink loop after 10s")
	}
}

func TestMigratePlan18_AdversarialPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; chmod 0000 doesn't deny root access")
	}
	t.Parallel()
	home := t.TempDir()
	readable := filepath.Join(home, ".zshrc")
	denied := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(readable, []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup readable: %v", err)
	}
	if err := os.WriteFile(denied, []byte("/zen-swarm:handoff\n"), 0o000); err != nil {
		t.Fatalf("setup denied: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, 0o644) })
	var out bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
	})
	if err != nil {
		t.Fatalf("RunMigratePlan18: %v", err)
	}
	if !strings.Contains(out.String(), "-/zen-swarm:start") {
		t.Errorf("expected .zshrc diff; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), ".bashrc") {
		t.Errorf("expected .bashrc skip line; output:\n%s", out.String())
	}
}

func TestMigratePlan18_AdversarialLargeFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	large := filepath.Join(home, ".config", "zen-swarm", "huge.log")
	if err := os.MkdirAll(filepath.Dir(large), 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	f, err := os.Create(large)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}
	if err := f.Truncate(int64(maxScanFileSize) + 1); err != nil {
		f.Close()
		t.Fatalf("setup truncate: %v", err)
	}
	f.Close()
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup zshrc: %v", err)
	}
	var out bytes.Buffer
	err = RunMigratePlan18(MigratePlan18Opts{
		HomeDir: home,
		DryRun:  true,
		Stdout:  &out,
	})
	if err != nil {
		t.Fatalf("RunMigratePlan18: %v", err)
	}
	if !strings.Contains(out.String(), "huge.log") {
		t.Errorf("expected huge.log skip line; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "file size") {
		t.Errorf("expected file size skip reason; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "-/zen-swarm:start") {
		t.Errorf("expected .zshrc diff; output:\n%s", out.String())
	}
}

func TestMigratePlan18_BytesContainsNUL(t *testing.T) {
	t.Parallel()
	if bytesContainsNUL(nil) {
		t.Errorf("bytesContainsNUL(nil) = true; want false")
	}
	if bytesContainsNUL([]byte{}) {
		t.Errorf("bytesContainsNUL(empty) = true; want false")
	}
	if !bytesContainsNUL([]byte{0x00}) {
		t.Errorf("bytesContainsNUL([0x00]) = false; want true")
	}
	if !bytesContainsNUL([]byte{1, 2, 0, 3}) {
		t.Errorf("bytesContainsNUL([1,2,0,3]) = false; want true")
	}
	if bytesContainsNUL([]byte("hello")) {
		t.Errorf("bytesContainsNUL(\"hello\") = true; want false")
	}
}

func TestMigratePlan18_UniqueAllowlistRoots(t *testing.T) {
	t.Parallel()
	roots := uniqueAllowlistRoots()
	if len(roots) == 0 {
		t.Fatalf("uniqueAllowlistRoots = []; want at least 8")
	}
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r] {
			t.Errorf("duplicate root in uniqueAllowlistRoots: %q", r)
		}
		seen[r] = true
	}
	wantSome := []string{".zshrc", ".bashrc", ".config", ".zen", ".hermes", ".claude"}
	for _, w := range wantSome {
		if !seen[w] {
			t.Errorf("expected root %q in uniqueAllowlistRoots; got %v", w, roots)
		}
	}
}

func TestMigratePlan18_ValidateMigrateP18Flags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		f         migrateP18Flags
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_from_alias_true",
			f:       migrateP18Flags{fromZenSwarmAliases: true, dryRun: true},
			wantErr: false,
		},
		{
			name:      "missing_from_alias",
			f:         migrateP18Flags{fromZenSwarmAliases: false},
			wantErr:   true,
			errSubstr: "--from-zen-swarm-aliases is required",
		},
		{

			name:    "apply_dry_run_off",
			f:       migrateP18Flags{fromZenSwarmAliases: true, apply: true, dryRun: false},
			wantErr: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := tc.f
			err := validateMigrateP18Flags(&f)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error containing %q; got nil", tc.errSubstr)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("want error %q; got %q", tc.errSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.f.apply && f.dryRun {
				t.Errorf("validateMigrateP18Flags: apply=true did not keep dryRun false")
			}
		})
	}
}

func TestMigratePlan18_IsInAllowlistEdgeCases(t *testing.T) {
	t.Parallel()

	if isInAllowlist("") {
		t.Errorf("isInAllowlist(\"\") = true; want false (empty path)")
	}

	if isInAllowlist("../etc/passwd") {
		t.Errorf("isInAllowlist(\"../etc/passwd\") = true; want false (traversal)")
	}

	if isInAllowlist(".config/zen-swarm/../../.ssh/id_rsa") {
		t.Errorf("isInAllowlist with .. component = true; want false")
	}
}

func TestMigratePlan18_CobraRunEHappyPath(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	cmd := newMigratePlan18Command()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--from-zen-swarm-aliases",
		"--dry-run",
		"--home", home,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cobra Execute: %v\noutput:\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "/hades:") {
		t.Errorf("expected /hades: in dry-run output; got:\n%s", out.String())
	}
}

func TestMigratePlan18_CobraRunEApply(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")
	cmd := newMigratePlan18Command()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--from-zen-swarm-aliases",
		"--apply",
		"--home", home,
		"--backup-root", backupRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cobra Execute apply: %v\noutput:\n%s", err, out.String())
	}

	data, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if strings.Contains(string(data), "/zen-swarm:") {
		t.Errorf(".zshrc still contains /zen-swarm: after apply\n%s", data)
	}
}

func TestMigratePlan18_CobraRunEValidationError(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)
	cmd := newMigratePlan18Command()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	cmd.SetArgs([]string{
		"--from-zen-swarm-aliases",
		"--apply",
		"--dry-run",
		"--home", home,
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected validation error; got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want mutually-exclusive error; got %q", err.Error())
	}
}

func TestMigratePlan18_ScanHomeDirStatError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	configPath := filepath.Join(home, ".config")
	if err := os.WriteFile(configPath, []byte("/zen-swarm:start"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = result
}

func TestMigratePlan18_ScanHomeDirNotExist(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesScanned != 0 {
		t.Errorf("FilesScanned = %d; want 0 (empty home)", result.FilesScanned)
	}
	if len(result.Matches) != 0 {
		t.Errorf("Matches = %d; want 0 (empty home)", len(result.Matches))
	}
}

func TestMigratePlan18_WalkAndScanWalkError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; chmod 0000 doesn't deny root access")
	}
	t.Parallel()
	home := t.TempDir()

	zenDir := filepath.Join(home, ".config", "zen-swarm", "subdir")
	if err := os.MkdirAll(zenDir, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(home, ".config", "zen-swarm", "config.toml"), []byte("/zen-swarm:start\n"), 0o644); err != nil {
		t.Fatalf("setup toml: %v", err)
	}

	if err := os.Chmod(zenDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(zenDir, 0o755) })
	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	if len(result.Matches) == 0 {
		t.Errorf("expected matches; got 0")
	}
}

func TestMigratePlan18_ApplyMigrationsDefaultBackupRoot(t *testing.T) {
	t.Parallel()
	home := setupHappyFixture(t)

	var out bytes.Buffer
	err := RunMigratePlan18(MigratePlan18Opts{
		HomeDir:    home,
		Apply:      true,
		BackupRoot: "",
		Stdout:     &out,
	})

	if err != nil {
		t.Fatalf("RunMigratePlan18 default backup root: %v\noutput:\n%s", err, out.String())
	}

	data, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if strings.Contains(string(data), "/zen-swarm:") {
		t.Errorf(".zshrc still contains /zen-swarm: after apply\n%s", data)
	}
}

func TestMigratePlan18_ReplaceLineUnquotedAlias(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "unquoted_alias",
			line: `alias zs=zen-swarm-cli`,
			want: `alias zs=hades-cli`,
		},
		{
			name: "unquoted_alias_with_path",
			line: `alias zsd=zen-swarm-doctor`,
			want: `alias zsd=hades-doctor`,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := replaceLine(tc.line, true)
			if got != tc.want {
				t.Errorf("replaceLine(%q, true) = %q; want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestMigratePlan18_ApplyFileReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bypass")
	}
	t.Parallel()
	home := setupHappyFixture(t)
	backupRoot := filepath.Join(t.TempDir(), "backups")

	result, err := scanHomeDir(home, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	zshrc := filepath.Join(home, ".zshrc")
	if err := os.Remove(zshrc); err != nil {
		t.Fatalf("remove: %v", err)
	}

	ts := "2026-01-01T00-00-00Z"
	tsDir := filepath.Join(backupRoot, ts)
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		t.Fatalf("mkdir tsDir: %v", err)
	}
	err = applyFile(home, zshrc, tsDir, false)
	if err == nil {
		t.Fatalf("expected migrate.write-failed; got nil")
	}
	if !isCodedError(err, "migrate.write-failed") {
		t.Errorf("want migrate.write-failed; got %v", err)
	}
	_ = result
}

func TestMigratePlan18_ApplyFileBackupWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bypass")
	}
	t.Parallel()
	home := setupHappyFixture(t)
	ts := "2026-01-01T00-00-00Z"
	tsDir := filepath.Join(t.TempDir(), ts)

	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		t.Fatalf("mkdir tsDir: %v", err)
	}

	if err := os.Chmod(tsDir, 0o555); err != nil {
		t.Fatalf("chmod tsDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tsDir, 0o755) })
	zshrc := filepath.Join(home, ".zshrc")
	err := applyFile(home, zshrc, tsDir, false)
	if err == nil {
		t.Fatalf("expected migrate.write-failed; got nil")
	}
	if !isCodedError(err, "migrate.write-failed") {
		t.Errorf("want migrate.write-failed; got %v", err)
	}
}
