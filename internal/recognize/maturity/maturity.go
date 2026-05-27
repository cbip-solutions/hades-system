// SPDX-License-Identifier: MIT
// Package maturity probes git + CI signals to surface project-maturity hints
// per spec §4.2. Used by B6 orchestrator to enrich recognize.Result.
//
// Strategy shell out to git for commit count + last-commit timestamp;
// glob filesystem for CI files. Graceful zero-value on probe failure (not a
// git repo, git missing, ctx cancelled) — downstream sees CommitCount=-1
// signalling "we don't know".
//
// Per invariant boundary discipline: this package does NOT import
// internal/store; stdlib os/exec + os.Stat only.
package maturity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Maturity struct {
	CommitCount       int
	LastCommitISO8601 string
	HasCI             bool
	CIPlatform        string
}

type ciSignal struct {
	Path     string
	Glob     string
	Platform string
}

var ciSignals = []ciSignal{
	{Path: ".github/workflows", Glob: "*.yml", Platform: "github-actions"},
	{Path: ".github/workflows", Glob: "*.yaml", Platform: "github-actions"},
	{Path: ".gitlab-ci.yml", Platform: "gitlab"},
	{Path: "azure-pipelines.yml", Platform: "azure-pipelines"},
	{Path: ".circleci/config.yml", Platform: "circleci"},
	{Path: "Jenkinsfile", Platform: "jenkins"},
	{Path: ".travis.yml", Platform: "travis"},
	{Path: "bitbucket-pipelines.yml", Platform: "bitbucket"},
	{Path: ".drone.yml", Platform: "drone"},
	{Path: ".buildkite/pipeline.yml", Platform: "buildkite"},
}

func Probe(ctx context.Context, repoPath string) (Maturity, error) {
	if err := ctx.Err(); err != nil {
		return Maturity{CommitCount: -1}, err
	}
	m := Maturity{CommitCount: -1}

	if isGitRepo(repoPath) && gitAvailable() {
		if count, err := gitCommitCount(ctx, repoPath); err == nil {
			m.CommitCount = count
		}
		if ts, err := gitLastCommitISO(ctx, repoPath); err == nil {
			m.LastCommitISO8601 = ts
		}
	}

	plat := detectCI(repoPath)
	if plat != "" {
		m.HasCI = true
		m.CIPlatform = plat
	}
	return m, nil
}

func isGitRepo(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ".git"))
	return err == nil
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func gitCommitCount(ctx context.Context, repoPath string) (int, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "HEAD")
	cmd.Dir = repoPath
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return -1, err
	}
	s := strings.TrimSpace(stdout.String())
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1, err
	}
	return n, nil
}

func gitLastCommitISO(ctx context.Context, repoPath string) (string, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%cI")
	cmd.Dir = repoPath
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func detectCI(repoPath string) string {
	for _, sig := range ciSignals {
		if sig.Glob == "" {

			if _, err := os.Stat(filepath.Join(repoPath, sig.Path)); err == nil {
				return sig.Platform
			}
			continue
		}

		dir := filepath.Join(repoPath, sig.Path)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, sig.Glob))
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			return sig.Platform
		}
	}
	return ""
}

var ErrNotGitRepo = errors.New("maturity: not a git repository")
