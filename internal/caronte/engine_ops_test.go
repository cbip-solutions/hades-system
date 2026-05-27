// go:build cgo
//go:build cgo
// +build cgo

// Package caronte — engine_ops_test.go.
//
// Sister-tests for the 8 federation ops on the cgo *Engine: each op
// MUST refuse with ErrFederationUnavailable when Deps.FederationDB is nil
// (the boot-time wiring gap; wires the FederationDB at composition
// root). The gateway proxy depends on this sentinel for per-mode escalate()
// (spec §15 graceful degradation — ops continue serving; only the
// 8 federation ops are unavailable).
//
// Before this file the 8 ops were exercised only via fakeCaronteEngine in
// the proxy tests (internal/daemon/mcpgateway/caronte_proxy_test.go); the
// fake bypasses the real engine. A refactor that removed the
// `if e.deps.FederationDB == nil { return..., ErrFederationUnavailable }`
// short-circuit would have gone undetected by the proxy tests. These 8
// sister-tests bite that regression directly + pin the documented behaviour
// of each op (per feedback_sister_test_pattern.md).
//
// GetContract is a special case post-Fix-I-2: it returns
// ErrFederationUnavailable UNCONDITIONALLY in (whether
// FederationDB is nil or not — see TestGetContract_ReturnsFederationUnavailableUntilPhaseH
// in engine_get_contract_test.go for the wired-DB variant). The nil-DB test
// here still applies (same return value), and the file kept symmetric across
// all 8 ops so a future wiring change must update every op's contract
// at once.
package caronte

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newEngineNilFederationDB returns an *Engine constructed with no
// FederationDB (the boot-time wiring gap simulation). All other Deps are
// zero — the 8 federation ops MUST short-circuit on the FederationDB nil
// check before touching any other Deps field. If a regression makes them
// dereference (e.g.) Dispatcher unconditionally, the resulting nil-pointer
// panic surfaces inside the sister-test rather than at agent runtime.
func newEngineNilFederationDB() *Engine {
	return &Engine{deps: Deps{}}
}

func TestEngineOps_GetContract_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.GetContract(context.Background(), "ep-1", "proj-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got != (ContractPayload{}) {
		t.Errorf("payload = %#v; want zero ContractPayload", got)
	}
}

func TestEngineOps_GetConsumers_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.GetConsumers(context.Background(), "ep-1", "ws-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got.EndpointID != "" || got.WorkspaceID != "" || len(got.Consumers) != 0 {
		t.Errorf("payload = %#v; want zero ConsumerList", got)
	}
}

func TestEngineOps_GetBreakingChanges_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.GetBreakingChanges(context.Background(), "ws-1", 0)
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got != nil {
		t.Errorf("payloads = %#v; want nil slice", got)
	}
}

func TestEngineOps_TraceAPICall_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.TraceAPICall(context.Background(), "call-1", "ws-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got.CallID != "" || got.WorkspaceID != "" {
		t.Errorf("trace = %#v; want zero APICallTrace", got)
	}
}

func TestEngineOps_GetWorkspace_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.GetWorkspace(context.Background(), "ws-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got.WorkspaceID != "" || len(got.Members) != 0 {
		t.Errorf("snapshot = %#v; want zero WorkspaceSnapshot", got)
	}
}

func TestEngineOps_FederationHealth_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.FederationHealth(context.Background(), "ws-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}

	if got.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q; want \"ws-1\" (echo on degrade)", got.WorkspaceID)
	}
	if got.Reachable {
		t.Errorf("Reachable = true; want false on nil-FederationDB")
	}
}

func TestEngineOps_ContractDiff_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.ContractDiff(context.Background(), "ep-1", 1700000000)
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got.EndpointID != "" || got.Severity != "" {
		t.Errorf("diff = %#v; want zero ContractDiff", got)
	}
}

func TestEngineOps_GetWhyBreakingChange_NilFederationDB(t *testing.T) {
	e := newEngineNilFederationDB()
	got, err := e.GetWhyBreakingChange(context.Background(), "bc-1")
	if !errors.Is(err, ErrFederationUnavailable) {
		t.Fatalf("err = %v; want ErrFederationUnavailable", err)
	}
	if got.ChangeID != "" {
		t.Errorf("why = %#v; want zero WhyBreakingChange", got)
	}
}

func newIndexEngine(t *testing.T) (e *Engine, projectID, srcRoot string) {
	t.Helper()
	deps, dirs := testDeps(t)
	projectID = "proj-idx"
	root := t.TempDir()
	srcRoot = root
	dirs[projectID] = root
	deps.RepoRootFor = func(_ context.Context, id string) (string, error) {
		if id == projectID {
			return srcRoot, nil
		}
		return "", errors.New("unknown project (test)")
	}
	en, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = en.Close() })
	return en, projectID, srcRoot
}

// TestIndexProjectEmptyDirReturnsZeroCounts asserts a reindex against an
// empty source root succeeds (Completed=true) with zero file/node counts.
// Walker MUST tolerate empty + the.zen/ subdir (skipped per skip-list).
func TestIndexProjectEmptyDirReturnsZeroCounts(t *testing.T) {
	e, projectID, _ := newIndexEngine(t)
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}
	if !rep.Completed {
		t.Error("Completed=false on empty success; want true")
	}
	if rep.FilesIndexed != 0 {
		t.Errorf("FilesIndexed = %d; want 0 (empty dir)", rep.FilesIndexed)
	}
	if rep.NodesCreated != 0 {
		t.Errorf("NodesCreated = %d; want 0 (empty dir)", rep.NodesCreated)
	}
	if rep.ProjectID != projectID {
		t.Errorf("ProjectID = %q; want %q", rep.ProjectID, projectID)
	}
	if rep.LanguageCounts == nil {
		t.Error("LanguageCounts = nil; want non-nil empty map")
	}
}

func TestIndexProjectGoOnly(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	const goSrc = `package x

// Foo does nothing.
func Foo() {}

// Bar does nothing else.
func Bar() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "x.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write x.go: %v", err)
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}
	if rep.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d; want 1", rep.FilesIndexed)
	}
	if rep.LanguageCounts["go"] != 1 {
		t.Errorf("LanguageCounts[go] = %d; want 1", rep.LanguageCounts["go"])
	}
	if rep.NodesCreated < 2 {
		t.Errorf("NodesCreated = %d; want ≥2 (Foo + Bar)", rep.NodesCreated)
	}
	if !rep.Completed {
		t.Error("Completed=false on happy path; want true")
	}

	if rep.DurationMillis < 0 {
		t.Errorf("DurationMillis = %d; want ≥0", rep.DurationMillis)
	}
	if rep.StartedAt.IsZero() {
		t.Error("StartedAt is zero; want non-zero")
	}
}

func TestIndexProjectIdempotent(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	const goSrc = `package y

func A() {}
func B() {}
func C() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "y.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write y.go: %v", err)
	}
	ctx := context.Background()
	rep1, err := e.IndexProject(ctx, projectID)
	if err != nil {
		t.Fatalf("IndexProject #1: %v", err)
	}
	rep2, err := e.IndexProject(ctx, projectID)
	if err != nil {
		t.Fatalf("IndexProject #2: %v", err)
	}

	if rep2.FilesIndexed != rep1.FilesIndexed {
		t.Errorf("FilesIndexed drift: pass1=%d, pass2=%d", rep1.FilesIndexed, rep2.FilesIndexed)
	}
	if rep2.NodesCreated != rep1.NodesCreated {
		t.Errorf("NodesCreated drift: pass1=%d, pass2=%d", rep1.NodesCreated, rep2.NodesCreated)
	}

	h, err := e.GetHealth(ctx, projectID)
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if h.NodeCount != rep1.NodesCreated {
		t.Errorf("Live NodeCount (%d) != first-pass NodesCreated (%d) — upsert drift",
			h.NodeCount, rep1.NodesCreated)
	}
}

// TestIndexProjectAutoResolvesEdges asserts invariant (v0.20.1 fix #4):
// after a successful walk against a valid Go module fixture, IndexProject
// auto-triggers semantic.Resolver.ResolveProject so the IndexReport
// surfaces a non-zero EdgesCreated count (the call-graph fan-out + the
// implements fan-out, derived from VTA/CHA over the SSA build).
//
// Before v0.20.1, EdgesCreated stayed 0 for the lifetime of the project
// because the resolver call was deferred (see the inline historical
// comment on engine_ops.go). v0.20.1 closes the wiring.
//
// Sister-test bite check: revert the pe.resolver.ResolveProject call OR
// drop the rep.EdgesCreated assignment; this test MUST fail because
// EdgesCreated returns to 0 even after a successful resolve pass.
func TestIndexProjectAutoResolvesEdges(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)

	goMod := []byte("module example.com/edgetest\n\ngo 1.22\n")
	if err := os.WriteFile(filepath.Join(srcRoot, "go.mod"), goMod, 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	const goSrc = `package main

func main() {
	helper()
}

func helper() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "main.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}
	if !rep.Completed {
		t.Error("Completed=false on happy Go-module path; want true")
	}
	if rep.NodesCreated < 2 {
		t.Errorf("NodesCreated = %d; want ≥2 (main + helper)", rep.NodesCreated)
	}
	if rep.EdgesCreated < 1 {
		t.Errorf("EdgesCreated = %d; want ≥1 (main → helper call edge); inv-zen-284 closes the auto-resolver wiring", rep.EdgesCreated)
	}
}

func TestIndexProjectResolverFailureGracefulDegrade(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)

	const goSrc = `package z

func Only() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "z.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write z.go: %v", err)
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v (expected graceful degrade)", err)
	}
	if !rep.Completed {
		t.Error("Completed=false on resolver-failure-but-walk-success; want true (§15 never-hard-fail)")
	}
	if rep.NodesCreated < 1 {
		t.Errorf("NodesCreated = %d; want ≥1 (the walk's node population is the load-bearing output)", rep.NodesCreated)
	}
	if rep.EdgesCreated != 0 {
		t.Errorf("EdgesCreated = %d; want 0 (resolver did not run successfully on no-go.mod fixture)", rep.EdgesCreated)
	}
}

func TestIndexProjectUnknownProject(t *testing.T) {
	deps, _ := testDeps(t)
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	rep, err := e.IndexProject(context.Background(), "nonexistent-project-id")
	if !errors.Is(err, ErrProjectUnavailable) {
		t.Errorf("IndexProject(unknown).err = %v; want ErrProjectUnavailable", err)
	}

	if rep.ProjectID != "nonexistent-project-id" {
		t.Errorf("rep.ProjectID = %q; want echo of input", rep.ProjectID)
	}
	if rep.Completed {
		t.Error("Completed=true on unknown project; want false")
	}
}

func TestIndexProjectAfterCloseReturnsEngineClosed(t *testing.T) {
	e, projectID, _ := newIndexEngine(t)
	_ = e.Close()
	_, err := e.IndexProject(context.Background(), projectID)
	if !errors.Is(err, ErrEngineClosed) {
		t.Errorf("IndexProject after Close = %v; want ErrEngineClosed", err)
	}
}

func TestIndexProjectSkipsHiddenAndVendorDirs(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	const visible = `package main

func Main() {}
`
	const hidden = `package hidden

func Hidden() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "main.go"), []byte(visible), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	for _, dir := range []string{".git", "node_modules", "vendor", "target", ".hidden"} {
		full := filepath.Join(srcRoot, dir)
		if err := os.MkdirAll(full, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(filepath.Join(full, "h.go"), []byte(hidden), 0o600); err != nil {
			t.Fatalf("write %s/h.go: %v", full, err)
		}
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}
	if rep.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d; want 1 (skip-list bypassed)", rep.FilesIndexed)
	}
	if rep.LanguageCounts["go"] != 1 {
		t.Errorf("LanguageCounts[go] = %d; want 1", rep.LanguageCounts["go"])
	}
}

func TestIndexProjectMissingRepoRoot(t *testing.T) {
	deps, dirs := testDeps(t)
	dirs["proj-noroot"] = t.TempDir()
	deps.RepoRootFor = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("repo root unavailable")
	}
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	_, indexErr := e.IndexProject(context.Background(), "proj-noroot")
	if !errors.Is(indexErr, ErrProjectUnavailable) {
		t.Errorf("IndexProject(missing repo root) err = %v; want ErrProjectUnavailable", indexErr)
	}
}

func TestIndexProjectMultiLanguage(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	const goSrc = `package x
func GoFn() {}
`
	const pySrc = `def py_fn():
    pass
`
	if err := os.WriteFile(filepath.Join(srcRoot, "a.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "b.py"), []byte(pySrc), 0o600); err != nil {
		t.Fatalf("write b.py: %v", err)
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}
	if rep.FilesIndexed != 2 {
		t.Errorf("FilesIndexed = %d; want 2", rep.FilesIndexed)
	}
	if rep.LanguageCounts["go"] != 1 {
		t.Errorf("LanguageCounts[go] = %d; want 1", rep.LanguageCounts["go"])
	}
	if rep.LanguageCounts["python"] != 1 {
		t.Errorf("LanguageCounts[python] = %d; want 1", rep.LanguageCounts["python"])
	}
}

func TestIndexProjectAllLanguages(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	cases := []struct {
		name string
		src  string
	}{
		{"a.go", "package x\nfunc GoFn() {}\n"},
		{"b.ts", "function tsFn() {}\n"},
		{"c.py", "def py_fn():\n    pass\n"},
		{"d.rs", "fn rust_fn() {}\n"},

		{"README.md", "# readme\n"},
		{"config.json", "{}\n"},
		{"LICENSE", "MIT\n"},
	}
	for _, c := range cases {
		full := filepath.Join(srcRoot, c.name)
		if err := os.WriteFile(full, []byte(c.src), 0o600); err != nil {
			t.Fatalf("write %s: %v", c.name, err)
		}
	}
	rep, err := e.IndexProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("IndexProject: %v", err)
	}

	if rep.FilesIndexed != 4 {
		t.Errorf("FilesIndexed = %d; want 4 (README/config/LICENSE skipped)", rep.FilesIndexed)
	}
	for _, lang := range []string{"go", "typescript", "python", "rust"} {
		if rep.LanguageCounts[lang] != 1 {
			t.Errorf("LanguageCounts[%s] = %d; want 1", lang, rep.LanguageCounts[lang])
		}
	}
}

func TestIndexProjectWalkErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod bypassed → cannot induce WalkDir error")
	}
	e, projectID, srcRoot := newIndexEngine(t)
	const goSrc = `package x
func A() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "a.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write a.go: %v", err)
	}

	blocked := filepath.Join(srcRoot, "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "b.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write blocked/b.go: %v", err)
	}

	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	rep, err := e.IndexProject(context.Background(), projectID)
	if err == nil {
		t.Fatal("IndexProject(blocked subdir) returned nil error; want wrapped EACCES")
	}
	if !strings.Contains(err.Error(), "walk") {
		t.Errorf("err = %q; want substring 'walk' (the wrapped walk error)", err.Error())
	}
	if rep.Completed {
		t.Error("Completed=true after walk error; want false")
	}

	if rep.DurationMillis < 0 {
		t.Errorf("DurationMillis = %d; want ≥0 (populated even on walk failure)", rep.DurationMillis)
	}
}

func TestIndexProjectFileReadError(t *testing.T) {
	deps, dirs := testDeps(t)
	dirs["proj-missing"] = t.TempDir()
	bogus := filepath.Join(t.TempDir(), "does-not-exist")
	deps.RepoRootFor = func(_ context.Context, _ string) (string, error) {
		return bogus, nil
	}
	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer e.Close()
	rep, err := e.IndexProject(context.Background(), "proj-missing")
	if err == nil {
		t.Fatal("IndexProject(missing root) returned nil; want error")
	}
	if rep.Completed {
		t.Error("Completed=true on missing root; want false")
	}
}

func TestIndexProjectCancelledContextPropagates(t *testing.T) {
	e, projectID, srcRoot := newIndexEngine(t)
	const goSrc = `package x
func A() {}
`
	if err := os.WriteFile(filepath.Join(srcRoot, "a.go"), []byte(goSrc), 0o600); err != nil {
		t.Fatalf("write a.go: %v", err)
	}

	if _, err := e.projectEngineFor(context.Background(), projectID); err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep, err := e.IndexProject(ctx, projectID)
	if err == nil {
		t.Fatal("IndexProject(cancelled ctx) returned nil; want context-cancel-rooted error")
	}
	if rep.Completed {
		t.Error("Completed=true on cancelled context; want false")
	}
}
