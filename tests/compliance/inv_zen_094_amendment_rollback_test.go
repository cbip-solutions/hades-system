// go:build orchestrator_chaos

// tests/compliance/inv_zen_094_amendment_rollback_test.go
//
// Compliance test for invariant:
//
// AmendmentApplier.ApplyTransacted MUST atomically roll back if any
// post-commit step (reload signal, event emit, panic) fails. Rollback
// leaves the zenswarm.toml byte-identical to pre-Apply (SHA-256 hash
// check); the audit trail contains: init + amendment commit + revert
// commit (3 commits total) — never a hard reset, every change keeps
// an auditable record.
//
// Build tag: orchestrator_chaos. Run with `go test -tags=orchestrator_chaos`.
package compliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type faultReload094 struct{}

func (faultReload094) Reload(_ context.Context) error {
	return errors.New("daemon unreachable (injected for inv-zen-094)")
}

type passValidator094 struct{}

func (passValidator094) ValidateTOML(_ []byte) error { return nil }

type noopReload094 struct{}

func (noopReload094) Reload(_ context.Context) error { return nil }

type capEmitter094 struct {
	events []eventlog.Event
}

func (c *capEmitter094) Append(_ context.Context, ev eventlog.Event) error {
	c.events = append(c.events, ev)
	return nil
}

func mkRepo094(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit094 := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit094("init", "-q")
	runGit094("config", "user.email", "t@t")
	runGit094("config", "user.name", "t")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "zenswarm.toml"), []byte("# initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit094("add", ".")
	runGit094("commit", "-q", "-m", "init")

	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0022-x.md"),
		[]byte("# ADR 0022: x\n```toml\n[autonomy.amendment]\nproposal_cooldown_hours = 48\n```\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return bytes.Count(b, []byte("\n"))
}

// invariant: post-commit failure must trigger atomic rollback; final
// zenswarm.toml hash MUST equal pre-Apply hash; rollback produces a
// revert commit (auditable, never `git reset --hard`).
func TestInvZen094AmendmentRollbackAtomicity(t *testing.T) {
	dir := mkRepo094(t)
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	pre, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	preHash := sha256.Sum256(pre)

	em := &capEmitter094{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    passValidator094{},
		Emitter:      em,
		ReloadSignal: faultReload094{},
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: noopReload094{},
	})
	err = a.ApplyTransacted(context.Background(), 22, "op", rev)
	if err == nil {
		t.Fatal("expected error from injected reload failure")
	}

	post, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	postHash := sha256.Sum256(post)
	if hex.EncodeToString(preHash[:]) != hex.EncodeToString(postHash[:]) {
		t.Errorf("inv-zen-094: zenswarm.toml hash diverged after rollback:\npre =%x\npost=%x\npre-content=%q\npost-content=%q",
			preHash, postHash, pre, post)
	}

	out, err := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if got := countLines(out); got != 3 {
		t.Errorf("inv-zen-094: expected 3 commits (init+amend+revert), got %d:\n%s", got, out)
	}

	out, _ = exec.Command("git", "-C", dir, "log", "-1", "--format=%s").CombinedOutput()
	if !bytes.Contains(out, []byte("Revert")) {
		t.Errorf("inv-zen-094: expected `git revert` commit on HEAD, got subject: %s", out)
	}

	revertEmitted := false
	for _, ev := range em.events {
		if ev.Type == eventlog.EvtDoctrineAmendmentReverted {
			if rb, _ := ev.Payload["rollback"].(bool); rb {
				revertEmitted = true
				if ev.Payload["expected_pre"] != ev.Payload["post_revert_hash"] {
					t.Errorf("inv-zen-094: rollback hash mismatch in event: %+v", ev.Payload)
				}
			}
		}
	}
	if !revertEmitted {
		t.Errorf("inv-zen-094: DoctrineAmendmentReverted{rollback:true} not emitted; got events: %+v", em.events)
	}
}
