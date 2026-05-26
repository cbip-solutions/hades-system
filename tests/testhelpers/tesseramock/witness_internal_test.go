package tesseramock

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"io"
	"testing"
)

var errForcedKeygenFailure = errors.New("tesseramock: forced keygen failure for test")

func TestMockWitness_NewMockWitnessKeygenError(t *testing.T) {
	saved := genECDSAKey
	t.Cleanup(func() { genECDSAKey = saved })
	genECDSAKey = func(c elliptic.Curve, r io.Reader) (*ecdsa.PrivateKey, error) {
		return nil, errForcedKeygenFailure
	}
	w, err := NewMockWitness()
	if !errors.Is(err, errForcedKeygenFailure) {
		t.Errorf("expected errForcedKeygenFailure, got %v", err)
	}
	if w != nil {
		t.Errorf("expected nil witness on error path, got %v", w)
	}
}
