#!/usr/bin/env bats

# Plan 15 Phase A A-6 — verify_license_disclosure.sh 4-redundant MIT tests
# (post-Stage-0 reconciliation per decisiones 6 + 15).

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "scripts/verify_license_disclosure.sh present + executable" {
    [ -x "scripts/verify_license_disclosure.sh" ]
}

@test "all 4 redundant MIT surfaces present" {
    [ -f "LICENSE" ]
    [ -f "README.md" ]
    [ -f "THIRD_PARTY_LICENSES.md" ]
    [ -f "Formula/hades.rb" ]
}

@test "INSTALL.md present (Installing HADES entrypoint)" {
    [ -f "INSTALL.md" ]
}

@test "README.md contains License section" {
    run grep -E "^## License" README.md
    [ "$status" -eq 0 ]
}

@test "README.md mentions MIT" {
    run grep "MIT" README.md
    [ "$status" -eq 0 ]
}

@test "INSTALL.md mentions Installing HADES" {
    run grep "Installing HADES" INSTALL.md
    [ "$status" -eq 0 ]
}

@test "THIRD_PARTY_LICENSES.md lists hermes-agent + smacker/go-tree-sitter + sqlite-vec" {
    run grep "hermes-agent" THIRD_PARTY_LICENSES.md
    [ "$status" -eq 0 ]
    run grep "smacker/go-tree-sitter" THIRD_PARTY_LICENSES.md
    [ "$status" -eq 0 ]
    run grep "sqlite-vec" THIRD_PARTY_LICENSES.md
    [ "$status" -eq 0 ]
}

@test "Formula/hades.rb declares license MIT" {
    run grep -E 'license "MIT"' Formula/hades.rb
    [ "$status" -eq 0 ]
}

@test "Formula/hades.rb caveats mentions Caronte" {
    run grep "Caronte" Formula/hades.rb
    [ "$status" -eq 0 ]
}

@test "verify_license_disclosure.sh exits 0 when 4 MIT surfaces consistent" {
    run bash scripts/verify_license_disclosure.sh
    [ "$status" -eq 0 ]
    [[ "$output" == *"OK: license disclosure"* ]]
}

@test "verify_license_disclosure.sh exits non-zero when README missing License section" {
    cp README.md /tmp/README.md.bak
    grep -v "^## License" README.md > /tmp/README.md.tmp
    mv /tmp/README.md.tmp README.md
    run bash scripts/verify_license_disclosure.sh
    mv /tmp/README.md.bak README.md
    [ "$status" -ne 0 ]
}
