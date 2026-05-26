// SPDX-License-Identifier: MIT
// Package compliance — Phase B Task B-10 `/v1` HTTP API contract
// FREEZE invariant test.
//
// Per decisión 17-d, the daemon ↔ sidecar HTTP contract over `/v1`
// is FROZEN FOREVER post-Phase-B-3 ship. The frozen sidecar-contract
// surface is the FIVE routes consumed across the loopback HTTP
// channel between the dev daemon (this repo) and the private
// zen-bypass-tier1 sidecar binary (cbip-solutions/zen-bypass-tier1):
//
//	POST /v1/messages                 (sidecar serves; B-3 sidecar binary)
//	GET  /health                      (sidecar serves; B-3 sidecar binary)
//	GET  /v1/sidecar/info             (sidecar serves; B-3 sidecar binary)
//	POST /v1/notifications/post       (daemon serves;  B-8 dev daemon)
//	POST /v1/bypass/update-config     (daemon serves;  pre-existing Plan 2)
//
// The freeze applies BOTH directions across the sidecar boundary —
// the daemon must NEVER mount a `/v2` route for these capabilities,
// and the sidecar must NEVER expose a `/v2` route either. Forward
// compatibility is achieved exclusively via the capability negotiation
// mechanism (`GET /v1/sidecar/info` capability vector; see
// `tests/integration/sidecar_capability_negotiation_test.go`).
//
// Test scope (inv-zen-B2 placeholder; concrete inv-zen-NNN allocated at
// merge-time renumber reconciliation per the renumber-on-merge playbook):
//
//  1. `TestInvZenB10_NoV2RoutesInDevRepo` — walks every `.go` file
//     (excluding `vendor/`) and asserts NO string literal of the form
//     `"/v2..."` appears in a `mux.Handle*` / `http.HandleFunc` /
//     `http.Handle` call. This is the dev-repo-side per-PR diff gate
//     that protects the freeze. The private sidecar repo carries a
//     mirrored test (private B-10 step 6 GitHub Actions workflow).
//
//  2. `TestInvZenB10_CanonicalDaemonSidecarContractRoutesPresent` —
//     asserts the TWO daemon-side routes of the canonical sidecar
//     contract (`POST /v1/notifications/post` for the sidecar→daemon
//     direction and `POST /v1/bypass/update-config` for the legacy
//     in-process bypass admin endpoint) ARE mounted in
//     `internal/daemon/server.go`. The other three routes (`POST
//     /v1/messages`, `GET /health`, `GET /v1/sidecar/info`) live in
//     the private sidecar binary; the dev-repo cannot assert their
//     presence directly, but the documented contract is encoded here
//     as canonical reference.
//
//  3. `TestInvZenB10_NoV2StringLiteralsInDevRepo` — defence-in-depth
//     scan over every `.go` file for the substring `"/v2` in any
//     string literal (not just route mounts). Catches future
//     `client.Do(http.NewRequest("GET", baseURL+"/v2/..."))` calls
//     or response-format references that would be a leading indicator
//     of contract drift even before a mux registration lands.
//
// inv-zen-031 boundary: this file imports go/ast / go/parser / go/token
// (stdlib AST tooling) + os / path/filepath / strings / testing. It
// does NOT import any internal/ package.
//
// Implementation note: tests use a textual scan (not full AST walk
// of CallExpr selectors) for the `mux.Handle*` matcher because
// (1) the dev repo's route mount calls are uniformly formatted —
// `mux.HandleFunc("METHOD /path", handler)` or
// `mux.Handle("METHOD /path", handler)` — and (2) a textual scan
// catches near-miss patterns (e.g. via wrapper helpers) that a strict
// AST walk on `mux.Handle*` would miss. The AST is used for the
// supplementary `TestInvZenB10_NoV2StringLiteralsInDevRepo` scan
// because that one looks at every string literal regardless of
// surrounding call context.
package compliance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRootB10(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not find go.mod root")
		}
		root = parent
	}
}

var muxHandleV2Pattern = regexp.MustCompile(
	`(?m)\b(?:mux\.Handle(?:Func)?|http\.Handle(?:Func)?)\s*\(\s*"(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|TRACE|CONNECT)?\s*/v2[^"]*"`,
)

var v2StringLiteralPattern = regexp.MustCompile(`"/v2[^"]*"`)

var pathSkipsForFreeze = []string{
	"vendor",
	"node_modules",
	filepath.Join("tests", "testdata"),
	"docs",
	"plan",
	"openspec",
	"bin",
	"build",
	".git",
}

func shouldSkipForFreeze(rel string) bool {
	for _, prefix := range pathSkipsForFreeze {
		if rel == prefix || strings.HasPrefix(rel, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func TestInvZenB10_NoV2RoutesInDevRepo(t *testing.T) {
	root := repoRootB10(t)
	var hits []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			if shouldSkipForFreeze(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "inv_zen_B10_v1_freeze_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(body), "\n") {
			if muxHandleV2Pattern.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s: %s", rel, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(hits) > 0 {
		t.Errorf("found %d /v2 route mount(s); /v1 frozen forever per decisión 17-d. Hits:\n  %s",
			len(hits), strings.Join(hits, "\n  "))
	}
}

func TestInvZenB10_NoV2StringLiteralsInDevRepo(t *testing.T) {
	root := repoRootB10(t)
	var hits []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			if shouldSkipForFreeze(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "inv_zen_B10_v1_freeze_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {

			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if v2StringLiteralPattern.MatchString(lit.Value) {
				pos := fset.Position(lit.Pos())
				hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, pos.Line, lit.Value))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(hits) > 0 {
		t.Errorf("found %d /v2 string literal(s) in route-bearing surfaces; /v1 frozen forever per decisión 17-d. Hits:\n  %s",
			len(hits), strings.Join(hits, "\n  "))
	}
}

func TestInvZenB10_CanonicalDaemonSidecarContractRoutesPresent(t *testing.T) {
	root := repoRootB10(t)
	serverPath := filepath.Join(root, "internal", "daemon", "server.go")
	body, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read internal/daemon/server.go: %v", err)
	}
	src := string(body)

	requiredMounts := []struct {
		desc    string
		pattern string
	}{
		{
			desc:    "POST /v1/notifications/post (sidecar→daemon consumer per inv-zen-282)",
			pattern: `"POST /v1/notifications/post"`,
		},
		{
			desc:    "POST /v1/bypass/update-config (legacy in-process bypass admin)",
			pattern: `"POST /v1/bypass/update-config"`,
		},
	}
	for _, mount := range requiredMounts {
		if !strings.Contains(src, mount.pattern) {
			t.Errorf("internal/daemon/server.go: missing mount for %s (pattern %q)", mount.desc, mount.pattern)
		}
	}
}

func TestInvZenB10_NoV2InGithubWorkflows(t *testing.T) {
	root := repoRootB10(t)
	workflowsDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {

		if os.IsNotExist(err) {
			t.Skip(".github/workflows directory absent; skipping workflow scan")
		}
		t.Fatalf("read .github/workflows: %v", err)
	}
	var hits []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if !strings.HasSuffix(ent.Name(), ".yml") && !strings.HasSuffix(ent.Name(), ".yaml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(workflowsDir, ent.Name()))
		if err != nil {
			t.Fatalf("read workflow %s: %v", ent.Name(), err)
		}

		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)

			if strings.Contains(trimmed, "uses:") || strings.Contains(trimmed, "@v") {
				continue
			}
			if strings.Contains(trimmed, "/v2/") || strings.Contains(trimmed, "/v2\"") || strings.HasSuffix(trimmed, "/v2") {
				hits = append(hits, fmt.Sprintf("%s: %s", ent.Name(), trimmed))
			}
		}
	}
	if len(hits) > 0 {
		t.Errorf("found %d /v2 reference(s) in .github/workflows; /v1 frozen forever per decisión 17-d. Hits:\n  %s",
			len(hits), strings.Join(hits, "\n  "))
	}
}
