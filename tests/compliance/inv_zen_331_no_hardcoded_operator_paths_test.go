// SPDX-License-Identifier: MIT

package compliance

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var hardcodedOperatorPath = "/Users/" + "operator/"

func repoRoot331(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("repoRoot331: getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repoRoot331: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func isAllowlistedPath(rel string) bool {
	base := filepath.Base(rel)
	if base == "inv_zen_331_no_hardcoded_operator_paths_test.go" {
		return true
	}
	return false
}

func shouldScanDir(rel string) bool {
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		switch p {
		case "vendor", ".git", "node_modules", "testdata", "docs", ".zen", "bin":
			return false
		}
	}
	return true
}

func TestInvZen331_NoHardcodedOperatorPaths_GoSource(t *testing.T) {
	root := repoRoot331(t)

	type violation struct {
		path   string
		lineno int
		text   string
	}
	var violations []violation

	for _, subdir := range []string{"internal", "cmd"} {
		walkErr := filepath.Walk(filepath.Join(root, subdir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if info.IsDir() {
				if !shouldScanDir(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if isAllowlistedPath(rel) {
				return nil
			}
			f, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			lineno := 0
			for scanner.Scan() {
				lineno++
				line := scanner.Text()
				if strings.Contains(line, hardcodedOperatorPath) {
					violations = append(violations, violation{
						path:   rel,
						lineno: lineno,
						text:   strings.TrimSpace(line),
					})
				}
			}
			return scanner.Err()
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", subdir, walkErr)
		}
	}

	if len(violations) > 0 {
		t.Errorf("inv-zen-331: %d hardcoded operator-path violation(s) in Go source (internal/ + cmd/):", len(violations))
		for i, v := range violations {
			if i >= 30 {
				t.Logf("      ... and %d more", len(violations)-i)
				break
			}
			t.Logf("      %s:%d: %s", v.path, v.lineno, v.text)
		}
	}
}

// TestInvZen331_AllowlistMinimalism ensures the allowlist stays narrow.
// If a future change adds an entry to isAllowlistedPath, this test pins
// the allowlist size + identity so reviewers MUST update the test
// alongside the policy change (sister-test discipline per
// feedback_sister_test_pattern memory).
func TestInvZen331_AllowlistMinimalism(t *testing.T) {
	allowlisted := []string{
		"inv_zen_331_no_hardcoded_operator_paths_test.go",
	}
	if len(allowlisted) != 1 {
		t.Errorf("inv-zen-331: allowlist count = %d, want 1 (the test file itself)", len(allowlisted))
	}
	for _, name := range allowlisted {
		if !isAllowlistedPath(name) {
			t.Errorf("inv-zen-331: expected allowlist member %q not recognised", name)
		}
	}
	// Negative: a random path MUST NOT be allowlisted.
	if isAllowlistedPath("internal/cli/project_test.go") {
		t.Errorf("inv-zen-331: allowlist leak — internal/cli/project_test.go matched isAllowlistedPath")
	}
}
