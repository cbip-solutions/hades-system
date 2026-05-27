// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type DoctrineValidator interface {
	ValidateTOML(diff []byte) error
}

type ReloadSignal interface {
	Reload(ctx context.Context) error
}

type ReloadAwaiter interface {
	NotifyForceAndWait(ctx context.Context, path string, timeout time.Duration) error
}

type GitRunner interface {
	Run(ctx context.Context, dir string, args ...string) error
}

// execGitRunner is the production GitRunner using os/exec. Subprocess
// timeouts MUST be derived from the caller's ctx; Run inherits ctx
// cancellation.
type execGitRunner struct{}

func (execGitRunner) Run(ctx context.Context, dir string, args ...string) error {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ApplierConfig wires the AmendmentApplier. RepoRoot must be the
// project root containing hadessystem.toml + architecture records
//
// ReloadAwaiter is OPTIONAL. When non-nil,
// ApplyWithValidation calls NotifyForceAndWait(path, ReloadWaitTimeout)
// after the inner Apply commit lands and synchronously awaits the
// matching DoctrineReloaded event. On timeout, emits
// DoctrineWatcherStalled and returns nil (the apply itself succeeded;
// reload-wait is operator-visibility per invariant atomicity).
//
// When nil, ApplyWithValidation falls through to the existing
// fire-and-forget ReloadSignal.Reload(ctx) path ( semantics
// preserved). The two are mutually compatible — ReloadSignal is the
// fire-and-forget kicker for callers who do not own a reload.Watcher
// instance; ReloadAwaiter is the synchronous wait for callers (the
// daemon) that DO own the Watcher and want the reload-wait visibility.
//
// ReloadWaitTimeout defaults to 5s when zero.
type ApplierConfig struct {
	RepoRoot          string
	Validator         DoctrineValidator
	Emitter           EventEmitter
	ReloadSignal      ReloadSignal
	ReloadAwaiter     ReloadAwaiter
	ReloadWaitTimeout time.Duration
	Git               GitRunner
}

type AmendmentApplier struct {
	cfg ApplierConfig
}

func NewApplier(cfg ApplierConfig) *AmendmentApplier {
	if cfg.RepoRoot == "" {
		panic("amendment: empty RepoRoot")
	}
	if cfg.Validator == nil {
		panic("amendment: nil Validator")
	}
	if cfg.Emitter == nil {
		panic("amendment: nil Emitter")
	}
	if cfg.Git == nil {
		cfg.Git = execGitRunner{}
	}
	return &AmendmentApplier{cfg: cfg}
}

var tomlBlockRE = regexp.MustCompile("(?s)```toml\\s*\\n(.*?)\\n```")

var ErrNoTOMLBlock = errors.New("no toml fenced block in ADR proposal")

func extractTOMLDiff(adrMD []byte) ([]byte, error) {
	m := tomlBlockRE.FindSubmatch(adrMD)
	if m == nil {
		return nil, ErrNoTOMLBlock
	}
	return m[1], nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (a *AmendmentApplier) findProposedADR(adrID int) (string, error) {
	pat := filepath.Join(a.cfg.RepoRoot, "docs", "decisions", "proposed",
		fmt.Sprintf("%04d-*.md", adrID))
	matches, err := filepath.Glob(pat)
	if err != nil {
		return "", fmt.Errorf("glob proposed ADRs: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no proposed ADR at %s", pat)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous proposed ADR matches: %v", matches)
	}
	return matches[0], nil
}

func (a *AmendmentApplier) Apply(ctx context.Context, adrID int, operator string) error {
	proposed, err := a.findProposedADR(adrID)
	if err != nil {
		return fmt.Errorf("locate ADR-%04d: %w", adrID, err)
	}
	body, err := os.ReadFile(proposed)
	if err != nil {
		return fmt.Errorf("read ADR-%04d: %w", adrID, err)
	}
	diff, err := extractTOMLDiff(body)
	if err != nil {
		_ = a.rejectADR(ctx, adrID, proposed, "no_toml_block", err.Error())
		return fmt.Errorf("ADR-%04d: %w", adrID, err)
	}

	if err := a.cfg.Validator.ValidateTOML(diff); err != nil {
		_ = a.rejectADR(ctx, adrID, proposed, "validate_failed", err.Error())
		return fmt.Errorf("ADR-%04d validate: %w", adrID, err)
	}
	tomlPath := filepath.Join(a.cfg.RepoRoot, "hadessystem.toml")
	pre, err := os.ReadFile(tomlPath)
	if err != nil {
		return fmt.Errorf("read hadessystem.toml: %w", err)
	}
	preHash := sha256Hex(pre)

	acceptedDir := filepath.Join(a.cfg.RepoRoot, "docs", "decisions")
	acceptedPath := filepath.Join(acceptedDir, filepath.Base(proposed))
	if err := os.Rename(proposed, acceptedPath); err != nil {
		return fmt.Errorf("move ADR-%04d to accepted: %w", adrID, err)
	}

	merged := append(append([]byte{}, pre...), '\n')
	merged = append(merged, diff...)
	if err := os.WriteFile(tomlPath, merged, 0o644); err != nil {

		_ = os.Rename(acceptedPath, proposed)
		return fmt.Errorf("apply TOML diff: %w", err)
	}
	commitMsg := fmt.Sprintf(
		"doctrine(amendment): apply ADR-%04d (operator=%s)\n\nADR=%s\npre_toml_sha256=%s",
		adrID, operator, filepath.Base(acceptedPath), preHash)
	if err := a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "add", "hadessystem.toml", "docs/decisions"); err != nil {
		_ = os.WriteFile(tomlPath, pre, 0o644)
		_ = os.Rename(acceptedPath, proposed)
		return fmt.Errorf("git add: %w", err)
	}
	if err := a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "commit", "-q", "-m", commitMsg); err != nil {
		_ = os.WriteFile(tomlPath, pre, 0o644)
		_ = os.Rename(acceptedPath, proposed)
		_ = a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "reset", "-q", "HEAD")
		return fmt.Errorf("git commit: %w", err)
	}

	if a.cfg.ReloadSignal != nil {
		_ = a.cfg.ReloadSignal.Reload(ctx)
	}
	if err := a.cfg.Emitter.Append(ctx, applyAppliedEvent(adrID, operator, preHash)); err != nil {
		return fmt.Errorf("emit DoctrineAmendmentApplied: %w", err)
	}
	return nil
}

func (a *AmendmentApplier) ApplyTransacted(ctx context.Context, adrID int, operator string, rev *AmendmentReverter) (retErr error) {
	if rev == nil {
		return errors.New("amendment: ApplyTransacted requires a non-nil Reverter")
	}
	proposed, err := a.findProposedADR(adrID)
	if err != nil {
		return fmt.Errorf("locate ADR-%04d: %w", adrID, err)
	}
	body, err := os.ReadFile(proposed)
	if err != nil {
		return fmt.Errorf("read ADR-%04d: %w", adrID, err)
	}
	diff, err := extractTOMLDiff(body)
	if err != nil {
		_ = a.rejectADR(ctx, adrID, proposed, "no_toml_block", err.Error())
		return fmt.Errorf("ADR-%04d: %w", adrID, err)
	}
	if err := a.cfg.Validator.ValidateTOML(diff); err != nil {
		_ = a.rejectADR(ctx, adrID, proposed, "validate_failed", err.Error())
		return fmt.Errorf("ADR-%04d validate: %w", adrID, err)
	}
	tomlPath := filepath.Join(a.cfg.RepoRoot, "hadessystem.toml")
	pre, err := os.ReadFile(tomlPath)
	if err != nil {
		return fmt.Errorf("read hadessystem.toml: %w", err)
	}
	preHash := sha256Hex(pre)

	acceptedDir := filepath.Join(a.cfg.RepoRoot, "docs", "decisions")
	acceptedPath := filepath.Join(acceptedDir, filepath.Base(proposed))
	if err := os.Rename(proposed, acceptedPath); err != nil {
		return fmt.Errorf("move ADR-%04d to accepted: %w", adrID, err)
	}
	merged := append(append([]byte{}, pre...), '\n')
	merged = append(merged, diff...)
	if err := os.WriteFile(tomlPath, merged, 0o644); err != nil {
		_ = os.Rename(acceptedPath, proposed)
		return fmt.Errorf("apply TOML diff: %w", err)
	}
	commitMsg := fmt.Sprintf(
		"doctrine(amendment): apply ADR-%04d (operator=%s)\n\nADR=%s\npre_toml_sha256=%s",
		adrID, operator, filepath.Base(acceptedPath), preHash)
	if err := a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "add", "hadessystem.toml", "docs/decisions"); err != nil {
		_ = os.WriteFile(tomlPath, pre, 0o644)
		_ = os.Rename(acceptedPath, proposed)
		return fmt.Errorf("git add: %w", err)
	}
	if err := a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "commit", "-q", "-m", commitMsg); err != nil {
		_ = os.WriteFile(tomlPath, pre, 0o644)
		_ = os.Rename(acceptedPath, proposed)
		_ = a.cfg.Git.Run(ctx, a.cfg.RepoRoot, "reset", "-q", "HEAD")
		return fmt.Errorf("git commit: %w", err)
	}

	committed := true
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("amendment apply panic: %v", r)
		}
		if retErr != nil && committed {

			_ = rev.Rollback(context.WithoutCancel(ctx), adrID, preHash)
		}
	}()

	if a.cfg.ReloadSignal != nil {
		if err := a.cfg.ReloadSignal.Reload(ctx); err != nil {
			retErr = fmt.Errorf("reload signal: %w", err)
			return retErr
		}
	}
	if err := a.cfg.Emitter.Append(ctx, applyAppliedEvent(adrID, operator, preHash)); err != nil {
		retErr = fmt.Errorf("emit DoctrineAmendmentApplied: %w", err)
		return retErr
	}
	return nil
}

func (a *AmendmentApplier) rejectADR(ctx context.Context, adrID int, proposedPath, reason, detail string) error {
	rejectedDir := filepath.Join(a.cfg.RepoRoot, "docs", "decisions", "rejected")
	if err := os.MkdirAll(rejectedDir, 0o755); err != nil {
		return fmt.Errorf("mkdir rejected: %w", err)
	}
	rejectedPath := filepath.Join(rejectedDir, filepath.Base(proposedPath))
	if err := os.Rename(proposedPath, rejectedPath); err != nil {
		return fmt.Errorf("move to rejected: %w", err)
	}
	return a.cfg.Emitter.Append(ctx, suppressedEvent(adrID, reason, detail))
}

func applyAppliedEvent(adrID int, operator, preHash string) eventlog.Event {
	return eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentApplied,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"adr_id":          adrID,
			"operator":        operator,
			"pre_toml_sha256": preHash,
		},
	}
}

func suppressedEvent(adrID int, reason, detail string) eventlog.Event {
	return eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentSuppressed,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"adr_id": adrID,
			"reason": reason,
			"detail": detail,
		},
	}
}
