// SPDX-License-Identifier: MIT
package tessera

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

type RotationResult struct {
	OldPubkey        *ecdsa.PublicKey
	NewPubkey        *ecdsa.PublicKey
	TransitionDigest [sha256.Size]byte
	OldSignature     []byte
	NewSignature     []byte
	Reason           string
	Compromised      bool
	Timestamp        time.Time
}

// Rotation orchestrates witness key rotation. Holds references to
// the Witness (to swap keys) and the Checkpoint (to durably anchor
// the rotation transition).
//
// Per spec § 7.4 the rotation cadence is doctrine-driven (max-scope
// 90 days; default 365; capa-firewall 90 + operator-triggered).
// wires the rotation primitive only; wires the
// scheduler + the `zen audit witness rotate --reason` CLI command.
//
// Concurrency a Rotation value is single-shot from the caller's
// perspective — callers MUST NOT invoke Rotate / RevokeAndRotate
// concurrently on the same Rotation. The underlying Witness's mutex
// serializes the individual sign / install / delete steps but the
// rotation flow as a whole is non-atomic across those steps (OLD
// signature → Delete → install NEW → NEW signature → Append). A
// concurrent second rotation would observe an inconsistent witness
// state and either fail fast on Sign (mid-Delete) or install over
// itself. scheduler runs at most one rotation in flight.
type Rotation struct {
	witness *Witness
	cp      *Checkpoint
}

func NewRotation(w *Witness, cp *Checkpoint) *Rotation {
	return &Rotation{witness: w, cp: cp}
}

// Rotate performs a scheduled rotation: signs the transition digest
// with both OLD and NEW keys (overlap), persists the rotation leaf
// to the daemon-global checkpoint log, and updates the witness to
// hold the NEW key. The reason MUST be non-empty (operator-attributable
// doctrine `--reason` mandatory).
//
// Flow
//
// 1. Validate reason (non-empty).
// 2. Snapshot the OLD pubkey via Witness.Load.
// 3. Pre-generate the NEW keypair off-witness so the transition
// digest can be computed before the OLD key is retired.
// 4. Compute transitionDigest(old, new, reason).
// 5. Sign the digest with the OLD key (overlap signature 1).
// 6. Witness.Delete (retire OLD).
// 7. Witness.installKey(NEW) (install pre-generated NEW key).
// 8. Sign the digest with the NEW key (overlap signature 2).
// 9. appendTransitionLeaf (synthetic SignedSTH using NEW signature).
//
// Forward-only failure semantics: if step 9 (Append) fails, the
// witness still holds the NEW key (steps 6+7 already executed). The
// caller observes the error and may retry the Append with the same
// RotationResult — the transition leaf format embeds the NEW pubkey
// fingerprint so downstream verifiers can reconstruct the chain
// even with a delayed Append.
//
// Errors
// - Reason validation: bare error ("rotation reason must be non-empty").
// - ErrWitnessKeyMissing if no OLD key was persisted.
// - Wrapped errors from Witness.Sign / Delete / installKey if any
// of the intermediate steps fail (these leave the witness in a
// half-rotated state — see TestRotationIsAtomicOnFailure for the
// forward-only contract).
// - Wrapped Append error from the final checkpoint write (the
// witness is already swapped forward; caller may retry).
func (r *Rotation) Rotate(ctx context.Context, reason string) (RotationResult, error) {
	if reason == "" {
		return RotationResult{}, fmt.Errorf("tessera: rotation reason must be non-empty")
	}
	old, err := r.witness.Load()
	if err != nil {
		return RotationResult{}, err
	}

	tmp := newKey()
	newPub := &tmp.PublicKey
	transition := transitionDigest(old, newPub, reason)

	oldSig, err := r.witness.Sign(transition[:])
	if err != nil {
		return RotationResult{}, fmt.Errorf("tessera: rotate OLD sign: %w", err)
	}

	if err := r.witness.Delete(); err != nil {
		return RotationResult{}, fmt.Errorf("tessera: rotate delete OLD: %w", err)
	}
	if err := r.witness.installKey(tmp); err != nil {
		return RotationResult{}, fmt.Errorf("tessera: rotate install NEW: %w", err)
	}

	newSig, err := r.witness.Sign(transition[:])
	if err != nil {

		return RotationResult{
			OldPubkey:        old,
			NewPubkey:        newPub,
			TransitionDigest: transition,
			OldSignature:     oldSig,
			Reason:           reason,
			Timestamp:        time.Now().UTC(),
		}, fmt.Errorf("tessera: rotate NEW sign: %w", err)
	}

	res := RotationResult{
		OldPubkey:        old,
		NewPubkey:        newPub,
		TransitionDigest: transition,
		OldSignature:     oldSig,
		NewSignature:     newSig,
		Reason:           reason,
		Timestamp:        time.Now().UTC(),
	}
	if err := r.appendTransitionLeaf(ctx, res); err != nil {
		return res, fmt.Errorf("tessera: rotate append transition: %w", err)
	}
	return res, nil
}

func (r *Rotation) RevokeAndRotate(ctx context.Context, reason string) (RotationResult, error) {
	if reason == "" {
		return RotationResult{}, fmt.Errorf("tessera: rotation reason must be non-empty")
	}
	old, err := r.witness.Load()
	if err != nil {
		return RotationResult{}, err
	}
	tmp := newKey()
	newPub := &tmp.PublicKey
	transition := transitionDigest(old, newPub, reason)
	if err := r.witness.Delete(); err != nil {
		return RotationResult{}, fmt.Errorf("tessera: revoke delete OLD: %w", err)
	}
	if err := r.witness.installKey(tmp); err != nil {
		return RotationResult{}, fmt.Errorf("tessera: revoke install NEW: %w", err)
	}
	newSig, err := r.witness.Sign(transition[:])
	if err != nil {
		return RotationResult{
			OldPubkey:        old,
			NewPubkey:        newPub,
			TransitionDigest: transition,
			Reason:           reason,
			Compromised:      true,
			Timestamp:        time.Now().UTC(),
		}, fmt.Errorf("tessera: revoke NEW sign: %w", err)
	}
	res := RotationResult{
		OldPubkey:        old,
		NewPubkey:        newPub,
		TransitionDigest: transition,
		OldSignature:     nil,
		NewSignature:     newSig,
		Reason:           reason,
		Compromised:      true,
		Timestamp:        time.Now().UTC(),
	}
	if err := r.appendTransitionLeaf(ctx, res); err != nil {
		return res, fmt.Errorf("tessera: revoke append transition: %w", err)
	}
	return res, nil
}

func (r *Rotation) appendTransitionLeaf(ctx context.Context, res RotationResult) error {
	syntheticSTH := STH{
		ProjectID: "__rotation__:" + res.Reason,
		Size:      0,
		RootHash:  res.TransitionDigest[:],
		Timestamp: res.Timestamp,
	}
	newPEM, err := r.witness.PubkeyPEM()
	if err != nil {
		return fmt.Errorf("tessera: rotate get new pubkey PEM: %w", err)
	}
	signed := SignedSTH{
		STH:               syntheticSTH,
		Signature:         res.NewSignature,
		PubkeyFingerprint: pubkeyFingerprint(newPEM),
	}
	return r.cp.Append(ctx, signed)
}

func transitionDigest(old, fresh *ecdsa.PublicKey, reason string) [sha256.Size]byte {
	var buf bytes.Buffer
	buf.WriteString("zen-swarm-tessera-rotation-v1\x00")
	buf.Write(marshalPubkey(old))
	buf.WriteByte(0)
	buf.Write(marshalPubkey(fresh))
	buf.WriteByte(0)
	buf.WriteString(reason)
	buf.WriteByte(0)
	return sha256.Sum256(buf.Bytes())
}

func marshalPubkey(pub *ecdsa.PublicKey) []byte {
	if pub == nil {
		return nil
	}
	xb := pub.X.Bytes()
	yb := pub.Y.Bytes()
	out := make([]byte, 0, 8+len(xb)+len(yb))
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(xb)))
	out = append(out, lenBuf[:]...)
	out = append(out, xb...)
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(yb)))
	out = append(out, lenBuf[:]...)
	out = append(out, yb...)
	return out
}

var ecdsaGenerate = func() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func newKey() *ecdsa.PrivateKey {
	priv, err := ecdsaGenerate()
	if err != nil {
		panic(fmt.Sprintf("tessera: ecdsa.GenerateKey: %v", err))
	}
	return priv
}

var ErrRotationNoOldKey = errors.New("tessera: rotation requires an existing OLD key")
