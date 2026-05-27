// SPDX-License-Identifier: MIT
// Package cli — internal/cli/migrate_plan18.go
//
// Operator migration tooling for legacy /hades-system:* slash command
// references in operator home-directory surfaces. Subcommand
// `hades migrate release --from-hades-system-aliases` (or `hades migrate release...`
// via the wrapper). Idempotent + dry-run-by-default + allowlist-scoped +
// atomic per-file writes + backup directory.
//
// Spec ref: internal design record §Q4
// Plan ref: internal design record
//
// Catalog codes consumed:
// - migrate.allowlist-violation
// - migrate.symlink-out-of-scope
// - migrate.write-failed
// - migrate.dry-run-required
// - cli.no-op
//
// Compliance gate: inv-hades-222 — idempotency + scope guarantee.
package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	zerrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MigratePlan18Opts struct {
	HomeDir string
	// DryRun, when true, prints diffs but does not mutate files.
	// Default (zero value) is FALSE; cobra flag parser sets to TRUE by default,
	// so the cobra path always overrides; programmatic callers MUST set
	// DryRun=true explicitly when they intend dry-run mode.
	DryRun bool

	Apply bool

	IncludeAliases bool

	BackupRoot string

	Stdout io.Writer
	Stderr io.Writer
}

type migrateP18Flags struct {
	fromHadesSystemAliases bool
	dryRun                 bool
	apply                  bool
	includeAliases         bool
	backupRoot             string
	homeOverride           string
}

type allowlistEntry struct {
	Glob string

	AcceptedExts []string
	// FollowSymlinks if false, symlinks are SKIPPED with a warning. If true,
	// symlinks are followed only when their target resolves within the
	// HomeDir; out-of-scope targets emit migrate.symlink-out-of-scope.
	FollowSymlinks bool
}

type migratePlan18Match struct {
	Path         string
	LineNo       int // 1-indexed; 0 = per-file warning/skip entry
	LineText     string
	ReplacedText string
}

type migratePlan18Result struct {
	Matches       []migratePlan18Match
	FilesScanned  int
	FilesModified int
}

var allowlistEntries = []allowlistEntry{

	{Glob: ".zshrc"},
	{Glob: ".bashrc"},
	{Glob: ".zprofile"},
	{Glob: ".bash_profile"},

	{Glob: ".config/hades-system/**"},

	{Glob: ".hades/**"},

	{Glob: ".hermes/plugins/hades-system/**"},

	{Glob: ".claude/CLAUDE.md"},
	{Glob: ".claude/settings.json"},
	{Glob: ".claude/keybindings.json"},
}

var denylistRoots = []string{".git", ".ssh", ".gnupg"}

var slashRefRegex = regexp.MustCompile(`/hades-system:`)

var aliasRefRegex = regexp.MustCompile(`^(\s*alias\s+\S+\s*=\s*)(['"])([^'"]*hades-system[^'"]*)(['"])\s*$`)

var aliasUnquotedRegex = regexp.MustCompile(`^(\s*alias\s+\S+\s*=\s*)(\S*hades-system\S*)\s*$`)

// maxScanFileSize caps the file size the scanner will read.
// Files larger than this are skipped with a per-file warning.
const maxScanFileSize int64 = 100 * 1024 * 1024

func ensureStdout(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}

func ensureStderr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}

func isCodedError(err error, code string) bool {
	return zerrors.IsCode(err, zerrors.Code(code))
}

func isInAllowlist(relPath string) bool {
	if relPath == "" {
		return false
	}
	relPath = strings.TrimPrefix(relPath, "./")
	if strings.Contains(relPath, "..") {
		return false
	}

	for _, root := range denylistRoots {
		if relPath == root || strings.HasPrefix(relPath, root+"/") {
			return false
		}
	}

	for _, entry := range allowlistEntries {
		if matchAllowlistGlob(entry.Glob, relPath) {
			return true
		}
	}
	return false
}

func matchAllowlistGlob(glob, relPath string) bool {
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		return relPath == prefix || strings.HasPrefix(relPath, prefix+"/")
	}
	return relPath == glob
}

func assertPathAllowed(home, abs string) error {
	rel, err := filepath.Rel(home, abs)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.allowlist-violation"),
			fmt.Errorf("compute relative path: %w", err),
			map[string]string{"home": home, "abs": abs},
		)
	}
	relSlash := filepath.ToSlash(rel)
	if !isInAllowlist(relSlash) {
		return zerrors.New(
			zerrors.Code("migrate.allowlist-violation"),
			fmt.Errorf("file %s is not in the migration allowlist", relSlash),
			map[string]string{"home": home, "rel": relSlash},
		)
	}
	return nil
}

func uniqueAllowlistRoots() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, entry := range allowlistEntries {
		glob := entry.Glob
		slash := strings.IndexByte(glob, '/')
		var top string
		if slash < 0 {
			top = glob
		} else {
			top = glob[:slash]
		}
		if !seen[top] {
			seen[top] = true
			out = append(out, top)
		}
	}
	return out
}

func scanHomeDir(home string, includeAliases bool) (*migratePlan18Result, error) {
	info, err := os.Stat(home)
	if err != nil {
		return nil, fmt.Errorf("stat home %s: %w", home, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("home %s is not a directory", home)
	}
	result := &migratePlan18Result{}
	roots := uniqueAllowlistRoots()
	for _, root := range roots {
		rootAbs := filepath.Join(home, root)
		if _, err := os.Stat(rootAbs); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     rootAbs,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: stat error: %v)", err),
			})
			continue
		}
		if err := walkAndScan(home, rootAbs, includeAliases, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func walkAndScan(home, rootAbs string, includeAliases bool, result *migratePlan18Result) error {
	info, err := os.Lstat(rootAbs)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return scanFileIfAllowed(home, rootAbs, includeAliases, result)
	}
	return filepath.Walk(rootAbs, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     p,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: walk error: %v)", walkErr),
			})
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		return scanFileIfAllowed(home, p, includeAliases, result)
	})
}

func scanFileIfAllowed(home, absPath string, includeAliases bool, result *migratePlan18Result) error {
	rel, err := filepath.Rel(home, absPath)
	if err != nil {
		return nil
	}
	if !isInAllowlist(filepath.ToSlash(rel)) {
		return nil
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: fmt.Sprintf("(skip: lstat error: %v)", err),
		})
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(absPath)
		if err != nil {
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     absPath,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: readlink error: %v)", err),
			})
			return nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(absPath), target)
		}
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     absPath,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: abs target error: %v)", err),
			})
			return nil
		}
		homeAbs, err := filepath.Abs(home)
		if err != nil {
			return zerrors.New(
				zerrors.Code("migrate.symlink-out-of-scope"),
				fmt.Errorf("resolve home dir: %w", err),
				map[string]string{"home": home},
			)
		}
		targetRel, err := filepath.Rel(homeAbs, targetAbs)
		if err != nil || strings.HasPrefix(targetRel, "..") {
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     absPath,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: symlink-out-of-scope target=%s)", targetAbs),
			})
			return nil
		}
		if !isInAllowlist(filepath.ToSlash(targetRel)) {
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:     absPath,
				LineNo:   0,
				LineText: fmt.Sprintf("(skip: symlink-out-of-scope target=%s)", targetAbs),
			})
			return nil
		}

		absPath = targetAbs
	}

	info2, err := os.Stat(absPath)
	if err != nil {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: fmt.Sprintf("(skip: stat error: %v)", err),
		})
		return nil
	}
	if info2.Size() > maxScanFileSize {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: fmt.Sprintf("(skip: file size %d > %d max)", info2.Size(), maxScanFileSize),
		})
		return nil
	}
	result.FilesScanned++
	return scanFile(absPath, includeAliases, result)
}

// scanFile reads absPath line by line, identifies /hades-system: references
// (and with includeAliases, broader alias-line matches), and appends each
// matching line to result.Matches with the replaced text.
//
// Binary detection: a file containing any NUL byte in the first 8 KiB is
// treated as binary and skipped with a per-file warning (graceful).
func scanFile(absPath string, includeAliases bool, result *migratePlan18Result) error {
	f, err := os.Open(absPath)
	if err != nil {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: fmt.Sprintf("(skip: open error: %v)", err),
		})
		return nil
	}
	defer f.Close()

	head := make([]byte, 8192)
	n, _ := f.Read(head)
	if bytesContainsNUL(head[:n]) {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: "(skip: binary file)",
		})
		return nil
	}

	if _, err := f.Seek(0, 0); err != nil {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   0,
			LineText: fmt.Sprintf("(skip: seek error: %v)", err),
		})
		return nil
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	modified := false
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		newLine := replaceLine(line, includeAliases)
		if newLine != line {
			modified = true
			result.Matches = append(result.Matches, migratePlan18Match{
				Path:         absPath,
				LineNo:       lineNo,
				LineText:     line,
				ReplacedText: newLine,
			})
		}
	}
	if err := sc.Err(); err != nil {
		result.Matches = append(result.Matches, migratePlan18Match{
			Path:     absPath,
			LineNo:   lineNo,
			LineText: fmt.Sprintf("(scan error after %d lines: %v)", lineNo, err),
		})
		return nil
	}
	if modified {
		result.FilesModified++
	}
	return nil
}

func replaceLine(line string, includeAliases bool) string {

	newLine := slashRefRegex.ReplaceAllString(line, "/hades:")
	if !includeAliases {
		return newLine
	}

	if m := aliasRefRegex.FindStringSubmatchIndex(line); m != nil {

		prefix := line[m[2]:m[3]]
		body := line[m[6]:m[7]]
		closeQuote := line[m[8]:m[9]]
		openQuote := line[m[4]:m[5]]
		newBody := strings.ReplaceAll(body, "hades-system", "hades")

		newBody = slashRefRegex.ReplaceAllString(newBody, "/hades:")
		newLine = prefix + openQuote + newBody + closeQuote
		return newLine
	}

	if m := aliasUnquotedRegex.FindStringSubmatchIndex(line); m != nil {
		prefix := line[m[2]:m[3]]
		body := line[m[4]:m[5]]
		newBody := strings.ReplaceAll(body, "hades-system", "hades")
		newLine = prefix + newBody
		return newLine
	}
	return newLine
}

func bytesContainsNUL(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0
}

func renderDiffs(w io.Writer, home string, result *migratePlan18Result) error {

	byFile := map[string][]migratePlan18Match{}
	for _, m := range result.Matches {
		byFile[m.Path] = append(byFile[m.Path], m)
	}
	paths := make([]string, 0, len(byFile))
	for p := range byFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, _ := filepath.Rel(home, p)
		relSlash := filepath.ToSlash(rel)
		matches := byFile[p]
		for _, m := range matches {
			if m.LineNo == 0 {
				fmt.Fprintf(w, "# SKIP: %s — %s\n", relSlash, m.LineText)
			}
		}
		realMatches := []migratePlan18Match{}
		for _, m := range matches {
			if m.LineNo > 0 {
				realMatches = append(realMatches, m)
			}
		}
		if len(realMatches) == 0 {
			continue
		}
		fmt.Fprintf(w, "--- a/%s\n", relSlash)
		fmt.Fprintf(w, "+++ b/%s\n", relSlash)
		for _, m := range realMatches {
			fmt.Fprintf(w, "@@ -%d,1 +%d,1 @@\n", m.LineNo, m.LineNo)
			fmt.Fprintf(w, "-%s\n", m.LineText)
			fmt.Fprintf(w, "+%s\n", m.ReplacedText)
		}
	}
	fmt.Fprintf(w, "\n(dry-run) %d files scanned; %d files would be modified; %d total replacements\n",
		result.FilesScanned, result.FilesModified, len(result.Matches))
	fmt.Fprintln(w, "Re-run with --apply to perform the migration.")
	return nil
}

func applyMigrations(home string, result *migratePlan18Result, opts MigratePlan18Opts) error {
	backupRoot := opts.BackupRoot
	if backupRoot == "" {
		udsHome, err := os.UserHomeDir()
		if err != nil {
			return zerrors.New(
				zerrors.Code("migrate.write-failed"),
				fmt.Errorf("resolve $HOME for default backup root: %w", err),
				nil,
			)
		}
		backupRoot = filepath.Join(udsHome, ".local", "share", "hades-system", "migrate-plan-18-backup")
	}
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("mkdir backup root %s: %w", backupRoot, err),
			map[string]string{"backupRoot": backupRoot},
		)
	}

	lockPath := filepath.Join(backupRoot, "lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("acquire lock at %s: %w (another migration may be in flight)", lockPath, err),
			map[string]string{"lockPath": lockPath},
		)
	}
	defer func() {
		_ = lockFile.Close()
		_ = os.Remove(lockPath)
	}()

	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	tsDir := filepath.Join(backupRoot, ts)
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("mkdir timestamped backup dir %s: %w", tsDir, err),
			map[string]string{"tsDir": tsDir},
		)
	}

	byFile := map[string]bool{}
	for _, m := range result.Matches {
		if m.LineNo > 0 {
			byFile[m.Path] = true
		}
	}
	paths := make([]string, 0, len(byFile))
	for p := range byFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	stdout := ensureStdout(opts.Stdout)
	filesApplied := 0
	for _, absPath := range paths {
		if err := applyFile(home, absPath, tsDir, opts.IncludeAliases); err != nil {
			return err
		}
		filesApplied++
	}
	fmt.Fprintf(stdout, "applied: %d files modified; backups at %s\n", filesApplied, tsDir)
	return nil
}

func applyFile(home, absPath, tsDir string, includeAliases bool) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("read %s: %w", absPath, err),
			map[string]string{"path": absPath},
		)
	}
	rel, err := filepath.Rel(home, absPath)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("rel path %s under %s: %w", absPath, home, err),
			map[string]string{"abs": absPath, "home": home},
		)
	}
	backupPath := filepath.Join(tsDir, rel)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("mkdir backup parent %s: %w", filepath.Dir(backupPath), err),
			map[string]string{"backupPath": backupPath},
		)
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("write backup %s: %w", backupPath, err),
			map[string]string{"backupPath": backupPath},
		)
	}

	_ = includeAliases

	newContent := rewriteContent(data, includeAliases)

	tempPath := absPath + ".plan18-tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("create temp %s: %w", tempPath, err),
			map[string]string{"tempPath": tempPath},
		)
	}
	if _, err := f.Write(newContent); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("write temp %s: %w", tempPath, err),
			map[string]string{"tempPath": tempPath},
		)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("fsync temp %s: %w", tempPath, err),
			map[string]string{"tempPath": tempPath},
		)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tempPath)
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("close temp %s: %w", tempPath, err),
			map[string]string{"tempPath": tempPath},
		)
	}

	if origInfo, err := os.Stat(absPath); err == nil {
		_ = os.Chmod(tempPath, origInfo.Mode())
	}
	if err := os.Rename(tempPath, absPath); err != nil {
		_ = os.Remove(tempPath)
		return zerrors.New(
			zerrors.Code("migrate.write-failed"),
			fmt.Errorf("atomic rename %s -> %s: %w", tempPath, absPath, err),
			map[string]string{"temp": tempPath, "dest": absPath},
		)
	}
	return nil
}

func rewriteContent(data []byte, includeAliases bool) []byte {
	if !includeAliases {
		return slashRefRegex.ReplaceAll(data, []byte("/hades:"))
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = replaceLine(line, true)
	}
	return []byte(strings.Join(lines, "\n"))
}

func RunMigratePlan18(opts MigratePlan18Opts) error {
	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return zerrors.New(
				zerrors.Code("migrate.allowlist-violation"),
				fmt.Errorf("resolve $HOME: %w", err),
				nil,
			)
		}
	}
	stdout := ensureStdout(opts.Stdout)
	_ = ensureStderr(opts.Stderr)

	result, err := scanHomeDir(home, opts.IncludeAliases)
	if err != nil {
		return zerrors.New(
			zerrors.Code("migrate.allowlist-violation"),
			fmt.Errorf("scan home %s: %w", home, err),
			map[string]string{"home": home},
		)
	}

	hasRealMatches := false
	for _, m := range result.Matches {
		if m.LineNo > 0 {
			hasRealMatches = true
			break
		}
	}
	if !hasRealMatches {

		info := zerrors.New(
			zerrors.Code("cli.no-op"),
			fmt.Errorf("no /hades-system: references found in allowlisted surfaces (idempotent: already migrated)"),
			map[string]string{"home": home},
		)
		msg := Render(info, RenderOpts{
			Verbose: false,
			NoColor: true,
			Stream:  stdout,
		})
		if msg != "" {
			fmt.Fprintln(stdout, msg)
		} else {
			fmt.Fprintln(stdout, "<no changes; already migrated>")
		}
		return nil
	}

	if opts.DryRun || !opts.Apply {
		return renderDiffs(stdout, home, result)
	}

	return applyMigrations(home, result, opts)
}

func newMigratePlan18Command() *cobra.Command {
	f := &migrateP18Flags{}
	cmd := &cobra.Command{
		Use:   "plan-18",
		Short: "Migrate legacy /hades-system:* slash command references to /hades:*",
		Long: `Migrate legacy /hades-system:* slash command references in operator home-dir
surfaces to /hades:* per spec §Q4. Scope:

  ~/.zshrc, ~/.bashrc, ~/.zprofile, ~/.bash_profile
  ~/.config/hades-system/**     (any file under)
  ~/.hades/**                  (any file under)
  ~/.hermes/plugins/hades-system/**  (legacy plugin dir; pre-18b artifacts)
  ~/.claude/CLAUDE.md
  ~/.claude/settings.json
  ~/.claude/keybindings.json

NEVER touches .git/, .ssh/, .gnupg/, or anything outside the allowlist.

Safety: dry-run-by-default; per-file atomic write + backup under
~/.local/share/hades-system/migrate-plan-18-backup/<ISO-timestamp>/ on --apply.
Idempotent: second invocation against an already-migrated home dir exits 0
with <no changes; already migrated> message.

Examples:
  hades migrate plan-18 --from-hades-system-aliases               # dry-run, default
  hades migrate plan-18 --from-hades-system-aliases --apply       # mutate files
  hades migrate plan-18 --from-hades-system-aliases --include-aliases   # broader scope

See docs/operations/hades-entry-point.md §"Migration tooling" for the full
operator workflow.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRunExplicit := cmd.Flags().Changed("dry-run")
			if err := validateMigrateP18FlagsWithChanged(f, dryRunExplicit); err != nil {
				return err
			}
			opts := MigratePlan18Opts{
				HomeDir:        f.homeOverride,
				DryRun:         f.dryRun,
				Apply:          f.apply,
				IncludeAliases: f.includeAliases,
				BackupRoot:     f.backupRoot,
				Stdout:         cmd.OutOrStdout(),
				Stderr:         cmd.ErrOrStderr(),
			}
			return RunMigratePlan18(opts)
		},
	}
	cmd.Flags().BoolVar(&f.fromHadesSystemAliases, "from-hades-system-aliases", false,
		"REQUIRED: operator-explicit acknowledgement (spec §Q4 contract)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", true,
		"Print diffs; no filesystem mutation (default true)")
	cmd.Flags().BoolVar(&f.apply, "apply", false,
		"Perform mutations (atomic write + backup); mutually exclusive with --dry-run=true")
	cmd.Flags().BoolVar(&f.includeAliases, "include-aliases", false,
		"Broader replacement scope: hades-system in shell alias declarations")
	cmd.Flags().StringVar(&f.backupRoot, "backup-root", "",
		"Backup destination root (default ~/.local/share/hades-system/migrate-plan-18-backup/)")
	cmd.Flags().StringVar(&f.homeOverride, "home", "",
		"Override home dir (testing; default $HOME)")
	if err := cmd.MarkFlagRequired("from-hades-system-aliases"); err != nil {
		panic(fmt.Sprintf("MarkFlagRequired from-hades-system-aliases: %v", err))
	}
	return cmd
}

func validateMigrateP18FlagsWithChanged(f *migrateP18Flags, dryRunExplicit bool) error {
	if !f.fromHadesSystemAliases {
		return fmt.Errorf("--from-hades-system-aliases is required (spec §Q4 operator-explicit contract)")
	}
	if f.apply && dryRunExplicit && f.dryRun {
		return fmt.Errorf("--apply and --dry-run are mutually exclusive (drop one)")
	}
	if f.apply {
		f.dryRun = false
	}
	return nil
}

func validateMigrateP18Flags(f *migrateP18Flags) error {
	return validateMigrateP18FlagsWithChanged(f, true)
}
