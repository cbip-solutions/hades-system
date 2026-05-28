// SPDX-License-Identifier: MIT
// invariant: removing or renaming Schema fields requires an ADR
// reference matching architecture records in the
// commit body. The Makefile target verify-doctrine-schema-additive-only
// invokes ValidateRange against HEAD~1..HEAD by default; CI runs the
// same target against the merge-commit range.
//
// The validator parses git diff output for the schema.go file and
// inspects removed lines (lines starting with "-" not "---") for
// toml:"..." tag declarations. Any removed tag without a corresponding
// ADR reference in the commit body is a violation.
//
// Pure parsing — no I/O during ValidateAdditive (RunGitDiff /
// RunGitCommitBody are the only I/O entry points, separated for
// testability).

package doctrine

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type ValidationResult struct {
	OK bool

	Violations []string
}

var adrPathPattern = regexp.MustCompile("architecture records")

var schemaFilePattern = regexp.MustCompile(`(?m)^diff --git a/internal/doctrine/schema\.go b/internal/doctrine/schema\.go`)

var safeRefPattern = regexp.MustCompile(`^[A-Za-z0-9_./~^][A-Za-z0-9_./~^-]*$`)

func validateRef(name, ref string) error {
	if !safeRefPattern.MatchString(ref) {
		return fmt.Errorf("doctrine: invalid ref %q for %s (refs must not start with '-' and use [A-Za-z0-9_./~^-])", ref, name)
	}
	return nil
}

var removedTomlTagPattern = regexp.MustCompile(`^-[^-].*` + "`" + `[^` + "`" + `]*toml:"([a-z0-9_]+)(?:,[^"]*)?"`)

func ValidateAdditive(diff, commitBody string) (ValidationResult, error) {
	if !schemaFilePattern.MatchString(diff) {
		return ValidationResult{OK: true}, nil
	}
	removed := extractRemovedTomlTags(diff)
	if len(removed) == 0 {
		return ValidationResult{OK: true}, nil
	}
	if adrPathPattern.MatchString(commitBody) {
		return ValidationResult{OK: true}, nil
	}
	violations := make([]string, 0, len(removed))
	for _, tag := range removed {
		violations = append(violations,
			fmt.Sprintf("toml tag %q removed/renamed without ADR ref architecture records", tag))
	}
	return ValidationResult{OK: false, Violations: violations}, nil
}

func extractRemovedTomlTags(diff string) []string {
	var tags []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "---") {
			continue
		}
		m := removedTomlTagPattern.FindStringSubmatch(line)
		if len(m) == 2 {
			tags = append(tags, m[1])
		}
	}
	return tags
}

func RunGitDiff(repoDir, base, head string) (string, error) {
	if err := validateRef("base", base); err != nil {
		return "", err
	}
	if err := validateRef("head", head); err != nil {
		return "", err
	}
	args := []string{"diff", base + ".." + head, "--", "internal/doctrine/schema.go"}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff %s..%s: %w", base, head, err)
	}
	return string(out), nil
}

func RunGitCommitBody(repoDir, head string) (string, error) {
	if err := validateRef("head", head); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "log", "--format=%B", "-n", "1", head)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log %s: %w", head, err)
	}
	return string(out), nil
}

func ValidateRange(repoDir, base, head string) (ValidationResult, error) {
	body, err := RunGitCommitBody(repoDir, head)
	if err != nil {
		return ValidationResult{}, err
	}
	diff, err := RunGitDiff(repoDir, base, head)
	if err != nil {
		return ValidationResult{}, err
	}
	return ValidateAdditive(diff, body)
}
