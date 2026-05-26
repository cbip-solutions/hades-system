#!/usr/bin/env bats

# Plan 15 Phase C C-4 — tests/scripts/test_insert_spdx_headers.bats
#
# Verifies scripts/insert_spdx_headers.sh:
#   - inserts SPDX-License-Identifier: MIT into .go/.py/.sh files
#   - uses correct comment prefix per file type (// for go, # for py/sh)
#   - respects insertion-position rules (after //go:build / shebang)
#   - is idempotent (re-run produces zero modifications)
#   - skips vendor/, virtualenvs, generated, and other excluded paths
#
# Per decisión 15 (2026-05-24): identifier is MIT (whole project).
# inv-zen-287: every production .go/.py/.sh has SPDX-MIT header.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    SCRIPT="${PROJECT_ROOT}/scripts/insert_spdx_headers.sh"
    TMPDIR_TEST="$(mktemp -d -t spdx-bats.XXXXXX)"
    # Make a fake git root so the script's `git rev-parse --show-toplevel`
    # locates TMPDIR_TEST and operates on the fixtures (not the real repo).
    (cd "${TMPDIR_TEST}" && git init -q .)
}

teardown() {
    if [ -n "${TMPDIR_TEST:-}" ] && [ -d "${TMPDIR_TEST}" ]; then
        rm -rf "${TMPDIR_TEST}"
    fi
}

@test "scripts/insert_spdx_headers.sh present + executable" {
    [ -x "${SCRIPT}" ]
}

@test "test_go_comment_style: .go files get // SPDX-License-Identifier: MIT" {
    printf 'package foo\n\nfunc Bar() {}\n' > "${TMPDIR_TEST}/foo.go"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    first_line="$(head -1 "${TMPDIR_TEST}/foo.go")"
    [ "${first_line}" = "// SPDX-License-Identifier: MIT" ]
}

@test "test_py_comment_style: .py files get # SPDX-License-Identifier: MIT" {
    printf 'def bar():\n    pass\n' > "${TMPDIR_TEST}/foo.py"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    first_line="$(head -1 "${TMPDIR_TEST}/foo.py")"
    [ "${first_line}" = "# SPDX-License-Identifier: MIT" ]
}

@test "test_sh_comment_style: .sh files get # SPDX-License-Identifier: MIT" {
    printf 'echo hello\n' > "${TMPDIR_TEST}/foo.sh"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    first_line="$(head -1 "${TMPDIR_TEST}/foo.sh")"
    [ "${first_line}" = "# SPDX-License-Identifier: MIT" ]
}

@test "test_position_after_go_build_tag: //go:build line preserved at top" {
    printf '//go:build tools\n// +build tools\n\npackage tools\n' > "${TMPDIR_TEST}/tools.go"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/tools.go")"
    [ "${line1}" = "//go:build tools" ]
    # SPDX header should appear within first 10 lines (inv-zen-287)
    run head -10 "${TMPDIR_TEST}/tools.go"
    [[ "${output}" =~ "SPDX-License-Identifier: MIT" ]]
}

@test "test_gofmt_canonical: //go:build + // +build block preserved + SPDX placed gofmt-canonical" {
    # Reproduces tools.go shape: both new + legacy build constraints.
    # gofmt requires: contiguous constraint block, blank line, then SPDX,
    # blank line, then package doc. The script must produce this canonical
    # form so subsequent `gofmt -l` reports zero diffs.
    printf '//go:build tools\n// +build tools\n\n// Package tools.\npackage tools\n' \
        > "${TMPDIR_TEST}/tools.go"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    if command -v gofmt >/dev/null 2>&1; then
        run gofmt -l "${TMPDIR_TEST}/tools.go"
        [ "${status}" -eq 0 ]
        [ -z "${output}" ]
    else
        skip "gofmt not available"
    fi
}

@test "test_position_after_shebang: shebang preserved at top" {
    printf '#!/usr/bin/env bash\n\nset -euo pipefail\necho hi\n' > "${TMPDIR_TEST}/run.sh"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/run.sh")"
    [ "${line1}" = "#!/usr/bin/env bash" ]
    line2="$(sed -n '2p' "${TMPDIR_TEST}/run.sh")"
    [ "${line2}" = "# SPDX-License-Identifier: MIT" ]
}

@test "test_idempotence: re-run produces 0 modifications" {
    printf 'package foo\n\nfunc Bar() {}\n' > "${TMPDIR_TEST}/foo.go"
    printf 'def bar():\n    pass\n' > "${TMPDIR_TEST}/bar.py"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    sha_before="$(shasum "${TMPDIR_TEST}/foo.go" "${TMPDIR_TEST}/bar.py" | awk '{print $1}' | sort | shasum | awk '{print $1}')"
    # Re-run; idempotent: bytes unchanged.
    run bash -c "cd '${TMPDIR_TEST}' && '${SCRIPT}' 2>&1"
    [ "${status}" -eq 0 ]
    [[ "${output}" =~ "0 files" ]] || [[ "${output}" =~ "Inserted SPDX-License-Identifier: MIT header into 0 files" ]]
    sha_after="$(shasum "${TMPDIR_TEST}/foo.go" "${TMPDIR_TEST}/bar.py" | awk '{print $1}' | sort | shasum | awk '{print $1}')"
    [ "${sha_before}" = "${sha_after}" ]
}

@test "test_dry_run_no_writes: --dry-run mode does NOT modify files" {
    printf 'package foo\n\nfunc Bar() {}\n' > "${TMPDIR_TEST}/foo.go"
    sha_before="$(shasum "${TMPDIR_TEST}/foo.go" | awk '{print $1}')"
    run bash -c "cd '${TMPDIR_TEST}' && '${SCRIPT}' --dry-run 2>&1"
    [ "${status}" -eq 0 ]
    [[ "${output}" =~ "Dry run" ]] || [[ "${output}" =~ "would be modified" ]]
    sha_after="$(shasum "${TMPDIR_TEST}/foo.go" | awk '{print $1}')"
    [ "${sha_before}" = "${sha_after}" ]
}

@test "test_skip_vendor: vendor/ is skipped" {
    mkdir -p "${TMPDIR_TEST}/vendor/example.com/foo"
    printf 'package foo\n' > "${TMPDIR_TEST}/vendor/example.com/foo/foo.go"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    # vendor file must remain untouched
    line1="$(sed -n '1p' "${TMPDIR_TEST}/vendor/example.com/foo/foo.go")"
    [ "${line1}" = "package foo" ]
}

@test "test_skip_virtualenv_dependencies: generated venv packages are skipped" {
    mkdir -p "${TMPDIR_TEST}/plugin/hades/.venv/lib/python3.14/site-packages/example"
    printf 'def generated():\n    pass\n' > "${TMPDIR_TEST}/plugin/hades/.venv/lib/python3.14/site-packages/example/generated.py"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/plugin/hades/.venv/lib/python3.14/site-packages/example/generated.py")"
    [ "${line1}" = "def generated():" ]
}

@test "test_skip_generated: \"Code generated by\" files are skipped" {
    cat > "${TMPDIR_TEST}/gen.go" <<EOF
// Code generated by some-tool. DO NOT EDIT.

package gen

func Bar() {}
EOF
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/gen.go")"
    [ "${line1}" = "// Code generated by some-tool. DO NOT EDIT." ]
}

@test "test_rewrite_apache_to_mit: existing SPDX-Apache-2.0 is rewritten to MIT" {
    # Per Plan 15 decisión 15: whole project MIT; legacy Apache-2.0 SPDX
    # identifiers from earlier planning era MUST be migrated in-place.
    cat > "${TMPDIR_TEST}/legacy.go" <<EOF
// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: Apache-2.0.

// Package legacy is a legacy file.
package legacy
EOF
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/legacy.go")"
    [[ "${line1}" =~ "SPDX-License-Identifier: MIT" ]]
    # Original Copyright preamble preserved.
    [[ "${line1}" =~ "Copyright 2026 zen-swarm contributors" ]]
    # No Apache trace left.
    run grep -i "apache" "${TMPDIR_TEST}/legacy.go"
    [ "${status}" -ne 0 ]
}

@test "test_rewrite_applies_to_test_files_regardless_of_include_tests_flag" {
    # Test files with non-MIT SPDX are contradictory-in-place; the rewrite
    # logic MUST address them even without --include-tests (the flag widens
    # INSERT scope only, never REWRITE).
    cat > "${TMPDIR_TEST}/foo_test.go" <<EOF
// Copyright 2026. SPDX-License-Identifier: Apache-2.0.
package foo
EOF
    # No --include-tests flag.
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/foo_test.go")"
    [[ "${line1}" =~ "SPDX-License-Identifier: MIT" ]]
}

@test "test_insert_skips_test_files_by_default" {
    # Without --include-tests, _test.go files without any SPDX are skipped
    # (INSERT scope is production-only per default; the compliance test
    # `inv_zen_287_spdx_headers_test.go` mirrors this exclusion).
    printf 'package foo\n' > "${TMPDIR_TEST}/foo_test.go"
    sha_before="$(shasum "${TMPDIR_TEST}/foo_test.go" | awk '{print $1}')"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" >/dev/null)
    sha_after="$(shasum "${TMPDIR_TEST}/foo_test.go" | awk '{print $1}')"
    [ "${sha_before}" = "${sha_after}" ]
}

@test "test_insert_includes_test_files_with_include_tests_flag" {
    # With --include-tests, _test.go files without SPDX get the header.
    printf 'package foo\n' > "${TMPDIR_TEST}/foo_test.go"
    (cd "${TMPDIR_TEST}" && "${SCRIPT}" --include-tests >/dev/null)
    line1="$(sed -n '1p' "${TMPDIR_TEST}/foo_test.go")"
    [ "${line1}" = "// SPDX-License-Identifier: MIT" ]
}
