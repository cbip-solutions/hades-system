// go:build cgo
//go:build cgo
// +build cgo

package evolution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestBuildChurnCountsTouchesAndAuthors(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)

	commitPairN(commit, 30, "alice@x.com", "x.go")
	commitPairN(commit, 25, "bob@x.com", "x.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildChurn(ctx, "proj", dir); err != nil {
		t.Fatalf("BuildChurn: %v", err)
	}
	got, err := s.GetChurn(ctx, "x.go", DefaultParams().WindowDays)
	if err != nil {
		t.Fatalf("GetChurn: %v", err)
	}
	if got.TouchCount != 55 {
		t.Errorf("TouchCount = %d; want 55", got.TouchCount)
	}
	if got.AuthorCount != 2 {
		t.Errorf("AuthorCount = %d; want 2 (alice, bob)", got.AuthorCount)
	}
	if got.LastTouched == 0 {
		t.Error("LastTouched = 0; want the latest commit unix time")
	}
	if got.WindowDays != DefaultParams().WindowDays {
		t.Errorf("WindowDays = %d; want %d", got.WindowDays, DefaultParams().WindowDays)
	}
}

func TestBuildChurnColdStartGate(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)
	commitPairN(commit, 10, "alice@x.com", "x.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildChurn(ctx, "proj", dir); !errors.Is(err, ErrInsufficientHistory) {
		t.Fatalf("BuildChurn cold-start err = %v; want ErrInsufficientHistory", err)
	}
	if _, gerr := s.GetChurn(ctx, "x.go", DefaultParams().WindowDays); !errors.Is(gerr, store.ErrNotFound) {
		t.Errorf("cold-start persisted churn: GetChurn err = %v; want store.ErrNotFound", gerr)
	}
}

func TestBuildChurnRenameSurvives(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)
	commitPairN(commit, 40, "alice@x.com", "old.go")

	mv := exec.Command("git", "mv", "old.go", "new.go")
	mv.Dir = dir
	mv.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=alice@x.com",
		"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=alice@x.com")
	if out, err := mv.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	commitFile(t, dir, "alice@x.com", "commit the rename")

	commitPairN(commit, 15, "alice@x.com", "new.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildChurn(ctx, "proj", dir); err != nil {
		t.Fatalf("BuildChurn: %v", err)
	}
	got, err := s.GetChurn(ctx, "new.go", DefaultParams().WindowDays)
	if err != nil {
		t.Fatalf("GetChurn(new.go): %v", err)
	}

	if got.TouchCount <= 16 {
		t.Errorf("TouchCount = %d; want > 16 (history survives rename via --follow)", got.TouchCount)
	}
}

func TestChurnSplitAuthorTimeParseEdgeCases(t *testing.T) {

	if email, ts, ok := churnSplitAuthorTime("alice@x.com-no-sep"); ok {
		t.Errorf("no-sep: got (%q,%d,true); want ok=false", email, ts)
	}

	if email, ts, ok := churnSplitAuthorTime("alice@x.com" + unitSep + "notanumber"); ok {
		t.Errorf("bad-ts: got (%q,%d,true); want ok=false", email, ts)
	}

	email, ts, ok := churnSplitAuthorTime("alice@x.com" + unitSep + "1700000000")
	if !ok || email != "alice@x.com" || ts != 1700000000 {
		t.Errorf("well-formed: got (%q,%d,%v); want (alice@x.com,1700000000,true)", email, ts, ok)
	}
}

func TestBuildChurnLogCommitsErrorPropagates(t *testing.T) {
	b := NewBuilder(nil, splitErrRunner{count: 50}, fixedParams{p: DefaultParams()})
	err := b.BuildChurn(context.Background(), "proj", "/unused")
	if !errors.Is(err, ErrGit) {
		t.Errorf("BuildChurn logCommits error: got %v; want wrapped ErrGit", err)
	}
}

type splitErrRunner struct{ count int }

func (r splitErrRunner) Log(_ context.Context, _ string, _ ...string) (string, error) {
	return "", ErrGit
}
func (r splitErrRunner) RevListCount(_ context.Context, _ string) (int, error) {
	return r.count, nil
}

func commitFile(t *testing.T, dir, author, msg string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL="+author,
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL="+author)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("commit", "-q", "-m", msg)
}
