// SPDX-License-Identifier: MIT
// Package conventional_commit implements conventionalCommitAnalyzer for
// hades-system HADES design (spec §1 design choice B). Enforces project instructions hard rule 2 +
// invariant.
//
// Mechanism shell out to `git log --pretty=%H %s` in the cwd OR a
// configured git directory; validate each subject against the
// conventional-commit regex; emit diagnostics for any non-conforming
// subject.
//
// Two scan modes are supported (see RunWithGitDir):
//
// - -base-ref=<ref> mode:
// scopes the scan to "<base-ref>..HEAD" so only branch-local commits
// are validated. Pre-existing commits on the base-ref are ignored;
// operators amend new violations instead of skip-hashing them.
// - -depth=N mode (legacy, default when -base-ref="" ): scans the last N
// commits via "git log -n<depth>". Retained for backward compat.
package conventional_commit

import (
	"flag"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
)

// conventionalCommitRegex matches the canonical conventional-commit subject
// format per project instructions hard rule 2:
//
// type(scope): subject
//
// # Where
//
// type = one of feat|fix|refactor|test|docs|chore|style|build|ci
// scope = lowercase letter followed by lowercase letters/digits/hyphens;
// optionally a comma-separated list of scopes (e.g., "cli, daemon")
// subject= starts with lowercase letter OR a path-like leading `/`, `(`, or
// backtick (these legitimately appear in subjects describing
// routes / files / code constructs); MUST NOT start with uppercase
// letter
var conventionalCommitRegex = regexp.MustCompile(
	`^(feat|fix|refactor|test|docs|chore|style|build|ci)\(([a-z][a-z0-9-]*(?:,\s*[a-z][a-z0-9-]*)*)\):\s+[a-z` + "`" + `/(]`)

var scopeRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*(,\s*[a-z][a-z0-9-]*)*$`)

var claudeAttributionRegex = regexp.MustCompile(
	`(?i)(co-authored-by:\s*claude|generated\s+with\s+claude)`)

var allowedTypes = map[string]bool{
	"feat":     true,
	"fix":      true,
	"refactor": true,
	"test":     true,
	"docs":     true,
	"chore":    true,
	"style":    true,
	"build":    true,
	"ci":       true,
}

type Diagnostic struct {
	CommitHash string
	Subject    string
	Message    string
}

var (
	depthFlag         = 50
	skipWhenNoGitFlag bool
	allowedScopesFlag string
	skipHashesFlag    string
	baseRefFlag       string
	flagSetOnce       = newFlagSet()
)

func newFlagSet() flag.FlagSet {
	fs := flag.NewFlagSet("conventional_commit", flag.ExitOnError)
	fs.IntVar(&depthFlag, "depth", 50,
		"how many recent commits to scan via git log --pretty=%s")
	fs.BoolVar(&skipWhenNoGitFlag, "skip-when-no-git", false,
		"silently return nil if git binary is unavailable (CI minimal images)")
	fs.StringVar(&allowedScopesFlag, "allowed-scopes", "",
		"comma-separated scope allowlist (empty = any [a-z][a-z0-9-]*)")
	fs.StringVar(&skipHashesFlag, "skip-hashes", "",
		"comma-separated commit hash prefixes to skip shape checks "+
			"(for pre-existing commits that violate the rule but cannot be amended; "+
			"Claude-attribution check still runs)")
	fs.StringVar(&baseRefFlag, "base-ref", "",
		"limit scan to commits in <base-ref>..HEAD (e.g., -base-ref=main); "+
			"when set, depth flag is ignored and ONLY branch-local commits are scanned. "+
			"Empty (default) preserves the legacy depth-based behaviour for backward compat.")
	return *fs
}

var Analyzer = &analysis.Analyzer{
	Name: "conventional_commit",
	Doc: "Scans git log subjects and verifies each matches the conventional-commit " +
		"regex (^(feat|fix|refactor|test|docs|chore|style|build|ci)\\(([a-z][a-z0-9-]*)\\): [a-z]). " +
		"Two modes: (a) -conventional_commit.base-ref=<ref> scopes the scan to " +
		"<ref>..HEAD (branch-local commits only — canonical for pre-merge gates); " +
		"(b) -conventional_commit.depth=N scans the last N commits via " +
		"`git log -n N` (legacy, default N=50). Scope allowlist via " +
		"-conventional_commit.allowed-scopes. Enforces project instructions hard rule 2 + " +
		"invariant (no Claude attribution in commit subjects).",
	Flags: flagSetOnce,
	Run:   run,
}

var runOnce atomic.Bool

func ResetOnceForTest() {
	runOnce.Store(false)
}

func run(pass *analysis.Pass) (any, error) {

	if !runOnce.CompareAndSwap(false, true) {
		return nil, nil
	}
	diags, err := RunWithGitDir(".", depthFlag, allowedScopesFlag, skipHashesFlag, baseRefFlag)
	if err != nil {
		if skipWhenNoGitFlag && strings.Contains(err.Error(), "exec: \"git\":") {
			return nil, nil
		}
		return nil, err
	}
	if len(pass.Files) == 0 {

		if len(diags) > 0 {
			scope := fmt.Sprintf("last %d commits", depthFlag)
			if baseRefFlag != "" {
				scope = fmt.Sprintf("commits in %s..HEAD", baseRefFlag)
			}
			return nil, fmt.Errorf("conventional_commit: %d violations found in %s (no source file to anchor)",
				len(diags), scope)
		}
		return nil, nil
	}
	anchor := pass.Files[0].Pos()
	for _, d := range diags {
		pass.Reportf(anchor, "%s (commit %s): %q", d.Message, d.CommitHash, d.Subject)
	}
	return nil, nil
}

func RunWithGitDir(gitDir string, depth int, allowedScopes string, skipHashes string, baseRef string) ([]Diagnostic, error) {
	if depth < 1 {
		depth = 1
	}
	var args []string
	if baseRef != "" {
		args = []string{"log", baseRef + "..HEAD", "--pretty=%H %s"}
	} else {
		args = []string{"log", "--pretty=%H %s", fmt.Sprintf("-n%d", depth)}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("conventional_commit: git log failed in %s: %v\n%s", gitDir, err, out)
	}

	allowSet := scopeAllowSet(allowedScopes)
	skipSet := skipHashSet(skipHashes)

	var diags []Diagnostic
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}

		idx := strings.IndexByte(line, ' ')
		var hash, subject string
		if idx < 0 {

			hash = line
			subject = ""
		} else {
			hash = line[:idx]
			subject = line[idx+1:]
		}

		if subject == "" {

			continue
		}

		isMergeOrRevert := strings.HasPrefix(subject, "Merge ") ||
			strings.HasPrefix(subject, `Revert "`)

		isSkippedHash := false
		for prefix := range skipSet {
			if prefix != "" && strings.HasPrefix(hash, prefix) {
				isSkippedHash = true
				break
			}
		}

		if claudeAttributionRegex.MatchString(subject) {
			diags = append(diags, Diagnostic{
				CommitHash: hash, Subject: subject,
				Message: "cc-claude-attribution: subject contains Claude attribution; " +
					"violates invariant (no Claude attribution in commits)",
			})
		}

		if isMergeOrRevert || isSkippedHash {
			continue
		}

		if strings.HasSuffix(subject, ".") {
			diags = append(diags, Diagnostic{
				CommitHash: hash, Subject: subject,
				Message: "cc-trailing-dot: subject ends with period; conventional-commit format forbids trailing period",
			})
		}

		if !conventionalCommitRegex.MatchString(subject) {

			diags = append(diags, classifyFailure(hash, subject)...)
			continue
		}

		if len(allowSet) > 0 {
			m := conventionalCommitRegex.FindStringSubmatch(subject)
			if len(m) >= 3 {
				if !allowSet[m[2]] {
					diags = append(diags, Diagnostic{
						CommitHash: hash, Subject: subject,
						Message: fmt.Sprintf("cc-bad-scope: scope %q not in allowlist (%s)",
							m[2], allowedScopes),
					})
				}
			}
		}
	}

	return diags, nil
}

func classifyFailure(hash, subject string) []Diagnostic {

	openParen := strings.IndexByte(subject, '(')
	colon := strings.Index(subject, "): ")

	if openParen < 0 || colon < 0 {
		return []Diagnostic{{
			CommitHash: hash, Subject: subject,
			Message: "cc-missing-scope: subject does not match type(scope): pattern; " +
				"format is type(scope): subject (e.g., feat(lint): add nostub analyzer)",
		}}
	}

	typeStr := subject[:openParen]
	if !allowedTypes[typeStr] {
		return []Diagnostic{{
			CommitHash: hash, Subject: subject,
			Message: fmt.Sprintf("cc-bad-type: type %q not in allowlist; "+
				"allowed: feat, fix, refactor, test, docs, chore, style, build, ci", typeStr),
		}}
	}

	scope := subject[openParen+1 : colon]
	if !scopeRegex.MatchString(scope) {
		return []Diagnostic{{
			CommitHash: hash, Subject: subject,
			Message: fmt.Sprintf("cc-bad-scope: scope %q does not match [a-z][a-z0-9-]* "+
				"(scope must start with lowercase letter; only lowercase letters, digits, hyphens)", scope),
		}}
	}

	body := subject[colon+3:]
	if body == "" {
		return []Diagnostic{{
			CommitHash: hash, Subject: subject,
			Message: "cc-bad-subject: subject body is empty after type(scope):",
		}}
	}
	if !isAllowedBodyStart(body[0]) {
		return []Diagnostic{{
			CommitHash: hash, Subject: subject,
			Message: fmt.Sprintf("cc-bad-subject: subject body must start with lowercase letter "+
				"(or /, (, `); got %q", body),
		}}
	}

	return []Diagnostic{{
		CommitHash: hash, Subject: subject,
		Message: "cc-bad-subject: subject does not match conventional-commit regex (unspecified failure)",
	}}
}

func skipHashSet(csv string) map[string]bool {
	if csv == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[s] = true
		}
	}
	return out
}

func scopeAllowSet(csv string) map[string]bool {
	if csv == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[s] = true
		}
	}
	return out
}

func isAllowedBodyStart(c byte) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	switch c {
	case '/', '(', '`':
		return true
	}
	return false
}
