// SPDX-License-Identifier: MIT
package merge

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func Validate(ctx context.Context, g GitClient, poolCapacity int, req MergeRequest) error {

	if strings.TrimSpace(req.TargetBranch) == "" {
		return fmt.Errorf("%w: TargetBranch empty or whitespace", ErrInvalidRequest)
	}
	if req.BaseSHA == "" {
		return fmt.Errorf("%w: BaseSHA empty", ErrInvalidRequest)
	}
	if req.Mode == ModeUnknown {
		return fmt.Errorf("%w: Mode=ModeUnknown (orchestrator mapping bug)", ErrInvalidRequest)
	}
	n := len(req.Candidates)
	if n < 1 || n > 5 {
		return fmt.Errorf("%w: N=%d outside 1≤N≤5", ErrInvalidRequest, n)
	}
	for i, c := range req.Candidates {
		if c.HeadSHA == "" {
			return fmt.Errorf("%w: Candidates[%d].HeadSHA empty", ErrInvalidRequest, i)
		}
		if strings.TrimSpace(c.Branch) == "" {
			return fmt.Errorf("%w: Candidates[%d].Branch empty or whitespace", ErrInvalidRequest, i)
		}
	}

	seen := make(map[string]int, n)
	for i, c := range req.Candidates {
		if prior, ok := seen[c.HeadSHA]; ok {
			return fmt.Errorf("%w: Candidates[%d].HeadSHA collides with Candidates[%d] (%s)",
				ErrCandidatesNotUnique, i, prior, c.HeadSHA)
		}
		seen[c.HeadSHA] = i
	}
	if poolCapacity < n {
		return fmt.Errorf("%w: capacity=%d < N=%d", ErrPoolInsufficient, poolCapacity, n)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := RevParse(ctx, g, "", "refs/heads/"+req.TargetBranch); err != nil {
		return err
	}
	if n >= 2 {
		heads := make([]string, n)
		for i, c := range req.Candidates {
			heads[i] = c.HeadSHA
		}
		mb, err := MergeBase(ctx, g, "", heads...)
		if err != nil {
			return fmt.Errorf("merge.Validate: merge-base lookup failed: %w", err)
		}
		if mb != req.BaseSHA {
			return fmt.Errorf("%w: declared=%s computed=%s heads=%v",
				ErrBaseNotMergeBase, req.BaseSHA, mb, heads)
		}
	}

	return nil
}

var _ = errors.New
