package tessera

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
)

type faultBackend struct {
	loadKey   *ecdsa.PrivateKey
	loadErr   error
	storeErr  error
	deleteErr error
}

func (f *faultBackend) Load() (*ecdsa.PrivateKey, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.loadKey != nil {
		return f.loadKey, nil
	}
	return nil, ErrWitnessKeyMissing
}

func (f *faultBackend) Store(priv *ecdsa.PrivateKey) error {
	if f.storeErr != nil {
		return f.storeErr
	}
	f.loadKey = priv
	return nil
}

func (f *faultBackend) Delete() error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.loadKey = nil
	return nil
}

func TestRotationProducesOverlapSignatures(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	res, err := rot.Rotate(context.Background(), "scheduled cadence")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if res.OldPubkey == nil {
		t.Error("RotationResult.OldPubkey is nil")
	}
	if res.NewPubkey == nil {
		t.Error("RotationResult.NewPubkey is nil")
	}
	if res.OldPubkey.Equal(res.NewPubkey) {
		t.Error("OldPubkey == NewPubkey (rotation did not actually swap)")
	}
	if len(res.OldSignature) == 0 {
		t.Error("OldSignature empty (overlap signature 1 missing)")
	}
	if len(res.NewSignature) == 0 {
		t.Error("NewSignature empty (overlap signature 2 missing)")
	}
	if res.Reason != "scheduled cadence" {
		t.Errorf("Reason = %q, want \"scheduled cadence\"", res.Reason)
	}

	if !ecdsa.VerifyASN1(res.OldPubkey, res.TransitionDigest[:], res.OldSignature) {
		t.Error("OldSignature does not verify with OldPubkey")
	}
	if !ecdsa.VerifyASN1(res.NewPubkey, res.TransitionDigest[:], res.NewSignature) {
		t.Error("NewSignature does not verify with NewPubkey")
	}
}

func TestRotationLoadAfterReturnsNewKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	old, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	if _, err := rot.Rotate(context.Background(), "test"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	got, err := w.Load()
	if err != nil {
		t.Fatalf("Load after rotate: %v", err)
	}
	if old.Equal(got) {
		t.Error("witness still holds OLD key after rotation")
	}
}

func TestRotationRequiresReason(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err := rot.Rotate(context.Background(), "")
	if err == nil {
		t.Fatal("Rotate accepted empty reason; want refusal (operator-attributable audit trail)")
	}
}

func TestRotationRefusesIfNoExistingKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err := rot.Rotate(context.Background(), "test")
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("Rotate without OLD key: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestRevokeAndRotateSkipsOldSignature(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	res, err := rot.RevokeAndRotate(context.Background(), "OLD compromised")
	if err != nil {
		t.Fatalf("RevokeAndRotate: %v", err)
	}
	if !res.Compromised {
		t.Error("RotationResult.Compromised flag not set")
	}
	if len(res.OldSignature) != 0 {
		t.Error("RevokeAndRotate produced OldSignature (OLD key was compromised)")
	}
	if len(res.NewSignature) == 0 {
		t.Error("RevokeAndRotate did not produce NewSignature")
	}
}

func TestRotationIsAtomicOnFailure(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	old, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Construct a rotation that points at an already-closed sink so
	// the final Append step fails. The witness key state MUST roll
	// forward (post-rotation NEW key) regardless because the OLD key
	// was deleted before the Append; we surface the failure but do
	// not roll back the swap (which would leave us with no usable
	// witness at all). The test asserts the failure path returns
	// the error so the caller can re-attempt the Append (the
	// transition is NEW-key-signed and recoverable).
	cp, _ := newTempCheckpoint(t)
	if err := cp.Close(); err != nil {
		t.Fatalf("pre-close cp: %v", err)
	}
	rot := NewRotation(w, cp)
	res, err := rot.Rotate(context.Background(), "test")
	if err == nil {
		t.Fatal("Rotate against closed cp: want error, got nil")
	}

	got, err := w.Load()
	if err != nil {
		t.Fatalf("Load post-failure: %v", err)
	}
	if old.Equal(got) {
		t.Error("witness still holds OLD key after failed Rotate (rotation should be forward-only)")
	}
	if res.NewPubkey == nil || !res.NewPubkey.Equal(got) {
		t.Error("RotationResult.NewPubkey != live witness key")
	}
}

// -----------------------------------------------------------------
// Coverage tests beyond the 6 plan-mandated. Per the project doctrine
// (≥95% on rotation.go for bar; ≥90% for security-critical
// files; no tech debt) every branch in rotation.go below must be
// exercised. Each block cites the branch + rationale.
// -----------------------------------------------------------------

func TestRevokeAndRotateRequiresReason(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err := rot.RevokeAndRotate(context.Background(), "")
	if err == nil {
		t.Fatal("RevokeAndRotate accepted empty reason; want refusal (operator-attributable audit trail)")
	}
}

func TestRevokeAndRotateRefusesIfNoExistingKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err := rot.RevokeAndRotate(context.Background(), "test")
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("RevokeAndRotate without OLD key: want ErrWitnessKeyMissing, got %v", err)
	}
}

// TestRevokeAndRotateAtomicOnFailure mirrors TestRotationIsAtomicOnFailure
// for the compromise-response path: Append fails (closed cp), but the
// witness MUST already hold the NEW key (forward-only). Coverage gap:
// the Append-error branch in RevokeAndRotate.
func TestRevokeAndRotateAtomicOnFailure(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	old, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	if err := cp.Close(); err != nil {
		t.Fatalf("pre-close cp: %v", err)
	}
	rot := NewRotation(w, cp)
	res, err := rot.RevokeAndRotate(context.Background(), "OLD compromised")
	if err == nil {
		t.Fatal("RevokeAndRotate against closed cp: want error, got nil")
	}
	got, err := w.Load()
	if err != nil {
		t.Fatalf("Load post-failure: %v", err)
	}
	if old.Equal(got) {
		t.Error("witness still holds OLD key after failed RevokeAndRotate (forward-only invariant violated)")
	}
	if !res.Compromised {
		t.Error("RotationResult.Compromised flag not set on RevokeAndRotate failure path")
	}
	if res.NewPubkey == nil || !res.NewPubkey.Equal(got) {
		t.Error("RotationResult.NewPubkey != live witness key")
	}
}

func TestRotateOldSignFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated OLD load failure")
	w.backend = &faultBackend{loadKey: priv}

	if _, err := w.Load(); err != nil {
		t.Fatalf("pre-Rotate Load: %v", err)
	}

	w.backend = &faultBackend{loadErr: sentinel}
	rot := NewRotation(w, cp)
	_, err = rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with failing OLD load: want error, got nil")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRotateOldSignErrorAfterLoadOK(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated OLD sign failure")
	fb := &flipBackend{
		loadResults: []loadResult{
			{key: priv},
			{err: sentinel},
		},
	}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with failing OLD sign: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRotateDeleteFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated delete failure")
	fb := &faultBackend{loadKey: priv, deleteErr: sentinel}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with Delete failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRotateInstallFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated store failure")
	fb := &faultBackend{loadKey: priv, storeErr: sentinel}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with install failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRotateNewSignErrorPopulatesNewPubkey(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated NEW sign failure")
	fb := &flipBackend{
		loadResults: []loadResult{
			{key: priv},
			{key: priv},
			{err: sentinel},
		},
	}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	res, err := rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with NEW sign failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
	if res.NewPubkey == nil {
		t.Error("forward-only invariant: RotationResult.NewPubkey must be populated on NEW-sign failure")
	}
	if len(res.OldSignature) == 0 {
		t.Error("forward-only invariant: RotationResult.OldSignature must be populated on NEW-sign failure")
	}
}

func TestRevokeAndRotateDeleteFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated delete failure on revoke")
	fb := &faultBackend{loadKey: priv, deleteErr: sentinel}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.RevokeAndRotate(context.Background(), "compromised")
	if err == nil {
		t.Fatal("RevokeAndRotate with Delete failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRevokeAndRotateInstallFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated store failure on revoke")
	fb := &faultBackend{loadKey: priv, storeErr: sentinel}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.RevokeAndRotate(context.Background(), "compromised")
	if err == nil {
		t.Fatal("RevokeAndRotate with install failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestRevokeAndRotateNewSignErrorPopulatesNewPubkey(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated NEW sign failure on revoke")
	fb := &flipBackend{
		loadResults: []loadResult{
			{key: priv},
			{err: sentinel},
		},
	}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	res, err := rot.RevokeAndRotate(context.Background(), "compromised")
	if err == nil {
		t.Fatal("RevokeAndRotate with NEW sign failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
	if res.NewPubkey == nil {
		t.Error("forward-only invariant: RotationResult.NewPubkey must be populated on NEW-sign failure")
	}
	if !res.Compromised {
		t.Error("RotationResult.Compromised flag must be true on RevokeAndRotate NEW-sign failure")
	}
}

func TestAppendTransitionLeafPubkeyPEMFailurePropagates(t *testing.T) {
	withTestKeychain(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sentinel := errors.New("simulated pubkeyPEM load failure")

	fb := &flipBackend{
		loadResults: []loadResult{
			{key: priv},
			{key: priv},
			{key: priv},
			{err: sentinel},
		},
	}
	w := &Witness{backend: fb}
	cp, _ := newTempCheckpoint(t)
	rot := NewRotation(w, cp)
	_, err = rot.Rotate(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("Rotate with PEM-load failure: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err missing sentinel chain: %v", err)
	}
}

func TestMarshalPubkeyHandlesNil(t *testing.T) {
	out := marshalPubkey(nil)
	if out != nil {
		t.Errorf("marshalPubkey(nil) = %v, want nil", out)
	}
}

func TestNewKeyPanicsOnRandFailure(t *testing.T) {
	prev := ecdsaGenerate
	t.Cleanup(func() { ecdsaGenerate = prev })
	sentinel := errors.New("simulated rand.Read failure")
	ecdsaGenerate = func() (*ecdsa.PrivateKey, error) {
		return nil, sentinel
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("newKey did not panic on simulated rand failure")
		}

		s, ok := r.(string)
		if !ok {
			t.Fatalf("newKey panic value not a string: %T %v", r, r)
		}
		if !errorContains(s, sentinel.Error()) {
			t.Errorf("newKey panic message does not contain underlying error: %q", s)
		}
	}()
	_ = newKey()
}

func errorContains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestErrRotationNoOldKeyIsDeclared(t *testing.T) {
	if ErrRotationNoOldKey == nil {
		t.Fatal("ErrRotationNoOldKey is nil")
	}
	if ErrRotationNoOldKey.Error() == "" {
		t.Fatal("ErrRotationNoOldKey has empty message")
	}
}

type loadResult struct {
	key *ecdsa.PrivateKey
	err error
}

type flipBackend struct {
	loadResults []loadResult
	loadCalls   int
	storedKey   *ecdsa.PrivateKey
}

func (f *flipBackend) Load() (*ecdsa.PrivateKey, error) {
	if f.loadCalls < len(f.loadResults) {
		r := f.loadResults[f.loadCalls]
		f.loadCalls++
		if r.err != nil {
			return nil, r.err
		}
		return r.key, nil
	}

	if f.storedKey != nil {
		return f.storedKey, nil
	}
	return nil, ErrWitnessKeyMissing
}

func (f *flipBackend) Store(priv *ecdsa.PrivateKey) error {
	f.storedKey = priv
	return nil
}

func (f *flipBackend) Delete() error {
	f.storedKey = nil
	return nil
}
