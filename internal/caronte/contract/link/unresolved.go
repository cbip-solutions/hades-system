//go:build cgo

// SPDX-License-Identifier: MIT

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type UnresolvedStorePort interface {
	Insert(ctx context.Context, row federation.UnresolvedRow) error
}

type unresolvedSurfacer struct {
	store       UnresolvedStorePort
	audit       federation.AuditEmitter
	workspaceID string
}

func (s *unresolvedSurfacer) Surface(ctx context.Context, call store.APICall, policy yaml.UnresolvedPolicy, reason string) error {
	switch policy {
	case yaml.PolicySurface:
		row := federation.UnresolvedRow{
			WorkspaceID: s.workspaceID,
			CallID:      call.CallID,
			CallRepo:    call.Repo,
			BaseURLRef:  call.BaseURLRef,
			Reason:      reason,
			RecordedAt:  time.Now().UnixNano(),
		}
		if err := s.store.Insert(ctx, row); err != nil {
			return fmt.Errorf("caronte/link: insert unresolved row: %w", err)
		}
		payload, _ := json.Marshal(row)

		if err := s.audit.Emit(ctx, federation.EvtUnresolvedCall, payload); err != nil {
			return fmt.Errorf("caronte/link: emit audit: %w", err)
		}
		return nil
	case yaml.PolicyFail:
		return fmt.Errorf("%w: %s", ErrNoManifestEntry, reason)
	case yaml.PolicySilent:

		return nil
	default:
		return fmt.Errorf("caronte/link: unknown unresolved_policy %q", policy)
	}
}
