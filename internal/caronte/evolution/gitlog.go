// SPDX-License-Identifier: MIT
package evolution

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var ErrGit = errors.New("evolution: git subprocess failed")

const (
	unitSep = "\x1f"
	recSep  = "\x1e"
)

var gitArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./~^:][A-Za-z0-9_./~^:+-]*$`)

func validateGitArg(arg string) error {
	if !gitArgPattern.MatchString(arg) {
		return fmt.Errorf("%w: unsafe git arg %q (must not start with '-')", ErrGit, arg)
	}
	return nil
}

type Commit struct {
	SHA         string
	AuthorEmail string
	UnixTime    int64
	Files       []string
}

type GitRunner interface {
	Log(ctx context.Context, repoDir string, args ...string) (string, error)

	RevListCount(ctx context.Context, repoDir string) (int, error)
}

type osGitRunner struct{}

func NewOSGitRunner() GitRunner { return osGitRunner{} }

// Log validates every NON-FLAG arg (flag-injection guard on interpolated
// values — paths, revs, since-timestamps) then execs `git log <args...>`
// with cmd.Dir = repoDir and ctx-bound cancellation.
//
// Package-constructed flags (--no-merges, --name-only, --pretty=..., -M,
// --since=<rfc3339>) start with '-' and are recognized git options built from
// constants in the caller (Builder), NOT user input — they pass through without
// validation. Any argument NOT beginning with '-' is treated as an interpolated
// value (a path, a rev, or a since-timestamp passed as a bare value) and MUST
// pass validateGitArg. This keeps an injected path/rev from masquerading as a
// git flag while letting the builder pass legitimate options.
//
// The literal `--` separator before path args is a package-constructed
// constant flag; it is not validated (it starts with '-').
//
// Mirrors internal/state/manifest/walkers/git.go runGit:
// cmd := exec.CommandContext(ctx, "git", args...); cmd.Dir = repoRoot;
// out, err := cmd.Output().
func (osGitRunner) Log(ctx context.Context, repoDir string, args ...string) (string, error) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {

			continue
		}
		if err := validateGitArg(a); err != nil {
			return "", err
		}
	}

	cmd := exec.CommandContext(ctx, "git", append([]string{"log"}, args...)...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: git log: %v", ErrGit, err)
	}
	return string(out), nil
}

func (osGitRunner) RevListCount(ctx context.Context, repoDir string) (int, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("%w: git rev-list --count: %v", ErrGit, err)
	}
	n, perr := strconv.Atoi(strings.TrimSpace(string(out)))
	if perr != nil {
		return 0, fmt.Errorf("%w: rev-list count parse %q: %v", ErrGit, strings.TrimSpace(string(out)), perr)
	}
	return n, nil
}

func parseLog(out string) ([]Commit, error) {
	records := strings.Split(out, recSep)
	commits := make([]Commit, 0, len(records))
	for _, r := range records {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		fields := strings.SplitN(r, unitSep, 4)
		if len(fields) < 4 {
			continue
		}
		ut, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {

			continue
		}
		var files []string
		for _, f := range strings.Split(fields[3], "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				files = append(files, f)
			}
		}
		commits = append(commits, Commit{
			SHA:         strings.TrimSpace(fields[0]),
			AuthorEmail: strings.TrimSpace(fields[1]),
			UnixTime:    ut,
			Files:       files,
		})
	}
	return commits, nil
}
