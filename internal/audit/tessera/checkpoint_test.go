package tessera

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tessera "github.com/transparency-dev/tessera"
	posix "github.com/transparency-dev/tessera/storage/posix"
)

func fastCheckpointConfig() Config {
	return Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

func newTempCheckpoint(t *testing.T) (*Checkpoint, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "global", "daemon_checkpoint")
	cp, err := NewCheckpoint(context.Background(), dir, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("NewCheckpoint: %v", err)
	}
	t.Cleanup(func() { _ = cp.Close() })
	return cp, dir
}

func TestNewCheckpointCreatesDirTree(t *testing.T) {
	cp, dir := newTempCheckpoint(t)
	_ = cp

	for _, sub := range []string{"checkpoints", "seq"} {
		st, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Fatalf("missing subdir %s: %v", sub, err)
		}
		if !st.IsDir() {
			t.Errorf("%s is not a dir", sub)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "tiles")); !os.IsNotExist(err) {
		t.Errorf("tiles/ (plural) should not exist after construction; got err=%v", err)
	}
}

func TestCheckpointAppendStoresSignedSTH(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	signed := SignedSTH{
		STH: STH{
			ProjectID: "p1",
			Size:      1,
			RootHash:  bytes32(0xab),
			Timestamp: time.Now().UTC(),
		},
		Signature:         []byte{0x30, 0x44, 0x02, 0x20, 0x00},
		PubkeyFingerprint: "abcd1234",
	}
	if err := cp.Append(context.Background(), signed); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func TestCheckpointAppendRejectsUnsignedSTH(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	signed := SignedSTH{
		STH:       STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab)},
		Signature: nil,
	}
	err := cp.Append(context.Background(), signed)
	if !errors.Is(err, ErrUnsignedSTH) {
		t.Fatalf("Append accepted unsigned STH: want ErrUnsignedSTH, got %v", err)
	}
}

func TestCheckpointVerifyRoundTrip(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	cs := NewCoSigner(w, cp)
	sth := STH{
		ProjectID: "p1",
		Size:      1,
		RootHash:  bytes32(0xab),
		Timestamp: time.Now().UTC(),
	}
	if err := cs.OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("OnSTH: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		ok, err = cp.Verify(context.Background(), pub, sth)
		if err == nil && ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("Verify rejected an STH we just co-signed and persisted")
	}
}

func TestCheckpointVerifyRejectsMissingSTH(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	other := STH{
		ProjectID: "p1",
		Size:      999,
		RootHash:  bytes32(0xff),
		Timestamp: time.Now().UTC(),
	}
	ok, err := cp.Verify(context.Background(), pub, other)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("Verify on absent STH: want ErrCheckpointNotFound, got %v", err)
	}
	if ok {
		t.Error("Verify returned true for absent STH")
	}
}

func TestCheckpointCloseIsIdempotent(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	if err := cp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cp.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCheckpointMethodsAfterCloseFail(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	_ = cp.Close()
	signed := SignedSTH{
		STH:       STH{ProjectID: "p1", RootHash: bytes32(0)},
		Signature: []byte{0xff},
	}
	err := cp.Append(context.Background(), signed)
	if !errors.Is(err, ErrCheckpointLogClosed) {
		t.Fatalf("Append after Close: want ErrCheckpointLogClosed, got %v", err)
	}
}

// -----------------------------------------------------------------
// Coverage tests beyond the 7 plan-mandated. Per the project doctrine
// (≥90% for security-critical files, no tech debt) every branch in
// checkpoint.go below must be exercised. Each block below cites the
// branch + rationale.
// -----------------------------------------------------------------

func TestCheckpointAccessorsRoundTrip(t *testing.T) {
	cp, dir := newTempCheckpoint(t)
	if got := cp.Dir(); got != dir {
		t.Errorf("Dir = %q, want %q", got, dir)
	}
	cfg := cp.Config()
	want := fastCheckpointConfig()
	if cfg.BatchMaxAge != want.BatchMaxAge ||
		cfg.BatchMaxSize != want.BatchMaxSize ||
		cfg.RotationCadenceDays != want.RotationCadenceDays {
		t.Errorf("Config = %+v, want %+v", cfg, want)
	}
}

func TestCheckpointVerifyAfterCloseFails(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	_ = cp.Close()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_, err = cp.Verify(context.Background(), &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if !errors.Is(err, ErrCheckpointLogClosed) {
		t.Fatalf("Verify after Close: want ErrCheckpointLogClosed, got %v", err)
	}
}

func TestNewCheckpointRejectsEmptyDir(t *testing.T) {
	_, err := NewCheckpoint(context.Background(), "", DefaultConfig())
	if err == nil {
		t.Fatal("NewCheckpoint with empty dir: want error, got nil")
	}
}

func TestNewCheckpointRejectsInvalidConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "global", "daemon_checkpoint")
	bad := Config{BatchMaxAge: 0, BatchMaxSize: 0, RotationCadenceDays: 0}
	_, err := NewCheckpoint(context.Background(), dir, bad)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewCheckpoint with invalid cfg: want ErrInvalidConfig, got %v", err)
	}
}

func TestNewCheckpointRejectsMkdirFailure(t *testing.T) {
	root := t.TempDir()

	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := filepath.Join(blocker, "global", "daemon_checkpoint")
	_, err := NewCheckpoint(context.Background(), dir, DefaultConfig())
	if err == nil {
		t.Fatal("NewCheckpoint on mkdir-blocking path: want error, got nil")
	}
}

func TestNewCheckpointPropagatesPosixDriverError(t *testing.T) {
	sentinel := errors.New("simulated posix.New failure")
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return nil, sentinel
	})
	dir := filepath.Join(t.TempDir(), "global", "daemon_checkpoint")
	_, err := NewCheckpoint(context.Background(), dir, fastCheckpointConfig())
	if !errors.Is(err, sentinel) {
		t.Fatalf("NewCheckpoint: want sentinel chain, got %v", err)
	}
}

func TestCheckpointVerifyMissingCheckpointReturnsNotFound(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("missing checkpoint: want ErrCheckpointNotFound, got %v", err)
	}
	if ok {
		t.Error("Verify returned true on missing checkpoint")
	}
}

func TestCheckpointVerifyPropagatesReadCheckpointError(t *testing.T) {
	sentinel := errors.New("simulated transient checkpoint read failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return nil, sentinel
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if ok {
		t.Error("Verify returned true on read-checkpoint failure")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err chain missing sentinel: %v", err)
	}
	if errors.Is(err, ErrCheckpointNotFound) {
		t.Error("transient I/O error must NOT collapse to ErrCheckpointNotFound")
	}
}

func TestCheckpointVerifyRejectsCorruptCheckpoint(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return []byte("totally not a checkpoint"), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if ok {
		t.Error("Verify returned true on corrupt checkpoint envelope")
	}
	if err == nil {
		t.Fatal("want non-nil parse error")
	}
}

func TestCheckpointVerifyEmptyTreeReturnsNotFound(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 0, bytes32(0)), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("empty tree: want ErrCheckpointNotFound, got %v", err)
	}
	if ok {
		t.Error("Verify returned true on empty tree")
	}
}

// TestCheckpointVerifyPropagatesReadEntryBundleError pins the
// ReadEntryBundle I/O failure branch. Mirrors
// TestVerifyInclusionPropagatesEntryBundleFetchError the upstream
// tessera/client GetEntryBundle uses %v (not %w) when wrapping the
// underlying error, so we cannot errors.Is back to the sentinel.
// Instead we assert non-nil + NOT ErrCheckpointNotFound — the
// security-relevant guarantee is that transient I/O does NOT
// silently collapse to "STH not found".
func TestCheckpointVerifyPropagatesReadEntryBundleError(t *testing.T) {
	sentinel := errors.New("simulated bundle read failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 5, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return nil, sentinel
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0)})
	if ok {
		t.Error("Verify returned true on bundle read failure")
	}
	if err == nil {
		t.Fatal("want non-nil error on bundle read failure")
	}
	if errors.Is(err, ErrCheckpointNotFound) {
		t.Error("transient bundle I/O error must NOT collapse to ErrCheckpointNotFound")
	}
}

func TestCheckpointVerifySkipsUndecodableLeaves(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return encodeEntryBundle([][]byte{[]byte("not a SignedSTH")}), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab)})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("undecodable leaves: want ErrCheckpointNotFound, got %v", err)
	}
	if ok {
		t.Error("Verify returned true after skipping undecodable leaves")
	}
}

// TestCheckpointVerifyRejectsBadSignature pins the (false, nil)
// branch: the STH matches a persisted leaf but the signature
// verifies under a DIFFERENT pubkey than the one supplied. Returns
// (false, nil) — distinct from ErrCheckpointNotFound — to surface
// the security-relevant signal.
func TestCheckpointVerifyRejectsBadSignature(t *testing.T) {

	sth := STH{
		ProjectID: "p1",
		Size:      1,
		RootHash:  bytes32(0xab),
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	signed := SignedSTH{
		STH:               sth,
		Signature:         []byte{0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01},
		PubkeyFingerprint: "deadbeef",
	}
	leaf := encodeSignedSTH(signed)
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return encodeEntryBundle([][]byte{leaf}), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, sth)
	if err != nil {
		t.Fatalf("Verify with bogus signature: unexpected error %v (want (false, nil))", err)
	}
	if ok {
		t.Error("Verify returned true on bogus signature; want (false, nil)")
	}
}

func TestCheckpointVerifySkipsNonMatchingLeaves(t *testing.T) {

	otherSigned := SignedSTH{
		STH: STH{
			ProjectID: "other-project",
			Size:      42,
			RootHash:  bytes32(0x33),
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		Signature:         []byte{0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01},
		PubkeyFingerprint: "abcd1234",
	}
	otherLeaf := encodeSignedSTH(otherSigned)
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return encodeEntryBundle([][]byte{otherLeaf}), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	target := STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab), Timestamp: time.Unix(1700000001, 0).UTC()}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, target)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("non-matching leaves: want ErrCheckpointNotFound, got %v", err)
	}
	if ok {
		t.Error("Verify returned true after walking past non-matching leaves")
	}
}

// TestCheckpointVerifyRejectsBundleOverflow pins the defensive guard:
// if a future Tessera bug returns more than EntryBundleWidth entries
// in a single bundle, Verify must refuse rather than walk the
// over-long bundle (would corrupt the security-relevant scan).
func TestCheckpointVerifyRejectsBundleOverflow(t *testing.T) {

	const overflow = 257
	entries := make([][]byte, overflow)
	for i := range entries {
		entries[i] = []byte("filler")
	}
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 257, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return encodeEntryBundle(entries), nil
		},
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ok, err := verifyCheckpointWithReader(context.Background(), lr, &priv.PublicKey, STH{ProjectID: "p1", RootHash: bytes32(0xab)})
	if err == nil {
		t.Fatal("bundle overflow: want non-nil error, got nil")
	}
	if ok {
		t.Error("Verify returned true on bundle overflow")
	}
}

func TestSignedSTHCodecRoundTrip(t *testing.T) {
	original := SignedSTH{
		STH: STH{
			ProjectID: "project-with-dashes_123",
			Size:      0xdeadbeef,
			RootHash:  bytes32(0xab),
			Timestamp: time.Unix(1700000000, 123456789).UTC(),
		},
		Signature:         []byte{0x30, 0x44, 0x02, 0x20, 0x12, 0x34},
		PubkeyFingerprint: "deadbeefcafebabe",
	}
	encoded := encodeSignedSTH(original)
	decoded, err := decodeSignedSTH(encoded)
	if err != nil {
		t.Fatalf("decodeSignedSTH on round-trip: %v", err)
	}
	if decoded.STH.ProjectID != original.STH.ProjectID {
		t.Errorf("ProjectID = %q, want %q", decoded.STH.ProjectID, original.STH.ProjectID)
	}
	if decoded.STH.Size != original.STH.Size {
		t.Errorf("Size = %d, want %d", decoded.STH.Size, original.STH.Size)
	}
	if !bytes.Equal(decoded.STH.RootHash, original.STH.RootHash) {
		t.Errorf("RootHash = %x, want %x", decoded.STH.RootHash, original.STH.RootHash)
	}
	if !decoded.STH.Timestamp.Equal(original.STH.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", decoded.STH.Timestamp, original.STH.Timestamp)
	}
	if !bytes.Equal(decoded.Signature, original.Signature) {
		t.Errorf("Signature = %x, want %x", decoded.Signature, original.Signature)
	}
	if decoded.PubkeyFingerprint != original.PubkeyFingerprint {
		t.Errorf("PubkeyFingerprint = %q, want %q", decoded.PubkeyFingerprint, original.PubkeyFingerprint)
	}
	// CanonicalBytes round-trip MUST be byte-identical: Phase B's
	// chain verifier compares CanonicalBytes when reconstructing
	// SignedSTHs from the daemon-global log.
	if !bytes.Equal(decoded.STH.CanonicalBytes(), original.STH.CanonicalBytes()) {
		t.Error("CanonicalBytes diverged after encode/decode round-trip")
	}
}

func TestDecodeSignedSTHRejectsTooShort(t *testing.T) {
	_, err := decodeSignedSTH([]byte("short"))
	if err == nil {
		t.Fatal("decodeSignedSTH on too-short input: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsBadMagic(t *testing.T) {
	bogus := append([]byte("zen-swarm-NOT-signed-sth-vX\x00"), make([]byte, 100)...)
	_, err := decodeSignedSTH(bogus)
	if err == nil {
		t.Fatal("decodeSignedSTH on bad magic: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsMissingSTHMagic(t *testing.T) {
	data := []byte(signedSTHMagic + "x")
	_, err := decodeSignedSTH(data)
	if err == nil {
		t.Fatal("decodeSignedSTH on missing STH magic: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsBadSTHMagic(t *testing.T) {
	data := []byte(signedSTHMagic + "zen-swarm-tessera-XXX\x00remainder...")
	_, err := decodeSignedSTH(data)
	if err == nil {
		t.Fatal("decodeSignedSTH on bad STH magic: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsMissingProjectIDTerminator(t *testing.T) {
	const sthMagic = "zen-swarm-tessera-sth\x00"

	data := []byte(signedSTHMagic + sthMagic + "no-nul-terminator-anywhere")
	_, err := decodeSignedSTH(data)
	if err == nil {
		t.Fatal("decodeSignedSTH on missing project_id NUL: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsTruncatedSTHFields(t *testing.T) {
	const sthMagic = "zen-swarm-tessera-sth\x00"

	data := []byte(signedSTHMagic + sthMagic + "p1\x00\xff")
	_, err := decodeSignedSTH(data)
	if err == nil {
		t.Fatal("decodeSignedSTH on truncated STH fields: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsMissingSigLen(t *testing.T) {
	original := SignedSTH{
		STH: STH{
			ProjectID: "p1",
			Size:      1,
			RootHash:  bytes32(0xab),
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		Signature:         []byte{0xaa},
		PubkeyFingerprint: "ff",
	}
	encoded := encodeSignedSTH(original)

	truncated := encoded[:99]
	_, err := decodeSignedSTH(truncated)
	if err == nil {
		t.Fatal("decodeSignedSTH on missing sigLen: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsTruncatedSignature(t *testing.T) {
	original := SignedSTH{
		STH: STH{
			ProjectID: "p1",
			Size:      1,
			RootHash:  bytes32(0xab),
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		Signature:         []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		PubkeyFingerprint: "ff",
	}
	encoded := encodeSignedSTH(original)

	truncated := encoded[:105]
	_, err := decodeSignedSTH(truncated)
	if err == nil {
		t.Fatal("decodeSignedSTH on truncated signature: want error, got nil")
	}
}

func TestDecodeSignedSTHRejectsMissingFingerprintTerminator(t *testing.T) {
	original := SignedSTH{
		STH: STH{
			ProjectID: "p1",
			Size:      1,
			RootHash:  bytes32(0xab),
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		Signature:         []byte{0xaa, 0xbb},
		PubkeyFingerprint: "ff",
	}
	encoded := encodeSignedSTH(original)

	truncated := encoded[:len(encoded)-1]
	_, err := decodeSignedSTH(truncated)
	if err == nil {
		t.Fatal("decodeSignedSTH on missing fingerprint terminator: want error, got nil")
	}
}
