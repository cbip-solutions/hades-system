// SPDX-License-Identifier: MIT
// Package compliance — Task B-16 anti-reintroduction sister-test.
//
// Defense-in-depth gate that catches regressions which would re-introduce
// either (a) the 12 migrated invariant test files, or (b) the
// private-tier1-module/ tree, into the PUBLIC-SNAPSHOT perimeter.
//
// CRITICAL semantic correction:
// the plan-text shape (lines 2830-2867) of the literal sister-test asserts
// "private-tier1-module/ tree ABSENT from dev repo". REALITY:
// private-tier1-module/ tree IS STILL PRESENT in this dev repo because
// cmd/zen-swarm-ctld/{orchestrator_wiring.go,bootstrap.go} still import it
// (the public daemon's in-process BypassBackend wiring callsite). The
// public-snapshot perimeter excludes the path via
// docs/public-manifest/allowlist.yml; future cleanup migrates the daemon
// callsites to the sidecar pattern and only then removes the tree from
// the dev repo. This test encodes ACTUAL semantics:
//
// 1. The allowlist manifest declares `private-tier1-module/**` as
// EXCLUDE so the snapshot REJECTS the tree.
//
// 2. The 12 fully-migrated invariant test files are absent from
// tests/compliance/ (W7-B2 surgical-split + W7-B6 split-discipline
// duplicate assertion as defense-in-depth).
//
// 3. POSITIVE assertion: the private-tier1-module/ tree IS present
// in the dev repo source — fails if
// a future cleanup removes the tree WITHOUT first migrating the
// daemon-wiring callsites.
//
// Cross-phase: scripts/build_public_snapshot.sh applies the same
// allowlist exclude patterns at sync-time (see scripts/build_public_snapshot.sh
// lines 232-251 "Applying exclude patterns"); this compile-time test
// is the source-level structural verifier..github/workflows/
// anti-bypass-reintroduction-on-pr.yml is the per-PR diff-time verifier.
// Three-place coverage: allowlist + this test + per-PR workflow.
//
// inv-zen-NNN placeholder; concrete ID allocated at merge-time
// reconciliation per the renumber-on-merge playbook.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootB1(t *testing.T) string {
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
			t.Fatal("could not find go.mod root walking up from cwd")
		}
		root = parent
	}
}

func TestInvZenB1_PublicManifestExcludesBypassTree(t *testing.T) {
	root := repoRootB1(t)
	manifestPath := filepath.Join(root, "docs", "public-manifest", "allowlist.yml")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("public-snapshot allowlist missing at %s (required for snapshot boundary; Phase C-13): %v", manifestPath, err)
	}
	manifest := string(body)

	excludeIdx := strings.Index(manifest, "\nexclude:")
	if excludeIdx == -1 {
		t.Fatalf("allowlist.yml missing top-level `exclude:` section (Phase C-13 manifest structure broken)")
	}
	requiredIdx := strings.Index(manifest[excludeIdx:], "\nrequired_present:")
	if requiredIdx == -1 {

		requiredIdx = len(manifest) - excludeIdx
	}
	excludeSpan := manifest[excludeIdx : excludeIdx+requiredIdx]

	requiredExcludePattern := "private-tier1-module/**"
	if !strings.Contains(excludeSpan, requiredExcludePattern) {
		t.Errorf("public-snapshot allowlist EXCLUDE block missing required pattern %q (Stage-0 correction #4 + decisión 17-a)",
			requiredExcludePattern)
		t.Logf("Searched span (first 200 chars): %.200q", excludeSpan)
	}
}

func TestInvZenB1_NoMigratedInvariantTests(t *testing.T) {
	root := repoRootB1(t)
	compRoot := filepath.Join(root, "tests", "compliance")

	fullyMigrated := []string{"053", "054", "055", "058", "059", "060", "071", "243", "247"}
	for _, inv := range fullyMigrated {
		matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_"+inv+"_*.go"))
		if len(matches) != 0 {
			t.Errorf("inv-zen-%s test file present at %v in public-snapshot perimeter; must be ABSENT post W7-B2 (decisión 17-a EXTENDED). Bypass-internal half lives in cbip-solutions/zen-bypass-tier1/tests/compliance/",
				inv, matches)
		}
	}

	devRetained := map[string]string{
		"242": "inv_zen_242_244_dev_repo_fingerprint",
		"246": "inv_zen_246_bootstrap_wires_munger",
	}
	for inv, allowedPrefix := range devRetained {
		matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_"+inv+"_*.go"))
		if len(matches) == 0 {
			t.Errorf("inv-zen-%s dev-repo fingerprint file MISSING (expected prefix %s_*); W7-B2 commit 592bebaa specified the dev-side scope-reduction residual; a removal here would silently lose the dev-side coverage",
				inv, allowedPrefix)
			continue
		}
		for _, m := range matches {
			base := filepath.Base(m)
			if !strings.HasPrefix(base, allowedPrefix) {
				t.Errorf("inv-zen-%s residual file %s does not match expected W7-B2 prefix %s_*; unexpected file (possible regression re-introducing bypass-internal scope)",
					inv, base, allowedPrefix)
			}
		}
	}

	inv244Matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_244_*.go"))
	for _, m := range inv244Matches {
		base := filepath.Base(m)

		t.Errorf("inv-zen-244 standalone file %s present; W7-B2 surgical split requires ONLY transitive coverage via inv_zen_242_244_dev_repo_fingerprint",
			base)
	}
}

// TestInvZenB1_DevRepoAnthropicBypassRetainedForDaemonWiring is the
// POSITIVE assertion encoding correction #4. The dev repo
// MUST retain private-tier1-module/ until a future cleanup task
// migrates the in-process BypassBackend daemon-wiring callsites to
// the sidecar pattern (SidecarBackend + sidecars.toml).
//
// Why a positive assertion? A future PR that runs `git rm -rf
// private-tier1-module/` would silently break the dev daemon
// build (cmd/zen-swarm-ctld would no longer compile) — but that
// build break is a noisy gate. The subtle failure mode this test
// catches: a PR that REMOVES the tree AND adds a no-op stub elsewhere,
// breaking the COPY-not-MOVE invariant without a compile failure.
// The POSITIVE assertion makes that scenario explicit: "the tree is
// load-bearing for daemon wiring; do not remove without coordinating
// the callsite migration".
//
// When the future cleanup lands (sidecar pattern fully replaces the
// in-process BypassBackend in the daemon), this test should be
// removed in the same commit that drops the tree — the cleanup
// commit will document the new ground truth in the spec.
func TestInvZenB1_DevRepoAnthropicBypassRetainedForDaemonWiring(t *testing.T) {
	root := repoRootB1(t)
	treePath := filepath.Join(root, "internal", "anthropic-bypass")
	info, err := os.Stat(treePath)
	if err != nil {
		t.Fatalf("private-tier1-module/ tree MISSING from dev repo (Stage-0 correction #4 violated; the in-process BypassBackend daemon wiring callsites in cmd/zen-swarm-ctld/{orchestrator_wiring.go,bootstrap.go} require this tree until a future cleanup migrates them to the sidecar pattern): %v",
			err)
	}
	if !info.IsDir() {
		t.Errorf("private-tier1-module exists at %s but is not a directory; expected the package tree", treePath)
	}

	matches, _ := filepath.Glob(filepath.Join(treePath, "*.go"))
	if len(matches) == 0 {
		t.Errorf("private-tier1-module/ contains zero .go files at %s; the daemon-wiring callsites require importable package contents (Stage-0 COPY-not-MOVE semantics)",
			treePath)
	}
}
