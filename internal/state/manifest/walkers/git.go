// SPDX-License-Identifier: MIT
package walkers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type GitResult struct {
	Released          []string
	InProgress        []string
	BrainstormPending []string
	MissingSources    []string
}

type GitWalker struct {
	repoRoot string
}

func NewGitWalker(repoRoot string) *GitWalker { return &GitWalker{repoRoot: repoRoot} }

var (
	versionTagRE           = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	planExecuteBranchRE    = regexp.MustCompile(`^plan-(\d+)-execute$`)
	brainstormPendingRowRE = regexp.MustCompile(`^\|\s*(plan-\d+)\s*\|\s*brainstorm-pending\s*\|`)
)

func (w *GitWalker) Walk(ctx context.Context) (GitResult, error) {
	res := GitResult{}

	if _, err := os.Stat(w.repoRoot); err != nil {
		res.MissingSources = append(res.MissingSources, "git")
		return res, nil
	}

	tags, ok := w.runGit(ctx, "tag", "--list")
	if !ok {
		res.MissingSources = append(res.MissingSources, "git")
		return res, nil
	}
	for _, line := range strings.Split(strings.TrimSpace(tags), "\n") {
		line = strings.TrimSpace(line)
		if versionTagRE.MatchString(line) {
			res.Released = append(res.Released, line)
		}
	}
	sort.Strings(res.Released)

	branches, ok := w.runGit(ctx, "branch", "--list", "--all")
	if !ok {
		res.MissingSources = append(res.MissingSources, "git")
		return res, nil
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(branches), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
		line = strings.TrimPrefix(line, "remotes/origin/")
		m := planExecuteBranchRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := fmt.Sprintf("plan-%s", m[1])
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		res.InProgress = append(res.InProgress, key)
	}
	sort.Strings(res.InProgress)

	docPath := filepath.Join(w.repoRoot, "docs", "operations", "parallel-execution-coordination.md")
	body, err := os.ReadFile(docPath)
	if err != nil {

		return res, nil
	}
	for _, line := range strings.Split(string(body), "\n") {
		m := brainstormPendingRowRE.FindStringSubmatch(line)
		if m != nil {
			res.BrainstormPending = append(res.BrainstormPending, m[1])
		}
	}
	sort.Strings(res.BrainstormPending)

	return res, nil
}

func (w *GitWalker) runGit(ctx context.Context, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
