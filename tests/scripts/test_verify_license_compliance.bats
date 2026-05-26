#!/usr/bin/env bats
# Plan 15 Phase C task C-8 — verify_license_compliance.sh tests.
#
# Verifies the umbrella composition contract:
#   - script exists + executable
#   - smoke against the live repo passes (inv-zen-174 + inv-zen-286 +
#     inv-zen-287 + inv-zen-290 all green)
#   - banner sentinel "ALL LICENSE-COMPLIANCE GATES PASSED" emitted (load-bearing
#     for tests/integration/license_compliance_test.go)
#   - NOTICE-present + sentinels-OK = OK (W4C scope override; decisión 15
#     plan default would also accept NOTICE absent)

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    SCRIPT_PATH="${PROJECT_ROOT}/scripts/verify_license_compliance.sh"
    cd "${PROJECT_ROOT}"
}

@test "scripts/verify_license_compliance.sh present + executable" {
    [ -x "$SCRIPT_PATH" ]
}

@test "umbrella verify against live repo: exit 0" {
    run bash "$SCRIPT_PATH"
    [ "$status" -eq 0 ]
}

@test "umbrella verify emits 'ALL LICENSE-COMPLIANCE GATES PASSED' banner" {
    run bash "$SCRIPT_PATH"
    [[ "$output" =~ "ALL LICENSE-COMPLIANCE GATES PASSED" ]]
}

@test "umbrella verify emits 'MIT-canonical per decisión 15' banner" {
    run bash "$SCRIPT_PATH"
    [[ "$output" =~ "MIT-canonical per decisión 15" ]]
}

@test "umbrella verify reports inv-zen-286 + inv-zen-287 + inv-zen-174 + inv-zen-290" {
    run bash "$SCRIPT_PATH"
    [[ "$output" =~ "inv-zen-286" ]]
    [[ "$output" =~ "inv-zen-287" ]]
    [[ "$output" =~ "inv-zen-174" ]]
    [[ "$output" =~ "inv-zen-290" ]]
}

@test "umbrella verify accepts NOTICE present + sentinels OK (W4C scope)" {
    run bash "$SCRIPT_PATH"
    [[ "$output" =~ "NOTICE present + all 4 sentinels verified" ]]
}
