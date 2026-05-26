#!/usr/bin/env bats
# Plan 15 Phase A task A-12 — CHANGELOG completeness gate tests.
# inv-zen-276: every git v* tag must have CHANGELOG entry OR allowlist row.
#
# Tests run inside an ephemeral repo fixture (mktemp) so they exercise the
# script against a controlled set of tags + CHANGELOG entries + allowlist
# rows, independent of the live repo state.

setup() {
    SCRIPT="${BATS_TEST_DIRNAME}/../../scripts/verify_changelog_completeness.sh"
    export TEST_REPO
    TEST_REPO="$(mktemp -d)"
    cd "$TEST_REPO" || exit 1
    git init --quiet
    git config user.email "t@e.com" && git config user.name "T"
    cat > CHANGELOG.md <<'EOF'
# Changelog

## [v0.1.0] - 2026-04-29
- Initial release.

## [v0.17.12] - 2026-05-24
- Various fixes.
EOF
    mkdir -p configs
    cat > configs/changelog-omission-allowlist.yaml <<'EOF'
omissions:
  - tag: v0.10.0
    rationale: "intermediate infra release (Plan 10 MLX); narrative consolidates at v1.0"
  - tag: v0.16.0
    rationale: "intermediate infra release (Plan 16 cascade); narrative consolidates at v1.0"
EOF
}

teardown() {
    rm -rf "$TEST_REPO"
}

@test "A-12: script exists + executable" {
    [ -x "$SCRIPT" ]
}

@test "A-12: passes when all tags have CHANGELOG entry or allowlist row" {
    git add . && git commit -m "init" --quiet
    git tag v0.1.0 && git tag v0.10.0 && git tag v0.16.0 && git tag v0.17.12
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
}

@test "A-12: fails when a tag has no CHANGELOG + no allowlist" {
    git add . && git commit -m "init" --quiet
    git tag v0.17.99
    run bash "$SCRIPT"
    [ "$status" -ne 0 ]
    [[ "$output" =~ "v0.17.99" ]]
}

@test "A-12: rejects allowlist entry >= v1.0.0 (flip-aware)" {
    cat >> configs/changelog-omission-allowlist.yaml <<'EOF'
  - tag: v1.0.5
    rationale: "should be rejected post-v1.0 flip"
EOF
    git add . && git commit -m "init" --quiet
    git tag v1.0.5
    run bash "$SCRIPT"
    [ "$status" -ne 0 ]
    [[ "$output" =~ "flip-aware" ]] || [[ "$output" =~ ">= v1.0" ]] || [[ "$output" =~ ">=v1.0" ]]
}

@test "A-12: emit summary banner on success" {
    git add . && git commit -m "init" --quiet
    git tag v0.1.0 && git tag v0.17.12
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [[ "$output" =~ "CHANGELOG completeness" ]]
}

@test "A-12: vacuous pass when no v* tags exist" {
    git add . && git commit -m "init" --quiet
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [[ "$output" =~ "no v\* tags" ]] || [[ "$output" =~ "vacuously" ]]
}

@test "A-12: fails with exit 3 when CHANGELOG.md missing" {
    rm CHANGELOG.md
    git add . && git commit -m "init" --quiet
    git tag v0.1.0
    run bash "$SCRIPT"
    [ "$status" -eq 3 ]
}

@test "A-12: allowlist with empty rationale fails (load-bearing rationale)" {
    cat > configs/changelog-omission-allowlist.yaml <<'EOF'
omissions:
  - tag: v0.5.0
    rationale: ""
EOF
    git add . && git commit -m "init" --quiet
    git tag v0.5.0
    run bash "$SCRIPT"
    [ "$status" -ne 0 ]
    [[ "$output" =~ "rationale" ]]
}

@test "A-12: tolerates absent allowlist when every tag has CHANGELOG entry" {
    rm configs/changelog-omission-allowlist.yaml
    git add . && git commit -m "init" --quiet
    git tag v0.1.0 && git tag v0.17.12
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
}
