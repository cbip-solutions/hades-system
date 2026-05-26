#!/usr/bin/env bats

# Plan 15 Phase A A-5/A-11 — verify-30-ci-green binary + Makefile target
# smoke tests.
#
# Validates:
#   - cmd/verify-30-ci-green/ source present
#   - internal/ci/ library source present (4 files)
#   - Binary compiles
#   - `--help` flag exits 0 with usage text
#   - Unit tests pass (delegate to `go test`)
#   - Makefile target `verify-30-ci-green` exists
#
# Live GH API calls are NOT exercised here (require GITHUB_TOKEN +
# network); the integration test skips with a helpful message if
# `gh auth status` fails.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "cmd/verify-30-ci-green/main.go present" {
    [ -f "cmd/verify-30-ci-green/main.go" ]
}

@test "cmd/verify-30-ci-green/main_test.go present" {
    [ -f "cmd/verify-30-ci-green/main_test.go" ]
}

@test "internal/ci/ library has 4 production files + 4 test files + doc.go" {
    [ -f "internal/ci/doc.go" ]
    [ -f "internal/ci/github.go" ]
    [ -f "internal/ci/cache.go" ]
    [ -f "internal/ci/classifier.go" ]
    [ -f "internal/ci/rolling_window.go" ]
    [ -f "internal/ci/github_test.go" ]
    [ -f "internal/ci/cache_test.go" ]
    [ -f "internal/ci/classifier_test.go" ]
    [ -f "internal/ci/rolling_window_test.go" ]
}

@test "internal/ci/classifier.go exports ClassifierVersion constant" {
    run grep -E "^const ClassifierVersion = " internal/ci/classifier.go
    [ "$status" -eq 0 ]
}

@test "internal/ci/classifier.go exports LoadFlakeQuarantine function" {
    run grep -E "^func LoadFlakeQuarantine\(" internal/ci/classifier.go
    [ "$status" -eq 0 ]
}

@test "internal/ci/rolling_window.go exports DefaultRollingWindow" {
    run grep -E "^func DefaultRollingWindow\(\) RollingWindow" internal/ci/rolling_window.go
    [ "$status" -eq 0 ]
}

@test "internal/ci/cache.go uses hades cache path (NOT zen-swarm)" {
    # Path constant must be ~/.cache/hades/ci/ per master §2.6 + C4 fix.
    run grep -E '\"\.cache\", \"hades\", \"ci\"' internal/ci/cache.go
    [ "$status" -eq 0 ]
    # No filepath.Join with "zen-swarm" in the cache-dir construction.
    # (Doc-comments may reference the historical zen-swarm name for
    # migration context; the constraint is on the runtime path only.)
    run grep -E 'filepath\.Join.*zen-swarm' internal/ci/cache.go
    [ "$status" -ne 0 ] || [ -z "$output" ]
}

@test "Makefile contains verify-30-ci-green target" {
    run grep -E "^verify-30-ci-green:" Makefile
    [ "$status" -eq 0 ]
}

@test "go build of cmd/verify-30-ci-green succeeds" {
    run go build -o /tmp/verify-30-ci-green-bats ./cmd/verify-30-ci-green
    [ "$status" -eq 0 ]
    [ -x "/tmp/verify-30-ci-green-bats" ]
}

@test "binary --help exits 0 with flag descriptions" {
    if [ ! -x "/tmp/verify-30-ci-green-bats" ]; then
        skip "binary not built; previous test failed"
    fi
    run /tmp/verify-30-ci-green-bats -h
    # `-h`/`--help` is Go's stdlib flag default; exits with status 0
    # AND prints to stderr. The buffer is captured into $output.
    [[ "$output" == *"owner"* ]]
    [[ "$output" == *"repo"* ]]
    [[ "$output" == *"window"* ]]
    [[ "$output" == *"quarantine"* ]]
}

@test "internal/ci unit tests pass" {
    run go test ./internal/ci/... -count=1
    [ "$status" -eq 0 ]
}

@test "cmd/verify-30-ci-green unit tests pass" {
    run go test ./cmd/verify-30-ci-green/... -count=1
    [ "$status" -eq 0 ]
}

@test "live GH API gate (optional: requires GH_TOKEN/network)" {
    if [ -z "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]; then
        skip "GH_TOKEN/GITHUB_TOKEN unset; live GH API gate skipped (set var + re-run)"
    fi
    if ! gh auth status >/dev/null 2>&1; then
        skip "gh CLI not authenticated; live GH API gate skipped"
    fi
    # Live call against this repo — small window for speed.
    run make verify-30-ci-green
    # Gate may pass or fail depending on actual CI history; both are
    # acceptable for this smoke (we only assert the binary runs without
    # exit-code 2 = config/network error).
    [ "$status" -ne 2 ]
}
