// SPDX-License-Identifier: MIT
package tessera

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
)

// Witness manages the daemon's ECDSA P-256 transparency-log witness
// keypair. per design contract, on darwin the private key resides en
// the macOS Keychain (kSecClassGenericPassword); on linux/non-darwin
// it lives en a 0600 file under ~/.config/hades-system/.
//
// Per invariant, the only externally observable signing surface is
// Sign(digest) → []byte; there is no unsigned pass-through. Per T2,
// the private key is never returned from any method (only the public
// key is); callers MUST go through Sign to use it.
type Witness struct {
	mu      sync.Mutex
	backend witnessBackend
}

func NewWitness() *Witness {
	return &Witness{backend: defaultWitnessBackend()}
}

func (w *Witness) Generate() (*ecdsa.PublicKey, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.backend.Load(); err == nil {
		return nil, ErrWitnessKeyAlreadyExists
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tessera: generate keypair: %w", err)
	}
	if err := w.backend.Store(priv); err != nil {
		return nil, fmt.Errorf("tessera: persist keypair: %w", err)
	}
	pub := priv.PublicKey
	return &pub, nil
}

func (w *Witness) Load() (*ecdsa.PublicKey, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	priv, err := w.backend.Load()
	if err != nil {
		return nil, err
	}
	pub := priv.PublicKey
	return &pub, nil
}

// Sign returns an ASN.1-encoded ECDSA signature over the supplied
// digest (caller MUST hash STH bytes; this method does not hash).
// ErrWitnessKeyMissing if no key persisted.
func (w *Witness) Sign(digest []byte) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	priv, err := w.backend.Load()
	if err != nil {
		return nil, err
	}
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest)
	if err != nil {
		return nil, fmt.Errorf("tessera: ECDSA sign: %w", err)
	}
	return sig, nil
}

func (w *Witness) PubkeyPEM() ([]byte, error) {
	pub, err := w.Load()
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("tessera: marshal pubkey: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func (w *Witness) Delete() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.backend.Delete()
}

// installKey overwrites the persisted keypair under the witness mutex.
// Used ONLY by the rotation flow (rotation.go) to install a NEW key
// pre-generated off-witness so the rotation digest can be computed
// before the OLD key is deleted (the OLD key MUST sign the transition
// before being retired). Callers MUST have already invoked Delete to
// clear the previous slot — otherwise the underlying backend's Store
// returns ErrWitnessKeyAlreadyExists.
//
// Lower-case (non-exported) on purpose: this is a same-package helper
// for the rotation primitive, not a public surface — direct external
// callers would bypass the OLD-key signing step that the rotation
// contract requires.
func (w *Witness) installKey(priv *ecdsa.PrivateKey) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.backend.Store(priv)
}

type witnessBackend interface {
	Load() (*ecdsa.PrivateKey, error)
	Store(priv *ecdsa.PrivateKey) error
	Delete() error
}

func ParsePubkeyPEM(data []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("tessera: PEM decode failed")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("tessera: parse PKIX: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("tessera: not ECDSA pubkey: %T", pub)
	}
	if ecPub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("tessera: not P-256 curve: %v", ecPub.Curve)
	}
	return ecPub, nil
}

func VerifyWithPubkey(pub *ecdsa.PublicKey, digest, sig []byte) bool {
	if pub == nil || pub.Curve != elliptic.P256() {
		return false
	}
	return ecdsa.VerifyASN1(pub, digest, sig)
}
