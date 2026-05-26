package amendment_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeValidator struct {
	err error
	got [][]byte
	mu  sync.Mutex
}

func (f *fakeValidator) ValidateTOML(b []byte) error {
	f.mu.Lock()
	cp := make([]byte, len(b))
	copy(cp, b)
	f.got = append(f.got, cp)
	f.mu.Unlock()
	return f.err
}

type fakeReloadSignal struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeReloadSignal) Reload(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zenswarm.toml"), []byte("# initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020: x\n```toml\n[autonomy.amendment]\nproposal_cooldown_hours = 48\n```\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestApplyValidatesBeforeCommit(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{err: errors.New("invalid TOML schema")},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.Apply(context.Background(), 20, "the-operator")
	if err == nil {
		t.Fatal("Apply with invalid TOML must return error")
	}
	if !strings.Contains(err.Error(), "validate") {
		t.Errorf("error should mention validate, got: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if strings.Count(string(out), "\n") != 1 {
		t.Errorf("expected exactly 1 commit (init), got log:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "decisions", "rejected", "0020-x.md")); err != nil {
		t.Errorf("expected ADR moved to rejected/, got: %v", err)
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].payload["reason"] != "validate_failed" {
		t.Fatalf("want DoctrineAmendmentSuppressed{reason:validate_failed}, got %+v", got)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(b) != "# initial\n" {
		t.Errorf("zenswarm.toml mutated despite validate-fail: %q", b)
	}
}

func TestApplyAtomicCommitOnSuccess(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	rs := &fakeReloadSignal{}
	v := &fakeValidator{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: v, Emitter: em, ReloadSignal: rs,
	})
	if err := a.Apply(context.Background(), 20, "the-operator"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	out, err := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), "\n") != 2 {
		t.Errorf("expected 2 commits (init + amendment), got:\n%s", out)
	}
	if !strings.Contains(string(out), "ADR-0020") {
		t.Errorf("amendment commit missing ADR-0020 marker:\n%s", out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "docs", "decisions", "0020-*.md"))
	if len(matches) != 1 {
		t.Errorf("expected ADR at docs/decisions/0020-*.md, got %v", matches)
	}
	if rs.calls != 1 {
		t.Errorf("expected one reload signal, got %d", rs.calls)
	}
	if len(v.got) != 1 || !strings.Contains(string(v.got[0]), "proposal_cooldown_hours = 48") {
		t.Errorf("validator did not receive parsed toml diff: %v", v.got)
	}
	tomlContent, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if !strings.Contains(string(tomlContent), "proposal_cooldown_hours = 48") {
		t.Errorf("zenswarm.toml missing applied diff: %q", tomlContent)
	}
	gotEv := em.snapshot()
	if len(gotEv) != 1 || gotEv[0].typ != eventlog.EvtDoctrineAmendmentApplied {
		t.Fatalf("want DoctrineAmendmentApplied, got %+v", gotEv)
	}
	if op, _ := gotEv[0].payload["operator"].(string); op != "the-operator" {
		t.Errorf("missing operator in payload, got %+v", gotEv[0].payload)
	}
}

func TestApplyMissingADR(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 99, "op")
	if err == nil || !strings.Contains(err.Error(), "no proposed ADR") {
		t.Fatalf("want missing ADR error, got %v", err)
	}
}

func TestApplyAmbiguousADR(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-y.md"),
		[]byte("```toml\nfoo=1\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous error, got %v", err)
	}
}

func TestApplyADRWithoutTOMLBlock(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020\nNo TOML here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !errors.Is(err, amendment.ErrNoTOMLBlock) {
		t.Fatalf("want ErrNoTOMLBlock, got %v", err)
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].payload["reason"] != "no_toml_block" {
		t.Fatalf("want suppressed{reason:no_toml_block}, got %+v", got)
	}
}

func TestApplyMissingZenswarmTOML(t *testing.T) {
	dir := initRepo(t)
	if err := os.Remove(filepath.Join(dir, "zenswarm.toml")); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "zenswarm.toml") {
		t.Fatalf("want zenswarm.toml read error, got %v", err)
	}
}

func TestApplyADRReadError(t *testing.T) {
	dir := initRepo(t)

	p := filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "read ADR") {
		t.Fatalf("want read ADR error, got %v", err)
	}
}

func TestNewApplierPanicsOnMissingConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  amendment.ApplierConfig
	}{
		{"empty repo", amendment.ApplierConfig{Validator: &fakeValidator{}, Emitter: &fakeEmitter{}}},
		{"nil validator", amendment.ApplierConfig{RepoRoot: "/", Emitter: &fakeEmitter{}}},
		{"nil emitter", amendment.ApplierConfig{RepoRoot: "/", Validator: &fakeValidator{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic")
				}
			}()
			amendment.NewApplier(c.cfg)
		})
	}
}

type failingGit struct {
	failOn string
}

func (f *failingGit) Run(ctx context.Context, dir string, args ...string) error {
	if len(args) > 0 && args[0] == f.failOn {
		return errors.New("simulated git failure")
	}
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func TestApplyGitAddFailureRollsBack(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{},
		Emitter:   em,
		Git:       &failingGit{failOn: "add"},
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "git add") {
		t.Fatalf("want git add error, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(b) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md")); err != nil {
		t.Errorf("ADR not restored to proposed/: %v", err)
	}
}

func TestApplyGitCommitFailureRollsBack(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{},
		Emitter:   em,
		Git:       &failingGit{failOn: "commit"},
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "git commit") {
		t.Fatalf("want git commit error, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(b) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored after commit fail: %q", b)
	}
}

type emitterErrOnApplied struct {
	mu sync.Mutex
}

func (e *emitterErrOnApplied) Append(_ context.Context, ev eventlog.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ev.Type == eventlog.EvtDoctrineAmendmentApplied {
		return errors.New("simulated emit failure")
	}
	return nil
}

func TestApplyEmitAppliedErrorWraps(t *testing.T) {
	dir := initRepo(t)
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      &emitterErrOnApplied{},
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "emit DoctrineAmendmentApplied") {
		t.Fatalf("want emit error wrapping, got %v", err)
	}
}

func TestApplyTOMLWriteError(t *testing.T) {
	dir := initRepo(t)

	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.Chmod(tomlPath, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tomlPath, 0o644) })

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil {
		t.Skip("filesystem allowed write (root or permissive); cannot exercise write-error path")
	}
	if !strings.Contains(err.Error(), "TOML") && !strings.Contains(err.Error(), "move ADR") {
		t.Logf("non-toml-write error: %v (acceptable; rename may fail first)", err)
	}
}

func TestApplyExecGitRunnerErrorPath(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zenswarm.toml"), []byte("# initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0030-x.md"),
		[]byte("```toml\nfoo=1\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{},
		Emitter:   em,
	})
	err := a.Apply(context.Background(), 30, "op")
	if err == nil || !strings.Contains(err.Error(), "git") {
		t.Fatalf("want git error from execGitRunner, got %v", err)
	}
}

func TestApplyADRMoveError(t *testing.T) {
	dir := initRepo(t)

	target := filepath.Join(dir, "docs", "decisions")
	if err := os.Chmod(target, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil {
		t.Skip("filesystem allowed rename across read-only dir; cannot exercise move-error path")
	}
}

func TestApplyGlobBadPattern(t *testing.T) {
	parent := t.TempDir()
	bad := filepath.Join(parent, "bad[")
	if err := os.MkdirAll(filepath.Join(bad, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  bad,
		Validator: &fakeValidator{},
		Emitter:   em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "glob") && !strings.Contains(err.Error(), "syntax") && !strings.Contains(err.Error(), "no proposed") {
		t.Fatalf("want glob error, got %v", err)
	}
}

func initTrackedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "test")
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
	runGit("add", ".")
	runGit("commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020: x\n```toml\n[autonomy.amendment]\nproposal_cooldown_hours = 48\n```\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestApplyTransactedHappyPath(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	rs := &fakeReloadSignal{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em, ReloadSignal: rs,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: rs,
	})
	if err := a.ApplyTransacted(context.Background(), 20, "op", rev); err != nil {
		t.Fatalf("ApplyTransacted: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if strings.Count(string(out), "\n") != 2 {
		t.Errorf("expected init + amend commits, got:\n%s", out)
	}
	tomlContent, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if !strings.Contains(string(tomlContent), "proposal_cooldown_hours = 48") {
		t.Errorf("zenswarm.toml missing diff: %q", tomlContent)
	}
}

func TestApplyTransactedNilReverter(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", nil)
	if err == nil || !strings.Contains(err.Error(), "Reverter") {
		t.Fatalf("want nil-Reverter error, got %v", err)
	}
}

func TestApplyTransactedReloadFailureRollsBack(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	failReload := &fakeReloadSignal{err: errors.New("reload denied")}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em, ReloadSignal: failReload,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "reload signal") {
		t.Fatalf("want reload error, got %v", err)
	}
	tomlContent, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(tomlContent) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored after rollback: %q", tomlContent)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if strings.Count(string(out), "\n") != 3 {
		t.Errorf("expected init+amend+revert commits, got:\n%s", out)
	}
}

func TestApplyTransactedEmitAppliedFailureRollsBack(t *testing.T) {
	dir := initTrackedRepo(t)
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      &emitterErrOnApplied{},
		ReloadSignal: &fakeReloadSignal{},
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: &fakeEmitter{}, ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "emit DoctrineAmendmentApplied") {
		t.Fatalf("want emit error, got %v", err)
	}
	tomlContent, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(tomlContent) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored: %q", tomlContent)
	}
}

func TestApplyTransactedPreCommitFailures(t *testing.T) {

	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{err: errors.New("bad toml")},
		Emitter:   em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "validate") {
		t.Fatalf("want validate error, got %v", err)
	}
}

func TestApplyTransactedMissingADR(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 99, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "no proposed ADR") {
		t.Fatalf("want missing ADR error, got %v", err)
	}
}

func TestApplyTransactedADRReadError(t *testing.T) {
	dir := initRepo(t)
	p := filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "read ADR") {
		t.Fatalf("want read ADR error, got %v", err)
	}
}

func TestApplyTransactedNoTOMLBlock(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020\nNo TOML\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !errors.Is(err, amendment.ErrNoTOMLBlock) {
		t.Fatalf("want ErrNoTOMLBlock, got %v", err)
	}
}

func TestApplyTransactedMissingTOMLFile(t *testing.T) {
	dir := initRepo(t)
	if err := os.Remove(filepath.Join(dir, "zenswarm.toml")); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "zenswarm.toml") {
		t.Fatalf("want zenswarm.toml error, got %v", err)
	}
}

func TestApplyTransactedGitAddFailure(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{},
		Emitter:   em,
		Git:       &failingGit{failOn: "add"},
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "git add") {
		t.Fatalf("want git add error, got %v", err)
	}
}

func TestApplyTransactedGitCommitFailure(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{},
		Emitter:   em,
		Git:       &failingGit{failOn: "commit"},
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "git commit") {
		t.Fatalf("want git commit error, got %v", err)
	}
}

type panicEmitter struct{}

func (panicEmitter) Append(_ context.Context, ev eventlog.Event) error {
	if ev.Type == eventlog.EvtDoctrineAmendmentApplied {
		panic("simulated panic in emit")
	}
	return nil
}

func TestApplyTransactedPanicTriggersRollback(t *testing.T) {
	dir := initTrackedRepo(t)
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      panicEmitter{},
		ReloadSignal: &fakeReloadSignal{},
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: &fakeEmitter{}, ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("want panic-recovery error, got %v", err)
	}
	tomlContent, _ := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if string(tomlContent) != "# initial\n" {
		t.Errorf("zenswarm.toml not restored after panic-rollback: %q", tomlContent)
	}
}

func TestApplyTransactedAmbiguousADR(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-y.md"),
		[]byte("```toml\nfoo=1\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous error, got %v", err)
	}
}

func TestApplyTransactedTOMLWriteError(t *testing.T) {
	dir := initRepo(t)
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.Chmod(tomlPath, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(tomlPath, 0o644)
		_ = os.Chmod(dir, 0o755)
	})
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil {
		t.Skip("filesystem allowed write (root or permissive); cannot exercise this branch")
	}
}

func TestApplyTransactedADRMoveError(t *testing.T) {
	dir := initRepo(t)
	target := filepath.Join(dir, "docs", "decisions")
	if err := os.Chmod(target, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: dir, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil {
		t.Skip("filesystem permissive; rename succeeded")
	}
}

func TestApplyTransactedGlobBadPattern(t *testing.T) {
	parent := t.TempDir()
	bad := filepath.Join(parent, "bad[")
	if err := os.MkdirAll(filepath.Join(bad, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot: bad, Validator: &fakeValidator{}, Emitter: em,
	})
	rev := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: bad, Emitter: em,
	})
	err := a.ApplyTransacted(context.Background(), 20, "op", rev)
	if err == nil {
		t.Fatal("expected error from bad glob pattern")
	}
}

func TestRejectADRMkdirFailure(t *testing.T) {
	dir := initRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "rejected"),
		[]byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{err: errors.New("bad toml")},
		Emitter:   em,
	})

	err := a.Apply(context.Background(), 20, "op")
	if err == nil || !strings.Contains(err.Error(), "validate") {
		t.Fatalf("want validate error wrapper, got %v", err)
	}
}

func TestRejectADRRenameFailure(t *testing.T) {
	dir := initRepo(t)
	rejectedDir := filepath.Join(dir, "docs", "decisions", "rejected")
	if err := os.MkdirAll(filepath.Join(rejectedDir, "0020-x.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(rejectedDir, "0020-x.md", "blocker"),
		[]byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: &fakeValidator{err: errors.New("bad toml")},
		Emitter:   em,
	})
	err := a.Apply(context.Background(), 20, "op")
	if err == nil {
		t.Skip("rename succeeded despite obstacle; cannot exercise this branch on this FS")
	}
}
