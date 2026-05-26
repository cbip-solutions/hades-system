package compliance

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

func TestInvZen129_NoNetHTTPImportInKnowledgePackage(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "knowledge")

	cmd := exec.Command("grep", "-rnE", `"net/http"`, target)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {

		if exitErr, ok := err.(*exec.ExitError); ok {

			if exitErr.ExitCode() == 1 {
				return
			}

			t.Logf("inv-zen-129: grep exited with code %d (empty output): %v",
				exitErr.ExitCode(), err)
			return
		}

		t.Skipf("inv-zen-129: grep launch failed: %v", err)
	}
	if len(out) > 0 {
		t.Errorf("inv-zen-129 violation: net/http import found in internal/knowledge/:\n%s",
			strings.TrimSpace(string(out)))
	}
}

func TestInvZen129_KnowledgePackageHasNoTransitiveNetHTTP(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain unavailable: %v", err)
	}

	const knowledgePkgPath = "github.com/cbip-solutions/hades-system/internal/knowledge"
	cmd := exec.Command("go", "list", "-deps", knowledgePkgPath)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\noutput: %s",
			knowledgePkgPath, err, string(out))
	}

	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if dep == "net/http" || strings.HasPrefix(dep, "net/http/") {
			t.Errorf("inv-zen-129 violated: %s has transitive dependency on forbidden package %q",
				knowledgePkgPath, dep)
		}
	}
}

// TestInvZen129_RemoteFlagReturnsErrRemoteNotShipped is the runtime
// witness. Constructs a fresh in-memory-style knowledge index (file-
// backed in a t.TempDir, since the package's Open requires a non-empty
// path), and calls Execute with Remote=true. The returned error MUST
// satisfy two predicates:
//
//	(i)  errors.Is(err, knowledge.ErrRemoteNotShipped) — the typed
//	     sentinel is preserved across the wrapping chain, so callers
//	     can `errors.Is`-check at any layer (CLI, daemon HTTP, tests).
//	(ii) The message contains "not yet shipped" AND "Plan 14" — the
//	     deferred-message wording is operator-facing UX, surfaced by
//	     the CLI G-12 layer. A refactor that drops "Plan 14" from the
//	     sentinel would break the operator's roadmap-pointer
//	     expectation; a refactor that drops "not yet shipped" would
//	     mute the deferred-message clarity.
//
// The wording assertion is intentionally permissive (substring match)
// so a future spec revision can extend the sentinel (e.g., "shipping
// in 2027 alongside Plan 14 Phase G") without breaking this test, as
// long as the two anchor phrases remain.
func TestInvZen129_RemoteFlagReturnsErrRemoteNotShipped(t *testing.T) {
	db := openKnowledgeIndexForTest(t)
	defer db.Close()

	_, err := knowledge.Execute(context.Background(), db, knowledge.Query{Remote: true})
	if err == nil {
		t.Fatal("inv-zen-129: Execute(Remote=true) returned nil, want ErrRemoteNotShipped")
	}
	if !errors.Is(err, knowledge.ErrRemoteNotShipped) {
		t.Errorf("inv-zen-129: errors.Is(err, ErrRemoteNotShipped) = false; got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "not yet shipped") {
		t.Errorf("inv-zen-129: message drift — want substring %q in %q",
			"not yet shipped", msg)
	}
	if !strings.Contains(msg, "Plan 14") {
		t.Errorf("inv-zen-129: message drift — want substring %q in %q",
			"Plan 14", msg)
	}
}

func TestInvZen129_NoRemoteSentinelReachable(t *testing.T) {
	if err := knowledge.NoRemoteSentinel(); err != nil {
		t.Errorf("inv-zen-129: NoRemoteSentinel() = %v, want nil", err)
	}
}

func openKnowledgeIndexForTest(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	db, err := knowledge.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("inv-zen-129: knowledge.Open: %v", err)
	}
	if err := knowledge.Init(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("inv-zen-129: knowledge.Init: %v", err)
	}
	return db
}
