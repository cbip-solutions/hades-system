#!/usr/bin/env bats
#
# Plan 15 Phase I-8 — verify_canonical_docs_hygiene.sh integration tests.
#
# Asserts the bash wrapper detects:
#   (a) missing canonical doc (any one of the 8-doc set)
#   (b) re-introduction of docs/operations/gitnexus-ux.md
#   (c) clean state passes (exits 0)
#
# Run via: bats tests/scripts/verify_canonical_docs_hygiene_test.bats
# Composes into the verify-canonical-docs-hygiene Makefile target.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "verify_canonical_docs_hygiene.sh script exists + executable" {
    [ -x "scripts/verify_canonical_docs_hygiene.sh" ]
}

@test "verify_canonical_docs_hygiene.sh exits 0 on a refreshed tree" {
    run bash scripts/verify_canonical_docs_hygiene.sh
    [ "$status" -eq 0 ]
}

@test "verify_canonical_docs_hygiene.sh detects missing canonical doc" {
    cp INSTALL.md INSTALL.md.bak
    rm INSTALL.md
    run bash scripts/verify_canonical_docs_hygiene.sh
    mv INSTALL.md.bak INSTALL.md
    [ "$status" -ne 0 ]
}

@test "verify_canonical_docs_hygiene.sh detects re-introduction of gitnexus-ux.md" {
    touch docs/operations/gitnexus-ux.md
    run bash scripts/verify_canonical_docs_hygiene.sh
    rm docs/operations/gitnexus-ux.md
    [ "$status" -ne 0 ]
}

@test "verify_canonical_docs_hygiene.sh detects missing llms.txt" {
    cp llms.txt llms.txt.bak
    rm llms.txt
    run bash scripts/verify_canonical_docs_hygiene.sh
    mv llms.txt.bak llms.txt
    [ "$status" -ne 0 ]
}
