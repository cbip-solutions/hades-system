#!/usr/bin/env bats

# Plan 15 Phase A Task A-1 — verify_brew_formula.sh lint tests
#
# Post-Stage-0 reconciliation (decisiones 4 + 6 + 15): MIT-licensed HADES
# formula declares mandatory hermes-agent dep; gitnexus DROPPED (Caronte
# in-tree sovereign per decisión 6 / Plan 19 SHIPPED). Caveats surface
# MIT-license simplicity + Plan 16 paygo cascade default + optional
# Tier 1 sidecar mention (decisión 17-i). inv-zen-174 widens to
# 4-redundant MIT (LICENSE + per-file SPDX + brew formula license +
# THIRD_PARTY_LICENSES inbound) per decisión 15.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "verify_brew_formula.sh script exists" {
    [ -x "scripts/verify_brew_formula.sh" ]
}

@test "Formula/hades.rb local mirror present" {
    [ -f "Formula/hades.rb" ]
}

@test "Formula declares depends_on hermes-agent" {
    run grep -E 'depends_on "hermes-agent"' Formula/hades.rb
    [ "$status" -eq 0 ]
}

@test "Formula does NOT declare depends_on gitnexus (decisión 6 Caronte sovereign)" {
    run grep -E 'depends_on "gitnexus"' Formula/hades.rb
    [ "$status" -ne 0 ]
}

@test "Formula caveats block mentions Caronte" {
    run grep "Caronte" Formula/hades.rb
    [ "$status" -eq 0 ]
}

@test "Formula license is MIT" {
    run grep -E 'license "MIT"' Formula/hades.rb
    [ "$status" -eq 0 ]
}

@test "verify_brew_formula.sh exits 0 on a well-formed formula" {
    run bash scripts/verify_brew_formula.sh
    [ "$status" -eq 0 ]
}

@test "verify_brew_formula.sh exits non-zero if Formula missing hermes-agent dep" {
    cp Formula/hades.rb Formula/hades.rb.bak
    sed -i.tmp '/depends_on "hermes-agent"/d' Formula/hades.rb
    run bash scripts/verify_brew_formula.sh
    mv Formula/hades.rb.bak Formula/hades.rb
    rm -f Formula/hades.rb.tmp
    [ "$status" -ne 0 ]
}
