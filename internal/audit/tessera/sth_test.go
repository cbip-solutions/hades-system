package tessera

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/transparency-dev/merkle/rfc6962"
	tessera "github.com/transparency-dev/tessera"
)

func TestSTHCanonicalBytesIncludesAllFields(t *testing.T) {
	sth := STH{
		ProjectID: "p1",
		Size:      42,
		RootHash:  bytes32(0x11),
		Timestamp: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
	}
	got := sth.CanonicalBytes()
	if len(got) == 0 {
		t.Fatal("CanonicalBytes returned empty slice")
	}
	other := sth
	other.Size = 43
	if string(sth.CanonicalBytes()) == string(other.CanonicalBytes()) {
		t.Error("CanonicalBytes did not change when Size changed")
	}
	other = sth
	other.RootHash = bytes32(0x22)
	if string(sth.CanonicalBytes()) == string(other.CanonicalBytes()) {
		t.Error("CanonicalBytes did not change when RootHash changed")
	}
	other = sth
	other.Timestamp = sth.Timestamp.Add(time.Second)
	if string(sth.CanonicalBytes()) == string(other.CanonicalBytes()) {
		t.Error("CanonicalBytes did not change when Timestamp changed")
	}
	other = sth
	other.ProjectID = "p2"
	if string(sth.CanonicalBytes()) == string(other.CanonicalBytes()) {
		t.Error("CanonicalBytes did not change when ProjectID changed")
	}
}

func TestSTHCanonicalBytesDeterministic(t *testing.T) {
	sth := STH{
		ProjectID: "p1",
		Size:      42,
		RootHash:  bytes32(0x11),
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	a := sth.CanonicalBytes()
	b := sth.CanonicalBytes()
	if string(a) != string(b) {
		t.Errorf("CanonicalBytes is non-deterministic: %x vs %x", a, b)
	}
}

func TestAdapterAppendLeafReturnsLeafID(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	digest := sha256.Sum256([]byte("payload"))
	rec := sha256.Sum256([]byte("record"))
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "test.event",
		PayloadHash: digest[:],
		RecordHash:  rec[:],
	}
	id, err := a.AppendLeaf(context.Background(), leaf)
	if err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}
	if id == "" {
		t.Error("AppendLeaf returned empty LeafID")
	}
}

func TestAdapterAppendLeafProducesSTH(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	rec := sha256.Sum256([]byte("r"))
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: rec[:],
		RecordHash:  rec[:],
	}
	captured := make(chan STH, 1)
	if err := a.SubscribeSTH(sthFunc(func(ctx context.Context, sth STH) error {
		select {
		case captured <- sth:
		default:
		}
		return nil
	})); err != nil {
		t.Fatalf("SubscribeSTH: %v", err)
	}
	if _, err := a.AppendLeaf(context.Background(), leaf); err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}
	select {
	case sth := <-captured:
		if sth.ProjectID != "p1" {
			t.Errorf("STH ProjectID = %q, want p1", sth.ProjectID)
		}
		if sth.Size == 0 {
			t.Error("STH Size = 0; expected >=1 after append+flush")
		}
		if len(sth.RootHash) != sha256.Size {
			t.Errorf("STH RootHash size = %d, want %d", len(sth.RootHash), sha256.Size)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no STH delivered within 2s of leaf append")
	}
}

func TestAdapterVerifyMerkleInclusionAcceptsKnownLeaf(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	rec := sha256.Sum256([]byte("r"))
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: rec[:],
		RecordHash:  rec[:],
	}
	id, err := a.AppendLeaf(context.Background(), leaf)
	if err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		ok, err = a.VerifyMerkleInclusion(context.Background(), id)
		if err == nil && ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("VerifyMerkleInclusion: %v", err)
	}
	if !ok {
		t.Error("VerifyMerkleInclusion rejected a leaf we just appended")
	}
}

func TestAdapterVerifyMerkleInclusionRejectsUnknownLeaf(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	ok, err := a.VerifyMerkleInclusion(context.Background(), LeafID("does-not-exist"))
	if err != nil && !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("VerifyMerkleInclusion: %v", err)
	}
	if ok {
		t.Error("VerifyMerkleInclusion accepted bogus LeafID")
	}
}

func TestSTHDigestMatchesCanonicalBytes(t *testing.T) {
	sth := STH{
		ProjectID: "p1",
		Size:      7,
		RootHash:  bytes32(0xab),
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	got := sth.Digest()
	want := sha256.Sum256(sth.CanonicalBytes())
	if got != want {
		t.Errorf("Digest = %x, want %x", got, want)
	}
}

func TestEncodeLeafIncludesAllFields(t *testing.T) {
	rec := sha256.Sum256([]byte("r"))
	pl := sha256.Sum256([]byte("p"))
	leaf := Leaf{
		EventID:     "evt-42",
		EventType:   "audit.event.test",
		PayloadHash: pl[:],
		RecordHash:  rec[:],
		ProjectID:   "p1",
	}
	got := encodeLeaf(leaf)
	if !bytes.HasPrefix(got, []byte("zen-swarm-leaf-v1\x00")) {
		t.Errorf("encodeLeaf missing magic prefix: %q", string(got))
	}
	if !bytes.Contains(got, []byte("evt-42")) {
		t.Errorf("encodeLeaf missing EventID")
	}
	if !bytes.Contains(got, []byte("audit.event.test")) {
		t.Errorf("encodeLeaf missing EventType")
	}
	if !bytes.Contains(got, pl[:]) {
		t.Errorf("encodeLeaf missing PayloadHash")
	}
	if !bytes.Contains(got, rec[:]) {
		t.Errorf("encodeLeaf missing RecordHash")
	}
	if !bytes.Contains(got, []byte("p1\x00")) {
		t.Errorf("encodeLeaf missing ProjectID terminator")
	}
}

func TestParseCheckpointEnvelopeRejectsMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		[]byte("not a checkpoint"),
		[]byte("origin\nnotanumber\nbadhash\n"),
	}
	for i, raw := range cases {
		if _, err := parseCheckpointEnvelope(raw); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestParseCheckpointEnvelopeAcceptsValid(t *testing.T) {
	raw := []byte("origin/p1\n42\nAAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=\n\n")
	cp, err := parseCheckpointEnvelope(raw)
	if err != nil {
		t.Fatalf("parseCheckpointEnvelope: %v", err)
	}
	if cp.Origin != "origin/p1" {
		t.Errorf("Origin = %q, want origin/p1", cp.Origin)
	}
	if cp.Size != 42 {
		t.Errorf("Size = %d, want 42", cp.Size)
	}
	if len(cp.Hash) != 32 {
		t.Errorf("Hash len = %d, want 32", len(cp.Hash))
	}
}

func TestVerifyInclusionRejectsForeignProjectPrefix(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	ok, err := a.VerifyMerkleInclusion(context.Background(), LeafID("p2:0"))
	if !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("want ErrLeafNotFound, got %v", err)
	}
	if ok {
		t.Error("VerifyMerkleInclusion accepted a foreign-project LeafID")
	}
}

func TestVerifyInclusionRejectsNonNumericIndex(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	ok, err := a.VerifyMerkleInclusion(context.Background(), LeafID("p1:not-a-number"))
	if !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("want ErrLeafNotFound, got %v", err)
	}
	if ok {
		t.Error("VerifyMerkleInclusion accepted non-numeric index")
	}
}

func TestVerifyInclusionRejectsOutOfRangeIndex(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	rec := sha256.Sum256([]byte("r"))
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: rec[:],
		RecordHash:  rec[:],
	}
	if _, err := a.AppendLeaf(context.Background(), leaf); err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	ok, err := a.VerifyMerkleInclusion(context.Background(), LeafID("p1:9999999"))
	if !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("want ErrLeafNotFound, got %v", err)
	}
	if ok {
		t.Error("VerifyMerkleInclusion accepted out-of-range index")
	}
}

func TestSubscribeSTHIgnoresNilSubscriber(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	if err := a.SubscribeSTH(nil); err != nil {
		t.Fatalf("SubscribeSTH(nil): %v", err)
	}
	rec := sha256.Sum256([]byte("r"))
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: rec[:],
		RecordHash:  rec[:],
	}
	if _, err := a.AppendLeaf(context.Background(), leaf); err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
}

func TestAdapterConfigReturnsConstructorConfig(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	got := a.Config()
	if got.BatchMaxSize != 1 {
		t.Errorf("Config.BatchMaxSize = %d, want 1 (testConfig)", got.BatchMaxSize)
	}
	if got.RotationCadenceDays != 365 {
		t.Errorf("Config.RotationCadenceDays = %d, want 365 (testConfig)", got.RotationCadenceDays)
	}
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

type sthFunc func(ctx context.Context, sth STH) error

func (f sthFunc) OnSTH(ctx context.Context, sth STH) error { return f(ctx, sth) }

type fakeLogReader struct {
	readCheckpoint  func(ctx context.Context) ([]byte, error)
	readTile        func(ctx context.Context, level, index uint64, p uint8) ([]byte, error)
	readEntryBundle func(ctx context.Context, index uint64, p uint8) ([]byte, error)
	nextIndex       func(ctx context.Context) (uint64, error)
	integratedSize  func(ctx context.Context) (uint64, error)
}

func (f *fakeLogReader) ReadCheckpoint(ctx context.Context) ([]byte, error) {
	if f.readCheckpoint == nil {
		return nil, errors.New("fakeLogReader.ReadCheckpoint not set in test")
	}
	return f.readCheckpoint(ctx)
}

func (f *fakeLogReader) ReadTile(ctx context.Context, level, index uint64, p uint8) ([]byte, error) {
	if f.readTile == nil {
		return nil, errors.New("fakeLogReader.ReadTile not set in test")
	}
	return f.readTile(ctx, level, index, p)
}

func (f *fakeLogReader) ReadEntryBundle(ctx context.Context, index uint64, p uint8) ([]byte, error) {
	if f.readEntryBundle == nil {
		return nil, errors.New("fakeLogReader.ReadEntryBundle not set in test")
	}
	return f.readEntryBundle(ctx, index, p)
}

func (f *fakeLogReader) NextIndex(ctx context.Context) (uint64, error) {
	if f.nextIndex == nil {
		return 0, errors.New("fakeLogReader.NextIndex not set in test")
	}
	return f.nextIndex(ctx)
}

func (f *fakeLogReader) IntegratedSize(ctx context.Context) (uint64, error) {
	if f.integratedSize == nil {
		return 0, errors.New("fakeLogReader.IntegratedSize not set in test")
	}
	return f.integratedSize(ctx)
}

var _ tessera.LogReader = (*fakeLogReader)(nil)

func encodeCheckpoint(origin string, size uint64, hash []byte) []byte {
	return []byte(origin + "\n" +
		fmt.Sprintf("%d", size) + "\n" +
		base64.StdEncoding.EncodeToString(hash) + "\n\n")
}

func encodeEntryBundle(entries [][]byte) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		var sz [2]byte
		binary.BigEndian.PutUint16(sz[:], uint16(len(e)))
		buf.Write(sz[:])
		buf.Write(e)
	}
	return buf.Bytes()
}

// TestVerifyInclusionPropagatesReadCheckpointError pins that a
// transient I/O failure on ReadCheckpoint surfaces as a wrapped
// error (NOT silently mapped to ErrLeafNotFound — the security
// review distinguishes "log is offline / disk failed" from "leaf is
// not in the log"; a verifier that conflates the two is a denial
// vector for legitimate inclusion checks).
func TestVerifyInclusionPropagatesReadCheckpointError(t *testing.T) {
	sentinel := errors.New("simulated transient checkpoint read failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return nil, sentinel
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on read-checkpoint failure")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err chain missing sentinel: %v", err)
	}
	if errors.Is(err, ErrLeafNotFound) {
		t.Error("transient error must NOT collapse to ErrLeafNotFound")
	}
}

func TestVerifyInclusionRejectsCorruptCheckpoint(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {

			return []byte("totally not a checkpoint"), nil
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on corrupt checkpoint")
	}
	if err == nil {
		t.Fatal("expected non-nil error on corrupt checkpoint")
	}
	if errors.Is(err, ErrLeafNotFound) {
		t.Error("corrupt checkpoint must NOT collapse to ErrLeafNotFound")
	}
}

func TestVerifyInclusionRejectsBundleOverflow(t *testing.T) {

	overSized := make([][]byte, 257)
	for i := range overSized {
		overSized[i] = []byte{}
	}
	bundleRaw := encodeEntryBundle(overSized)
	rootHash := sha256.Sum256([]byte("any root"))
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {

			return encodeCheckpoint("zen-swarm-tessera/p1", 1, rootHash[:]), nil
		},
		readEntryBundle: func(ctx context.Context, index uint64, p uint8) ([]byte, error) {
			return bundleRaw, nil
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on bundle overflow")
	}
	if !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("want ErrLeafNotFound on bundle overflow, got %v", err)
	}
}

func TestVerifyInclusionRejectsProofMismatch(t *testing.T) {
	leafBytes := []byte("ze leaf payload")
	leafHash := rfc6962.DefaultHasher.HashLeaf(leafBytes)

	wrongRoot := sha256.Sum256([]byte("different bytes"))
	if bytes.Equal(leafHash, wrongRoot[:]) {
		t.Fatal("test setup: wrongRoot accidentally == leafHash")
	}
	bundleRaw := encodeEntryBundle([][]byte{leafBytes})
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint("zen-swarm-tessera/p1", 1, wrongRoot[:]), nil
		},
		readEntryBundle: func(ctx context.Context, index uint64, p uint8) ([]byte, error) {
			return bundleRaw, nil
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on proof mismatch")
	}
	if err == nil {
		t.Fatal("expected non-nil error on proof mismatch")
	}
	if errors.Is(err, ErrLeafNotFound) {
		t.Error("proof mismatch must NOT collapse to ErrLeafNotFound (security signal)")
	}
}

// TestVerifyInclusionPropagatesEntryBundleFetchError pins that a
// transient I/O failure on ReadEntryBundle surfaces as a wrapped
// error (NOT silently mapped to ErrLeafNotFound — distinguishing
// "log unreachable" from "leaf not in log" is the same security
// invariant as TestVerifyInclusionPropagatesReadCheckpointError).
func TestVerifyInclusionPropagatesEntryBundleFetchError(t *testing.T) {
	rootHash := sha256.Sum256([]byte("any root"))
	sentinel := errors.New("simulated entry-bundle read failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint("zen-swarm-tessera/p1", 1, rootHash[:]), nil
		},
		readEntryBundle: func(ctx context.Context, index uint64, p uint8) ([]byte, error) {
			return nil, sentinel
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on entry-bundle fetch failure")
	}
	if err == nil {
		t.Fatal("expected non-nil error on entry-bundle fetch failure")
	}
	if errors.Is(err, ErrLeafNotFound) {
		t.Error("transient entry-bundle error must NOT collapse to ErrLeafNotFound")
	}
}

func TestVerifyInclusionPropagatesInclusionProofFetchError(t *testing.T) {
	rootHash := sha256.Sum256([]byte("any root"))
	sentinel := errors.New("simulated tile read failure")
	bundleRaw := encodeEntryBundle([][]byte{[]byte("leaf-bytes")})
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {

			return encodeCheckpoint("zen-swarm-tessera/p1", 2, rootHash[:]), nil
		},
		readEntryBundle: func(ctx context.Context, index uint64, p uint8) ([]byte, error) {
			return bundleRaw, nil
		},
		readTile: func(ctx context.Context, level, index uint64, p uint8) ([]byte, error) {
			return nil, sentinel
		},
	}
	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:0"))
	if ok {
		t.Error("expected ok=false on inclusion-proof tile fetch failure")
	}
	if err == nil {
		t.Fatal("expected non-nil error on inclusion-proof tile fetch failure")
	}
	if errors.Is(err, ErrLeafNotFound) {
		t.Error("transient tile error must NOT collapse to ErrLeafNotFound")
	}
}

func TestWatcherPollIntervalMatchesDocumentedFormula(t *testing.T) {
	got := watcherPollInterval()
	half := checkpointInterval / 2
	want := minWatcherPoll
	if half > want {
		want = half
	}
	if got != want {
		t.Errorf("watcherPollInterval = %v, want %v (formula: max(%v, %v/2))",
			got, want, minWatcherPoll, checkpointInterval)
	}
}
