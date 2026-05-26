// SPDX-License-Identifier: MIT
// internal/orchestrator/apply/apply_engine.go
//
// ApplyEngine — Q1 D live correction (real Plan 5). See doc.go for the
// package-level Q1 D split rationale + inv-zen-089/097 statements.
package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type EventType int

const (
	EventApplyAttempted EventType = iota + 1

	EventApplySucceeded

	EventApplyReverted
)

type Event struct {
	Type      EventType
	Branch    string
	FixID     string
	CommitSHA string
	Files     []string
	Stderr    string
}

// Emitter is the apply-engine's collaborator (Phase A eventlog.Log satisfies
// it via the Phase N adapter that converts apply.Event → eventlog.Event).
// Kept narrow to honour inv-zen-089 — apply has no direct dependency on
// internal/store, AND no direct dependency on the eventlog package either.
//
// Implementations MUST be safe to call from a context.WithoutCancel-derived
// context (the engine emits with cancellation-detached audit context so a
// caller cancellation cannot silently drop audit rows).
type Emitter interface {
	Append(ctx context.Context, e Event) error
}

type Config struct {
	RepoDir string

	Emitter Emitter

	Timeout time.Duration
}

type FixPrompt struct {
	ID string

	Patch string

	TestCmd []string

	Prompt string
}

type Result struct {
	CommitSHA string

	FilesTouched []string

	TestsPassed bool

	TestStderr string

	Reverted bool
}

// ApplyEngine is the Q1 D contract: live correction at a worker branch's
// commit boundary, sequential, single-branch. Plan 6 owns cross-worker
// integration via MergeEngine (declared in merge_engine.go).
//
// Implementations MUST:
//
//   - Honour ctx cancellation between every shell-out (the engine
//     short-circuits on ctx.Err() before each subprocess).
//   - Emit EventApplyAttempted before `git apply` runs.
//   - Emit EventApplySucceeded XOR EventApplyReverted (never both,
//     never neither, on a non-error return path).
//   - Use context.WithoutCancel(ctx) for the audit emit so a caller
//     cancellation does not drop the row.
//   - Refuse to operate on a dirty working tree (ErrWorkingTreeDirty).
type ApplyEngine interface {
	ApplyFix(ctx context.Context, workerBranch string, fix FixPrompt) (Result, error)
}

type realEngine struct {
	cfg Config
}

func New(cfg Config) ApplyEngine {
	if cfg.RepoDir == "" {
		panic("apply.New: Config.RepoDir is required")
	}
	if cfg.Emitter == nil {
		panic("apply.New: Config.Emitter is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &realEngine{cfg: cfg}
}

var (
	ErrPatchRejected = errors.New("apply: git apply rejected patch")

	ErrWorkingTreeDirty = errors.New("apply: working tree dirty before ApplyFix")

	ErrCheckoutFailed = errors.New("apply: git checkout failed")

	ErrCommitFailed = errors.New("apply: git commit failed")

	ErrTestsFailed = errors.New("apply: post-apply tests failed (reverted)")
	// ErrRevertFailed fires when TestCmd exited non-zero AND the
	// `git reset --hard <priorSHA>` revert itself failed. The branch is
	// in an indeterminate state; caller MUST escalate (not retry).
	ErrRevertFailed = errors.New("apply: revert via git reset failed")
)

func gitEnv() []string {
	out := []string{
		"GIT_AUTHOR_NAME=zen-apply",
		"GIT_AUTHOR_EMAIL=apply@zen-swarm.local",
		"GIT_COMMITTER_NAME=zen-apply",
		"GIT_COMMITTER_EMAIL=apply@zen-swarm.local",
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") {
			continue
		}
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "TMPDIR=") ||
			strings.HasPrefix(e, "SystemRoot=") {
			out = append(out, e)
		}
	}
	return out
}

func (e *realEngine) git(ctx context.Context, stdin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.cfg.RepoDir
	cmd.Env = gitEnv()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// emitDetached emits an apply.Event using a cancellation-detached audit
// context (context.WithoutCancel); a caller cancellation MUST NOT drop
// the audit row. Mirrors the discipline established by hra/voting_fmv.go
// (emitWinner, emitAllFailed, emitDegraded).
func (e *realEngine) emitDetached(ctx context.Context, ev Event) {
	auditCtx := context.WithoutCancel(ctx)
	_ = e.cfg.Emitter.Append(auditCtx, ev)
}

func (e *realEngine) ApplyFix(ctx context.Context, workerBranch string, fix FixPrompt) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()

	statusOut, _, err := e.git(ctx, "", "status", "--porcelain")
	if err != nil {
		return Result{}, fmt.Errorf("%w: status: %v", ErrWorkingTreeDirty, err)
	}
	if strings.TrimSpace(statusOut) != "" {
		return Result{}, fmt.Errorf("%w: %s", ErrWorkingTreeDirty, strings.TrimSpace(statusOut))
	}

	if _, stderr, err := e.git(ctx, "", "checkout", "-q", workerBranch); err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrCheckoutFailed, strings.TrimSpace(stderr), err)
	}

	priorOut, _, err := e.git(ctx, "", "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("%w: rev-parse HEAD: %v", ErrCheckoutFailed, err)
	}
	priorSHA := strings.TrimSpace(priorOut)

	e.emitDetached(ctx, Event{Type: EventApplyAttempted, Branch: workerBranch, FixID: fix.ID})

	if _, stderr, err := e.git(ctx, fix.Patch, "apply", "--index", "--whitespace=nowarn"); err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrPatchRejected, strings.TrimSpace(stderr), err)
	}

	filesOut, _, _ := e.git(ctx, "", "diff", "--cached", "--name-only")
	files := splitLines(filesOut)

	if _, stderr, err := e.git(ctx, "", "commit", "-q", "-m", "fix:"+fix.ID); err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrCommitFailed, strings.TrimSpace(stderr), err)
	}
	commitOut, _, err := e.git(ctx, "", "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("%w: rev-parse HEAD post-commit: %v", ErrCommitFailed, err)
	}
	commitSHA := strings.TrimSpace(commitOut)

	res := Result{CommitSHA: commitSHA, FilesTouched: files, TestsPassed: true}

	if len(fix.TestCmd) > 0 {
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, fix.TestCmd[0], fix.TestCmd[1:]...)
		cmd.Dir = e.cfg.RepoDir
		cmd.Env = gitEnv()
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			res.TestsPassed = false
			res.TestStderr = stderr.String()
			res.Reverted = true
			if _, rstderr, rerr := e.git(ctx, "", "reset", "--hard", priorSHA); rerr != nil {
				e.emitDetached(ctx, Event{
					Type:      EventApplyReverted,
					Branch:    workerBranch,
					FixID:     fix.ID,
					CommitSHA: commitSHA,
					Files:     files,
					Stderr:    strings.TrimSpace(rstderr),
				})
				return res, fmt.Errorf("%w: %s: %v", ErrRevertFailed, strings.TrimSpace(rstderr), rerr)
			}
			e.emitDetached(ctx, Event{
				Type:      EventApplyReverted,
				Branch:    workerBranch,
				FixID:     fix.ID,
				CommitSHA: commitSHA,
				Files:     files,
				Stderr:    res.TestStderr,
			})
			return res, fmt.Errorf("%w: %v", ErrTestsFailed, err)
		}
	}

	e.emitDetached(ctx, Event{
		Type:      EventApplySucceeded,
		Branch:    workerBranch,
		FixID:     fix.ID,
		CommitSHA: commitSHA,
		Files:     files,
	})
	return res, nil
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

var _ ApplyEngine = (*realEngine)(nil)
