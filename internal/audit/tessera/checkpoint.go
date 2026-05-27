// SPDX-License-Identifier: MIT
package tessera

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	tessera "github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/api/layout"
	tesseraclient "github.com/transparency-dev/tessera/client"
)

// ErrCheckpointNotFound is returned by Checkpoint.Verify when an STH
// is not present in the daemon-global log. Distinct from a
// signature-verification failure (which is the security-relevant
// signal); not-found is the "your STH was never co-signed" signal
// the doctor can surface as a degraded state.
var ErrCheckpointNotFound = errors.New("tessera: checkpoint not found")

const checkpointOrigin = sthOriginPrefix + "-checkpoint"

const checkpointShutdownTimeout = 5 * time.Second

type Checkpoint struct {
	dir            string
	cfg            Config
	storage        *posixStorage
	tessAppend     *tessera.Appender
	shutdownFn     func(context.Context) error
	logReader      tessera.LogReader
	appenderCancel context.CancelFunc
	closed         atomic.Bool
}

func NewCheckpoint(ctx context.Context, dir string, cfg Config) (*Checkpoint, error) {
	if dir == "" {
		return nil, fmt.Errorf("tessera: checkpoint dir must be non-empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("tessera: mkdir %s: %w", sub, err)
		}
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("tessera: chmod %s: %w", dir, err)
	}
	storage, err := openPosixStorage(ctx, dir)
	if err != nil {
		return nil, err
	}
	signer, err := newCheckpointSigner(checkpointOrigin)
	if err != nil {
		_ = storage.Close()
		return nil, fmt.Errorf("tessera: checkpoint signer: %w", err)
	}
	opts := tessera.NewAppendOptions().
		WithBatching(uint(cfg.BatchMaxSize), cfg.BatchMaxAge).
		WithCheckpointInterval(checkpointInterval).
		WithCheckpointSigner(signer)

	appenderCtx, appenderCancel := context.WithCancel(ctx)
	app, shutdownFn, logReader, err := tessera.NewAppender(appenderCtx, storage.Driver(), opts)
	if err != nil {
		appenderCancel()
		_ = storage.Close()
		return nil, fmt.Errorf("tessera: NewAppender: %w", err)
	}
	return &Checkpoint{
		dir:            dir,
		cfg:            cfg,
		storage:        storage,
		tessAppend:     app,
		shutdownFn:     shutdownFn,
		logReader:      logReader,
		appenderCancel: appenderCancel,
	}, nil
}

func (c *Checkpoint) Dir() string { return c.dir }

func (c *Checkpoint) Config() Config { return c.cfg }

func (c *Checkpoint) Append(ctx context.Context, signed SignedSTH) error {
	if c.closed.Load() {
		return ErrCheckpointLogClosed
	}
	if len(signed.Signature) == 0 {
		return ErrUnsignedSTH
	}
	encoded := encodeSignedSTH(signed)
	idxFuture := c.tessAppend.Add(ctx, tessera.NewEntry(encoded))
	if _, err := idxFuture(); err != nil {
		return fmt.Errorf("tessera: checkpoint Add: %w", err)
	}
	return nil
}

// Verify walks the daemon-global log looking for a SignedSTH whose
// underlying STH matches the supplied target byte-for-byte (via
// CanonicalBytes), then verifies the persisted signature against the
// supplied pubkey. Returns:
//
// - (true, nil) — STH was found and the signature verifies under pub.
// - (false, nil) — STH was found but the signature does NOT verify
// under pub (security-relevant: caller's pubkey is wrong, or
// the persisted signature was tampered with).
// - (false, ErrCheckpointNotFound) — no matching STH in the log
// (degraded-substrate signal: project STH was never co-signed).
// - (false, err) — any transient I/O / parse failure (propagated
// so the doctor can distinguish "log is unreadable" from "STH
// never appeared").
//
// Mirrors verifyInclusionWithReader's bundle-walk pattern from sth.go:
// reads ReadCheckpoint to bound the walk, then iterates entry bundles
// of width EntryBundleWidth (256), decoding each leaf into a
// SignedSTH and comparing CanonicalBytes. Decode errors on individual
// leaves are skipped (allows future leaf-format extensions to coexist
// with the v1 codec); the only fatal errors are I/O on
// ReadCheckpoint / ReadEntryBundle, an empty log (Size == 0 →
// ErrCheckpointNotFound), or no matching leaf after walking the full
// committed tree.
//
// Note on dedupe: per the IMPORTANT comment on STH in sth.go,
// Timestamp is wall-clock at synthesis time and is included in
// CanonicalBytes. A daemon restart can produce a second STH with the
// same (ProjectID, Size, RootHash) but a fresh Timestamp; both will
// be persisted in the log and either will Verify. audit
// chain verifier dedupes on (ProjectID, Size, RootHash) before
// querying Verify; this method is intentionally
// timestamp-discriminating so a callers passing a specific STH (e.g.
// the operator's `verify-chain` invocation against a known
// rotation-event STH) can pin the exact entry.
func (c *Checkpoint) Verify(ctx context.Context, pub *ecdsa.PublicKey, sth STH) (bool, error) {
	if c.closed.Load() {
		return false, ErrCheckpointLogClosed
	}
	return verifyCheckpointWithReader(ctx, c.logReader, pub, sth)
}

// verifyCheckpointWithReader is the Verify core. It accepts a
// tessera.LogReader so unit tests can drive adversarial inputs
// (missing checkpoint blob, corrupt envelope, malformed bundle,
// signature-mismatch leaf) without standing up the live Tessera
// library — mirrors the verifyInclusionWithReader split in sth.go.
//
// Steps
//
// 1. Compute the target CanonicalBytes from the supplied STH; this
// is the byte-for-byte key we compare against persisted leaves.
// 2. Read the latest signed checkpoint via lr.ReadCheckpoint. A
// missing-file error collapses to ErrCheckpointNotFound (the
// log has not yet published any checkpoint, so by definition
// no STH is durable). Other I/O errors propagate.
// 3. Parse the envelope; if cp.Size == 0 the log is empty —
// ErrCheckpointNotFound.
// 4. Walk each EntryBundleWidth-sized bundle, decoding every leaf
// into a SignedSTH and comparing CanonicalBytes. Decode errors
// on individual leaves are skipped (forward-compat with future
// codec revs).
// 5. On a CanonicalBytes match, verify the persisted signature
// against the supplied pubkey. Signature verification failure
// returns (false, nil) — distinct from not-found — because the
// caller has been given a security-relevant negative answer.
// 6. Exhaust the committed tree without a hit → ErrCheckpointNotFound.
func verifyCheckpointWithReader(ctx context.Context, lr tessera.LogReader, pub *ecdsa.PublicKey, sth STH) (bool, error) {
	target := sth.CanonicalBytes()

	rawCheckpoint, err := lr.ReadCheckpoint(ctx)
	if err != nil {

		if errors.Is(err, os.ErrNotExist) {
			return false, ErrCheckpointNotFound
		}
		return false, fmt.Errorf("tessera: checkpoint ReadCheckpoint: %w", err)
	}
	cp, err := parseCheckpointEnvelope(rawCheckpoint)
	if err != nil {
		return false, err
	}
	if cp.Size == 0 {
		return false, ErrCheckpointNotFound
	}

	const bundleWidth = uint64(layout.EntryBundleWidth)
	for bundleIdx := uint64(0); bundleIdx*bundleWidth < cp.Size; bundleIdx++ {
		bundle, err := tesseraclient.GetEntryBundle(ctx, lr.ReadEntryBundle, bundleIdx, cp.Size)
		if err != nil {
			return false, fmt.Errorf("tessera: checkpoint GetEntryBundle: %w", err)
		}

		if uint64(len(bundle.Entries)) > bundleWidth {
			return false, fmt.Errorf("tessera: checkpoint bundle overflow at idx %d (%d > %d)", bundleIdx, len(bundle.Entries), bundleWidth)
		}
		for _, leafBytes := range bundle.Entries {
			signed, decodeErr := decodeSignedSTH(leafBytes)
			if decodeErr != nil {

				continue
			}
			if !bytes.Equal(signed.STH.CanonicalBytes(), target) {
				continue
			}
			// Match verify the persisted signature against the
			// supplied pubkey. A signature mismatch returns
			// (false, nil) — distinct from not-found — because the
			// caller has been given a security-relevant negative
			// answer ("yes, this STH was co-signed, but not by the
			// pubkey you presented").
			digest := sth.Digest()
			if !VerifyWithPubkey(pub, digest[:], signed.Signature) {
				return false, nil
			}
			return true, nil
		}
	}
	return false, ErrCheckpointNotFound
}

// Latest returns the most-recent SignedSTH co-signed into the daemon-
// global checkpoint log along with the current committed tree size.
// Used by `zen doctor audit.tessera.checkpoint` — through
// CheckpointDoctor.Check (doctor.go) — to surface freshness without
// reaching into the unexported logReader.
//
// # Returns
//
// - (signed, size, nil) — the log has at least one SignedSTH; signed
// is the last leaf, size is cp.Size from the latest published
// checkpoint envelope.
// - (zero, 0, ErrCheckpointNotFound) — the log is empty (no STHs
// co-signed yet — normal at daemon-cold-start) or the underlying
// posix backend has not yet written a checkpoint blob.
// - (zero, size, err) — any transient I/O / parse failure (e.g.
// corrupt envelope, malformed leaf bundle); size is best-effort
// populated when the envelope parsed but the leaf walk failed.
//
// The signature on the returned SignedSTH is NOT verified here — Verify
// is the security-relevant entrypoint for that. Latest is purely an
// "is the substrate fresh?" probe; the doctor wraps the timestamp
// against a doctrine cadence to decide WARN vs OK.
func (c *Checkpoint) Latest(ctx context.Context) (SignedSTH, uint64, error) {
	if c.closed.Load() {
		return SignedSTH{}, 0, ErrCheckpointLogClosed
	}
	return latestWithReader(ctx, c.logReader)
}

func latestWithReader(ctx context.Context, lr tessera.LogReader) (SignedSTH, uint64, error) {
	rawCheckpoint, err := lr.ReadCheckpoint(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SignedSTH{}, 0, ErrCheckpointNotFound
		}
		return SignedSTH{}, 0, fmt.Errorf("tessera: checkpoint Latest ReadCheckpoint: %w", err)
	}
	cp, err := parseCheckpointEnvelope(rawCheckpoint)
	if err != nil {
		return SignedSTH{}, 0, err
	}
	if cp.Size == 0 {
		return SignedSTH{}, 0, ErrCheckpointNotFound
	}
	const bundleWidth = uint64(layout.EntryBundleWidth)
	lastIdx := cp.Size - 1
	bundleIdx := lastIdx / bundleWidth
	bundle, err := tesseraclient.GetEntryBundle(ctx, lr.ReadEntryBundle, bundleIdx, cp.Size)
	if err != nil {
		return SignedSTH{}, cp.Size, fmt.Errorf("tessera: checkpoint Latest GetEntryBundle: %w", err)
	}
	if len(bundle.Entries) == 0 {

		return SignedSTH{}, cp.Size, ErrCheckpointNotFound
	}
	if uint64(len(bundle.Entries)) > bundleWidth {
		return SignedSTH{}, cp.Size, fmt.Errorf("tessera: checkpoint Latest bundle overflow at idx %d (%d > %d)", bundleIdx, len(bundle.Entries), bundleWidth)
	}
	last := bundle.Entries[len(bundle.Entries)-1]
	signed, err := decodeSignedSTH(last)
	if err != nil {
		return SignedSTH{}, cp.Size, fmt.Errorf("tessera: checkpoint Latest decode: %w", err)
	}
	return signed, cp.Size, nil
}

func (c *Checkpoint) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	var firstErr error
	if c.shutdownFn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), checkpointShutdownTimeout)
		if err := c.shutdownFn(ctx); err != nil {
			firstErr = err
		}
		cancel()
	}

	if c.appenderCancel != nil {
		c.appenderCancel()
	}
	if c.storage != nil {
		if err := c.storage.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

const signedSTHMagic = "zen-swarm-signed-sth-v1\x00"

func encodeSignedSTH(s SignedSTH) []byte {
	var buf bytes.Buffer
	buf.WriteString(signedSTHMagic)
	buf.Write(s.STH.CanonicalBytes())
	var sigLen [4]byte
	binary.BigEndian.PutUint32(sigLen[:], uint32(len(s.Signature)))
	buf.Write(sigLen[:])
	buf.Write(s.Signature)
	buf.WriteString(s.PubkeyFingerprint)
	buf.WriteByte(0)
	return buf.Bytes()
}

func decodeSignedSTH(data []byte) (SignedSTH, error) {
	if len(data) < len(signedSTHMagic) {
		return SignedSTH{}, fmt.Errorf("tessera: leaf too short for signed-STH magic")
	}
	if string(data[:len(signedSTHMagic)]) != signedSTHMagic {
		return SignedSTH{}, fmt.Errorf("tessera: leaf magic mismatch")
	}
	cur := data[len(signedSTHMagic):]

	const sthMagic = "zen-swarm-tessera-sth\x00"
	if len(cur) < len(sthMagic) {
		return SignedSTH{}, fmt.Errorf("tessera: STH magic missing")
	}
	if string(cur[:len(sthMagic)]) != sthMagic {
		return SignedSTH{}, fmt.Errorf("tessera: STH magic mismatch")
	}
	cur = cur[len(sthMagic):]
	zero := bytes.IndexByte(cur, 0)
	if zero < 0 {
		return SignedSTH{}, fmt.Errorf("tessera: STH project_id terminator missing")
	}
	projectID := string(cur[:zero])
	cur = cur[zero+1:]
	const fixedTail = 8 + 32 + 8
	if len(cur) < fixedTail {
		return SignedSTH{}, fmt.Errorf("tessera: STH fields truncated")
	}
	size := binary.BigEndian.Uint64(cur[:8])
	rootHash := append([]byte(nil), cur[8:8+32]...)
	tsNanos := int64(binary.BigEndian.Uint64(cur[8+32 : 8+32+8]))
	cur = cur[fixedTail:]
	if len(cur) < 4 {
		return SignedSTH{}, fmt.Errorf("tessera: signature length missing")
	}
	sigLen := binary.BigEndian.Uint32(cur[:4])
	cur = cur[4:]
	if uint32(len(cur)) < sigLen {
		return SignedSTH{}, fmt.Errorf("tessera: signature truncated")
	}
	sig := append([]byte(nil), cur[:sigLen]...)
	cur = cur[sigLen:]
	zero = bytes.IndexByte(cur, 0)
	if zero < 0 {
		return SignedSTH{}, fmt.Errorf("tessera: fingerprint terminator missing")
	}
	fp := string(cur[:zero])
	return SignedSTH{
		STH: STH{
			ProjectID: projectID,
			Size:      size,
			RootHash:  rootHash,

			Timestamp: time.Unix(0, tsNanos).UTC(),
		},
		Signature:         sig,
		PubkeyFingerprint: fp,
	}, nil
}
