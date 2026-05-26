// SPDX-License-Identifier: MIT
package auditadapter

import (
	"context"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type TesseraAdapter interface {
	AppendLeaf(ctx context.Context, leaf tessera.Leaf) (tessera.LeafID, error)
}

func (a *Adapter) dispatchTessera(ctx context.Context, projectID, eventID string, payloadHash, recordHash []byte) (tessera.LeafID, error) {
	if a.tessera == nil {
		return "", nil
	}
	leaf := tessera.Leaf{
		EventID:     eventID,
		EventType:   "",
		PayloadHash: payloadHash,
		RecordHash:  recordHash,
		ProjectID:   projectID,
	}
	id, err := a.tessera.AppendLeaf(ctx, leaf)
	if err != nil {
		return "", fmt.Errorf("auditadapter: dispatch tessera (project=%s event=%s): %w", projectID, eventID, err)
	}
	return id, nil
}
