// SPDX-License-Identifier: MIT
// activation.go — release : orchestration entry point.
//
// Activate is the load-bearing function every release+ caller consumes
// for project-identity bootstrap. Per spec §3.1, it resolves the
// canonical project_id from canonicalPath, looks up the alias in the
// store, detects mv if applicable, and INSERTs or UPDATEs as needed.
//
// inv-hades-031 + inv-hades-122: Activate operates against the
// ProjectStore interface; the concrete adapter (internal/daemon/
// projectctxadapter) is the ONLY package crossing the projectctx /
// store boundary.
package projectctx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type ActivationResult struct {
	Project           *Project
	IsFirstActivation bool
	MvDetected        *MvDetection
}

func Activate(ctx context.Context, store ProjectStore, canonicalPath string, alias Alias) (*ActivationResult, error) {
	if store == nil {
		return nil, errors.New("projectctx.Activate: store is nil")
	}
	if err := alias.Validate(); err != nil {
		return nil, fmt.Errorf("projectctx.Activate: %w", err)
	}

	canonical, err := CanonicalPath(canonicalPath)
	if err != nil {
		return nil, fmt.Errorf("projectctx.Activate: %w", err)
	}
	sum := sha256.Sum256([]byte(canonical))
	currentID := ProjectID(hex.EncodeToString(sum[:]))
	now := time.Now()

	known, err := store.GetByAlias(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("projectctx.Activate: GetByAlias: %w", err)
	}
	if known == nil {

		p := &Project{
			ID:            currentID,
			Alias:         alias,
			CanonicalPath: canonical,
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}
		if err := store.Insert(ctx, p); err != nil {
			return nil, fmt.Errorf("projectctx.Activate: Insert: %w", err)
		}
		if err := store.AppendPathHistory(ctx, &PathHistoryEntry{
			ProjectID:   currentID,
			Path:        canonical,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}); err != nil {
			return nil, fmt.Errorf("projectctx.Activate: AppendPathHistory: %w", err)
		}

		fresh, _ := store.GetByAlias(ctx, alias)
		var snapshot Project
		if fresh != nil {
			snapshot = *fresh
		} else {
			snapshot = *p
		}
		return &ActivationResult{
			Project:           &snapshot,
			IsFirstActivation: true,
		}, nil
	}
	if known.ID == currentID {

		if err := store.UpdateLastSeen(ctx, alias, now); err != nil {
			return nil, fmt.Errorf("projectctx.Activate: UpdateLastSeen: %w", err)
		}
		if err := store.AppendPathHistory(ctx, &PathHistoryEntry{
			ProjectID:   currentID,
			Path:        canonical,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}); err != nil {
			return nil, fmt.Errorf("projectctx.Activate: AppendPathHistory: %w", err)
		}
		fresh, _ := store.GetByAlias(ctx, alias)
		var snapshot Project
		if fresh != nil {
			snapshot = *fresh
		} else {
			snapshot = *known
		}
		return &ActivationResult{
			Project:           &snapshot,
			IsFirstActivation: false,
		}, nil
	}

	history, err := store.GetPathHistory(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("projectctx.Activate: GetPathHistory: %w", err)
	}
	mv := DetectMv(alias, canonical, currentID, history)

	snapshot := *known
	return &ActivationResult{
		Project:    &snapshot,
		MvDetected: mv,
	}, nil
}
