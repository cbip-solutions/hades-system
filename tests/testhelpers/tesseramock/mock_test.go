package tesseramock_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tesseramock"
)

func makeLeaf(projectID, eventID string) tessera.Leaf {
	pl := sha256.Sum256([]byte("payload-" + eventID))
	rec := sha256.Sum256([]byte("record-" + eventID))
	return tessera.Leaf{
		EventID:     eventID,
		EventType:   "audit.event",
		PayloadHash: pl[:],
		RecordHash:  rec[:],
		ProjectID:   projectID,
	}
}

func TestMockTesseraAdapter_AppendLeafReturnsLeafID(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	id, err := a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	if err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}
	if id == "" {
		t.Errorf("empty LeafID")
	}
}

func TestMockTesseraAdapter_AppendLeafCrossProjectRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	_, err := a.AppendLeaf(context.Background(), makeLeaf("proj-B", "evt-001"))
	if !errors.Is(err, tessera.ErrCrossProjectAccess) {
		t.Errorf("err = %v, want ErrCrossProjectAccess", err)
	}
}

func TestMockTesseraAdapter_AppendLeafBadHashSize(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	bad := makeLeaf("proj-A", "evt-001")
	bad.PayloadHash = []byte{1, 2, 3}
	_, err := a.AppendLeaf(context.Background(), bad)
	if err == nil {
		t.Errorf("expected error for short PayloadHash")
	}
	bad2 := makeLeaf("proj-A", "evt-001")
	bad2.RecordHash = []byte{1, 2, 3}
	_, err = a.AppendLeaf(context.Background(), bad2)
	if err == nil {
		t.Errorf("expected error for short RecordHash")
	}
}

func TestMockTesseraAdapter_AppendLeafAfterCloseReturnsAdapterClosed(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	_ = a.Close()
	_, err := a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_AppendSealIdempotent(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	ctx := context.Background()
	id1, err := a.AppendSeal(ctx, "proj-A", "part-1", []byte("payload"))
	if err != nil {
		t.Fatalf("AppendSeal first: %v", err)
	}
	id2, err := a.AppendSeal(ctx, "proj-A", "part-1", []byte("payload-different"))
	if err != nil {
		t.Fatalf("AppendSeal second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("idempotency broken: %q vs %q", id1, id2)
	}
}

func TestMockTesseraAdapter_AppendSealCrossProjectRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	_, err := a.AppendSeal(context.Background(), "proj-B", "part-1", []byte("p"))
	if !errors.Is(err, tessera.ErrCrossProjectAccess) {
		t.Errorf("err = %v, want ErrCrossProjectAccess", err)
	}
}

func TestMockTesseraAdapter_VerifyMerkleInclusionTrueForLiveLeaf(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	id, _ := a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	ok, err := a.VerifyMerkleInclusion(context.Background(), id)
	if err != nil {
		t.Fatalf("VerifyMerkleInclusion: %v", err)
	}
	if !ok {
		t.Errorf("inclusion verify returned false for valid leaf")
	}
}

func TestMockTesseraAdapter_VerifyMerkleInclusionUnknownLeaf(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	ok, err := a.VerifyMerkleInclusion(context.Background(), tessera.LeafID("bogus-id"))
	if !errors.Is(err, tessera.ErrLeafNotFound) {
		t.Errorf("err = %v, want ErrLeafNotFound", err)
	}
	if ok {
		t.Errorf("ok=true for unknown leaf")
	}
}

func TestMockTesseraAdapter_VerifyMerkleInclusionAfterCorruption(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	id, _ := a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	a.SetCorruption(id)
	ok, _ := a.VerifyMerkleInclusion(context.Background(), id)
	if ok {
		t.Errorf("verify accepted corrupted leaf")
	}
}

func TestMockTesseraAdapter_SubscribeSTHDeliversOnFlush(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	var got []tessera.STH
	sub := tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		got = append(got, s)
		return nil
	})
	if err := a.SubscribeSTH(sub); err != nil {
		t.Fatalf("SubscribeSTH: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, _ = a.AppendLeaf(context.Background(), makeLeaf("proj-A", string(rune('a'+i))))
	}
	a.FlushAndPublishSTH(context.Background())
	if len(got) == 0 {
		t.Errorf("subscriber did not receive STH after Flush")
	}
	last := got[len(got)-1]
	if last.ProjectID != "proj-A" {
		t.Errorf("STH.ProjectID = %q, want proj-A", last.ProjectID)
	}
	if last.Size != 3 {
		t.Errorf("STH.Size = %d, want 3", last.Size)
	}
	if len(last.RootHash) != 32 {
		t.Errorf("STH.RootHash len = %d, want 32", len(last.RootHash))
	}
}

func TestMockTesseraAdapter_SubscribeSTHAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	_ = a.Close()
	err := a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, _ tessera.STH) error { return nil }))
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_WitnessCoSignSealRoundtrip(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	w, _ := tesseramock.NewMockWitness()
	a.Attach(w)
	id, _ := a.AppendSeal(context.Background(), "proj-A", "p1", []byte("payload"))
	sig, err := a.WitnessCoSignSeal(context.Background(), id, []byte("payload"))
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}
	ok, err := a.VerifySealSignature(context.Background(), []byte("payload"), sig)
	if err != nil {
		t.Fatalf("VerifySealSignature: %v", err)
	}
	if !ok {
		t.Errorf("witness sign+verify roundtrip failed")
	}
}

func TestMockTesseraAdapter_WitnessMissingErrors(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()

	_, err := a.WitnessCoSignSeal(context.Background(), tessera.LeafID("anything"), []byte("p"))
	if !errors.Is(err, tessera.ErrWitnessKeyMissing) {
		t.Errorf("WitnessCoSignSeal err = %v, want ErrWitnessKeyMissing", err)
	}
	_, err = a.VerifySealSignature(context.Background(), []byte("p"), []byte("sig"))
	if !errors.Is(err, tessera.ErrWitnessKeyMissing) {
		t.Errorf("VerifySealSignature err = %v, want ErrWitnessKeyMissing", err)
	}
}

func TestMockTesseraAdapter_AttachNilDetaches(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	w, _ := tesseramock.NewMockWitness()
	a.Attach(w)
	a.Attach(nil)
	_, err := a.WitnessCoSignSeal(context.Background(), tessera.LeafID("x"), []byte("p"))
	if !errors.Is(err, tessera.ErrWitnessKeyMissing) {
		t.Errorf("post-detach err = %v, want ErrWitnessKeyMissing", err)
	}
}

func TestMockTesseraAdapter_VerifySealSignatureRejectsTamperedSig(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	w, _ := tesseramock.NewMockWitness()
	a.Attach(w)
	id, _ := a.AppendSeal(context.Background(), "proj-A", "p1", []byte("payload"))
	sig, _ := a.WitnessCoSignSeal(context.Background(), id, []byte("payload"))
	tampered := bytes.Clone(sig)
	tampered[0] ^= 0xff
	ok, err := a.VerifySealSignature(context.Background(), []byte("payload"), tampered)
	if err != nil {
		t.Fatalf("VerifySealSignature: %v", err)
	}
	if ok {
		t.Errorf("verify accepted tampered signature")
	}
}

func TestMockTesseraAdapter_CloseIdempotent(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestMockTesseraAdapter_ProjectIDAndDir(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	if got := a.ProjectID(); got != "proj-A" {
		t.Errorf("ProjectID = %q, want proj-A", got)
	}
	if got := a.Dir(); got == "" {
		t.Errorf("Dir() empty (synthetic but should be non-empty for parity with real Adapter)")
	}
}

func TestMockTesseraAdapter_RaceSafeParallelAppends(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	const n = 50
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			eid := fmt.Sprintf("evt-%d", i)
			_, err := a.AppendLeaf(context.Background(), makeLeaf("proj-A", eid))
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("parallel AppendLeaf: %v", err)
		}
	}
}

func TestMockTesseraAdapter_SetClock(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	fixed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	a.SetClock(func() time.Time { return fixed })
	var got tessera.STH
	a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		got = s
		return nil
	}))
	_, _ = a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	a.FlushAndPublishSTH(context.Background())
	if !got.Timestamp.Equal(fixed) {
		t.Errorf("STH.Timestamp = %v, want %v", got.Timestamp, fixed)
	}
}

func TestMockTesseraAdapter_NoGoroutineLeak(t *testing.T) {
	t.Parallel()
	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		a := tesseramock.New("proj-A")
		_, _ = a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
		_ = a.Close()
	}

	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+5 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestMockTesseraAdapter_NewPanicsOnEmptyProjectID(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on empty projectID, got none")
		}
	}()
	_ = tesseramock.New("")
}

func TestMockTesseraAdapter_VerifyMerkleInclusionMultiLeafTree(t *testing.T) {
	t.Parallel()
	for _, N := range []int{1, 2, 3, 4, 5, 7, 8, 11, 16, 17} {
		N := N
		t.Run(fmt.Sprintf("N=%d", N), func(t *testing.T) {
			t.Parallel()
			a := tesseramock.New("proj-A")
			defer a.Close()
			ids := make([]tessera.LeafID, N)
			for i := 0; i < N; i++ {
				id, err := a.AppendLeaf(context.Background(), makeLeaf("proj-A", fmt.Sprintf("evt-%03d", i)))
				if err != nil {
					t.Fatalf("AppendLeaf[%d]: %v", i, err)
				}
				ids[i] = id
			}
			for i, id := range ids {
				ok, err := a.VerifyMerkleInclusion(context.Background(), id)
				if err != nil {
					t.Errorf("VerifyMerkleInclusion[%d]: %v", i, err)
					continue
				}
				if !ok {
					t.Errorf("VerifyMerkleInclusion[%d] returned false for valid leaf in tree of size %d", i, N)
				}
			}
		})
	}
}

func TestMockTesseraAdapter_FlushAndPublishSTHEmptyTree(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	var got tessera.STH
	a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		got = s
		return nil
	}))
	if err := a.FlushAndPublishSTH(context.Background()); err != nil {
		t.Fatalf("FlushAndPublishSTH: %v", err)
	}
	if got.Size != 0 {
		t.Errorf("STH.Size = %d, want 0 for empty tree", got.Size)
	}
	if len(got.RootHash) != 32 {
		t.Errorf("STH.RootHash len = %d, want 32 even for empty tree", len(got.RootHash))
	}
	zero := make([]byte, 32)
	if !bytes.Equal(got.RootHash, zero) {
		t.Errorf("empty-tree STH.RootHash = %x, want 32 zero bytes", got.RootHash)
	}
}

func TestMockTesseraAdapter_FlushAndPublishSTHReturnsFirstSubscriberError(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	want := errors.New("subscriber-A failure")
	calls := 0
	a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, _ tessera.STH) error {
		calls++
		return want
	}))
	a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, _ tessera.STH) error {
		calls++
		return nil
	}))
	err := a.FlushAndPublishSTH(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("FlushAndPublishSTH err = %v, want %v", err, want)
	}
	if calls != 2 {
		t.Errorf("subscriber calls = %d, want 2 (both subs dispatched despite first error)", calls)
	}
}

func TestMockTesseraAdapter_FlushAndPublishSTHAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	_ = a.Close()
	err := a.FlushAndPublishSTH(context.Background())
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("FlushAndPublishSTH after Close: err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_VerifyMerkleInclusionAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	id, _ := a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	_ = a.Close()
	ok, err := a.VerifyMerkleInclusion(context.Background(), id)
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("VerifyMerkleInclusion after Close: err = %v, want ErrAdapterClosed", err)
	}
	if ok {
		t.Errorf("VerifyMerkleInclusion after Close: ok = true, want false")
	}
}

func TestMockTesseraAdapter_WitnessCoSignSealAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	w, _ := tesseramock.NewMockWitness()
	a.Attach(w)
	_ = a.Close()
	_, err := a.WitnessCoSignSeal(context.Background(), tessera.LeafID("x"), []byte("p"))
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("WitnessCoSignSeal after Close: err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_VerifySealSignatureAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	w, _ := tesseramock.NewMockWitness()
	a.Attach(w)
	_ = a.Close()
	_, err := a.VerifySealSignature(context.Background(), []byte("p"), []byte("sig"))
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("VerifySealSignature after Close: err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_AppendSealAfterCloseRejected(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	_ = a.Close()
	_, err := a.AppendSeal(context.Background(), "proj-A", "part-1", []byte("p"))
	if !errors.Is(err, tessera.ErrAdapterClosed) {
		t.Errorf("AppendSeal after Close: err = %v, want ErrAdapterClosed", err)
	}
}

func TestMockTesseraAdapter_SetClockNilResets(t *testing.T) {
	t.Parallel()
	a := tesseramock.New("proj-A")
	defer a.Close()
	fixed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	a.SetClock(func() time.Time { return fixed })
	a.SetClock(nil)
	var got tessera.STH
	a.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		got = s
		return nil
	}))
	_, _ = a.AppendLeaf(context.Background(), makeLeaf("proj-A", "evt-001"))
	before := time.Now()
	a.FlushAndPublishSTH(context.Background())
	after := time.Now()
	if got.Timestamp.Before(before) || got.Timestamp.After(after) {
		t.Errorf("SetClock(nil) did not reset to time.Now: got = %v, want between %v and %v", got.Timestamp, before, after)
	}
}

func TestVerifyWithPubkey_NilPubkeyReturnsFalse(t *testing.T) {
	t.Parallel()
	if tesseramock.VerifyWithPubkey(nil, []byte("digest"), []byte("sig")) {
		t.Errorf("VerifyWithPubkey(nil, ...) = true, want false")
	}
}

func TestVerifyWithPubkey_EmptySigReturnsFalse(t *testing.T) {
	t.Parallel()
	w, _ := tesseramock.NewMockWitness()
	pub, _ := w.Load()
	if tesseramock.VerifyWithPubkey(pub, []byte("digest"), nil) {
		t.Errorf("VerifyWithPubkey(pub, _, nil) = true, want false")
	}
	if tesseramock.VerifyWithPubkey(pub, []byte("digest"), []byte{}) {
		t.Errorf("VerifyWithPubkey(pub, _, []) = true, want false")
	}
}
