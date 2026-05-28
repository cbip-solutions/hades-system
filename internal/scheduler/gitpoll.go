// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type GitPoller interface {
	HeadSHA(ctx context.Context, repoURL, branch string) (string, error)
}

type GhPoller struct{}

func (GhPoller) HeadSHA(ctx context.Context, repoURL, branch string) (string, error) {
	owner, repo, err := ParseGitHubURL(repoURL)
	if err != nil {
		return "", fmt.Errorf("gitpoll.parseURL: %w", err)
	}
	if branch == "" {
		branch = "main"
	}
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/%s/commits/%s", owner, repo, branch),
		"--jq", ".sha",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh api: %w", err)
	}
	sha := strings.TrimSpace(string(out))

	if !isHexSHA(sha) {

		snippet := sha
		const maxSnippet = 80
		if len(snippet) > maxSnippet {
			snippet = snippet[:maxSnippet] + "..."
		}
		return "", fmt.Errorf("gh api: unexpected SHA shape %q", snippet)
	}
	return sha, nil
}

func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func ParseGitHubURL(u string) (owner, repo string, err error) {

	if strings.HasPrefix(u, "git@github.com:") {
		rest := strings.TrimPrefix(u, "git@github.com:")
		rest = strings.TrimSuffix(rest, ".git")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid ssh url %q", u)
		}
		return parts[0], parts[1], nil
	}

	parsed, perr := url.Parse(u)
	if perr != nil {
		return "", "", fmt.Errorf("invalid url %q: %w", u, perr)
	}
	path := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github url %q", u)
	}
	return parts[0], parts[1], nil
}

// GitPollOnce performs one debounced poll cycle against a TriggerGitPoll
// schedule. The poller fetches the current HEAD SHA; the cursor in
// s.TriggerConfig.LastSeenSHA is compared and advanced; the boolean return
// signals whether the scheduler loop should fire.
//
// Returns
// - (true, nil): a NEW SHA was observed (cursor advanced; fire).
// - (false, nil): no change OR first poll establishing baseline (cursor
// advanced on first poll; do not fire, otherwise every freshly created
// git-poll routine would fire immediately on its first cycle).
// - (false, non-nil err): poller error (gh missing / network / non-zero
// exit / unexpected output / cancelled ctx). Cursor is NOT advanced —
// the next poll attempt re-tries against the same baseline.
//
// Mutates s.TriggerConfig.LastSeenSHA in-place on every successful poll.
// Callers must persist the mutation to the store so the
// cursor survives daemon restart; the cursor is the entire debounce state.
//
// Defense-in-depth: rejects nil schedule / non-TriggerGitPoll / nil poller
// with ErrInvalidSchedule. The scheduler loop only routes git-poll rows
// here, but a routing bug must not silently exec `gh` against an HTTP / cron
// row's empty RepoURL.
func GitPollOnce(ctx context.Context, s *Schedule, poller GitPoller) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("%w: nil Schedule", ErrInvalidSchedule)
	}
	if s.TriggerType != TriggerGitPoll {
		return false, fmt.Errorf("%w: trigger type %v != GitPoll", ErrInvalidSchedule, s.TriggerType)
	}
	if poller == nil {
		return false, fmt.Errorf("%w: nil GitPoller", ErrInvalidSchedule)
	}
	sha, err := poller.HeadSHA(ctx, s.TriggerConfig.RepoURL, s.TriggerConfig.Branch)
	if err != nil {
		return false, err
	}
	previous := s.TriggerConfig.LastSeenSHA
	s.TriggerConfig.LastSeenSHA = sha
	if previous == "" {
		// First poll: establish cursor; do not fire. Otherwise every
		// freshly created git-poll routine would fire on its first cycle
		// (semantics from spec §1 design choice: fire on a NEW commit since
		// registration, not on whatever was HEAD at registration time).
		return false, nil
	}
	if previous == sha {

		return false, nil
	}
	return true, nil
}
