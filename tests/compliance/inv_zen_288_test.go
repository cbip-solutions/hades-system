// tests/compliance/inv_zen_288_test.go
//
// Compliance gate for invariant (v0.20.6 fix #1): the
// tests/testhelpers/tesseramock package indirects its ECDSA keygen
// primitive through an unexported package-level variable
// `genECDSAKey = ecdsa.GenerateKey` so the internal test package can
// swap it to exercise the keygen-error branch of NewMockWitness.
//
// Why this gate exists: since Go 1.26, crypto/ecdsa.GenerateKey ignores
// its rand.Reader argument and pulls entropy from an internal
// FIPS-validated source (see https://pkg.go.dev/crypto/ecdsa#GenerateKey).
// The pre-Go-1.26 idiom of swapping crypto/rand.Reader with a failing
// reader no longer triggers a keygen error — the supplied Reader is
// dropped on the floor unless GODEBUG=cryptocustomrand=1, which is
// itself slated for removal. The Go documentation steers callers
// toward testing/cryptotest.SetGlobalRandom, but that primitive sets a
// DETERMINISTIC source (uint64 seed) and provides no way to force a
// keygen failure for negative-path testing.
//
// The indirection pattern (`var genECDSAKey = ecdsa.GenerateKey`) is
// the canonical Go-1.26+ alternative — production callers go through
// ecdsa.GenerateKey as before; the internal test
// witness_internal_test.go swaps genECDSAKey to observe the error path.
//
// Anchor 1 (positive): tests/testhelpers/tesseramock/witness.go MUST
// contain the literal `var genECDSAKey = ecdsa.GenerateKey`. A
// regression that removes the indirection (e.g. via copy-paste from
// older Go-1.25 idiom code) trips the gate immediately.
//
// Anchor 2 (positive): tests/testhelpers/tesseramock/witness.go MUST
// contain a call to `genECDSAKey(` (proves NewMockWitness uses the
// indirection rather than referencing it but calling ecdsa.GenerateKey
// directly anyway).
//
// Anchor 3 (negative): tests/testhelpers/tesseramock/witness.go MUST
// NOT contain a direct call to `ecdsa.GenerateKey(` (no parens —
// "ecdsa.GenerateKey" without parens is fine for the indirection
// initialiser). A regression that inlines the call back trips this
// gate.
//
// Sister-test bite check: revert the indirection (replace
// `genECDSAKey(elliptic.P256(), rand.Reader)` with
// `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)`) — Anchor 2 + 3
// both fail. Remove the `var genECDSAKey = ecdsa.GenerateKey` line
// without changing the call site — the internal test fails to compile
// AND Anchor 1 trips. Both behaviours were verified at v0.20.6 ship
// time.
//
// invariant (v0.20.6 fix #1).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inv288WitnessPath = "tests/testhelpers/tesseramock/witness.go"

func TestInvZen288_GenECDSAKeyIndirectionExists(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv288WitnessPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv288WitnessPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "var genECDSAKey = ecdsa.GenerateKey"
	if !strings.Contains(src, required) {
		t.Errorf("inv-zen-288 violated: %s does not contain the literal %q. The genECDSAKey indirection is what enables the internal test package to swap the keygen primitive to exercise the error-return branch of NewMockWitness; without it, the Go-1.26+ ecdsa.GenerateKey ignores its rand.Reader argument and the error path is untestable.", inv288WitnessPath, required)
	}
}

func TestInvZen288_NewMockWitnessUsesIndirection(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv288WitnessPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv288WitnessPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "genECDSAKey("
	if !strings.Contains(src, required) {
		t.Errorf("inv-zen-288 violated: %s does not contain a call to %q. NewMockWitness must invoke the genECDSAKey indirection (not ecdsa.GenerateKey directly) so the internal test can swap the keygen primitive.", inv288WitnessPath, required)
	}
}

func TestInvZen288_NoDirectEcdsaGenerateKeyCall(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv288WitnessPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv288WitnessPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	forbidden := "ecdsa.GenerateKey("
	if strings.Contains(src, forbidden) {
		t.Errorf("inv-zen-288 violated: %s contains the forbidden direct call %q. The Go-1.26+ keygen-error branch is only testable via the genECDSAKey indirection — inlining ecdsa.GenerateKey() back makes the error path unreachable in tests.", inv288WitnessPath, forbidden)
	}
}
