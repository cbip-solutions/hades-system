// SPDX-License-Identifier: MIT
package merge

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type gitClient interface {
	Run(ctx context.Context, repoDir, stdin string, args ...string) (stdout, stderr string, err error)
}

type GitClient = gitClient

type realGit struct {
	path string
}

func NewRealGit() (GitClient, error) {
	p, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitNotFound, err)
	}
	return &realGit{path: p}, nil
}

func (g *realGit) Run(ctx context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, g.path, args...)
	cmd.Dir = repoDir
	cmd.Env = GitEnv()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func GitEnv() []string {
	out := []string{
		"GIT_AUTHOR_NAME=zen-merge",
		"GIT_AUTHOR_EMAIL=merge@zen-swarm.local",
		"GIT_COMMITTER_NAME=zen-merge",
		"GIT_COMMITTER_EMAIL=merge@zen-swarm.local",
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") {
			continue
		}
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "TMPDIR=") {
			out = append(out, e)
		}
	}
	return out
}

func ParseGitVersion(s string) (major, minor int, ok bool) {
	s = strings.TrimSpace(s)
	const prefix = "git version "
	if !strings.HasPrefix(s, prefix) {
		return 0, 0, false
	}
	s = s[len(prefix):]
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return maj, min, true
}

type FakeOutput struct {
	Stdout string
	Stderr string
	Err    error
}

type FakeCall struct {
	RepoDir string
	Stdin   string
	Args    []string
}

type FakeGit struct {
	mu      sync.Mutex
	calls   []FakeCall
	outputs []FakeOutput
}

func NewFakeGit(outputs ...FakeOutput) *FakeGit {
	return &FakeGit{outputs: append([]FakeOutput{}, outputs...)}
}

func (f *FakeGit) Run(_ context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{RepoDir: repoDir, Stdin: stdin, Args: append([]string{}, args...)})
	if len(f.outputs) == 0 {
		return "", "", nil
	}
	o := f.outputs[0]
	f.outputs = f.outputs[1:]
	return o.Stdout, o.Stderr, o.Err
}

func (f *FakeGit) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *FakeGit) Push(o ...FakeOutput) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputs = append(f.outputs, o...)
}

var (
	_ GitClient = (*realGit)(nil)
	_ GitClient = (*FakeGit)(nil)
)

func VersionCheck(ctx context.Context, g GitClient) error {
	stdout, stderr, err := g.Run(ctx, "", "", "--version")
	if err != nil {
		return fmt.Errorf("merge.VersionCheck: git --version failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	major, minor, ok := ParseGitVersion(stdout)
	if !ok {
		return fmt.Errorf("%w: unparseable output %q", ErrGitVersionTooOld, strings.TrimSpace(stdout))
	}
	if major < 2 || (major == 2 && minor < 40) {
		return fmt.Errorf("%w: got %d.%d", ErrGitVersionTooOld, major, minor)
	}
	return nil
}

func RevParse(ctx context.Context, g GitClient, repoDir, ref string) (string, error) {
	stdout, stderr, err := g.Run(ctx, repoDir, "", "rev-parse", "--verify", ref)
	if err != nil {
		return "", fmt.Errorf("%w: ref=%q: %s: %v", ErrTargetNotExist, ref, strings.TrimSpace(stderr), err)
	}
	sha := strings.TrimSpace(stdout)
	if sha == "" {
		return "", fmt.Errorf("%w: ref=%q: empty rev-parse output", ErrTargetNotExist, ref)
	}
	return sha, nil
}

func MergeBase(ctx context.Context, g GitClient, repoDir string, heads ...string) (string, error) {
	if len(heads) < 2 {
		return "", fmt.Errorf("merge.MergeBase: need ≥2 heads, got %d", len(heads))
	}
	args := append([]string{"merge-base", "--octopus"}, heads...)
	stdout, stderr, err := g.Run(ctx, repoDir, "", args...)
	if err != nil {
		return "", fmt.Errorf("merge.MergeBase: %s: %w", strings.TrimSpace(stderr), err)
	}
	sha := strings.TrimSpace(stdout)
	if sha == "" {
		return "", fmt.Errorf("merge.MergeBase: empty output for heads=%v", heads)
	}
	return sha, nil
}
