#!/usr/bin/env bats

# Plan 15 Phase J Task J-9 — verify_no_personal_references.sh tests
#
# Coverage: positive (clean tree exits 0), negative (planted leak exits 1)
# per pattern class, allowlist exemption suppression, flag parsing.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "verify_no_personal_references.sh exists and is executable" {
    [ -x "scripts/verify_no_personal_references.sh" ]
}

@test "verify_no_personal_references_backend.py exists" {
    [ -f "scripts/verify_no_personal_references_backend.py" ]
}

@test "allowlist YAML exists" {
    [ -f "configs/personal-references-allowlist.yaml" ]
}

@test "scanner --help exits 0 and prints usage" {
    run bash scripts/verify_no_personal_references.sh --help
    [ "$status" -eq 0 ]
    [[ "$output" == *"Phase J denylists"* ]]
}

@test "scanner rejects unknown flag" {
    run bash scripts/verify_no_personal_references.sh --bogus
    [ "$status" -eq 2 ]
}

@test "scanner exits 0 on clean tree (post J-1..J-8)" {
    run bash scripts/verify_no_personal_references.sh
    [ "$status" -eq 0 ]
    [[ "$output" == *"Phase J scan clean"* ]]
}

@test "scanner exits non-zero on planted /Users/operator/ leak" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'const Path = "/Users/operator/projects/zen-swarm"' > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "scanner exits non-zero on planted OAuth account_uuid literal" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'const AccountUUID = "00000000-0000-0000-0000-000000000000"' > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "scanner exits non-zero on planted Tailscale CGNAT IP" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'const HostIP = "192.0.2.10"' > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "scanner exits non-zero on planted operator username (bare word)" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'var operator = "operator"' > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "scanner does NOT flag cbip-solutions (J-7 cascade exemption via inline lookahead)" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'const repo = "github.com/cbip-solutions/hades-system"' > "$TMP_LEAK_DIR/legit.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 0 ]
}

@test "scanner ignores generated virtualenv dependencies" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    mkdir -p "$TMP_LEAK_DIR/.venv/lib/python3.14/site-packages/example"
    echo 'def visit_func_def(self, fdef): pass' > "$TMP_LEAK_DIR/.venv/lib/python3.14/site-packages/example/generated.py"
    echo 'const clean = "public"' > "$TMP_LEAK_DIR/legit.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 0 ]
}

@test "scanner exits non-zero on planted operator personal-name" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo "// hello operator, this is a test" > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "scanner exits non-zero on planted operator TitleCase" {
    TMP_LEAK_DIR=$(mktemp -d -t verify-leak.XXXXXX)
    echo 'const greeting = "Hello operator!"' > "$TMP_LEAK_DIR/leak.go"
    run bash scripts/verify_no_personal_references.sh --target-dir "$TMP_LEAK_DIR"
    rm -rf "$TMP_LEAK_DIR"
    [ "$status" -eq 1 ]
}

@test "verify-no-personal-references make target invokes script" {
    run make verify-no-personal-references
    [ "$status" -eq 0 ]
    [[ "$output" == *"Phase J scan clean"* ]]
}

@test "allowlist contains LEGIT-PROVENANCE entries for ADR corpus" {
    run grep -q "argus-foundational-decisions-originals" configs/personal-references-allowlist.yaml
    [ "$status" -eq 0 ]
}

@test "allowlist contains Copyright Ika el Zur entry (decisión 15-1)" {
    run grep -q "copyright-cbip-solutions" configs/personal-references-allowlist.yaml
    [ "$status" -eq 0 ]
}

@test "allowlist contains cbip-solutions cascade entry (J-7)" {
    run grep -q "cbip-solutions-repo-url-pre-flip" configs/personal-references-allowlist.yaml
    [ "$status" -eq 0 ]
}
