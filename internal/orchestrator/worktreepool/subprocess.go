// SPDX-License-Identifier: MIT
// Package worktreepool subprocess.go is the only file in the package
// permitted to import os/exec. Every other file in the package — and every
// caller across the project — invokes git through the [Executor] interface
// (see pool.go), which subprocess.go satisfies via [osExecExecutor]
// (constructed by [NewOSExecutor]).
//
// Why a single os/exec entry-point?
//
// - **Audit (invariant/090 ⊥)**: subprocess.go has no awareness of
// internal/store, internal/workforce/queue, internal/eventlog. Any
// attempt to import them is a hard compile failure visible in CR.
// - **Privacy**: stderr from git subprocesses can contain
// absolute filesystem paths, env values, refs that disclose host
// layout. The [classify] function builds public Error() strings that
// redact paths; raw stderr is preserved in [subprocessErr.rawStderr]
// for in-process audit emission only and is NEVER concatenated into
// the public Error() output.
// - **Determinism**: every wrapper has a single os/exec call site, so
// fault injection happens at the [Executor] seam in tests, never via
// monkey-patching exec.CommandContext.
//
// Sentinel error classes:
//
// - errClassENOSPC, errClassWorktreeLocked, errClassBranchExists,
// errClassNotARepo, errClassNetwork, errClassOther — stderr-pattern
// classifications.
// - errClassTimeout — ctx.Canceled / ctx.DeadlineExceeded (deliberate
// kill from caller; retry-with-backoff semantics).
// - errClassSignal — external SIGKILL / SIGABRT (`signal: killed`,
// `signal: aborted`); possibly disk/memory pressure; retry-once
// semantics.
// - errClassPanic — in-process Go runtime error (`runtime error:...`);
// deterministic bug; do-not-retry semantics.
//
// Plus the two exported sentinels ([ErrPoolDegraded],
// [ErrSubprocessTimeout]) drive callers' degradation paths via errors.Is.
//
// IMP-4 deviation from B-2 plan: the plan specified seven classes with
// errClassPanic conflating ctx-kill, external SIGKILL, and in-process
// runtime error. Per max-scope doctrine ("the most complete solution the
// domain deserves"), these three semantics are split into nine total
// classes (Timeout / Signal / Panic) so B-3..B-9 retry policy can act on
// the correct underlying cause without subtly-wrong dispatch. Reviewer
// flagged this exactly as scope-creep-but-correct; resolved by splitting.
package worktreepool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var (
	errClassENOSPC         = errors.New("worktreepool: subprocess: ENOSPC")
	errClassWorktreeLocked = errors.New("worktreepool: subprocess: worktree locked or in use")
	errClassBranchExists   = errors.New("worktreepool: subprocess: branch already exists")
	errClassNotARepo       = errors.New("worktreepool: subprocess: not a git repository")
	errClassNetwork        = errors.New("worktreepool: subprocess: network failure")

	errClassTimeout = errors.New("worktreepool: subprocess: timeout / cancelled")
	errClassSignal  = errors.New("worktreepool: subprocess: signal (killed / aborted)")
	errClassPanic   = errors.New("worktreepool: subprocess: panic (runtime error)")
	errClassOther   = errors.New("worktreepool: subprocess: other")
)

var ErrPoolDegraded = errors.New("worktreepool: pool degraded (disk pressure / saturation)")

var ErrSubprocessTimeout = errors.New("worktreepool: subprocess timeout")

// pathRedactor matches absolute Unix paths (`/seg1/seg2/...`) so the public
// Error() string does not leak the host filesystem layout. The pattern
// matches one or more `/word` segments greedily; segments include letters,
// digits, dots, underscores, hyphens. Trailing punctuation (`'`, `"`, `:`,
// `,`, `.`) is intentionally NOT consumed so the surrounding stderr text
// remains readable. Quoted paths (`'/path'`, `"/path"`) lose only the
// path body; the quotes survive.
//
// We do not attempt to redact env vars or refs here — env values live in
// os.Environ() which the wrappers never log, and refs are public by
// design (the branch name appears in the wrapper signature). The regex
// is intentionally conservative: false-negatives (un-redacted relative
// paths) are accepted; false-positives (over-redaction) would break
// observability.
var pathRedactor = regexp.MustCompile(`(/[A-Za-z0-9._\-]+)+`)

// refPattern + urlPattern (IMP-3): refs and remote URLs MUST survive
// sanitize() because they are part of the public transport / branch
// namespace and over-redacting them garbles incident triage for
// B-3..B-9 (a `cannot lock ref refs<path>` line cannot be distinguished
// from a `cannot create file at <path>` line).
//
// sanitize() pre-extracts these into placeholder tokens BEFORE
// pathRedactor runs, then restores them afterward. The placeholder uses
// NUL bytes which cannot legally appear in stderr (\x00 is forbidden in
// POSIX paths and never produced by git), so collisions are impossible.
var (
	refPattern = regexp.MustCompile(`refs/(heads|tags|remotes)/[\w./\-]+`)
	urlPattern = regexp.MustCompile(`(https?|ssh)://[^\s'"]+|git@[\w.\-]+:[\w./\-]+`)
)

type subprocessErr struct {
	class     error
	extra     error
	sanitized string
	rawStderr []byte
	cause     error
}

func (e *subprocessErr) Error() string {
	cause := ""
	if e.cause != nil {
		cause = ": " + e.cause.Error()
	}
	if e.sanitized == "" {
		return e.class.Error() + cause
	}
	return e.class.Error() + ": " + e.sanitized + cause
}

func (e *subprocessErr) Unwrap() []error {
	var out []error
	if e.class != nil {
		out = append(out, e.class)
	}
	if e.extra != nil {
		out = append(out, e.extra)
	}
	if e.cause != nil {
		out = append(out, e.cause)
	}
	return out
}

// rawStderrBytes returns the unredacted stderr captured from the failing
// subprocess. Audit-only: callers MUST NOT include this in any
// user-visible string. Returns a defensive copy.
//
// Currently unused outside subprocess.go; B-3..B-9 wire it into the
// audit pipeline (eventlog payload, never to the user-facing error).
func (e *subprocessErr) rawStderrBytes() []byte {
	if len(e.rawStderr) == 0 {
		return nil
	}
	out := make([]byte, len(e.rawStderr))
	copy(out, e.rawStderr)
	return out
}

func classify(stderr []byte, err error) error {
	raw := append([]byte(nil), stderr...)
	sanitized := sanitize(stderr)

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &subprocessErr{
			class:     errClassTimeout,
			extra:     ErrSubprocessTimeout,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	}

	switch {
	case bytes.Contains(stderr, []byte("No space left on device")) ||
		bytes.Contains(stderr, []byte("ENOSPC")):
		return &subprocessErr{
			class:     errClassENOSPC,
			extra:     ErrPoolDegraded,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	case bytes.Contains(stderr, []byte("already used by worktree")) ||
		bytes.Contains(stderr, []byte("is locked")) ||

		bytes.Contains(stderr, []byte("cannot lock ref")):
		return &subprocessErr{
			class: errClassWorktreeLocked,

			extra:     ErrPoolDegraded,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	case (bytes.Contains(stderr, []byte("branch named")) && bytes.Contains(stderr, []byte("already exists"))) ||

		bytes.Contains(stderr, []byte("refusing to overwrite existing branch")):
		return &subprocessErr{
			class:     errClassBranchExists,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	case bytes.Contains(stderr, []byte("not a git repository")):
		return &subprocessErr{
			class:     errClassNotARepo,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}

	case bytes.Contains(stderr, []byte("Could not resolve host")) ||
		bytes.Contains(stderr, []byte("Network is unreachable")) ||
		bytes.Contains(stderr, []byte("Connection timed out")) ||
		bytes.Contains(stderr, []byte("Connection refused")) ||
		bytes.Contains(stderr, []byte("unable to access")) ||
		bytes.Contains(stderr, []byte("Permission denied (publickey)")) ||
		bytes.Contains(stderr, []byte("Could not read from remote repository")) ||
		bytes.Contains(stderr, []byte("SSL certificate problem")):
		return &subprocessErr{
			class: errClassNetwork,

			extra:     ErrPoolDegraded,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}

	case bytes.Contains(stderr, []byte("signal: ")):
		return &subprocessErr{
			class: errClassSignal,

			extra:     ErrPoolDegraded,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}

	case bytes.Contains(stderr, []byte("runtime error")):
		return &subprocessErr{
			class:     errClassPanic,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	default:
		return &subprocessErr{
			class:     errClassOther,
			sanitized: sanitized,
			rawStderr: raw,
			cause:     err,
		}
	}
}

// sanitize redacts absolute paths from stderr so the public Error() string
// does not leak host filesystem layout. Returns a trimmed string suitable
// for embedding in Error(). Empty stderr → empty string.
//
// IMP-3 contract: refs (`refs/heads/*`, `refs/tags/*`, `refs/remotes/*`)
// and remote URLs (`https://...`, `http://...`, `ssh://...`,
// `git@host:repo`) MUST round-trip through sanitize unchanged. The
// pre-extract / redact / restore pattern uses NUL-delimited placeholders
// that cannot collide with any legal stderr content (NUL is forbidden in
// POSIX paths and never emitted by git).
func sanitize(stderr []byte) string {
	s := strings.TrimSpace(string(stderr))
	if s == "" {
		return ""
	}

	// 1. Extract refs + URLs into ordered placeholder slots so we can
	// restore them after path redaction. We scan refs first then URLs
	// (refs cannot match the URL pattern; URLs starting with `https://`
	// do not contain the literal `refs/heads|tags|remotes/` prefix in
	// practice, so the two pattern domains are disjoint for our
	// inputs). Using FindAllStringIndex would let us interleave
	// correctly if needed; the simpler ReplaceAllString-with-counter
	// pattern below is sufficient because each placeholder is unique
	// per occurrence.
	const refSentinel = "\x00ZENREF\x00"
	const urlSentinel = "\x00ZENURL\x00"

	refs := refPattern.FindAllString(s, -1)
	s = refPattern.ReplaceAllString(s, refSentinel)

	urls := urlPattern.FindAllString(s, -1)
	s = urlPattern.ReplaceAllString(s, urlSentinel)

	s = pathRedactor.ReplaceAllString(s, "<path>")

	for _, r := range refs {
		s = strings.Replace(s, refSentinel, r, 1)
	}
	for _, u := range urls {
		s = strings.Replace(s, urlSentinel, u, 1)
	}

	return s
}

func gitWorktreeAdd(ctx context.Context, ex Executor, repoRoot, dir, branch, base string) error {
	out, err := ex.Run(ctx, "git", "-C", repoRoot, "worktree", "add", "-B", branch, dir, base)
	if err != nil {
		return classify(out, err)
	}
	return nil
}

func gitWorktreeRemove(ctx context.Context, ex Executor, repoRoot, dir string) error {
	out, err := ex.Run(ctx, "git", "-C", repoRoot, "worktree", "remove", "--force", dir)
	if err != nil {
		return classify(out, err)
	}
	return nil
}

type worktreeEntry struct {
	path     string
	head     string
	branch   string
	detached bool
	bare     bool
}

func gitWorktreeList(ctx context.Context, ex Executor, repoRoot string) ([]worktreeEntry, error) {
	out, err := ex.Run(ctx, "git", "-C", repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, classify(out, err)
	}
	var entries []worktreeEntry
	var cur worktreeEntry
	flush := func() {
		if cur.path != "" {
			entries = append(entries, cur)
		}
		cur = worktreeEntry{}
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.branch = strings.TrimPrefix(line, "branch ")
		case line == "detached":
			cur.detached = true
		case line == "bare":
			cur.bare = true
		}
	}
	flush()
	return entries, nil
}

func gitWorktreePrune(ctx context.Context, ex Executor, repoRoot string) error {
	out, err := ex.Run(ctx, "git", "-C", repoRoot, "worktree", "prune")
	if err != nil {
		return classify(out, err)
	}
	return nil
}

func gitReset(ctx context.Context, ex Executor, dir, ref string) error {
	out, err := ex.Run(ctx, "git", "-C", dir, "reset", "--hard", ref)
	if err != nil {
		return classify(out, err)
	}
	return nil
}

func gitClean(ctx context.Context, ex Executor, dir string) error {
	out, err := ex.Run(ctx, "git", "-C", dir, "clean", "-fdx")
	if err != nil {
		return classify(out, err)
	}
	return nil
}

type osExecExecutor struct{}

func NewOSExecutor() Executor {
	return &osExecExecutor{}
}

func (e *osExecExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {

		if ctxErr := ctx.Err(); ctxErr != nil {

			return out, fmt.Errorf("%w: %w", ctxErr, err)
		}
	}
	return out, err
}
