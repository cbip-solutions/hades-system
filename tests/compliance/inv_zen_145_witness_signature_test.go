// tests/compliance/inv_zen_145_witness_signature_test.go
//
// Compliance gate for inv-zen-145 (daemon witness signature mandatory on
// daemon_global_checkpoint_log + hash-then-sign discipline).
//
// Invariant text (inv-zen-145):
//
//	"Every entry appended to the daemon-global checkpoint log MUST carry a
//	 valid daemon witness signature. The Checkpoint.Append surface refuses
//	 SignedSTH values whose Signature slice is empty by returning
//	 ErrUnsignedSTH. Additionally, every signing surface in the tessera
//	 package hashes its input via sha256 BEFORE invoking ECDSA — the witness
//	 private key never signs raw payload bytes."
//
// Two enforcement points, two test assertions:
//
//  1. Mandatory-signature gate (Checkpoint.Append):
//     Construct a SignedSTH with empty Signature; call Checkpoint.Append;
//     assert errors.Is(err, ErrUnsignedSTH). This catches a regression
//     where the empty-signature guard at internal/audit/tessera/checkpoint.go
//     line ~169-175 is relaxed.
//
//  2. Hash-then-sign discipline (Adapter.WitnessCoSignSeal):
//     A signature produced by WitnessCoSignSeal over a payload P MUST be a
//     valid ECDSA-P256 signature over sha256(P), NOT over raw P. The
//     compliance check verifies the signature against sha256(P) (must
//     succeed) AND against P un-hashed (must fail), proving the hashing
//     happens on the production path.
//
// Plan-file: docs/superpowers/plans/2026-05-07-plan-9-phase-K-tests.md
// lines 4176-4225 (Task K-11 Step 3). The plan-file's pseudocode used
// speculative API ("CheckpointLog{store}", "AppendUnsigned", an
// "UnsignedRejectedError" wrapper struct, and an
// "audit.checkpoint_unsigned_rejected" event); the real enforcement uses
// tessera.NewCheckpoint + Checkpoint.Append returning the canonical
// ErrUnsignedSTH sentinel. This test exercises the production path
// rather than the speculative one — the invariant guarantee (an unsigned
// STH cannot reach the daemon-global log) is identical, and we add the
// hash-then-sign half explicitly because the plan-file's mission notes
// it ("inv-zen-145 hash-then-sign witness; payload bytes hashed before
// signing").
//
// Driver isolation note: this test imports
// github.com/cbip-solutions/hades-system/internal/audit/tessera which does not
// link any SQLite driver, so it coexists cleanly with the compliance
// package's existing ncruces driver registration (inv_zen_073_test.go).
//
// Refs: spec §7.2 lines 1666-1679 (Plan 9 invariant declaration);
// sentinel declared in internal/audit/tessera/errors.go;
// WitnessCoSignSeal hash-then-sign at adapter.go line ~367-372.
package compliance

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

func inv145TestConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

// TestInvZen145_CheckpointRejectsUnsignedSTH asserts the mandatory-
// signature gate at the daemon-global checkpoint log: a SignedSTH whose
// Signature slice is empty MUST be refused with ErrUnsignedSTH, never
// persisted.
//
// This is the load-bearing inv-zen-145 assertion. Without it, an
// internal bug or a malicious caller could short-circuit the witness
// path and write an STH with no signature — making the chain anchor
// non-verifiable downstream.
func TestInvZen145_CheckpointRejectsUnsignedSTH(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx := context.Background()
	dir := t.TempDir()

	cp, err := tessera.NewCheckpoint(ctx, dir, inv145TestConfig())
	if err != nil {
		t.Fatalf("NewCheckpoint: %v", err)
	}
	t.Cleanup(func() { _ = cp.Close() })

	unsigned := tessera.SignedSTH{
		STH: tessera.STH{
			ProjectID: "p-a",
			Size:      1,
			RootHash:  make([]byte, 32),
			Timestamp: time.Unix(1_700_000_000, 0).UTC(),
		},
		Signature:         nil,
		PubkeyFingerprint: "",
	}

	err = cp.Append(ctx, unsigned)
	if err == nil {
		t.Fatalf("inv-zen-145: Checkpoint.Append accepted SignedSTH with empty Signature; mandatory-signature gate broken")
	}
	if !errors.Is(err, tessera.ErrUnsignedSTH) {
		t.Errorf("inv-zen-145: Checkpoint.Append returned %v; want errors.Is(..., ErrUnsignedSTH)", err)
	}

	// Belt-and-suspenders: a zero-length non-nil slice MUST also be
	// rejected. len(nil) == len([]byte{}) == 0, but the guard SHOULD
	// fire on the zero-length condition rather than on the nil-ness
	// of the slice header.
	emptyButNonNil := tessera.SignedSTH{
		STH: tessera.STH{
			ProjectID: "p-b",
			Size:      1,
			RootHash:  make([]byte, 32),
			Timestamp: time.Unix(1_700_000_001, 0).UTC(),
		},
		Signature:         []byte{},
		PubkeyFingerprint: "",
	}
	err = cp.Append(ctx, emptyButNonNil)
	if err == nil {
		t.Fatalf("inv-zen-145: Checkpoint.Append accepted SignedSTH with zero-length Signature; mandatory-signature gate broken")
	}
	if !errors.Is(err, tessera.ErrUnsignedSTH) {
		t.Errorf("inv-zen-145: Checkpoint.Append (empty-but-non-nil) returned %v; want errors.Is(..., ErrUnsignedSTH)", err)
	}
}

// TestInvZen145_WitnessCoSignSealHashesPayloadFirst asserts the
// hash-then-sign discipline: a signature produced by WitnessCoSignSeal
// over payload P MUST validate against sha256(P) and MUST NOT validate
// against the un-hashed payload P.
//
// Why this matters: a signing surface that signs raw bytes is vulnerable
// to length-extension reasoning (in non-hash-then-sign primitives) and,
// more practically, to caller confusion — a verifier expecting a hash
// preimage but given a raw byte signature will fail silently. The
// hash-then-sign rule unifies the audit-chain verifier path: every
// signature in the system is over a 32-byte sha256 digest.
//
// Test strategy:
//
//   - Generate a witness, attach it to a per-project Adapter.
//   - Call WitnessCoSignSeal(payload) to obtain a signature `sig`.
//   - Load the witness pubkey.
//   - Compute h = sha256(payload).
//   - Assert VerifyWithPubkey(pub, h, sig) is TRUE.
//   - Assert VerifyWithPubkey(pub, payload, sig) is FALSE (proves the
//     signing surface hashed; otherwise the un-hashed verification
//     would also succeed for short payloads, and the discipline would
//     be undetectable).
func TestInvZen145_WitnessCoSignSealHashesPayloadFirst(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx := context.Background()
	root := t.TempDir()

	adapter, err := tessera.NewProjectAdapter(ctx, "p-witness-145", root, inv145TestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	witness := tessera.NewWitness()
	_ = witness.Delete()
	pub, err := witness.Generate()
	if err != nil {
		t.Fatalf("Witness.Generate: %v", err)
	}
	t.Cleanup(func() { _ = witness.Delete() })
	adapter.Attach(witness)

	payload := []byte(`{"partition_id":"2026_05","final_record_hash":"abc123","seal":"phase-9-inv-145"}`)

	leaf := tessera.Leaf{
		EventID:     "evt-inv145-1",
		EventType:   "audit.partition_sealed",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
		ProjectID:   "p-witness-145",
	}
	leafID, err := adapter.AppendLeaf(ctx, leaf)
	if err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}

	sig, err := adapter.WitnessCoSignSeal(ctx, leafID, payload)
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("inv-zen-145: WitnessCoSignSeal returned zero-length signature")
	}

	// (a) Signature MUST verify against sha256(payload). If this
	// fails, WitnessCoSignSeal is signing something OTHER than the
	// sha256 digest of the supplied payload — either a different
	// preimage or a different hash function — and inv-zen-145 is
	// violated.
	digest := sha256.Sum256(payload)
	if !tessera.VerifyWithPubkey(pub, digest[:], sig) {
		t.Errorf("inv-zen-145: signature does NOT verify against sha256(payload); hash-then-sign discipline broken")
	}

	// (b) Signature MUST NOT verify against the un-hashed payload
	// bytes. If this succeeds, WitnessCoSignSeal is signing raw
	// payload bytes — exactly the regression inv-zen-145 forbids.
	//
	// VerifyWithPubkey expects a 32-byte digest input; passing an
	// arbitrary-length payload normally returns false because ECDSA
	// is digest-based. We assert false explicitly so the failure
	// surfaces clearly if the implementation drifted to sign raw
	// bytes.
	if tessera.VerifyWithPubkey(pub, payload, sig) {
		t.Errorf("inv-zen-145: signature unexpectedly verifies against un-hashed payload; WitnessCoSignSeal may be signing raw bytes instead of sha256(payload)")
	}

	ok, err := adapter.VerifySealSignature(ctx, payload, sig)
	if err != nil {
		t.Fatalf("VerifySealSignature: %v", err)
	}
	if !ok {
		t.Error("inv-zen-145: VerifySealSignature returned false on a sig WitnessCoSignSeal just produced (verify-side hash-then-sign mismatch)")
	}
}
