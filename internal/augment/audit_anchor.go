// SPDX-License-Identifier: MIT
// Package augment — audit_anchor.go ships HADES design Tessera-anchored leaf
// emission for the 7 augmentation event types.
//
// Single chokepoint for audit-chain emission from internal/augment/:
// AuditAnchor.Emit(ctx, eventType, payload, projectID) (anchor, err).
// All other augment files invoke this method exclusively.
//
// Anchor format: <partition>:<eventID>:<recordHash>
// where:
// - partition = YYYY_MM from clock.Now().UTC() (chain.PartitionID convention)
// - eventID = "evt-<unix_nano>-<hex(8 random bytes)>"
// - recordHash = chain.Compute(prevHash, eventType, payload, unix_seconds)
//
// package shipped a private `computeRecordHash` using NUL-byte
// separators + hex-encoded nano timestamps; chain.Compute (the canonical
// verification) uses pipe '|' separators + decimal unix-seconds. Hashes
// diverged for 100% of augmentation events, flagging every event as
// Tampered by the audit-chain-integrity doctor. Fix: delete
// computeRecordHash and route through chain.Compute. Cross-implementation
// algorithm compatibility is now load-bearing — TestAuditAnchor_RecordHash*
// pin byte-identical output across both call sites.

package augment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
)

func NewAuditAnchor(store ChainStore, clock Clock) *AuditAnchor {
	if clock == nil {
		clock = SystemClock{}
	}
	return &AuditAnchor{store: store, clock: clock}
}

func (a *AuditAnchor) Emit(ctx context.Context, eventType EventType, payload []byte, projectID string) (string, error) {
	prevHash, err := a.store.GetChainTip(ctx)
	if err != nil {
		return "", fmt.Errorf("audit_anchor: GetChainTip: %w", err)
	}

	now := a.clock.Now().UTC()
	timestampNano := now.UnixNano()
	timestampSeconds := now.Unix()

	eventID, err := generateEventID(timestampNano)
	if err != nil {

		return "", fmt.Errorf("audit_anchor: generateEventID: %w", err)
	}

	partitionID := chain.PartitionID(timestampSeconds)

	recordHash, err := chain.Compute(prevHash, eventType.String(), payload, timestampSeconds)
	if err != nil {
		return "", fmt.Errorf("audit_anchor: chain.Compute %s: %w", eventID, err)
	}

	if err := a.store.UpdateChainColumns(ctx, eventID, prevHash, eventType.String(), payload, timestampSeconds, recordHash, partitionID); err != nil {
		return "", fmt.Errorf("audit_anchor: UpdateChainColumns %s: %w", eventID, err)
	}

	leafID, err := a.store.AppendTesseraLeaf(ctx, TesseraLeafInput{
		EventID:    eventID,
		EventType:  eventType.String(),
		ProjectID:  projectID,
		Partition:  partitionID,
		Payload:    payload,
		RecordHash: recordHash,
	})
	if err != nil {
		return "", fmt.Errorf("audit_anchor: AppendTesseraLeaf %s: %w", eventID, err)
	}

	if err := a.store.UpdateTesseraLeafID(ctx, eventID, leafID); err != nil {
		return "", fmt.Errorf("audit_anchor: UpdateTesseraLeafID %s: %w", eventID, err)
	}

	anchor := fmt.Sprintf("%s:%s:%s", partitionID, eventID, recordHash)
	return anchor, nil
}

func generateEventID(timestampNano int64) (string, error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {

		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return fmt.Sprintf("evt-%d-%s", timestampNano, hex.EncodeToString(rnd[:])), nil
}
