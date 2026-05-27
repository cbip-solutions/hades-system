// SPDX-License-Identifier: MIT
package tessera

import (
	"context"
	"crypto/sha256"
	"fmt"
)

const pubkeyFingerprintHexLen = 16

// SignedSTH bundles a per-project STH (from Adapter.SubscribeSTH /
// the watcher in sth.go) with the daemon witness's ECDSA-ASN.1
// signature over STH.Digest() and a short opaque correlation token
// derived from the witness public key.
//
// Per invariant every entry in the daemon-global checkpoint log
// (A-7) MUST be a SignedSTH; there is NO unsigned variant in the
// public type system. The only public path to construct one is via
// CoSigner.Sign, so a caller cannot bypass the witness signing path
// short of forging the SignedSTH zero-value (which CheckpointSink
// implementations reject — memCheckpointSink.Append returns
// ErrUnsignedSTH on len(Signature)==0; the daemon-global Tessera
// log in A-7 will do the same at append time).
//
// PubkeyFingerprint is the leading 8 bytes of sha256(witness pubkey
// PEM) hex-encoded (16 chars). It is opaque — it does NOT reveal the
// pubkey itself — but it is stable across SignedSTHs produced by the
// same witness, so audit-chain verifier and
// doctor can correlate signatures to a witness-rotation generation
// without parsing the pubkey out of the rotation event payload.
type SignedSTH struct {
	STH               STH
	Signature         []byte
	PubkeyFingerprint string
}

type CheckpointSink interface {
	Append(ctx context.Context, signed SignedSTH) error
}

type CoSigner struct {
	witness *Witness
	sink    CheckpointSink
}

// NewCoSigner wires a CoSigner to the supplied witness + sink. The
// witness MUST already have a generated keypair before any Sign /
// OnSTH call; if not, those methods return ErrWitnessKeyMissing.
//
// We do not pre-load the witness here because (a) the daemon
// bootstraps the cosigner BEFORE running the doctor's witness check
// — the doctor surfaces the missing-key condition with operator-
// actionable instructions — and (b) failing in NewCoSigner would
// require the daemon to re-construct the cosigner on every doctor
// run, which would re-subscribe to every Adapter and silently double
// up STH publishes.
func NewCoSigner(w *Witness, sink CheckpointSink) *CoSigner {
	return &CoSigner{witness: w, sink: sink}
}

func (c *CoSigner) Sign(ctx context.Context, sth STH) (SignedSTH, error) {
	digest := sth.Digest()
	sig, err := c.witness.Sign(digest[:])
	if err != nil {
		return SignedSTH{}, err
	}
	pem, err := c.witness.PubkeyPEM()
	if err != nil {
		return SignedSTH{}, fmt.Errorf("tessera: cosigner pubkey: %w", err)
	}
	fp := pubkeyFingerprint(pem)
	return SignedSTH{
		STH:               sth,
		Signature:         sig,
		PubkeyFingerprint: fp,
	}, nil
}

func (c *CoSigner) OnSTH(ctx context.Context, sth STH) error {
	signed, err := c.Sign(ctx, sth)
	if err != nil {
		return err
	}
	if len(signed.Signature) == 0 {

		return ErrUnsignedSTH
	}
	return c.sink.Append(ctx, signed)
}

func (c *CoSigner) SubscribeAdapter(a *Adapter) error {
	return a.SubscribeSTH(c)
}

func pubkeyFingerprint(pem []byte) string {
	sum := sha256.Sum256(pem)
	const hex = "0123456789abcdef"
	out := make([]byte, pubkeyFingerprintHexLen)
	for i := 0; i < pubkeyFingerprintHexLen/2; i++ {
		out[i*2] = hex[sum[i]>>4]
		out[i*2+1] = hex[sum[i]&0x0f]
	}
	return string(out)
}
