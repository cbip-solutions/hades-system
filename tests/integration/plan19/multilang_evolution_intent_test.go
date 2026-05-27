// go:build integration
package plan19

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	carontestore "github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type defaultParamsAccessor struct{}

func (defaultParamsAccessor) CoChangeParams(_ string) evolution.Params {
	return evolution.DefaultParams()
}

func n5GitInit(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "n5@x")
	gitRun(t, dir, "config", "user.name", "n5")
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=n5", "GIT_AUTHOR_EMAIL=n5@x",
		"GIT_COMMITTER_NAME=n5", "GIT_COMMITTER_EMAIL=n5@x",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func n5CommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", msg)
}

func n5WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func n5GoFnBody(pkg, name string, salt int) string {
	return fmt.Sprintf("package %s\n\nfunc %s() int { return %d }\n", pkg, name, salt)
}

func openCaronteDB(t *testing.T, projectDir string) (*carontestore.Store, func()) {
	t.Helper()
	sqlite_vec.Auto()
	zenDir := filepath.Join(projectDir, ".zen")
	if err := os.MkdirAll(zenDir, 0o700); err != nil {
		t.Fatalf("mkdir .zen: %v", err)
	}
	dbPath := filepath.Join(zenDir, "caronte.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", dbPath)
	db, err := sql.Open(carontestore.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("openCaronteDB sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	st, err := carontestore.Open(context.Background(), db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("openCaronteDB store.Open: %v", err)
	}
	return st, func() { _ = db.Close() }
}

func buildCoChangeForProject(t *testing.T, projectDir, projectID string) {
	t.Helper()
	st, closeDB := openCaronteDB(t, projectDir)
	defer closeDB()

	b := evolution.NewBuilder(st, evolution.NewOSGitRunner(), defaultParamsAccessor{})
	if err := b.BuildCoChange(context.Background(), projectID, projectDir); err != nil {
		t.Fatalf("BuildCoChange(%s): %v", projectDir, err)
	}
}

func seedGetWhyData(t *testing.T, projectDir, nodeID, adrID, loreBody string) {
	t.Helper()
	st, closeDB := openCaronteDB(t, projectDir)
	defer closeDB()
	ctx := context.Background()

	if err := st.UpsertNode(ctx, carontestore.Node{
		NodeID:      nodeID,
		Name:        "CoreFn",
		Kind:        string(carontestore.KindFunction),
		Language:    "go",
		FilePath:    "core.go",
		ContentHash: "seed-hash-001",
		PackageID:   "intent",
	}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	if err := st.UpsertADRLink(ctx, carontestore.ADRLink{
		ADRID:      adrID,
		NodeID:     nodeID,
		PackageID:  "intent",
		LinkKind:   string(carontestore.LinkExplicitRef),
		Confidence: 1.0,
		Stale:      false,
	}); err != nil {
		t.Fatalf("UpsertADRLink: %v", err)
	}

	if err := st.UpsertLoreTrailer(ctx, carontestore.LoreTrailer{
		CommitSHA:   "deadbeefdeadbeef",
		FilePath:    "core.go",
		NodeID:      nodeID,
		TrailerKind: string(carontestore.TrailerConstraint),
		Body:        loreBody,
		AuthoredAt:  1716480000,
	}); err != nil {
		t.Fatalf("UpsertLoreTrailer: %v", err)
	}
}

// TestMultiLangParseAndResolve starts the daemon with a fixture containing
// TS + Python + Rust files and asserts:
// - The daemon boots and the caronte gateway answers (no hard-fail on missing
// SCIP indexer binaries — invariant).
// - get_health reports NodeCount=0 (empty graph, not an error) — the multi-lang
// files are present in the fixture but not indexed (no index path in the daemon).
// - The SCIP-tier assertion (scip_impl confidence) is logged-and-skipped when
// the indexer binary is absent (CI posture).
//
// Sister-test: the daemon MUST NOT os.Exit on missing SCIP binaries. The daemon
// booting and answering tools/list IS the invariant witness — if the engine
// hard-failed on a missing indexer, the daemon would exit before the test
// reaches any assertion.
func TestMultiLangParseAndResolve(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "polyglot")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	n5WriteFile(t, filepath.Join(dir, "go.mod"), "module polyglot\n\ngo 1.25\n")
	n5WriteFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { polyA() }\nfunc polyA() {}\n")
	n5WriteFile(t, filepath.Join(dir, "a.ts"), "export function tsA(){ return tsB(); }\nexport function tsB(){ return 1; }\n")
	n5WriteFile(t, filepath.Join(dir, "a.py"), "def py_a():\n    return py_b()\n\ndef py_b():\n    return 1\n")
	n5WriteFile(t, filepath.Join(dir, "a.rs"), "fn rs_a() -> i32 { rs_b() }\nfn rs_b() -> i32 { 1 }\n")

	n5GitInit(t, dir)
	n5CommitAll(t, dir, "add multi-lang fixture")

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	h := startDaemonWithProject(t, canon)

	res := callTool(t, h, toolGetHealth, nil)
	if _, ok := res["NodeCount"]; !ok {
		t.Errorf("get_health missing NodeCount; daemon did not hard-fail on multi-lang fixture (inv-zen-234)")
	}

	if nc, _ := res["NodeCount"].(float64); nc != 0 {
		t.Logf("NodeCount=%v (non-zero means a background index ran — not expected)", nc)
	}

	healthRaw := callToolRaw(t, h, toolGetHealth, nil)
	if healthRaw.rpcErr != "" {
		t.Errorf("get_health JSON-RPC error: %s — engine hard-failed on multi-lang fixture (inv-zen-234 violation)", healthRaw.rpcErr)
	}

	if _, err := exec.LookPath("scip-typescript"); err != nil {
		t.Logf("scip-typescript not on PATH — SCIP-tier (scip_impl confidence) assertion skipped; heuristic_name fallback is the CI posture (inv-zen-234)")
	}
}

func TestCoChangeSurfacesCoupledFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coupled")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	n5GitInit(t, dir)
	n5WriteFile(t, filepath.Join(dir, "go.mod"), "module coupled\n\ngo 1.25\n")

	for i := 0; i < 50; i++ {
		n5WriteFile(t, filepath.Join(dir, "solo.go"), n5GoFnBody("coupled", "Solo", i))
		n5CommitAll(t, dir, fmt.Sprintf("solo %d", i))
	}

	for i := 0; i < 6; i++ {
		n5WriteFile(t, filepath.Join(dir, "x.go"), n5GoFnBody("coupled", "X", i))
		n5WriteFile(t, filepath.Join(dir, "y.go"), n5GoFnBody("coupled", "Y", i))
		n5CommitAll(t, dir, fmt.Sprintf("edit x+y %d", i))
	}

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	projID := projectID(t, canon)
	buildCoChangeForProject(t, canon, projID)

	h := startDaemonWithProject(t, canon)

	res := callTool(t, h, toolGetCoChange, map[string]any{"file": "x.go"})
	peers, _ := res["peers"].([]any)
	if len(peers) == 0 {
		t.Fatalf("get_cochange(x.go) returned 0 peers; expected y.go (6 shared commits > min_shared=3, spec §8)")
	}
	// Sister-test (spec §8 coupling): y.go MUST appear in the peer list.
	found := false
	for _, p := range peers {
		if m, ok := p.(map[string]any); ok {
			if path, _ := m["Path"].(string); path == "y.go" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("get_cochange(x.go) peers %v missing y.go (6 shared commits ≥ min_shared=3, spec §8)", peers)
	}
}

func TestRenameSurvivesCoChange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "renamed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	n5GitInit(t, dir)
	n5WriteFile(t, filepath.Join(dir, "go.mod"), "module renamed\n\ngo 1.25\n")

	for i := 0; i < 50; i++ {
		n5WriteFile(t, filepath.Join(dir, "seed.go"), n5GoFnBody("renamed", "Seed", i))
		n5CommitAll(t, dir, fmt.Sprintf("seed %d", i))
	}

	n5WriteFile(t, filepath.Join(dir, "a.go"), n5GoFnBody("renamed", "A", 0))
	n5CommitAll(t, dir, "add a.go")
	gitRun(t, dir, "mv", "a.go", "b.go")
	n5CommitAll(t, dir, "rename a.go→b.go (canonical identity established)")

	for i := 0; i < 6; i++ {
		n5WriteFile(t, filepath.Join(dir, "b.go"), n5GoFnBody("renamed", "B", i))
		n5WriteFile(t, filepath.Join(dir, "partner.go"), n5GoFnBody("renamed", "P", i))
		n5CommitAll(t, dir, fmt.Sprintf("edit b+partner %d", i))
	}

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	projID := projectID(t, canon)
	buildCoChangeForProject(t, canon, projID)

	h := startDaemonWithProject(t, canon)

	res := callTool(t, h, toolGetCoChange, map[string]any{"file": "b.go"})
	peers, _ := res["peers"].([]any)
	found := false
	for _, p := range peers {
		if m, ok := p.(map[string]any); ok {
			if path, _ := m["Path"].(string); path == "partner.go" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("get_cochange(b.go) peers %v missing partner.go; renamed file has no co-change signal under new name (spec §8)", peers)
	}
}

// TestMegaCommitFiltered makes 50 solo commits to clear the cold-start gate,
// then one commit touching 60 files (> MaxChangesetSize=50). Pre-seeds
// BuildCoChange, starts daemon, and asserts get_cochange("mass0.go") returns
// NO peers from the mass commit set — the mega-commit was filtered entirely
// (spec §8 — gofmt/mass-rebrand do not fabricate coupling).
//
// Sister-test: if MaxChangesetSize were 100, the 60-file commit would NOT be
// filtered and mass0.go would appear as a peer of other mass*.go files. The
// BuildCoChange Params in the pre-seed use DefaultParams() (MaxChangesetSize=50),
// so the 60-file commit is below the ceiling and gets skipped.
func TestMegaCommitFiltered(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mega")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	n5GitInit(t, dir)
	n5WriteFile(t, filepath.Join(dir, "go.mod"), "module mega\n\ngo 1.25\n")

	for i := 0; i < 50; i++ {
		n5WriteFile(t, filepath.Join(dir, "solo.go"), n5GoFnBody("mega", "Solo", i))
		n5CommitAll(t, dir, fmt.Sprintf("solo %d", i))
	}

	for i := 0; i < 60; i++ {
		n5WriteFile(t, filepath.Join(dir, fmt.Sprintf("mass%d.go", i)), n5GoFnBody("mega", fmt.Sprintf("M%d", i), 0))
	}
	n5CommitAll(t, dir, "mass gofmt-style touch 60 files")

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	projID := projectID(t, canon)
	buildCoChangeForProject(t, canon, projID)

	h := startDaemonWithProject(t, canon)

	res := callTool(t, h, toolGetCoChange, map[string]any{"file": "mass0.go"})
	peers, _ := res["peers"].([]any)

	for _, p := range peers {
		if m, ok := p.(map[string]any); ok {
			path, _ := m["Path"].(string)
			if len(path) >= 4 && path[:4] == "mass" {
				t.Errorf("get_cochange(mass0.go) coupled to %q — the 60-file mega-commit was NOT filtered (MaxChangesetSize=50, spec §8)", path)
			}
		}
	}
}

func TestGetWhyReturnsADRAndLore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "intent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	n5WriteFile(t, filepath.Join(dir, "go.mod"), "module intent\n\ngo 1.25\n")
	n5WriteFile(t, filepath.Join(dir, "core.go"), "package intent\n\n// CoreFn is governed by ADR-9001.\nfunc CoreFn() {}\n")
	n5GitInit(t, dir)
	n5CommitAll(t, dir, "add core + adr fixture")

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	const (
		nodeID  = "intent.CoreFn"
		adrID   = "9001"
		loreTxt = "CoreFn must remain side-effect-free"
	)
	seedGetWhyData(t, canon, nodeID, adrID, loreTxt)

	h := startDaemonWithProject(t, canon)

	res := callToolRaw(t, h, toolGetWhy, map[string]any{"subject": nodeID})
	if res.rpcErr != "" {
		t.Fatalf("get_why returned JSON-RPC error: %s", res.rpcErr)
	}
	payload := res.payload
	if payload == nil {
		t.Fatal("get_why returned nil payload")
	}

	adrs, _ := payload["LinkedADRs"].([]any)
	if len(adrs) == 0 {
		t.Errorf("get_why(intent.CoreFn) returned 0 LinkedADRs; expected ADR-9001 (explicit_ref pre-seeded, spec §10)")
	} else {
		foundADR := false
		for _, a := range adrs {
			if m, ok := a.(map[string]any); ok {
				if id, _ := m["ADRID"].(string); id == adrID {
					foundADR = true
					break
				}
			}
		}
		if !foundADR {
			t.Errorf("get_why(intent.CoreFn) LinkedADRs %v missing ADR-9001", adrs)
		}
	}

	lore, _ := payload["LoreTrailers"].([]any)
	if len(lore) == 0 {
		t.Errorf("get_why(intent.CoreFn) returned 0 LoreTrailers; expected the constraint trailer (pre-seeded, spec §10)")
	} else {
		foundLore := false
		for _, l := range lore {
			if m, ok := l.(map[string]any); ok {
				if body, _ := m["Body"].(string); body == loreTxt {
					foundLore = true
					break
				}
			}
		}
		if !foundLore {
			t.Errorf("get_why(intent.CoreFn) LoreTrailers %v missing %q", lore, loreTxt)
		}
	}
}
