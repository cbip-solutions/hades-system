package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type stubChainStore struct {
	records  []ChainRecord
	queryErr error
}

func (s *stubChainStore) QueryAll(ctx context.Context, projectID string) ([]ChainRecord, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	out := make([]ChainRecord, len(s.records))
	copy(out, s.records)
	return out, nil
}

type stubTesseraAdapter struct {
	rejectLeafID tessera.LeafID
}

func (s *stubTesseraAdapter) VerifyMerkleInclusion(ctx context.Context, leafID tessera.LeafID) (bool, error) {
	if leafID == s.rejectLeafID {
		return false, errors.New("merkle proof failed")
	}
	return true, nil
}

type stubWitnessVerifier struct {
	verifyResult      bool
	verifyErr         error
	rejectPayloadHash string
	rejectErr         error
}

func (s *stubWitnessVerifier) VerifySealSignature(ctx context.Context, payload, sig []byte) (bool, error) {
	if s.rejectPayloadHash != "" {
		got := sha256.Sum256(payload)
		if hex.EncodeToString(got[:]) == s.rejectPayloadHash {
			if s.rejectErr != nil {
				return false, s.rejectErr
			}
			return false, nil
		}
	}

	if !s.verifyResult && s.verifyErr == nil {
		return true, nil
	}
	return s.verifyResult, s.verifyErr
}

type stubSealStoreReader struct {
	rows []SealMeta
}

func (s *stubSealStoreReader) ListSeals(ctx context.Context, projectID string) ([]SealMeta, error) {
	out := make([]SealMeta, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func computeRecordHashTest(prevHash, eventType, payload string, createdAt int64) string {
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte("|"))
	h.Write([]byte(eventType))
	h.Write([]byte("|"))
	h.Write([]byte(payload))
	h.Write([]byte("|"))
	var buf [20]byte
	n := writeInt64(buf[:], createdAt)
	h.Write(buf[len(buf)-n:])
	return hex.EncodeToString(h.Sum(nil))
}

func writeInt64(buf []byte, n int64) int {
	if n == 0 {
		buf[len(buf)-1] = '0'
		return 1
	}
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return len(buf) - i
}

func makeChainRecord(prevRecord *ChainRecord, id int64, eventType, payload string, createdAt int64, leafID string) ChainRecord {
	prev := ""
	if prevRecord != nil {
		prev = prevRecord.RecordHash
	}
	return ChainRecord{
		ID:            id,
		ProjectID:     "zen-swarm",
		EventType:     eventType,
		Payload:       payload,
		PrevHash:      prev,
		RecordHash:    computeRecordHashTest(prev, eventType, payload, createdAt),
		CreatedAt:     createdAt,
		PartitionID:   "2026_05",
		TesseraLeafID: leafID,
	}
}

func canonicalSealPayloadHashHex(partitionID, finalRecordHash string, eventCount int64, lastID string) string {
	payload := buildSealPayloadCanonical(partitionID, finalRecordHash, eventCount, lastID)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func TestVerifyChainHappyPath(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	r2 := makeChainRecord(&r1, 2, "audit.bar", `{"k":"w"}`, 1700000001, "leaf2")
	r3 := makeChainRecord(&r2, 3, "audit.baz", `{"k":"x"}`, 1700000002, "leaf3")

	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1, r2, r3}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{rows: []SealMeta{
			{
				PartitionID:            "2026_05",
				FinalRecordHash:        r3.RecordHash,
				TesseraSealLeafID:      "seal-leaf-1",
				DaemonWitnessSignature: "sig",
				EventCount:             3,
				LastID:                 "evt-3",
			},
		}},
	)

	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !res.Clean {
		t.Errorf("Clean = false; want true; result = %+v", res)
	}
	if res.RecordsChecked != 3 {
		t.Errorf("RecordsChecked = %d, want 3", res.RecordsChecked)
	}
	if res.PartitionSealsChecked != 1 {
		t.Errorf("PartitionSealsChecked = %d, want 1", res.PartitionSealsChecked)
	}
}

func TestVerifyChainDetectsLocalHashMismatch(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	r2 := makeChainRecord(&r1, 2, "audit.bar", `{"k":"w"}`, 1700000001, "leaf2")

	r2.RecordHash = "deadbeef"

	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1, r2}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Clean {
		t.Error("Clean = true on tampered record")
	}
	if res.FirstTamperRecordID != 2 {
		t.Errorf("FirstTamperRecordID = %d, want 2", res.FirstTamperRecordID)
	}
	if res.FirstTamperPath != PathLocalChainMismatch {
		t.Errorf("FirstTamperPath = %v, want PathLocalChainMismatch", res.FirstTamperPath)
	}
}

func TestVerifyChainDetectsBrokenPrevHashLink(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")

	r2 := makeChainRecord(&r1, 2, "audit.bar", `{"k":"w"}`, 1700000001, "leaf2")
	r2.PrevHash = "wrong-prev-hash"

	r2.RecordHash = computeRecordHashTest("wrong-prev-hash", r2.EventType, r2.Payload, r2.CreatedAt)

	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1, r2}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Clean {
		t.Error("Clean = true on broken chain link")
	}
	if res.FirstTamperRecordID != 2 {
		t.Errorf("FirstTamperRecordID = %d", res.FirstTamperRecordID)
	}
	if res.FirstTamperPath != PathPrevHashLinkBroken {
		t.Errorf("FirstTamperPath = %v, want PathPrevHashLinkBroken", res.FirstTamperPath)
	}
}

func TestVerifyChainDetectsTesseraProofFailure(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1}},
		&stubTesseraAdapter{rejectLeafID: "leaf1"},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Clean {
		t.Error("Clean = true; expected tessera proof failure detected")
	}
	if res.FirstTamperPath != PathTesseraProofFail {
		t.Errorf("FirstTamperPath = %v, want PathTesseraProofFail", res.FirstTamperPath)
	}
}

func TestVerifyChainDetectsWitnessSignatureFailure(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")

	seal := SealMeta{
		PartitionID:            "2026_05",
		FinalRecordHash:        r1.RecordHash,
		TesseraSealLeafID:      "seal",
		DaemonWitnessSignature: "s",
		EventCount:             1,
		LastID:                 "evt-1",
	}

	rejectHash := canonicalSealPayloadHashHex(seal.PartitionID, seal.FinalRecordHash, seal.EventCount, seal.LastID)

	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{rejectPayloadHash: rejectHash},
		&stubSealStoreReader{rows: []SealMeta{seal}},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Clean {
		t.Error("Clean = true; expected witness signature failure detected")
	}
	if res.FirstTamperPath != PathWitnessSignatureInvalid {
		t.Errorf("FirstTamperPath = %v, want PathWitnessSignatureInvalid", res.FirstTamperPath)
	}
	if res.FirstTamperPartition != "2026_05" {
		t.Errorf("FirstTamperPartition = %q, want 2026_05", res.FirstTamperPartition)
	}
}

// TestVerifyChainPropagatesWitnessVerifyError exercises the
// (false, non-nil) branch on WitnessVerifier — VerifyChain MUST bubble
// the error back to the caller WITHOUT setting PathWitnessSignatureInvalid
// (a verify-path infrastructure failure is NOT a tamper signal).
// Closure cross-phase review CRITICAL-1.
func TestVerifyChainPropagatesWitnessVerifyError(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	seal := SealMeta{
		PartitionID:            "2026_05",
		FinalRecordHash:        r1.RecordHash,
		TesseraSealLeafID:      "seal",
		DaemonWitnessSignature: "s",
		EventCount:             1,
		LastID:                 "evt-1",
	}
	rejectHash := canonicalSealPayloadHashHex(seal.PartitionID, seal.FinalRecordHash, seal.EventCount, seal.LastID)
	infraErr := errors.New("witness key detached")

	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{rejectPayloadHash: rejectHash, rejectErr: infraErr},
		&stubSealStoreReader{rows: []SealMeta{seal}},
	)
	_, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err == nil {
		t.Fatal("expected error bubbled from witness verify infra failure")
	}
	if !errors.Is(err, infraErr) {
		t.Errorf("err does NOT wrap infraErr: %v", err)
	}
}

func TestVerifyChainEmptyProject(t *testing.T) {
	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !res.Clean {
		t.Errorf("empty project should report clean")
	}
	if res.RecordsChecked != 0 {
		t.Errorf("RecordsChecked = %d, want 0", res.RecordsChecked)
	}
}

func TestVerifyChainPropagatesQueryError(t *testing.T) {
	v := NewVerifier(
		&stubChainStore{queryErr: errors.New("db down")},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	_, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err == nil {
		t.Error("expected error from db down")
	}
}

func TestVerifyChainRejectsEmptyProjectID(t *testing.T) {
	v := NewVerifier(
		&stubChainStore{},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{},
	)
	_, err := v.VerifyChain(context.Background(), "")
	if err == nil {
		t.Error("expected error on empty project_id")
	}
}

type stubSealStoreReaderErr struct{}

func (s *stubSealStoreReaderErr) ListSeals(ctx context.Context, projectID string) ([]SealMeta, error) {
	return nil, errors.New("seals db down")
}

func TestVerifyChainPropagatesListSealsError(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReaderErr{},
	)
	_, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err == nil {
		t.Error("expected error when ListSeals returns error")
	}
}

func TestVerifyChainRejectsIncompleteWiring(t *testing.T) {

	v := &Verifier{}
	_, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err == nil {
		t.Error("expected error on nil verifier wiring")
	}
}

func TestVerifyChainDetectsPartitionSealMismatch(t *testing.T) {
	r1 := makeChainRecord(nil, 1, "audit.foo", `{"k":"v"}`, 1700000000, "leaf1")
	v := NewVerifier(
		&stubChainStore{records: []ChainRecord{r1}},
		&stubTesseraAdapter{},
		&stubWitnessVerifier{},
		&stubSealStoreReader{rows: []SealMeta{

			{
				PartitionID:            "2026_05",
				FinalRecordHash:        "deadbeef-mismatch",
				TesseraSealLeafID:      "seal",
				DaemonWitnessSignature: "s",
				EventCount:             1,
				LastID:                 "evt-1",
			},
		}},
	)
	res, err := v.VerifyChain(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Clean {
		t.Error("Clean = true; expected partition seal mismatch detected")
	}
	if res.FirstTamperPath != PathPartitionSealMismatch {
		t.Errorf("FirstTamperPath = %v, want PathPartitionSealMismatch", res.FirstTamperPath)
	}
	if res.FirstTamperPartition != "2026_05" {
		t.Errorf("FirstTamperPartition = %q, want 2026_05", res.FirstTamperPartition)
	}
}

func TestTamperPathString(t *testing.T) {
	cases := []struct {
		path TamperPath
		want string
	}{
		{PathClean, "clean"},
		{PathLocalChainMismatch, "local-chain-mismatch"},
		{PathPrevHashLinkBroken, "prev-hash-link-broken"},
		{PathTesseraProofFail, "tessera-merkle-proof-fail"},
		{PathWitnessSignatureInvalid, "daemon-witness-signature-invalid"},
		{PathPartitionSealMismatch, "partition-seal-mismatch"},
		{TamperPath(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.path.String(); got != tc.want {
			t.Errorf("TamperPath(%d).String() = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestComputeRecordHashCanonicalIsThinWrapper(t *testing.T) {
	cases := []struct {
		prev    string
		evType  string
		payload string
		ts      int64
	}{
		{"", "audit.foo", `{"k":"v"}`, 1700000000},
		{"", "audit.bar", "", 1},
		{"abc123", "x", "y", 999999999},
	}
	for _, tc := range cases {
		want := computeRecordHash(tc.prev, tc.evType, tc.payload, tc.ts)
		got := ComputeRecordHashCanonical(tc.prev, tc.evType, tc.payload, tc.ts)
		if got != want {
			t.Errorf("ComputeRecordHashCanonical drift: prev=%q type=%q payload=%q ts=%d got=%s want=%s",
				tc.prev, tc.evType, tc.payload, tc.ts, got, want)
		}
	}
}

func TestTesseraAdapterSatisfiesWitnessVerifier(t *testing.T) {
	var _ WitnessVerifier = (*tessera.Adapter)(nil)
}
