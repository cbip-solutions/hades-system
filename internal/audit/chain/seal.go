// SPDX-License-Identifier: MIT
package chain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
)

var ErrPartitionEmpty = errors.New("chain: partition has no events to seal")

// SealAppender is the Tessera-side contract SealPartition needs.
// Satisfied by an adapter that bridges chain (pure-Go, no tessera
// import) to internal/audit/tessera.Adapter. review
// CRITICAL-1 + CRITICAL-7 alignment: the LeafID is exchanged here as
// `string` (caller cast from tessera.LeafID via `string(leafID)`); the
// daemon witness signature is exchanged as `[]byte` (raw ECDSA bytes).
// Both shapes match audit_partition_seals column types
// (tessera_seal_leaf_id TEXT, daemon_witness_signature BLOB) directly
// so the auditadapter.InsertPartitionSeal call site is a no-op
// translation.
//
// AppendSeal MUST be idempotent on (projectID, partitionID): if a leaf
// for this partition already exists, return the existing leaf_id without
// appending again. The spec mandates this property (
// task A-5b); B-6 relies on it for the recovery path (spec §4.1
// failure mode #6).
//
// WitnessCoSignSeal signs the seal payload bytes with the daemon
// witness key (ECDSA P-256) and returns the raw signature bytes.
// Backed by task A-6b.
//
// VerifySealSignature returns true iff `sig` is a valid daemon witness
// signature over sha256(payload) under the currently-attached daemon
// witness pubkey. tessera.Adapter satisfies this directly
// (see internal/audit/tessera.Adapter.VerifySealSignature); recovery
// callers and seal-verify worker
// consume it via this same interface. Closes the gap CRITICAL-2
// surfaced: pre-fix VerifySeal silently returned nil even when the
// stored daemon_witness_signature was missing, forged, or
// bytes-corrupted.
//
// Error semantics: a non-nil error means the verify path itself
// failed (witness key detached, backend I/O failure, etc.) — chain
// callers MUST treat that as a transient infra error and bubble it
// without conflating with ErrChainTampered. A (false, nil) return
// means the signature is well-formed but does not validate under the
// active pubkey, which IS a tamper signal.
type SealAppender interface {
	AppendSeal(ctx context.Context, projectID, partitionID string, payload []byte) (leafID string, err error)
	WitnessCoSignSeal(ctx context.Context, leafID string, payload []byte) (signature []byte, err error)
	VerifySealSignature(ctx context.Context, payload, sig []byte) (ok bool, err error)
}

func SealPartition(
	ctx context.Context,
	store EventStore,
	tessera SealAppender,
	projectID, partitionID string,
	now int64,
) (SealRecord, error) {

	existing, err := store.GetPartitionSeal(ctx, partitionID)
	if err == nil {
		return *existing, nil
	}
	if !errors.Is(err, ErrPartitionSealNotFound) {
		return SealRecord{}, fmt.Errorf("chain.SealPartition: get existing seal: %w", err)
	}

	parts, err := store.ListPartitions(ctx)
	if err != nil {
		return SealRecord{}, fmt.Errorf("chain.SealPartition: list partitions: %w", err)
	}
	var ps *PartitionStat
	for i := range parts {
		if parts[i].PartitionID == partitionID {
			ps = &parts[i]
			break
		}
	}
	if ps == nil || ps.EventCount == 0 {
		return SealRecord{}, ErrPartitionEmpty
	}

	payload := buildSealPayload(partitionID, ps.FinalRecordHash, ps.EventCount, ps.LastID)

	leafID, err := tessera.AppendSeal(ctx, projectID, partitionID, payload)
	if err != nil {
		return SealRecord{}, fmt.Errorf("chain.SealPartition: append tessera seal: %w", err)
	}

	sig, err := tessera.WitnessCoSignSeal(ctx, leafID, payload)
	if err != nil {
		return SealRecord{}, fmt.Errorf("chain.SealPartition: witness cosign seal: %w", err)
	}

	seal := SealRecord{
		PartitionID:            partitionID,
		SealedAt:               now,
		FinalRecordHash:        ps.FinalRecordHash,
		TesseraSealLeafID:      leafID,
		DaemonWitnessSignature: string(sig),
	}
	if err := store.InsertPartitionSeal(ctx, seal); err != nil {

		existing, gerr := store.GetPartitionSeal(ctx, partitionID)
		if gerr == nil {
			return *existing, nil
		}
		return SealRecord{}, fmt.Errorf("chain.SealPartition: insert seal: %w", err)
	}
	return seal, nil
}

func buildSealPayload(partitionID, finalRecordHash string, eventCount int64, lastID string) []byte {
	h := sha256.New()
	h.Write([]byte(partitionID))
	h.Write([]byte{'|'})
	h.Write([]byte(finalRecordHash))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(eventCount, 10)))
	h.Write([]byte{'|'})
	h.Write([]byte(lastID))
	return h.Sum(nil)
}

// VerifySeal checks that the partition's stored seal is consistent with
// the events in audit_events_raw AND that the daemon witness signature
// stored on the seal row validates against the daemon's currently-active
// witness pubkey. Used by verify-chain and the
// audit.chain-integrity doctor check.
//
// # Steps
//
// 1. Read seal from store.
// 2. Read current partition stats.
// 3. Compare seal.FinalRecordHash against current partition tip
// (catches post-seal row mutation / append).
// 4. Reconstruct the canonical seal payload (byte-identical to the
// buffer SealPartition originally signed) and verify the daemon
// witness signature via the SealAppender's VerifySealSignature.
//
// Step 4 closes the gap CRITICAL-2 surfaced: pre-fix the verify path
// returned nil silently even when DaemonWitnessSignature was missing,
// forged, or bytes-corrupted. The verification primitive lives on the
// tessera-side adapter (chain stays stdlib-pure / no crypto/ecdsa
// import per design); the SealAppender extension makes it accessible
// without importing internal/audit/tessera here (preserves
// invariant).
//
// Returns
// - ErrPartitionSealMissing when no seal row exists.
// - ErrChainTampered when final_record_hash drifts OR when the
// witness signature does not validate under the active pubkey.
// - wrapped non-typed errors for transient infra failures (store
// I/O, witness-verify backend errors). Callers in
// seal-verify worker MUST distinguish via errors.Is so
// doctrine-specific halt routing fires only on real tamper.
func VerifySeal(
	ctx context.Context,
	store EventStore,
	tessera SealAppender,
	projectID, partitionID string,
) error {

	seal, err := store.GetPartitionSeal(ctx, partitionID)
	if err != nil {
		if errors.Is(err, ErrPartitionSealNotFound) {
			return ErrPartitionSealMissing
		}
		return fmt.Errorf("chain.VerifySeal: get seal: %w", err)
	}

	parts, err := store.ListPartitions(ctx)
	if err != nil {
		return fmt.Errorf("chain.VerifySeal: list partitions: %w", err)
	}
	var ps *PartitionStat
	for i := range parts {
		if parts[i].PartitionID == partitionID {
			ps = &parts[i]
			break
		}
	}
	if ps == nil {
		return fmt.Errorf("chain.VerifySeal: partition %q vanished", partitionID)
	}

	if seal.FinalRecordHash != ps.FinalRecordHash {
		return fmt.Errorf("%w: partition %q seal=%s actual=%s",
			ErrChainTampered, partitionID, seal.FinalRecordHash, ps.FinalRecordHash)
	}

	payload := buildSealPayload(partitionID, seal.FinalRecordHash, ps.EventCount, ps.LastID)
	ok, verr := tessera.VerifySealSignature(ctx, payload, []byte(seal.DaemonWitnessSignature))
	if verr != nil {
		// Transient infra error (witness key detached, backend I/O,
		// etc.). Wrap + bubble; do NOT classify as tamper.
		return fmt.Errorf("chain.VerifySeal: witness sig verify: %w", verr)
	}
	if !ok {
		return fmt.Errorf("%w: partition %q witness signature invalid",
			ErrChainTampered, partitionID)
	}

	return nil
}
