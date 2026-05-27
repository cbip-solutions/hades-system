package compliance

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type osGitProber struct{ repoRoot string }

func (p osGitProber) LastTouchedUnix(ctx context.Context, repoRel string) (int64, bool) {
	cmd := exec.CommandContext(ctx, "git", "-C", p.repoRoot, "log", "-1", "--format=%ct", "--", repoRel)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return 0, false
	}
	var ts int64
	if _, err := fmtSscan(string(out), &ts); err != nil {
		return 0, false
	}
	return ts, true
}

func fmtSscan(s string, out *int64) (int, error) {
	var n int64
	read := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int64(r-'0')
		read++
	}
	if read == 0 {
		return 0, os.ErrInvalid
	}
	*out = n
	return read, nil
}

func gitRunStale(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitRunStaleAt(t, dir, 0, args...)
}

func gitRunStaleAt(t *testing.T, dir string, ts int64, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if ts != 0 {
		date := formatUnixDate(ts)
		env = append(env,
			"GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_DATE="+date,
		)
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func formatUnixDate(ts int64) string {

	return "@" + int64ToString(ts) + " +0000"
}

func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func openCaronteStoreAt(t *testing.T, dir string) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(dir, "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

func initStalenessRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	gitRunStale(t, dir, "init", "-q")
	gitRunStale(t, dir, "config", "user.email", "t@example.com")
	gitRunStale(t, dir, "config", "user.name", "t")
	gitRunStale(t, dir, "config", "commit.gpgsign", "false")
	gitRunStale(t, dir, "config", "gc.auto", "0")
	gitRunStale(t, dir, "config", "maintenance.auto", "false")
	gitRunStale(t, dir, "config", "core.fsmonitor", "false")
}

func TestInvZen237ADRStalenessFlipsOnCodeChange(t *testing.T) {
	const adrCommitTS int64 = 1000
	const codeCommitTS int64 = 2000

	repo := t.TempDir()
	initStalenessRepo(t, repo)

	adrDir := filepath.Join(repo, "docs", "decisions")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adrRel := filepath.ToSlash(filepath.Join("docs", "decisions", "0100-caronte.md"))
	if err := os.WriteFile(filepath.Join(repo, adrRel), []byte("---\nid: ADR-0111\ntitle: Caronte\n---\n# decision\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunStale(t, repo, "add", ".")
	gitRunStaleAt(t, repo, adrCommitTS, "commit", "-q", "-m", "docs(adr): add ADR-0111")

	codeRel := filepath.ToSlash(filepath.Join("internal", "caronte", "intent", "getwhy.go"))
	codeDir := filepath.Dir(filepath.Join(repo, codeRel))
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, codeRel), []byte("package intent\n// changed after the ADR\nfunc GetWhy() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunStale(t, repo, "add", ".")
	gitRunStaleAt(t, repo, codeCommitTS, "commit", "-q", "-m", "feat(caronte-intent): change getwhy after the adr")

	s := openCaronteStoreAt(t, repo)
	ctx := context.Background()
	node := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: codeRel, PackageID: "internal/caronte/intent", ContentHash: "current",
	}
	if err := s.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: adrRel, NodeID: node.NodeID, PackageID: node.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatalf("UpsertADRLink: %v", err)
	}

	checker := intent.NewStalenessChecker(s, repo, osGitProber{repoRoot: repo})
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}

	// invariant: the link MUST now be stale (code committed after the ADR).
	links, err := s.ListADRLinksForNode(ctx, node.NodeID)
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("got %d links; want 1", len(links))
	}
	if !links[0].Stale {
		t.Errorf("inv-zen-237 violated: ADR link not flagged stale though the linked code was committed after the ADR")
	}
}

func TestInvZen237FreshWhenADRNewerThanCode(t *testing.T) {
	const codeCommitTS int64 = 1000
	const adrCommitTS int64 = 2000

	repo := t.TempDir()
	initStalenessRepo(t, repo)

	codeRel := filepath.ToSlash(filepath.Join("internal", "caronte", "intent", "getwhy.go"))
	codeDir := filepath.Dir(filepath.Join(repo, codeRel))
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, codeRel), []byte("package intent\nfunc GetWhy() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunStale(t, repo, "add", ".")
	gitRunStaleAt(t, repo, codeCommitTS, "commit", "-q", "-m", "feat(caronte-intent): initial getwhy")

	adrDir := filepath.Join(repo, "docs", "decisions")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adrRel := filepath.ToSlash(filepath.Join("docs", "decisions", "0100-caronte.md"))
	if err := os.WriteFile(filepath.Join(repo, adrRel), []byte("---\nid: ADR-0111\ntitle: Caronte\n---\n# decision\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunStale(t, repo, "add", ".")
	gitRunStaleAt(t, repo, adrCommitTS, "commit", "-q", "-m", "docs(adr): add ADR-0111 after the code")

	s := openCaronteStoreAt(t, repo)
	ctx := context.Background()
	node := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: codeRel, PackageID: "internal/caronte/intent", ContentHash: "v1",
	}
	if err := s.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: adrRel, NodeID: node.NodeID, PackageID: node.PackageID,
		LinkKind: string(store.LinkExplicitRef), Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatalf("UpsertADRLink: %v", err)
	}

	checker := intent.NewStalenessChecker(s, repo, osGitProber{repoRoot: repo})
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}

	links, err := s.ListADRLinksForNode(ctx, node.NodeID)
	if err != nil {
		t.Fatalf("ListADRLinksForNode: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("got %d links; want 1", len(links))
	}

	if links[0].Stale {
		t.Errorf("inv-zen-237 violated: ADR link incorrectly flagged stale when ADR was committed after the code")
	}
}

func TestInvZen237CoverageManifestNeverStaled(t *testing.T) {
	repo := t.TempDir()
	initStalenessRepo(t, repo)

	codeRel := filepath.ToSlash(filepath.Join("internal", "caronte", "intent", "getwhy.go"))
	codeDir := filepath.Dir(filepath.Join(repo, codeRel))
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, codeRel), []byte("package intent\nfunc GetWhy() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunStale(t, repo, "add", ".")
	gitRunStaleAt(t, repo, 1000, "commit", "-q", "-m", "feat(caronte-intent): getwhy")

	adrRel := filepath.ToSlash(filepath.Join("docs", "decisions", "0100-caronte.md"))

	s := openCaronteStoreAt(t, repo)
	ctx := context.Background()
	node := store.Node{
		NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction),
		Language: "go", FilePath: codeRel, PackageID: "internal/caronte/intent", ContentHash: "v1",
	}
	if err := s.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	if err := s.UpsertADRLink(ctx, store.ADRLink{
		ADRID: adrRel, NodeID: "", PackageID: node.PackageID,
		LinkKind: string(store.LinkCoverageManifest), Confidence: 1.0, Stale: false,
	}); err != nil {
		t.Fatalf("UpsertADRLink (coverage_manifest): %v", err)
	}

	checker := intent.NewStalenessChecker(s, repo, osGitProber{repoRoot: repo})
	if err := checker.Recompute(ctx); err != nil {
		t.Fatalf("Recompute: %v", err)
	}

	rows, err := s.DB().QueryContext(ctx,
		`SELECT stale FROM adr_links WHERE link_kind = ?`,
		string(store.LinkCoverageManifest),
	)
	if err != nil {
		t.Fatalf("query coverage_manifest links: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var stale bool
		if err := rows.Scan(&stale); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if stale {
			t.Errorf("inv-zen-237 violated: coverage_manifest link was auto-staled; it must never be")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if !found {
		t.Fatal("sentinel: no coverage_manifest link found in DB; test setup failed")
	}
}
