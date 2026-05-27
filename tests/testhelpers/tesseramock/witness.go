// SPDX-License-Identifier: MIT
//
// Stand-in for internal/audit/tessera.Witness (which uses macOS Keychain in
// production). MockWitness produces real ECDSA-P256 signatures verifiable
// with the same primitives as production. Boundary: this file lives in
// tests/testhelpers/tesseramock/ — testhelpers may import internal/* freely
// .
package tesseramock

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

var genECDSAKey = ecdsa.GenerateKey

type MockWitness struct {
	priv *ecdsa.PrivateKey
}

func NewMockWitness() (*MockWitness, error) {
	k, err := genECDSAKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &MockWitness{priv: k}, nil
}

// Sign returns an ASN.1-encoded ECDSA signature over digest using the
// underlying P-256 private key. digest MUST already be a hash output
// (callers hash payload first; mirrors the production hash-then-sign
// pattern enforced by invariant).
func (w *MockWitness) Sign(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, w.priv, digest)
}

func (w *MockWitness) Load() (*ecdsa.PublicKey, error) {
	return &w.priv.PublicKey, nil
}

func VerifyWithPubkey(pub *ecdsa.PublicKey, digest, sig []byte) bool {
	if pub == nil || len(sig) == 0 {
		return false
	}
	return ecdsa.VerifyASN1(pub, digest, sig)
}
