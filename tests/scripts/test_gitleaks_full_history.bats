#!/usr/bin/env bats
# Plan 15 Phase C task C-1 — gitleaks full-history scan tests.
#
# Verifies (per inv-zen-288 + plan §C-1 acceptance):
#   (a) script exists + executable
#   (b) script exits 0 on clean repo (no secrets); skip if gitleaks absent
#   (c) script exits non-zero (3) when planted secret detected
#   (d) script writes JSON report to expected path
#   (e) script exits 1 when gitleaks binary missing (PATH stripped)

setup() {
    SCRIPT_PATH="${BATS_TEST_DIRNAME}/../../scripts/gitleaks_full_history.sh"
    export ZEN_GITLEAKS_REPORT_DIR="$(mktemp -d -t hades-gitleaks-tests.XXXXXX)"
    export TEST_REPO="$(mktemp -d -t hades-gitleaks-repo.XXXXXX)"
    cd "$TEST_REPO" || exit 1
    git init --quiet
    git config user.email "test@example.com"
    git config user.name "Test"
    echo "hello world" > README.md
    git add README.md
    git commit -m "initial" --quiet
}

teardown() {
    rm -rf "$ZEN_GITLEAKS_REPORT_DIR" "$TEST_REPO"
}

@test "C-1: script exists and is executable" {
    [ -x "$SCRIPT_PATH" ]
}

@test "C-1: exits 0 on clean repo (when gitleaks available)" {
    if ! command -v gitleaks >/dev/null 2>&1; then
        skip "gitleaks not installed"
    fi
    run bash "$SCRIPT_PATH"
    [ "$status" -eq 0 ]
}

@test "C-1: detects planted AWS access key and exits 3" {
    if ! command -v gitleaks >/dev/null 2>&1; then
        skip "gitleaks not installed"
    fi
    # AKIA... is a synthetic AWS-style access key id; gitleaks default ruleset matches.
    echo "AWS_KEY=AKIAIOSFODNN7EXAMPLE" > leak.txt
    git add leak.txt
    git commit -m "leak" --quiet
    run bash "$SCRIPT_PATH"
    [ "$status" -eq 3 ]
}

@test "C-1: writes JSON report to expected path" {
    if ! command -v gitleaks >/dev/null 2>&1; then
        skip "gitleaks not installed"
    fi
    run bash "$SCRIPT_PATH"
    [ -f "$ZEN_GITLEAKS_REPORT_DIR/gitleaks-pre-flip.json" ]
}

@test "C-1: exits 1 with helpful message when gitleaks binary missing" {
    # Strip PATH so gitleaks (and brew) cannot resolve; preserve a working bash.
    PATH="/usr/bin:/bin" run bash "$SCRIPT_PATH"
    [ "$status" -eq 1 ]
    [[ "$output" =~ "gitleaks not installed" ]]
}
