// SPDX-License-Identifier: MIT
package federation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type tesseraAdapterShim = any
type tesseraLeafShim = tessera.Leaf
type tesseraLeafIDShim = tessera.LeafID

var appendLeafFn = func(ctx context.Context, adapter tesseraAdapterShim, leaf tesseraLeafShim) (tesseraLeafIDShim, error) {
	real, ok := adapter.(*tessera.Adapter)
	if !ok {
		return "", fmt.Errorf("caronte/store/federation: appendLeafFn invoked with non-*tessera.Adapter: %T", adapter)
	}
	return real.AppendLeaf(ctx, leaf)
}

func EmitAudit(ctx context.Context, adapter *tessera.Adapter, e Event) (tessera.LeafID, error) {
	if !e.Type.Valid() {
		return "", fmt.Errorf("%w: got %q", ErrUnknownEventType, string(e.Type))
	}
	if adapter == nil {

		return "", nil
	}
	payloadHash, err := computeAuditPayloadHash(e)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorruptAuditLeaf, err)
	}
	recordHash := computeAuditRecordHash(e, payloadHash)
	leaf := tessera.Leaf{
		EventID:     fmt.Sprintf("%s:%s:%d", e.Type, e.WorkspaceID, e.OccurredAt),
		EventType:   string(e.Type),
		PayloadHash: payloadHash,
		RecordHash:  recordHash,
	}
	id, err := appendLeafFn(ctx, adapter, leaf)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorruptAuditLeaf, err)
	}
	return id, nil
}

func computeAuditPayloadHash(e Event) ([]byte, error) {

	encoded, err := json.Marshal(struct {
		Type        string `json:"type"`
		WorkspaceID string `json:"workspace_id"`
		Payload     []byte `json:"payload"`
		OccurredAt  int64  `json:"occurred_at"`
	}{
		Type:        string(e.Type),
		WorkspaceID: e.WorkspaceID,
		Payload:     e.Payload,
		OccurredAt:  e.OccurredAt,
	})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	return sum[:], nil
}

func computeAuditRecordHash(e Event, payloadHash []byte) []byte {
	h := sha256.New()
	h.Write([]byte(string(e.Type)))
	h.Write([]byte{0x1f})
	h.Write([]byte(e.WorkspaceID))
	h.Write([]byte{0x1f})
	var ts [8]byte
	for i := 0; i < 8; i++ {
		ts[i] = byte(e.OccurredAt >> (8 * (7 - i)))
	}
	h.Write(ts[:])
	h.Write([]byte{0x1e})
	h.Write(payloadHash)
	return h.Sum(nil)
}

type emitterImpl struct {
	adapter     *tessera.Adapter
	workspaceID string
}

func (e *emitterImpl) Emit(ctx context.Context, t EventType, payload []byte) error {
	_, err := EmitAudit(ctx, e.adapter, Event{
		Type:        t,
		WorkspaceID: e.workspaceID,
		Payload:     payload,
		OccurredAt:  time.Now().UnixNano(),
	})
	return err
}

func NewAuditEmitter(adapter *tessera.Adapter, workspaceID string) AuditEmitter {
	return &emitterImpl{adapter: adapter, workspaceID: workspaceID}
}
