// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type ReverterConfig struct {
	RepoRoot     string
	Emitter      EventEmitter
	ReloadSignal ReloadSignal

	Git GitRunner
}

type AmendmentReverter struct {
	cfg ReverterConfig
}

func NewReverter(cfg ReverterConfig) *AmendmentReverter {
	if cfg.RepoRoot == "" {
		panic("amendment: empty RepoRoot")
	}
	if cfg.Emitter == nil {
		panic("amendment: nil Emitter")
	}
	if cfg.Git == nil {
		cfg.Git = execGitRunner{}
	}
	return &AmendmentReverter{cfg: cfg}
}

func (r *AmendmentReverter) findCommitByADR(ctx context.Context, adrID int) (string, error) {
	c := exec.CommandContext(ctx, "git", "log", "--format=%H",
		fmt.Sprintf("--grep=ADR-%04d", adrID))
	c.Dir = r.cfg.RepoRoot
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log --grep ADR-%04d: %w: %s", adrID, err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no commit for ADR-%04d", adrID)
	}
	return lines[0], nil
}

func (r *AmendmentReverter) headSHA(ctx context.Context) (string, error) {
	c := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	c.Dir = r.cfg.RepoRoot
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Revert is the operator-initiated path (`zen doctrine revert ADR-NNNN`).
// It locates the amendment commit, runs `git revert --no-edit`, issues
// the doctrine reload signal, and emits DoctrineAmendmentReverted.
//
// On reload-signal failure the revert commit has already landed; the
// operator is informed via the wrapped error and audit captures the
// state. We do NOT roll back the revert in that case (it would create
// an oscillation).
func (r *AmendmentReverter) Revert(ctx context.Context, adrID int, operator string) error {
	sha, err := r.findCommitByADR(ctx, adrID)
	if err != nil {
		return err
	}
	if err := r.cfg.Git.Run(ctx, r.cfg.RepoRoot, "revert", "--no-edit", sha); err != nil {
		return fmt.Errorf("git revert %s: %w", sha, err)
	}
	revertSHA, _ := r.headSHA(ctx)
	if r.cfg.ReloadSignal != nil {
		if err := r.cfg.ReloadSignal.Reload(ctx); err != nil {
			return fmt.Errorf("reload after revert: %w", err)
		}
	}
	return r.cfg.Emitter.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentReverted,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"adr_id":          adrID,
			"operator":        operator,
			"original_commit": sha,
			"revert_commit":   revertSHA,
		},
	})
}

func (r *AmendmentReverter) Rollback(ctx context.Context, adrID int, expectedPreHash string) error {
	sha, err := r.findCommitByADR(ctx, adrID)
	if err != nil {
		return err
	}
	if err := r.cfg.Git.Run(ctx, r.cfg.RepoRoot, "revert", "--no-edit", sha); err != nil {
		return fmt.Errorf("git revert %s: %w", sha, err)
	}
	tomlPath := filepath.Join(r.cfg.RepoRoot, "zenswarm.toml")
	post, err := os.ReadFile(tomlPath)
	if err != nil {
		return fmt.Errorf("read zenswarm.toml post-rollback: %w", err)
	}
	postHash := sha256.Sum256(post)
	postHex := hex.EncodeToString(postHash[:])
	if postHex != expectedPreHash {

		_ = r.cfg.Emitter.Append(ctx, eventlog.Event{
			Type:      eventlog.EvtDoctrineAmendmentSuppressed,
			Timestamp: time.Now().UTC(),
			Payload: map[string]any{
				"reason":           "rollback_hash_mismatch",
				"adr_id":           adrID,
				"expected_pre":     expectedPreHash,
				"post_revert_hash": postHex,
				"mismatch":         true,
			},
		})
		return fmt.Errorf("rollback hash mismatch: expected %s, got %s", expectedPreHash, postHex)
	}
	if r.cfg.ReloadSignal != nil {
		if err := r.cfg.ReloadSignal.Reload(ctx); err != nil {
			return fmt.Errorf("reload after rollback: %w", err)
		}
	}
	revertSHA, _ := r.headSHA(ctx)
	return r.cfg.Emitter.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentReverted,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"adr_id":           adrID,
			"original_commit":  sha,
			"revert_commit":    revertSHA,
			"rollback":         true,
			"expected_pre":     expectedPreHash,
			"post_revert_hash": postHex,
		},
	})
}
