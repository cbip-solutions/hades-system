// SPDX-License-Identifier: MIT
package evolution

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func (b *Builder) BuildChurn(ctx context.Context, projectID, repoDir string) error {
	p := b.paramsFor(projectID)

	total, err := b.runner.RevListCount(ctx, repoDir)
	if err != nil {
		return err
	}
	if total < p.MinTotalCommits {
		return fmt.Errorf("%w: %d commits < min_total_commits %d", ErrInsufficientHistory, total, p.MinTotalCommits)
	}

	commits, err := b.logCommits(ctx, repoDir, p)
	if err != nil {
		return err
	}
	files := map[string]struct{}{}
	for _, c := range commits {
		if len(c.Files) == 0 || len(c.Files) > p.MaxChangesetSize {
			continue
		}
		for _, f := range c.Files {
			files[f] = struct{}{}
		}
	}

	now := timeNow().Unix()
	for f := range files {
		ch, err := b.churnForFile(ctx, repoDir, f, p)
		if err != nil {
			return err
		}
		if ch.touch == 0 {
			continue
		}
		if err := b.store.UpsertChurn(ctx, store.Churn{
			Path:        f,
			WindowDays:  p.WindowDays,
			TouchCount:  ch.touch,
			AuthorCount: ch.authors,
			LastTouched: ch.last,
			UpdatedAt:   now,
		}); err != nil {
			return fmt.Errorf("evolution: persist churn %q: %w", f, err)
		}
	}
	return nil
}

type fileChurn struct {
	touch   int
	authors int
	last    int64
}

func (b *Builder) churnForFile(ctx context.Context, repoDir, file string, p Params) (fileChurn, error) {
	args := []string{
		"--no-merges",
		"--pretty=format:%ae" + unitSep + "%ct" + recSep,
	}
	if p.FollowRenames {
		args = append(args, "--follow")
	}
	if s := sinceArg(p.WindowDays); s != "" {
		args = append(args, "--since="+s)
	}

	args = append(args, "--", file)

	out, err := b.runner.Log(ctx, repoDir, args...)
	if err != nil {
		return fileChurn{}, err
	}

	var fc fileChurn
	seen := map[string]struct{}{}
	for _, rec := range churnSplitRecords(out) {
		email, ts, ok := churnSplitAuthorTime(rec)
		if !ok {
			continue
		}
		fc.touch++
		if _, dup := seen[email]; !dup {
			seen[email] = struct{}{}
		}
		if ts > fc.last {
			fc.last = ts
		}
	}
	fc.authors = len(seen)
	return fc, nil
}

func churnSplitRecords(out string) []string {
	raw := strings.Split(out, recSep)
	recs := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r != "" {
			recs = append(recs, r)
		}
	}
	return recs
}

func churnSplitAuthorTime(rec string) (email string, unixTime int64, ok bool) {
	fields := strings.SplitN(rec, unitSep, 2)
	if len(fields) < 2 {
		return "", 0, false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(fields[0]), ts, true
}
