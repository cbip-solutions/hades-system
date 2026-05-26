package chain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeStore struct {
	events      map[string]*EventRow
	insertOrder []string
	seals       map[string]*SealRecord
	tip         string

	getSealErr     error
	listPartsErr   error
	insertSealErr  error
	hideSeal       bool
	hidePartitions bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		events: map[string]*EventRow{},
		seals:  map[string]*SealRecord{},
	}
}

func (f *fakeStore) GetChainTip(ctx context.Context) (string, error) {
	if f.tip == "" {
		return "", ErrNoChainTip
	}
	return f.tip, nil
}

func (f *fakeStore) GetEventByID(ctx context.Context, id string) (*EventRow, error) {
	r, ok := f.events[id]
	if !ok {
		return nil, ErrEventNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeStore) GetByEventID(ctx context.Context, id string) (*EventRow, error) {
	return f.GetEventByID(ctx, id)
}

func (f *fakeStore) UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error {
	r, ok := f.events[id]
	if !ok {
		return ErrEventNotFound
	}
	if r.PrevHash != "" || r.RecordHash != "" || r.PartitionID != "" {
		return errors.New("fakeStore: chain columns already set")
	}
	r.PrevHash = prevHash
	r.RecordHash = recordHash
	r.PartitionID = partitionID
	f.tip = recordHash
	return nil
}

func (f *fakeStore) UpdateTesseraLeafID(ctx context.Context, id, leafID string) error {
	r, ok := f.events[id]
	if !ok {
		return ErrEventNotFound
	}
	if r.TesseraLeafID != nil {
		return errors.New("fakeStore: tessera_leaf_id already set")
	}
	r.TesseraLeafID = &leafID
	return nil
}

func (f *fakeStore) InsertPartitionSeal(ctx context.Context, seal SealRecord) error {
	if f.insertSealErr != nil {
		return f.insertSealErr
	}
	if _, ok := f.seals[seal.PartitionID]; ok {
		return errors.New("fakeStore: seal already exists (PK conflict)")
	}
	cp := seal
	f.seals[seal.PartitionID] = &cp
	return nil
}

func (f *fakeStore) GetPartitionSeal(ctx context.Context, partitionID string) (*SealRecord, error) {
	if f.getSealErr != nil {
		return nil, f.getSealErr
	}
	if f.hideSeal {
		return nil, ErrPartitionSealNotFound
	}
	s, ok := f.seals[partitionID]
	if !ok {
		return nil, ErrPartitionSealNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStore) ListPartitions(ctx context.Context) ([]PartitionStat, error) {
	if f.listPartsErr != nil {
		return nil, f.listPartsErr
	}
	if f.hidePartitions {
		return nil, nil
	}
	pmap := map[string]*PartitionStat{}
	for _, id := range f.insertOrder {
		r := f.events[id]
		if r.PartitionID == "" {
			continue
		}
		ps, ok := pmap[r.PartitionID]
		if !ok {
			ps = &PartitionStat{PartitionID: r.PartitionID, FirstID: r.ID}
			pmap[r.PartitionID] = ps
		}
		ps.LastID = r.ID
		ps.EventCount++
		ps.FinalRecordHash = r.RecordHash
	}
	var out []PartitionStat
	for _, p := range pmap {
		out = append(out, *p)
	}
	return out, nil
}

func (f *fakeStore) ListEventsForPartition(ctx context.Context, partitionID string) ([]EventRow, error) {
	var out []EventRow
	for _, id := range f.insertOrder {
		r := f.events[id]
		if r.PartitionID == partitionID {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeStore) BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]BackfillCursorRow, error) {
	var out []BackfillCursorRow
	for i, id := range f.insertOrder {
		rowID := int64(i + 1)
		if rowID <= afterRowID {
			continue
		}
		r := f.events[id]
		if r.PrevHash != "" || r.RecordHash != "" {
			continue
		}
		cp := *r
		out = append(out, BackfillCursorRow{RowID: rowID, EventRow: cp})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type fakeTessera struct {
	seals      map[string]string
	sigs       map[string][]byte
	failNext   bool
	failOnSign bool

	verifySigErr        error
	verifySigForceFalse bool
}

func newFakeTessera() *fakeTessera {
	return &fakeTessera{seals: map[string]string{}, sigs: map[string][]byte{}}
}

func (t *fakeTessera) AppendSeal(ctx context.Context, projectID, partitionID string, payload []byte) (string, error) {
	if t.failNext {
		t.failNext = false
		return "", errors.New("fakeTessera: injected failure")
	}
	if leaf, ok := t.seals[partitionID]; ok {
		return leaf, nil
	}
	leaf := "seal-" + projectID + "-" + partitionID
	t.seals[partitionID] = leaf
	return leaf, nil
}

func (t *fakeTessera) WitnessCoSignSeal(ctx context.Context, leafID string, payload []byte) ([]byte, error) {
	if t.failOnSign {
		t.failOnSign = false
		return nil, errors.New("fakeTessera: injected sig failure")
	}
	if t.failNext {
		t.failNext = false
		return nil, errors.New("fakeTessera: injected sig failure")
	}
	sig := []byte("sig-" + leafID)
	t.sigs[leafID] = sig
	return sig, nil
}

func (t *fakeTessera) VerifySealSignature(ctx context.Context, payload, sig []byte) (bool, error) {
	if t.verifySigErr != nil {
		return false, t.verifySigErr
	}
	if t.verifySigForceFalse {
		return false, nil
	}

	for leafID, want := range t.sigs {
		if bytesEqual(want, sig) {
			_ = leafID
			return true, nil
		}
	}
	return false, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func seedPartitionEvents(t *testing.T, fs *fakeStore, projectID, partitionID string, n int, baseTS int64) {
	t.Helper()
	for i := 0; i < n; i++ {
		id := "evt-" + projectID + "-" + partitionID + "-" + string(rune('a'+i))
		row := &EventRow{
			ID:          id,
			ProjectID:   projectID,
			Type:        "test.event",
			PayloadJSON: `{}`,
			EmittedAt:   baseTS + int64(i),
		}
		fs.events[id] = row
		fs.insertOrder = append(fs.insertOrder, id)

		tip, err := fs.GetChainTip(context.Background())
		if errors.Is(err, ErrNoChainTip) {
			tip = ""
		} else if err != nil {
			t.Fatalf("GetChainTip: %v", err)
		}
		h, err := Compute(tip, row.Type, []byte(row.PayloadJSON), row.EmittedAt)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if err := fs.UpdateChainColumns(context.Background(), id, tip, h, partitionID); err != nil {
			t.Fatalf("UpdateChainColumns: %v", err)
		}
	}
}

func TestSealPartitionSuccess(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)

	seal, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err != nil {
		t.Fatalf("SealPartition: %v", err)
	}
	if seal.PartitionID != "2023_11" {
		t.Errorf("seal.PartitionID = %q", seal.PartitionID)
	}
	if seal.SealedAt != 1700100000 {
		t.Errorf("seal.SealedAt = %d, want 1700100000", seal.SealedAt)
	}
	if seal.FinalRecordHash == "" {
		t.Error("seal.FinalRecordHash empty")
	}
	if !strings.HasPrefix(seal.TesseraSealLeafID, "seal-p1-2023_11") {
		t.Errorf("seal.TesseraSealLeafID = %q", seal.TesseraSealLeafID)
	}
	if seal.DaemonWitnessSignature == "" {
		t.Error("seal.DaemonWitnessSignature empty")
	}
}

func TestSealPartitionIdempotent(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 2, 1700000000)

	first, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err != nil {
		t.Fatalf("first SealPartition: %v", err)
	}
	second, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 9999999999)
	if err != nil {
		t.Fatalf("second SealPartition: %v", err)
	}
	if first.PartitionID != second.PartitionID ||
		first.FinalRecordHash != second.FinalRecordHash ||
		first.TesseraSealLeafID != second.TesseraSealLeafID {
		t.Errorf("idempotent re-run returned different seal:\n first  %+v\n second %+v", first, second)
	}

	if second.SealedAt != first.SealedAt {
		t.Errorf("idempotent SealedAt drifted: %d vs %d", first.SealedAt, second.SealedAt)
	}
}

func TestSealPartitionEmptyPartitionRejected(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	_, err := SealPartition(context.Background(), fs, ft, "p1", "2099_12", 1700100000)
	if !errors.Is(err, ErrPartitionEmpty) {
		t.Errorf("want ErrPartitionEmpty, got %v", err)
	}
}

func TestSealPartitionTesseraFailureBubblesError(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	ft.failNext = true
	_, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err == nil {
		t.Fatal("expected tessera AppendSeal failure to bubble; got nil")
	}
}

func TestVerifySealSuccess(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)
	seal, _ := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	_ = seal
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if err != nil {
		t.Errorf("VerifySeal: %v", err)
	}
}

func TestVerifySealTamperDetected(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 2, 1700000000)
	_, _ = SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)

	fs.seals["2023_11"].FinalRecordHash = "tampered"
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if !errors.Is(err, ErrChainTampered) {
		t.Errorf("want ErrChainTampered, got %v", err)
	}
}

func TestVerifySealMissingSeal(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if !errors.Is(err, ErrPartitionSealMissing) {
		t.Errorf("want ErrPartitionSealMissing, got %v", err)
	}
}

// TestVerifySealRejectsBadWitnessSignature pins the C-fix-2 fix:
// chain.VerifySeal MUST verify the daemon witness signature stored on
// the seal row.  Pre-fix the verify body returned (nil) even when the
// signature was missing, forged, or bytes-corrupted (a major audit
// gap surfaced by Stage 2 cross-phase review CRITICAL-2).
//
// Failure shape: errors.Is(err, ErrChainTampered) — same sentinel the
// final-record-hash mismatch branch returns, so doctor + Phase J
// seal-verify worker can react uniformly.
func TestVerifySealRejectsBadWitnessSignature(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 2, 1700000000)
	if _, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000); err != nil {
		t.Fatalf("SealPartition: %v", err)
	}

	ft.verifySigForceFalse = true
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if !errors.Is(err, ErrChainTampered) {
		t.Fatalf("want ErrChainTampered for bad witness sig, got %v", err)
	}
	if !strings.Contains(err.Error(), "witness signature invalid") {
		t.Errorf("err message = %q, want 'witness signature invalid' phrase", err.Error())
	}
}

// TestVerifySealBubblesWitnessVerifyError closes the verify-error
// branch in chain.VerifySeal: if the underlying SealAppender's
// VerifySealSignature returns a non-nil error (e.g. witness key
// detached during compromise-response, or backend I/O failure), the
// chain verifier MUST wrap + bubble it instead of either silently
// passing or conflating with ErrChainTampered.  Distinct sentinel
// avoids misclassifying transient infra errors as tamper events
// (which would trigger doctrine-specific halts in Phase J).
func TestVerifySealBubblesWitnessVerifyError(t *testing.T) {
	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	if _, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000); err != nil {
		t.Fatalf("SealPartition: %v", err)
	}
	ft.verifySigErr = errBoom
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if err == nil {
		t.Fatal("expected witness verify error to bubble; got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("want wrapped errBoom; got %v", err)
	}

	if errors.Is(err, ErrChainTampered) {
		t.Errorf("verify infra error misclassified as ErrChainTampered: %v", err)
	}
}

// --- Adversarial coverage tests (no defer, doctrine ≥95%). ---
// These exercise the wrapped-error / PK-conflict-race / partition-vanish
// branches in seal.go that the canonical 7 plan-file tests do not reach.

var errBoom = errors.New("boom: injected fakeStore failure")

func TestSealPartitionGetSealNonNotFoundErrorBubbles(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	fs.getSealErr = errBoom
	_, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom, got %v", err)
	}
}

func TestSealPartitionListPartitionsErrorBubbles(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	fs.listPartsErr = errBoom
	_, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom, got %v", err)
	}
}

func TestSealPartitionWitnessCoSignFailureBubbles(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	ft.failOnSign = true
	_, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err == nil {
		t.Fatal("expected witness cosign failure to bubble; got nil")
	}
	if !strings.Contains(err.Error(), "witness cosign seal") {
		t.Errorf("err message = %q, want contains 'witness cosign seal'", err.Error())
	}

	if _, err := fs.GetPartitionSeal(context.Background(), "2023_11"); !errors.Is(err, ErrPartitionSealNotFound) {
		t.Errorf("seal row leaked on witness-cosign failure; got err=%v", err)
	}
}

func TestSealPartitionInsertPKConflictRaceRecovers(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)

	racingSeal := SealRecord{
		PartitionID:            "2023_11",
		SealedAt:               1700050000,
		FinalRecordHash:        "deadbeef",
		TesseraSealLeafID:      "race-leaf",
		DaemonWitnessSignature: "race-sig",
	}

	fs.seals["2023_11"] = &racingSeal
	fs.hideSeal = true
	fs.insertSealErr = errors.New("UNIQUE constraint violated (PK race)")

	stageFlip := &stageFlipStore{fakeStore: fs}
	got, err := SealPartition(context.Background(), stageFlip, ft, "p1", "2023_11", 1700100000)
	if err != nil {
		t.Fatalf("expected race-recovery to return existing seal; got err=%v", err)
	}
	if got.TesseraSealLeafID != "race-leaf" {
		t.Errorf("race-recovered seal mismatch; want race-leaf, got %q", got.TesseraSealLeafID)
	}
	if got.SealedAt != 1700050000 {
		t.Errorf("race-recovered SealedAt mismatch; want 1700050000, got %d", got.SealedAt)
	}
}

type stageFlipStore struct {
	*fakeStore
	getSealCallCount int
}

func (s *stageFlipStore) GetPartitionSeal(ctx context.Context, partitionID string) (*SealRecord, error) {
	s.getSealCallCount++
	if s.getSealCallCount == 1 {

		return nil, ErrPartitionSealNotFound
	}

	if seal, ok := s.fakeStore.seals[partitionID]; ok {
		cp := *seal
		return &cp, nil
	}
	return nil, ErrPartitionSealNotFound
}

func TestSealPartitionInsertFailsAndRefetchAlsoFails(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	fs.insertSealErr = errBoom

	_, err := SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	if err == nil {
		t.Fatal("expected insert+refetch double-failure to bubble; got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected wrapped errBoom; got %v", err)
	}
}

func TestVerifySealGetSealNonNotFoundErrorBubbles(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	fs.getSealErr = errBoom
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom, got %v", err)
	}
}

func TestVerifySealListPartitionsErrorBubbles(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	_, _ = SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)

	fs.listPartsErr = errBoom
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom, got %v", err)
	}
}

func TestVerifySealPartitionVanishedAfterSeal(t *testing.T) {

	fs := newFakeStore()
	ft := newFakeTessera()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	_, _ = SealPartition(context.Background(), fs, ft, "p1", "2023_11", 1700100000)
	fs.hidePartitions = true
	err := VerifySeal(context.Background(), fs, ft, "p1", "2023_11")
	if err == nil {
		t.Fatal("expected partition-vanished error; got nil")
	}
	if !strings.Contains(err.Error(), "vanished") {
		t.Errorf("err = %q, want contains 'vanished'", err.Error())
	}
}
