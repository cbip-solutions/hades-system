#!/usr/bin/env bats
# Plan 15 Phase C task C-14 — DCO pre-commit hook tests.
#
# Verifies .githooks/pre-commit-dco:
#   - exists + executable
#   - BLOCKS commit messages lacking a Signed-off-by: trailer (DCO required)
#   - PASSES commit messages that include Signed-off-by:
#   - SKIPS merge commits (auto-generated; no sign-off expected)
#   - SKIPS fixup!/squash!/amend!/Revert commits (commit-tree-mechanic auto-msgs)
#
# inv-zen-292 (decisión 15-2; 2026-05-24): DCO sign-off enforced on public-bound
# commits via pre-commit hook (client-side) + CI workflow (server-side).
#
# Hook invocation contract: git passes the path to the prepared commit-message
# file as argv[1]; the hook reads that file. Tests simulate that by writing a
# COMMIT_MSG_FILE and invoking the hook with the path.

setup() {
    HOOK_PATH="${BATS_TEST_DIRNAME}/../../.githooks/pre-commit-dco"
    export TEST_REPO="$(mktemp -d)"
    cd "$TEST_REPO" || exit 1
    git init --quiet
    git config user.email "test@example.com"
    git config user.name "Test User"
    # Suppress the dispatcher's "Run: make install-hooks" suggestion noise:
    # each test invokes the dco hook directly with the bash interpreter, never
    # via `git commit`.
    export COMMIT_MSG_FILE="$(mktemp)"
}

teardown() {
    rm -rf "$TEST_REPO"
    rm -f "${COMMIT_MSG_FILE:-}"
}

@test "C-14: hook exists and is executable" {
    [ -x "$HOOK_PATH" ]
}

@test "C-14: hook BLOCKS commit without sign-off" {
    echo "feat: no sign-off here" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -ne 0 ]
    [[ "$output" =~ "Signed-off-by" ]]
}

@test "C-14: hook PASSES commit with sign-off" {
    printf "feat: with sign-off\n\nSigned-off-by: Test User <test@example.com>\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook SKIPS merge commits (no sign-off required)" {
    # `git merge` auto-generates "Merge branch 'foo'" as the first line.
    printf "Merge branch 'feature/x'\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook SKIPS revert commits (no sign-off required)" {
    # `git revert` auto-generates "Revert \"<original subject>\"" as the first line.
    printf "Revert \"feat: original commit\"\n\nThis reverts commit abcdef0.\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook SKIPS fixup! commits (no sign-off required)" {
    printf "fixup! feat: original subject\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook SKIPS squash! commits (no sign-off required)" {
    printf "squash! feat: original subject\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook SKIPS amend! commits (no sign-off required)" {
    printf "amend! feat: original subject\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook PASSES sign-off with co-author identity (presence-only check)" {
    # Per spec: identity-match against committer is NOT enforced in v1.0
    # (Phase H follow-up). The hook only requires Signed-off-by: presence;
    # a different name in the trailer is allowed (DCO covers third-party
    # contributions verified at the PR stage).
    printf "feat: third-party signoff\n\nSigned-off-by: Other Person <other@example.com>\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -eq 0 ]
}

@test "C-14: hook BLOCKS malformed Signed-off-by (missing email)" {
    # A "Signed-off-by:" line without <email@host> form is malformed under DCO.
    # The hook's regex requires the canonical "<name> <email@host>" trailer.
    printf "feat: malformed trailer\n\nSigned-off-by: Some Name\n" > "$COMMIT_MSG_FILE"
    run bash "$HOOK_PATH" "$COMMIT_MSG_FILE"
    [ "$status" -ne 0 ]
}
