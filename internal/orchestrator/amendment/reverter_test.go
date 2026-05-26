package amendment_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func applyAmendment(t *testing.T, dir string, em amendment.EventEmitter, adrID int) {
	t.Helper()
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	if err := a.Apply(context.Background(), adrID, "op"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestRevertOperatorInitiated(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	if err := r.Revert(context.Background(), 20, "op"); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if strings.Count(string(out), "\n") != 3 {
		t.Errorf("expected init+amend+revert (3 commits), got:\n%s", out)
	}
	cur, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(cur) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored: %q", cur)
	}
	found := false
	for _, e := range em.snapshot() {
		if e.typ == eventlog.EvtDoctrineAmendmentReverted {
			if op, _ := e.payload["operator"].(string); op == "op" {
				if _, has := e.payload["original_commit"]; has {
					if _, has2 := e.payload["revert_commit"]; has2 {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Errorf("DoctrineAmendmentReverted not emitted: %+v", em.snapshot())
	}
}

func TestRevertNoCommitForADR(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	err := r.Revert(context.Background(), 99, "op")
	if err == nil || !strings.Contains(err.Error(), "no commit for ADR-0099") {
		t.Fatalf("want missing-commit error, got %v", err)
	}
}

func TestRevertGitRevertFailure(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
		Git:          &failingGit{failOn: "revert"},
	})
	err := r.Revert(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "git revert") {
		t.Fatalf("want git revert error, got %v", err)
	}
}

func TestRevertReloadFailure(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{err: errors.New("daemon down")},
	})
	err := r.Revert(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "reload after revert") {
		t.Fatalf("want reload error, got %v", err)
	}
}

func TestRollbackHashMatch(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	pre, _ := os.ReadFile(tomlPath)
	preHash := sha256.Sum256(pre)
	expected := hex.EncodeToString(preHash[:])
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	if err := r.Rollback(context.Background(), 20, expected); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	post, _ := os.ReadFile(tomlPath)
	postHash := sha256.Sum256(post)
	if hex.EncodeToString(postHash[:]) != expected {
		t.Errorf("post-rollback hash mismatch")
	}

	found := false
	for _, e := range em.snapshot() {
		if e.typ == eventlog.EvtDoctrineAmendmentReverted {
			if rb, _ := e.payload["rollback"].(bool); rb {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("Reverted{rollback:true} not emitted: %+v", em.snapshot())
	}
}

func TestRollbackHashMismatchEmitsAlert(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	err := r.Rollback(context.Background(), 20, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil || !strings.Contains(err.Error(), "rollback hash mismatch") {
		t.Fatalf("want hash mismatch error, got %v", err)
	}
	mismatch := false
	for _, e := range em.snapshot() {
		if e.typ == eventlog.EvtDoctrineAmendmentSuppressed {
			if r, _ := e.payload["reason"].(string); r == "rollback_hash_mismatch" {
				if m, _ := e.payload["mismatch"].(bool); m {
					mismatch = true
				}
			}
		}
	}
	if !mismatch {
		t.Errorf("rollback_hash_mismatch event not emitted: %+v", em.snapshot())
	}
}

func TestRollbackNoCommitForADR(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	err := r.Rollback(context.Background(), 99, "expected")
	if err == nil || !strings.Contains(err.Error(), "no commit for ADR-0099") {
		t.Fatalf("want missing-commit error, got %v", err)
	}
}

func TestRollbackGitRevertFailure(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
		Git: &failingGit{failOn: "revert"},
	})
	err := r.Rollback(context.Background(), 20, "anyhash")
	if err == nil || !strings.Contains(err.Error(), "git revert") {
		t.Fatalf("want git revert error, got %v", err)
	}
}

func TestRollbackTOMLReadError(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)

	tomlPath := filepath.Join(dir, "zenswarm.toml")
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
		Git: &deleteFileAfterRevertGit{tomlPath: tomlPath},
	})
	err := r.Rollback(context.Background(), 20, "anyhash")
	if err == nil || !strings.Contains(err.Error(), "read zenswarm.toml") {
		t.Fatalf("want toml read error, got %v", err)
	}
}

type deleteFileAfterRevertGit struct {
	tomlPath string
}

func (d *deleteFileAfterRevertGit) Run(ctx context.Context, dir string, args ...string) error {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	if len(args) > 0 && args[0] == "revert" {
		_ = os.Remove(d.tomlPath)
	}
	return nil
}

func TestRollbackReloadFailure(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	pre, _ := os.ReadFile(tomlPath)
	preHash := sha256.Sum256(pre)
	expected := hex.EncodeToString(preHash[:])
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
		ReloadSignal: &fakeReloadSignal{err: errors.New("daemon down")},
	})
	err := r.Rollback(context.Background(), 20, expected)
	if err == nil || !strings.Contains(err.Error(), "reload after rollback") {
		t.Fatalf("want reload error, got %v", err)
	}
}

func TestNewReverterPanicsOnMissing(t *testing.T) {
	cases := []struct {
		name string
		cfg  amendment.ReverterConfig
	}{
		{"empty repo", amendment.ReverterConfig{Emitter: &fakeEmitter{}}},
		{"nil emitter", amendment.ReverterConfig{RepoRoot: "/"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic")
				}
			}()
			amendment.NewReverter(c.cfg)
		})
	}
}

func TestRevertNilReloadSignal(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	if err := r.Revert(context.Background(), 20, "op"); err != nil {
		t.Fatalf("Revert with nil ReloadSignal: %v", err)
	}
}

func TestRollbackNilReloadSignal(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	pre, _ := os.ReadFile(tomlPath)
	preHash := sha256.Sum256(pre)
	expected := hex.EncodeToString(preHash[:])
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	if err := r.Rollback(context.Background(), 20, expected); err != nil {
		t.Fatalf("Rollback with nil ReloadSignal: %v", err)
	}
}

func TestRevertHeadSHAErrorPath(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)

	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
		Git:          &nukeGitAfterRevert{repo: dir},
	})
	err := r.Revert(context.Background(), 20, "op")

	if err != nil {
		t.Fatalf("Revert with broken .git after revert: unexpected err=%v", err)
	}
}

type nukeGitAfterRevert struct {
	repo string
}

func (n *nukeGitAfterRevert) Run(ctx context.Context, dir string, args ...string) error {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	if len(args) > 0 && args[0] == "revert" {
		_ = os.RemoveAll(filepath.Join(n.repo, ".git"))
	}
	return nil
}

func TestRevertFindCommitFailure(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	err := r.Revert(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "git log") {
		t.Fatalf("want git log error, got %v", err)
	}
}
