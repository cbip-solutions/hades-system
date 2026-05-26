package tessera

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"testing"
)

func withTestKeychain(t *testing.T) {
	t.Helper()
	old := os.Getenv("ZEN_BYPASS_DISABLE_KEYCHAIN")
	os.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	resetTestWitnessKeychain()
	t.Cleanup(func() {
		os.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", old)
		resetTestWitnessKeychain()
	})
}

func TestWitnessLoadReturnsErrWhenKeyMissing(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	_, err := w.Load()
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("Load on empty backend: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestWitnessGenerateBootstrapsKeypair(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if pub == nil {
		t.Fatal("Generate returned nil pubkey")
	}
	if pub.Curve != elliptic.P256() {
		t.Errorf("expected P-256 curve, got %v", pub.Curve)
	}
}

func TestWitnessGenerateRefusesIfKeyExists(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	_, err := w.Generate()
	if !errors.Is(err, ErrWitnessKeyAlreadyExists) {
		t.Fatalf("second Generate: want ErrWitnessKeyAlreadyExists, got %v", err)
	}
}

func TestWitnessLoadAfterGenerateReturnsKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub1, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pub2, err := w.Load()
	if err != nil {
		t.Fatalf("Load after Generate: %v", err)
	}
	if !pub1.Equal(pub2) {
		t.Error("Load returned a different pubkey than Generate")
	}
}

func TestWitnessSignProducesVerifiableSignature(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	digest := sha256.Sum256([]byte("test STH bytes"))
	sig, err := w.Sign(digest[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("Sign returned empty signature")
	}
	pub, err := w.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Error("VerifyASN1 rejected our own signature")
	}
}

func TestWitnessSignReturnsErrWhenKeyMissing(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	digest := sha256.Sum256([]byte("test"))
	_, err := w.Sign(digest[:])
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("Sign on empty backend: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestWitnessPubkeyPEMRoundTrip(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub1, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pem, err := w.PubkeyPEM()
	if err != nil {
		t.Fatalf("PubkeyPEM: %v", err)
	}
	if len(pem) == 0 {
		t.Fatal("PubkeyPEM returned empty bytes")
	}
	pub2, err := ParsePubkeyPEM(pem)
	if err != nil {
		t.Fatalf("ParsePubkeyPEM: %v", err)
	}
	if !pub1.Equal(pub2) {
		t.Error("PEM round-trip changed pubkey")
	}
}

func TestWitnessDeleteRemovesKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := w.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := w.Load()
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("Load after Delete: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestVerifyExternalSignatureUsesProvidedPubkey(t *testing.T) {

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	digest := sha256.Sum256([]byte("external"))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}
	if !VerifyWithPubkey(&priv.PublicKey, digest[:], sig) {
		t.Error("VerifyWithPubkey rejected a valid external signature")
	}
}

func TestVerifyWithPubkeyRejectsCorruptSignature(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	digest := sha256.Sum256([]byte("data"))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}

	if len(sig) >= 4 {
		sig[2] ^= 0xff
	}
	if VerifyWithPubkey(&priv.PublicKey, digest[:], sig) {
		t.Error("VerifyWithPubkey accepted corrupted signature")
	}
}

func TestParsePubkeyPEMRejectsMalformedPEM(t *testing.T) {
	_, err := ParsePubkeyPEM([]byte("not a pem block"))
	if err == nil {
		t.Fatal("ParsePubkeyPEM on garbage: want error, got nil")
	}
}

func TestParsePubkeyPEMRejectsMalformedPKIX(t *testing.T) {

	bad := []byte("-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n")
	_, err := ParsePubkeyPEM(bad)
	if err == nil {
		t.Fatal("ParsePubkeyPEM on bad PKIX bytes: want error, got nil")
	}
}

func TestParsePubkeyPEMRejectsNonP256Curve(t *testing.T) {

	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey P-384: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	_, err = ParsePubkeyPEM(pemBytes)
	if err == nil {
		t.Fatal("ParsePubkeyPEM on P-384 pubkey: want curve-rejection error, got nil")
	}
}

func TestParsePubkeyPEMRejectsNonECDSA(t *testing.T) {

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	_, err = ParsePubkeyPEM(pemBytes)
	if err == nil {
		t.Fatal("ParsePubkeyPEM on Ed25519 pubkey: want type-rejection error, got nil")
	}
}

func TestVerifyWithPubkeyRejectsNil(t *testing.T) {
	if VerifyWithPubkey(nil, []byte("x"), []byte("y")) {
		t.Error("VerifyWithPubkey(nil) returned true")
	}
}

func TestVerifyWithPubkeyRejectsNonP256(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey P-384: %v", err)
	}
	digest := sha256.Sum256([]byte("data"))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}

	if VerifyWithPubkey(&priv.PublicKey, digest[:], sig) {
		t.Error("VerifyWithPubkey accepted P-384 pubkey")
	}
}

func TestWitnessPubkeyPEMReturnsErrWhenKeyMissing(t *testing.T) {

	withTestKeychain(t)
	w := NewWitness()
	_, err := w.PubkeyPEM()
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("PubkeyPEM on empty backend: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestMemBackendStoreRejectsDuplicate(t *testing.T) {
	withTestKeychain(t)
	priv1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey 1: %v", err)
	}
	priv2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey 2: %v", err)
	}
	if err := defaultMemWitnessBackend.Store(priv1); err != nil {
		t.Fatalf("first Store: %v", err)
	}
	err = defaultMemWitnessBackend.Store(priv2)
	if !errors.Is(err, ErrWitnessKeyAlreadyExists) {
		t.Fatalf("second Store: want ErrWitnessKeyAlreadyExists, got %v", err)
	}
}
