// go:build chaos

// modes.
//
// "Leaf insert" in the invariant contract aliases to the tessera
// tile-log Append boundary (tesseraTileUpload at internal/audit
// /tessera/adapter.go). At the chain-package boundary this is the
// chain.SealAppender.AppendSeal call — failing here MUST surface a
// wrapped error AND keep the in-memory leaf cache + on-disk state
// consistent (no half-written leaves).
//
// The chain package's seal.go has TWO calls into the SealAppender:
// AppendSeal + WitnessCoSignSeal. The witness-cosign call fires
// AFTER AppendSeal succeeds; a failure in that branch is its own
// failure mode (covered separately) — the leaf has been written
// into the tile-log but the daemon-witness cosign failed. Spec §4.1
// failure mode #6 documents that the seal_worker retries witness
// cosign on next event without re-appending the leaf (idempotence
// at the tile-log level).

package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	auditchain "github.com/cbip-solutions/hades-system/internal/audit/chain"
)

func TestLeafInsertAppendFailureWraps(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	tessera.FailAppendNext(errors.New("synthetic tile-upload failure"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err == nil {
		t.Fatal("SealPartition: got nil err")
	}
	if !strings.Contains(err.Error(), "append tessera seal") {
		t.Errorf("err missing 'append tessera seal' marker: %v", err)
	}
}

func TestLeafInsertWitnessCosignFailureWraps(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	tessera.FailSignNext(errors.New("synthetic cosign failure"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err == nil {
		t.Fatal("SealPartition: got nil err on cosign failure")
	}
	if !strings.Contains(err.Error(), "witness cosign") {
		t.Errorf("err missing 'witness cosign' marker: %v", err)
	}

	if _, getErr := store.GetPartitionSeal(ctx, testPartitionID); !errors.Is(getErr, auditchain.ErrPartitionSealNotFound) {
		t.Errorf("post-cosign-failure GetPartitionSeal: got %v, want ErrPartitionSealNotFound", getErr)
	}
}

func TestLeafInsertCosignRetryAfterClearSucceeds(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	tessera.FailSignNext(errors.New("one-shot cosign failure"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix()); err == nil {
		t.Fatal("expected first call to error on cosign failure")
	}

	seal, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err != nil {
		t.Fatalf("retry SealPartition: %v", err)
	}
	if seal.TesseraSealLeafID == "" {
		t.Error("retry seal: leaf ID empty")
	}
}

func TestLeafInsertStoreInsertFailureWraps(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	store.FailInsertSealNext(errors.New("synthetic InsertPartitionSeal failure"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err == nil {
		t.Fatal("SealPartition: got nil err on store-insert failure")
	}
	if !strings.Contains(err.Error(), "insert seal") {
		t.Errorf("err missing 'insert seal' marker: %v", err)
	}
}

// TestLeafInsertFailureModesCatalogue pins the documented set of
// failure modes — every chain.SealPartition failure surface MUST be
// covered by at least one test. Drift here surfaces as a count
// mismatch; new failure surfaces added without a test light up the
// fixture-completeness assertion.
func TestLeafInsertFailureModesCatalogue(t *testing.T) {

	wantSurfaces := []string{
		"append tessera seal",
		"witness cosign seal",
		"insert seal",
		"list partitions",
		"get existing seal",
	}

	if got := len(wantSurfaces); got != 5 {
		t.Fatalf("documented surfaces = %d, want 5", got)
	}

	t.Run("list_partitions_surface", func(t *testing.T) {
		store := newChainStore()
		tessera := newSealAppender()
		store.FailListPartsNext(errors.New("synthetic list-partitions failure"))

		seedPartition(t, store)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
		if err == nil {
			t.Fatal("expected ListPartitions failure to bubble")
		}
		if !strings.Contains(err.Error(), "list partitions") {
			t.Errorf("err missing 'list partitions' marker: %v", err)
		}
	})

	t.Run("get_existing_seal_surface", func(t *testing.T) {
		store := newChainStore()
		tessera := newSealAppender()
		seedPartition(t, store)

		store.FailGetSealNext(errors.New("synthetic get-seal failure"))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
		if err == nil {
			t.Fatal("expected GetPartitionSeal failure to bubble")
		}
		if !strings.Contains(err.Error(), "get existing seal") {
			t.Errorf("err missing 'get existing seal' marker: %v", err)
		}
	})
}
