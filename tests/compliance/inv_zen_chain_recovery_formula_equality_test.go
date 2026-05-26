// tests/compliance/inv_zen_chain_recovery_formula_equality_test.go
//
// Compliance test: chain.Compute (the audit-event chain hash producer)
// and recovery.ComputeRecordHashCanonical (the verify-side hash
// reproducer) MUST produce byte-identical output for the same inputs.
// Closes IMPORTANT-4 (I-4) from the Plan 9 cross-phase A+B+C review.
//
// Why this test exists:
//
//	The recovery package re-implements the chain-hash formula
//	(see internal/audit/recovery/verify_chain.go computeRecordHash)
//	rather than importing internal/audit/chain. The orthogonality is
//	intentional under the project doctrine + dispatch DAG — Phase B
//	(chain) and Phase C (recovery) ship in parallel; cross-importing
//	couples them. But two copies of the same formula is an opportunity
//	for silent bit-rot: a future change to chain.Compute that misses
//	the recovery copy would break verify-chain only on tampered inputs
//	that THIS specific formula version flags, surfacing as a
//	subtle-and-late integrity bug.
//
//	This test catches the drift in two complementary ways:
//
//	  1. Property-based — testing/quick fires 1000 random
//	     (prevHash, eventType, payload, ts) tuples through both
//	     functions and asserts equality on the chain-rejection-domain
//	     (chain.Compute validates inputs; recovery does not — when
//	     chain.Compute returns an error we skip that input as
//	     out-of-formula-domain).
//
//	  2. Pinned vector — a hard-coded canonical input + expected
//	     output. Mirrors chain.TestComputeKnownVector so the same
//	     sha256 byte sequence is asserted from both sides. Locks the
//	     algorithm to "sha256(prev || \"|\" || type || \"|\" || payload || \"|\" || ts_decimal)".
//
// Inv-zen-031 boundary check: this test imports BOTH internal/audit/chain
// AND internal/audit/recovery. tests/compliance/ is the integration
// boundary specifically permitted to cross substrate-package lines for
// invariant assertions; it is NOT production code (the recovery + chain
// runtime packages remain orthogonal).
package compliance

import (
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
)

func TestChainComputeAndRecoveryComputeRecordHashAgree(t *testing.T) {
	property := func(prevHash, eventType string, payload []byte, ts int64) bool {
		chainOut, errChain := chain.Compute(prevHash, eventType, payload, ts)
		if errChain != nil {
			// Out-of-domain input — chain.Compute rejected. We do not
			// assert anything about recovery in this case; both
			// formulas only need to agree where chain.Compute accepts.
			return true
		}
		recoveryOut := recovery.ComputeRecordHashCanonical(prevHash, eventType, string(payload), ts)
		if chainOut != recoveryOut {
			t.Logf("formula drift: prev=%q type=%q payload=%q ts=%d chain=%s recovery=%s",
				prevHash, eventType, payload, ts, chainOut, recoveryOut)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("formula equality property violated: %v", err)
	}
}

func TestChainRecoveryComputeKnownVector(t *testing.T) {
	const (
		prev    = ""
		evType  = "test.event"
		payload = `{"k":1}`
		ts      = int64(1700000000)

		want = "4960576aca8dd89bf56c2d6dd8c63c1a525329ab0852afdd1c1b6899750e2575"
	)

	chainOut, err := chain.Compute(prev, evType, []byte(payload), ts)
	if err != nil {
		t.Fatalf("chain.Compute(known vector): %v", err)
	}
	recoveryOut := recovery.ComputeRecordHashCanonical(prev, evType, payload, ts)

	if chainOut != want {
		t.Errorf("chain.Compute drift on known vector:\n got  %s\n want %s", chainOut, want)
	}
	if recoveryOut != want {
		t.Errorf("recovery.ComputeRecordHashCanonical drift on known vector:\n got  %s\n want %s", recoveryOut, want)
	}
	if chainOut != recoveryOut {
		t.Errorf("chain.Compute / recovery.ComputeRecordHashCanonical mismatch:\n chain    %s\n recovery %s", chainOut, recoveryOut)
	}
}
