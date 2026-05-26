#!/usr/bin/env bats

# Plan 15 Phase K Task K-9 — verify_no_task_context_comments.sh permanent
# gate enforcing K-1 regex pre-pass anti-recurrence.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "verify_no_task_context_comments.sh script exists" {
    [ -x "scripts/verify_no_task_context_comments.sh" ]
}

@test "verify_no_task_context_comments.sh exits non-zero on re-introduced rot" {
    # Inject a rot pattern into a temp file outside testdata fixtures
    TMPFILE="internal/dummytest_$$.go"
    cat > "$TMPFILE" <<'EOG'
package internal

// Plan 5 dispatcher boundary check
func Dummy() {}
EOG
    run bash scripts/verify_no_task_context_comments.sh
    rm -f "$TMPFILE"
    [ "$status" -ne 0 ]
}

@test "verify_no_task_context_comments.sh exits 0 when no rot present (synthetic clean tree)" {
    # Synthetic isolated tree (no rot) — verify the gate accepts a clean state.
    # Pre-K-2 the live tree still has rot; the gate's correct rejection of
    # that state is what the previous test verifies. This test verifies the
    # positive case using an isolated sub-tree.
    TMPDIR_T="$(mktemp -d)"
    mkdir -p "${TMPDIR_T}/internal" "${TMPDIR_T}/cmd" "${TMPDIR_T}/plugin"
    cat > "${TMPDIR_T}/internal/clean.go" <<'EOG'
package internal

// Dummy performs work.
func Dummy() {}
EOG
    cp scripts/verify_no_task_context_comments.sh "${TMPDIR_T}/"
    chmod +x "${TMPDIR_T}/verify_no_task_context_comments.sh"
    cd "${TMPDIR_T}"
    run bash verify_no_task_context_comments.sh
    cd "${PROJECT_ROOT}"
    rm -rf "${TMPDIR_T}"
    [ "$status" -eq 0 ]
}

@test "verify_no_task_context_comments.sh ignores generated virtualenv dependencies" {
    TMPDIR_T="$(mktemp -d)"
    mkdir -p "${TMPDIR_T}/internal" "${TMPDIR_T}/cmd" "${TMPDIR_T}/plugin/.venv/lib/python3.14/site-packages/example"
    cat > "${TMPDIR_T}/internal/clean.go" <<'EOG'
package internal

// Dummy performs work.
func Dummy() {}
EOG
    echo '# TODO generated dependency comment' > "${TMPDIR_T}/plugin/.venv/lib/python3.14/site-packages/example/generated.py"
    cp scripts/verify_no_task_context_comments.sh "${TMPDIR_T}/"
    chmod +x "${TMPDIR_T}/verify_no_task_context_comments.sh"
    cd "${TMPDIR_T}"
    run bash verify_no_task_context_comments.sh
    cd "${PROJECT_ROOT}"
    rm -rf "${TMPDIR_T}"
    [ "$status" -eq 0 ]
}
