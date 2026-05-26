package tesseramock_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/cbip-solutions/hades-system/tests/testhelpers/tesseramock"
)

func TestMockWitness_SignVerifyRoundtrip(t *testing.T) {
	t.Parallel()
	w, err := tesseramock.NewMockWitness()
	if err != nil {
		t.Fatalf("NewMockWitness: %v", err)
	}
	digest := sha256.Sum256([]byte("payload-bytes"))
	sig, err := w.Sign(digest[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatalf("empty signature")
	}
	pub, err := w.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pub == nil {
		t.Fatal("Load returned nil pub")
	}
	if !tesseramock.VerifyWithPubkey(pub, digest[:], sig) {
		t.Errorf("verify roundtrip failed")
	}
}

func TestMockWitness_VerifyRejectsTamperedDigest(t *testing.T) {
	t.Parallel()
	w, _ := tesseramock.NewMockWitness()
	good := sha256.Sum256([]byte("good-payload"))
	sig, _ := w.Sign(good[:])
	bad := sha256.Sum256([]byte("bad-payload"))
	pub, _ := w.Load()
	if tesseramock.VerifyWithPubkey(pub, bad[:], sig) {
		t.Errorf("verify accepted tampered digest")
	}
}

func TestMockWitness_VerifyRejectsTamperedSignature(t *testing.T) {
	t.Parallel()
	w, _ := tesseramock.NewMockWitness()
	digest := sha256.Sum256([]byte("payload"))
	sig, _ := w.Sign(digest[:])
	corrupted := bytes.Clone(sig)
	corrupted[0] ^= 0xff
	pub, _ := w.Load()
	if tesseramock.VerifyWithPubkey(pub, digest[:], corrupted) {
		t.Errorf("verify accepted tampered signature")
	}
}

func TestMockWitness_DistinctKeypairsProduceDistinctSigs(t *testing.T) {
	t.Parallel()
	w1, _ := tesseramock.NewMockWitness()
	w2, _ := tesseramock.NewMockWitness()
	digest := sha256.Sum256([]byte("payload"))
	s1, _ := w1.Sign(digest[:])
	s2, _ := w2.Sign(digest[:])
	if bytes.Equal(s1, s2) {
		t.Errorf("two fresh witnesses produced identical signatures (impossible for ECDSA)")
	}
	pub1, _ := w1.Load()
	if tesseramock.VerifyWithPubkey(pub1, digest[:], s2) {
		t.Errorf("w1.pub verified w2.sig — pub/key cross-binding broken")
	}
}
