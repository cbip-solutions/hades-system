// go:build cgo
//go:build cgo
// +build cgo

package evolution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

type fixedParams struct{ p Params }

func (f fixedParams) CoChangeParams(string) Params { return f.p }

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestCouplingDegreeFormula(t *testing.T) {
	got := CouplingDegree(store.CoChange{SharedRevs: 4, RevsA: 8, RevsB: 12})
	if !approxEqual(got, 40.0) {
		t.Errorf("CouplingDegree = %v; want 40.0", got)
	}

	if got := CouplingDegree(store.CoChange{SharedRevs: 5, RevsA: 5, RevsB: 5}); !approxEqual(got, 100.0) {
		t.Errorf("CouplingDegree(perfect) = %v; want 100.0", got)
	}

	if got := CouplingDegree(store.CoChange{SharedRevs: 3, RevsA: 3, RevsB: 97}); !approxEqual(got, 6.0) {
		t.Errorf("CouplingDegree(asymmetric) = %v; want 6.0", got)
	}
}

func TestCouplingDegreeZeroGuard(t *testing.T) {
	if got := CouplingDegree(store.CoChange{SharedRevs: 0, RevsA: 0, RevsB: 0}); got != 0 {
		t.Errorf("CouplingDegree(zero) = %v; want 0 (no divide-by-zero)", got)
	}
}

func TestGetCouplingReturnsStoredPair(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertCoChange(ctx, store.CoChange{
		FileA: "a.go", FileB: "b.go", SharedRevs: 4, RevsA: 8, RevsB: 12,
		WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed UpsertCoChange: %v", err)
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	res, err := b.GetCoupling(ctx, "proj", "b.go", "a.go", 90)
	if err != nil {
		t.Fatalf("GetCoupling: %v", err)
	}
	if res.FileA != "a.go" || res.FileB != "b.go" {
		t.Errorf("pair = (%s,%s); want canonical (a.go,b.go)", res.FileA, res.FileB)
	}
	if !approxEqual(res.CouplingPercent, 40.0) {
		t.Errorf("CouplingPercent = %v; want 40.0", res.CouplingPercent)
	}
	if res.SharedRevs != 4 {
		t.Errorf("SharedRevs = %d; want 4", res.SharedRevs)
	}
}

func TestGetCouplingNotFound(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	_, err := b.GetCoupling(context.Background(), "proj", "nope.go", "gone.go", 90)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetCoupling(absent) err = %v; want store.ErrNotFound", err)
	}
}

func TestNewBuilderNilParamsFallsBackToDefault(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, nil)
	if got := b.paramsFor("any"); got.WindowDays != DefaultParams().WindowDays {
		t.Errorf("nil-accessor params WindowDays = %d; want default %d", got.WindowDays, DefaultParams().WindowDays)
	}
}

type invalidParams struct{}

func (invalidParams) CoChangeParams(string) Params {
	p := DefaultParams()
	p.MinRevisions = 1
	return p
}

func TestParamsForValidationFallback(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, invalidParams{})
	got := b.paramsFor("proj")
	want := DefaultParams()
	if got.MinRevisions != want.MinRevisions {
		t.Errorf("paramsFor(invalid) MinRevisions = %d; want default %d (must not use below-floor value)", got.MinRevisions, want.MinRevisions)
	}
}

func TestIsCoupledThreshold(t *testing.T) {
	s := newTestStore(t)
	p := DefaultParams()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: p})

	at := Coupling{CouplingPercent: 30.0}
	if !b.IsCoupled("proj", at) {
		t.Errorf("IsCoupled(30.0) = false; want true (at-threshold must pass)")
	}

	below := Coupling{CouplingPercent: 29.9}
	if b.IsCoupled("proj", below) {
		t.Errorf("IsCoupled(29.9) = true; want false (below-threshold must fail)")
	}

	above := Coupling{CouplingPercent: 85.0}
	if !b.IsCoupled("proj", above) {
		t.Errorf("IsCoupled(85.0) = false; want true")
	}
}

func initGitRepo(t *testing.T, dir string) func(authorEmail string, files ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(env []string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-q")
	run(nil, "config", "user.email", "seed@example.com")
	run(nil, "config", "user.name", "seed")
	run(nil, "config", "commit.gpgsign", "false")
	run(nil, "config", "gc.auto", "0")
	run(nil, "config", "maintenance.auto", "false")
	run(nil, "config", "core.fsmonitor", "false")
	n := 0
	return func(authorEmail string, files ...string) {
		n++
		for _, f := range files {
			p := filepath.Join(dir, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			cur, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(cur, []byte("line\n")...), 0o644); err != nil {
				t.Fatalf("write %s: %v", f, err)
			}
		}
		run(nil, "add", "-A")
		env := []string{
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=" + authorEmail,
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=" + authorEmail,
		}
		run(env, "commit", "-q", "-m", "c")
	}
}

func commitPairN(commit func(string, ...string), n int, author string, files ...string) {
	for i := 0; i < n; i++ {
		commit(author, files...)
	}
}

func TestListCouplingRanksPeersByDegree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seed := []store.CoChange{
		{FileA: "a.go", FileB: "b.go", SharedRevs: 5, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1},
		{FileA: "a.go", FileB: "c.go", SharedRevs: 1, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1},
	}
	for _, r := range seed {
		if err := s.UpsertCoChange(ctx, r); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	got, err := b.ListCoupling(ctx, "proj", "a.go", 90)
	if err != nil {
		t.Fatalf("ListCoupling: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	if !approxEqual(got[0].CouplingPercent, 50.0) || !approxEqual(got[1].CouplingPercent, 10.0) {
		t.Errorf("order = [%v, %v]; want [50.0, 10.0] (descending)", got[0].CouplingPercent, got[1].CouplingPercent)
	}

	if got[0].FileB != "b.go" && got[0].FileA != "b.go" {
		t.Errorf("top peer pair = (%s,%s); want one side b.go", got[0].FileA, got[0].FileB)
	}
}

func TestListCouplingEmptyForUncoupledFile(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	got, err := b.ListCoupling(context.Background(), "proj", "lonely.go", 90)
	if err != nil {
		t.Fatalf("ListCoupling(uncoupled) = %v; want nil error", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d; want 0", len(got))
	}
}

func TestBuildCoChangePersistsCoupledPair(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)

	commitPairN(commit, 50, "alice@x.com", "a.go", "b.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildCoChange(ctx, "proj", dir); err != nil {
		t.Fatalf("BuildCoChange: %v", err)
	}
	res, err := b.GetCoupling(ctx, "proj", "a.go", "b.go", DefaultParams().WindowDays)
	if err != nil {
		t.Fatalf("GetCoupling after build: %v", err)
	}
	if res.SharedRevs != 50 {
		t.Errorf("SharedRevs = %d; want 50", res.SharedRevs)
	}
	if res.RevsA != 50 || res.RevsB != 50 {
		t.Errorf("RevsA=%d RevsB=%d; want 50,50", res.RevsA, res.RevsB)
	}
	if !approxEqual(res.CouplingPercent, 100.0) {
		t.Errorf("CouplingPercent = %v; want 100.0", res.CouplingPercent)
	}
}

func TestBuildCoChangeSparseBelowMinShared(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)

	commitPairN(commit, 50, "alice@x.com", "a.go")
	commitPairN(commit, 50, "alice@x.com", "c.go")
	commit("alice@x.com", "a.go", "b.go")
	commit("alice@x.com", "a.go", "b.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildCoChange(ctx, "proj", dir); err != nil {
		t.Fatalf("BuildCoChange: %v", err)
	}
	_, err := b.GetCoupling(ctx, "proj", "a.go", "b.go", DefaultParams().WindowDays)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a.go/b.go (shared=2 < min_shared=3) err = %v; want store.ErrNotFound (sparse-skip)", err)
	}
}

func TestBuildCoChangeMegaCommitFiltered(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)
	commitPairN(commit, 50, "alice@x.com", "seed.go")

	mega := make([]string, 60)
	for i := range mega {
		mega[i] = fmt.Sprintf("m%d.go", i)
	}
	commit("alice@x.com", mega...)

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	if err := b.BuildCoChange(ctx, "proj", dir); err != nil {
		t.Fatalf("BuildCoChange: %v", err)
	}

	_, err := b.GetCoupling(ctx, "proj", "m0.go", "m1.go", DefaultParams().WindowDays)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("mega-commit pair m0.go/m1.go err = %v; want store.ErrNotFound (filtered)", err)
	}
}

func TestBuildCoChangeColdStartGate(t *testing.T) {
	dir := t.TempDir()
	commit := initGitRepo(t, dir)

	commitPairN(commit, 10, "alice@x.com", "a.go", "b.go")

	s := newTestStore(t)
	b := NewBuilder(s, NewOSGitRunner(), fixedParams{p: DefaultParams()})
	ctx := context.Background()
	err := b.BuildCoChange(ctx, "proj", dir)
	if !errors.Is(err, ErrInsufficientHistory) {
		t.Fatalf("BuildCoChange with 10 commits err = %v; want ErrInsufficientHistory", err)
	}

	_, gerr := b.GetCoupling(ctx, "proj", "a.go", "b.go", DefaultParams().WindowDays)
	if !errors.Is(gerr, store.ErrNotFound) {
		t.Errorf("cold-start persisted a row: GetCoupling err = %v; want store.ErrNotFound", gerr)
	}
}
