#!/usr/bin/env bats

# Plan 15 Phase A A-4 — verify_spikes_current.sh wrapper smoke tests.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
}

@test "scripts/verify_spikes_current.sh present + executable" {
    [ -x "scripts/verify_spikes_current.sh" ]
}

@test "8 spike report files present in docs/spikes/" {
    run ls docs/spikes/
    [ "$status" -eq 0 ]
    for n in 01 02 03 04 05 06 07 08; do
        run bash -c "ls docs/spikes/spike_${n}_*.md"
        [ "$status" -eq 0 ]
    done
}

@test "8 spike harness Go files present in docs/spikes/" {
    for n in 01 02 03 04 05 06 07 08; do
        run bash -c "ls docs/spikes/spike_${n}_*.go"
        [ "$status" -eq 0 ]
    done
}

@test "verify_spikes_current.sh --offline exits 0 with all 8 reports present" {
    # Build the binary first.
    go build -o bin/verify-spikes ./cmd/verify-spikes
    run bash scripts/verify_spikes_current.sh --offline
    [ "$status" -eq 0 ]
    [[ "$output" == *"ALL 8 PHASE 0 SPIKES PASS"* ]]
}
