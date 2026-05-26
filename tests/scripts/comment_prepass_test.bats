#!/usr/bin/env bats

# Plan 15 Phase K Task K-1 — comment_prepass.sh deterministic regex pre-pass tests.
#
# Per decisión 12: deletes 5 categories of clearly-rotten patterns from Go +
# Python source comments. Dry-run mode default; --apply destructive.
#
# Each test seeds a throwaway temp git repo with a known mix of rot + load-bearing
# comments, invokes the script (which is invoked with its absolute path so it
# operates on the temp repo's cwd, not the real worktree), then asserts the
# observed deltas.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    TMP_REPO="$(mktemp -d)"
    cd "${TMP_REPO}"
    git init -q
    git config user.email "test@example.invalid"
    git config user.name "test"
    mkdir -p internal/foo plugin/bar
    cat > internal/foo/rot.go <<'EOG'
package foo

// Plan 5 dispatcher boundary check
// v0.17.8 fix for refresh-token rotation
// TODO fix this later
// per spec docs/superpowers/specs/2026-04-29-zen-swarm-design.md §3
// added by claude session abc123
//
// LegitFunction performs the actual work.
// MUST hold mu before reading; race seen in Plan 9 chaos run.
// inv-zen-031: bypass MUST NOT import internal/store directly.
func LegitFunction() {}
EOG
    cat > plugin/bar/rot.py <<'EOP'
# Plan 7 helper script
# TODO add error handling
# inv-zen-338: comment hygiene gate
def helper(): pass
EOP
    git add -A
    git commit -q -m "init"
}

teardown() {
    rm -rf "${TMP_REPO}"
}

@test "comment_prepass.sh script exists + executable" {
    [ -x "${PROJECT_ROOT}/scripts/comment_prepass.sh" ]
}

@test "comment_prepass.sh --help prints usage" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --help
    [ "$status" -eq 0 ]
    [[ "$output" == *"Usage:"* ]]
    [[ "$output" == *"--dry-run"* ]]
    [[ "$output" == *"--apply"* ]]
}

@test "comment_prepass.sh --dry-run reports planned deletions without modifying files" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --dry-run
    [ "$status" -eq 0 ]
    [[ "$output" == *"Plan 5 dispatcher"* ]]
    [[ "$output" == *"v0.17.8 fix"* ]]
    [[ "$output" == *"TODO fix this later"* ]]
    [[ "$output" == *"added by claude"* ]]
    [[ "$output" == *"per spec docs/superpowers"* ]]
    # Files unchanged
    run grep "Plan 5 dispatcher" internal/foo/rot.go
    [ "$status" -eq 0 ]
    run grep "v0.17.8" internal/foo/rot.go
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh default (no arg) is dry-run" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh"
    [ "$status" -eq 0 ]
    # Default == dry-run; files unchanged
    run grep "Plan 5 dispatcher" internal/foo/rot.go
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh --apply removes rot patterns + preserves load-bearing" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    [ "$status" -eq 0 ]
    # Rot deleted
    run grep "Plan 5 dispatcher" internal/foo/rot.go
    [ "$status" -ne 0 ]
    run grep "v0.17.8" internal/foo/rot.go
    [ "$status" -ne 0 ]
    run grep "added by claude" internal/foo/rot.go
    [ "$status" -ne 0 ]
    run grep "TODO fix this later" internal/foo/rot.go
    [ "$status" -ne 0 ]
    run grep "per spec docs/superpowers" internal/foo/rot.go
    [ "$status" -ne 0 ]
    # Load-bearing preserved
    run grep "MUST hold mu before reading" internal/foo/rot.go
    [ "$status" -eq 0 ]
    run grep "inv-zen-031" internal/foo/rot.go
    [ "$status" -eq 0 ]
    run grep "LegitFunction performs" internal/foo/rot.go
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh preserves TODO with owner-date" {
    cat >> internal/foo/rot.go <<'EOG'

// TODO(testuser 2026-06-01): refactor after Plan 21 ships
func WithOwner() {}
EOG
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    [ "$status" -eq 0 ]
    run grep "TODO(testuser 2026-06-01)" internal/foo/rot.go
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh handles both .go and .py" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    [ "$status" -eq 0 ]
    run grep "Plan 7 helper script" plugin/bar/rot.py
    [ "$status" -ne 0 ]
    run grep "TODO add error handling" plugin/bar/rot.py
    [ "$status" -ne 0 ]
    # inv-zen-338 preserved (load-bearing invariant ref)
    run grep "inv-zen-338" plugin/bar/rot.py
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh idempotent: second --apply produces zero diff" {
    bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    git add -A
    git commit -q -m "first prepass" || true
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    [ "$status" -eq 0 ]
    run git diff --quiet
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh rejects unknown flag" {
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --bogus
    [ "$status" -ne 0 ]
}

@test "comment_prepass.sh excludes ./vendor and */testdata/ paths" {
    mkdir -p vendor/x internal/y/testdata
    cat > vendor/x/keep.go <<'EOV'
package x

// Plan 99 vendor noise — SHOULD NOT be touched (vendor excluded).
func V() {}
EOV
    cat > internal/y/testdata/keep.go <<'EOT'
package testdata

// Plan 99 testdata fixture — SHOULD NOT be touched (testdata excluded).
func T() {}
EOT
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply
    [ "$status" -eq 0 ]
    run grep "Plan 99 vendor noise" vendor/x/keep.go
    [ "$status" -eq 0 ]
    run grep "Plan 99 testdata fixture" internal/y/testdata/keep.go
    [ "$status" -eq 0 ]
}

@test "comment_prepass.sh --target-dir limits scope (dispatcher spec)" {
    mkdir -p other/pkg
    cat > other/pkg/rot.go <<'EOO'
package pkg

// Plan 1 outside scope — SHOULD remain when --target-dir=internal/foo.
func O() {}
EOO
    run bash "${PROJECT_ROOT}/scripts/comment_prepass.sh" --apply --target-dir internal/foo
    [ "$status" -eq 0 ]
    # In-scope deleted
    run grep "Plan 5 dispatcher" internal/foo/rot.go
    [ "$status" -ne 0 ]
    # Out-of-scope preserved
    run grep "Plan 1 outside scope" other/pkg/rot.go
    [ "$status" -eq 0 ]
}
