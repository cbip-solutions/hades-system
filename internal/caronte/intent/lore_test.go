// go:build cgo
//go:build cgo
// +build cgo

package intent

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	caronteevo "github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func openMattn(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func runWithStdin(cmd *exec.Cmd, s string) (string, error) {
	cmd.Stdin = strings.NewReader(s)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func initGitRepo(t *testing.T) (string, func(msg, author, file, content string)) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "seed@example.com")
	run("config", "user.name", "seed")
	run("config", "commit.gpgsign", "false")
	run("config", "gc.auto", "0")
	run("config", "maintenance.auto", "false")
	run("config", "core.fsmonitor", "false")
	commit := func(msg, author, file, content string) {
		full := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", file, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		run("add", file)
		cmd := exec.Command("git", "commit", "-q", "-m", msg)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL="+author,
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL="+author)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	return dir, commit
}

func TestIndexLoreLinksTrailerToTouchedNode(t *testing.T) {
	dir, commit := initGitRepo(t)
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertNode(ctx, store.Node{
		NodeID: "pkg/x.Reader", Name: "Reader", Kind: string(store.KindInterface),
		Language: "go", FilePath: "pkg/x/x.go", ContentHash: "h1",
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	body := "feat(x): add reader\n\nLore-Constraint: Reader must stay io-free\n"
	commit(body, "dev@example.com", "pkg/x/x.go", "package x\ntype Reader interface{}\n")

	idx := NewLoreIndexer(s, caronteevo.NewOSGitRunner())
	res, err := idx.IndexLore(ctx, "proj", dir)
	if err != nil {
		t.Fatalf("IndexLore: %v", err)
	}
	if res.Trailers != 1 || res.WrittenRows != 1 {
		t.Errorf("result = %+v; want Trailers=1 WrittenRows=1", res)
	}

	got, err := s.ListLoreTrailersForNode(ctx, "pkg/x.Reader")
	if err != nil {
		t.Fatalf("ListLoreTrailersForNode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d trailers; want 1", len(got))
	}
	if got[0].TrailerKind != string(store.TrailerConstraint) {
		t.Errorf("TrailerKind = %q; want %q", got[0].TrailerKind, store.TrailerConstraint)
	}
	if got[0].Body != "Reader must stay io-free" {
		t.Errorf("Body = %q; want %q", got[0].Body, "Reader must stay io-free")
	}
	if got[0].FilePath != "pkg/x/x.go" {
		t.Errorf("FilePath = %q; want pkg/x/x.go", got[0].FilePath)
	}
	if got[0].AuthoredAt == 0 {
		t.Error("AuthoredAt = 0; want the commit unix time")
	}
}

func TestIndexLoreIdempotent(t *testing.T) {
	dir, commit := initGitRepo(t)
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{NodeID: "pkg/x.A", Name: "A", Kind: string(store.KindFunction), Language: "go", FilePath: "a.go", ContentHash: "h"})
	commit("feat(x): a\n\nLore-Rejected: tried sync, too slow\n", "d@example.com", "a.go", "package x\nfunc A(){}\n")

	idx := NewLoreIndexer(s, caronteevo.NewOSGitRunner())
	if _, err := idx.IndexLore(ctx, "proj", dir); err != nil {
		t.Fatalf("IndexLore #1: %v", err)
	}
	if _, err := idx.IndexLore(ctx, "proj", dir); err != nil {
		t.Fatalf("IndexLore #2: %v", err)
	}
	got, _ := s.ListLoreTrailersForNode(ctx, "pkg/x.A")
	if len(got) != 1 {
		t.Errorf("after re-index got %d rows; want 1 (idempotent)", len(got))
	}
}

func TestIndexLoreNoTrailersNoRows(t *testing.T) {
	dir, commit := initGitRepo(t)
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{NodeID: "pkg/x.A", Name: "A", Kind: string(store.KindFunction), Language: "go", FilePath: "a.go", ContentHash: "h"})
	commit("feat(x): plain commit no trailer", "d@example.com", "a.go", "package x\nfunc A(){}\n")

	idx := NewLoreIndexer(s, caronteevo.NewOSGitRunner())
	res, err := idx.IndexLore(ctx, "proj", dir)
	if err != nil {
		t.Fatalf("IndexLore: %v", err)
	}
	if res.Trailers != 0 || res.WrittenRows != 0 {
		t.Errorf("result = %+v; want zero trailers/rows", res)
	}
}

func TestIndexLoreInterpretTrailersAgreement(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	body := "feat(x): t\n\nbody para\n\nLore-Constraint: stay pure\nLore-Verification: inv-zen-238\n"
	cmd := exec.Command("git", "interpret-trailers", "--parse")
	out, err := runWithStdin(cmd, body)
	if err != nil {
		t.Skipf("git interpret-trailers unavailable: %v", err)
	}

	parsed := parseTrailerLines(body)
	if len(parsed) != 2 {
		t.Fatalf("pure parser found %d Lore trailers; want 2", len(parsed))
	}

	for _, want := range []string{"Lore-Constraint: stay pure", "Lore-Verification: inv-zen-238"} {
		if !contains(out, want) {
			t.Errorf("interpret-trailers output missing %q; got:\n%s", want, out)
		}
	}
}

func TestIndexLoreMultiFileDeterministicPrimary(t *testing.T) {
	dir, _ := initGitRepo(t)
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{NodeID: "pkg/a.A", Name: "A", Kind: string(store.KindFunction), Language: "go", FilePath: "a/a.go", ContentHash: "h"})
	_ = s.UpsertNode(ctx, store.Node{NodeID: "pkg/z.Z", Name: "Z", Kind: string(store.KindFunction), Language: "go", FilePath: "z/z.go", ContentHash: "h"})

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	_ = os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "z"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "a/a.go"), []byte("package a\nfunc A(){}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "z/z.go"), []byte("package z\nfunc Z(){}\n"), 0o600)
	run("add", "a/a.go", "z/z.go")
	run("commit", "-q", "-m", "feat(x): both\n\nLore-Constraint: a and z evolve together\n")

	idx := NewLoreIndexer(s, caronteevo.NewOSGitRunner())
	if _, err := idx.IndexLore(ctx, "proj", dir); err != nil {
		t.Fatalf("IndexLore: %v", err)
	}

	gotA, _ := s.ListLoreTrailersForNode(ctx, "pkg/a.A")
	if len(gotA) != 1 || gotA[0].Body != "a and z evolve together" {
		t.Errorf("pkg/a.A trailers = %+v; want one constraint", gotA)
	}
	gotZ, _ := s.ListLoreTrailersForNode(ctx, "pkg/z.Z")
	if len(gotZ) != 0 {
		t.Errorf("pkg/z.Z trailers = %+v; want 0 (primary-node rule)", gotZ)
	}
}

type fakeRunner struct {
	out string
	err error
}

func (f fakeRunner) Log(_ context.Context, _ string, _ ...string) (string, error) {
	return f.out, f.err
}
func (f fakeRunner) RevListCount(_ context.Context, _ string) (int, error) { return 0, f.err }

func TestIndexLoreGitErrorWrapped(t *testing.T) {
	s := newTestStore(t)
	idx := NewLoreIndexer(s, fakeRunner{err: caronteevo.ErrGit})
	if _, err := idx.IndexLore(context.Background(), "proj", "/nonexistent"); err == nil {
		t.Fatal("IndexLore: want wrapped git error, got nil")
	}
}

func TestIndexLoreClosedDBError(t *testing.T) {
	s := newTestStore(t)

	dir := t.TempDir()
	db, err := openMattn(dir + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	s2, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	_ = db.Close()

	const us = "\x1f"
	const rs = "\x1e"
	out := "deadbeef" + us + "d@e.com" + us + "1700000000" + us +
		"feat(x): a\n\nLore-Constraint: c\n" + rs + "\nx.go\n"
	idx := NewLoreIndexer(s2, fakeRunner{out: out})
	if _, err := idx.IndexLore(ctx, "proj", "/repo"); err == nil {
		t.Fatal("IndexLore with closed DB: want error, got nil")
	}
	_ = s // silence unused warning
}

func TestIndexLoreDocsOnlyCommitRecordsEmptyNode(t *testing.T) {
	dir, commit := initGitRepo(t)
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertNode(ctx, store.Node{
		NodeID:      "pkg/core.Processor",
		Name:        "Processor",
		Kind:        string(store.KindInterface),
		Language:    "go",
		FilePath:    "pkg/core/core.go",
		ContentHash: "h1",
	}); err != nil {
		t.Fatalf("seed noded node: %v", err)
	}

	body := "docs(notes): add design constraint\n\nLore-Constraint: docs-only lore must not be dropped\n"
	commit(body, "author@example.com", "notes.md", "# Notes\nsome docs\n")

	idx := NewLoreIndexer(s, caronteevo.NewOSGitRunner())
	res, err := idx.IndexLore(ctx, "proj", dir)
	if err != nil {
		t.Fatalf("IndexLore: %v", err)
	}

	if res.Trailers != 1 || res.WrittenRows != 1 {
		t.Errorf("result = %+v; want Trailers=1 WrittenRows=1", res)
	}

	got, err := s.ListLoreTrailersForNode(ctx, "")
	if err != nil {
		t.Fatalf("ListLoreTrailersForNode(\"\"): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("empty-node trailers: got %d rows; want 1", len(got))
	}
	if got[0].NodeID != "" {
		t.Errorf("NodeID = %q; want empty string (docs-only node)", got[0].NodeID)
	}
	if got[0].Body != "docs-only lore must not be dropped" {
		t.Errorf("Body = %q; want %q", got[0].Body, "docs-only lore must not be dropped")
	}
	if got[0].FilePath != "notes.md" {
		t.Errorf("FilePath = %q; want %q", got[0].FilePath, "notes.md")
	}
	if got[0].TrailerKind != string(store.TrailerConstraint) {
		t.Errorf("TrailerKind = %q; want %q", got[0].TrailerKind, store.TrailerConstraint)
	}
	if got[0].CommitSHA == "" {
		t.Error("CommitSHA must be non-empty")
	}

	noded, err := s.ListLoreTrailersForNode(ctx, "pkg/core.Processor")
	if err != nil {
		t.Fatalf("ListLoreTrailersForNode(pkg/core.Processor): %v", err)
	}
	if len(noded) != 0 {
		t.Errorf("noded node trailers = %+v; want 0 (untouched by docs-only commit)", noded)
	}
}

func TestIndexLoreFakeRunnerHappyPath(t *testing.T) {
	const us = "\x1f"
	const rs = "\x1e"
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertNode(ctx, store.Node{NodeID: "pkg/x.A", Name: "A", Kind: string(store.KindFunction), Language: "go", FilePath: "x.go", ContentHash: "h"})

	out := "deadbeef" + us + "d@e.com" + us + "1700000000" + us +
		"feat(x): a\n\nLore-Agent-Directive: keep it pure\n" + rs + "\nx.go\n"
	idx := NewLoreIndexer(s, fakeRunner{out: out})
	res, err := idx.IndexLore(ctx, "proj", "/repo")
	if err != nil {
		t.Fatalf("IndexLore: %v", err)
	}
	if res.WrittenRows != 1 {
		t.Errorf("WrittenRows = %d; want 1", res.WrittenRows)
	}
	got, _ := s.ListLoreTrailersForNode(ctx, "pkg/x.A")
	if len(got) != 1 || got[0].TrailerKind != string(store.TrailerAgentDirective) {
		t.Errorf("trailers = %+v; want one agent_directive", got)
	}
}
