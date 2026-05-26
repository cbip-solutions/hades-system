// SPDX-License-Identifier: MIT
package daemon

import (
	"context"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/bypassadmin"
	"github.com/cbip-solutions/hades-system/internal/store"
)

const defaultAuditRetention = 30 * 24 * time.Hour

type AuditRetention struct {
	store     *store.Store
	retention time.Duration
}

var _ bypassadmin.Retention = (*AuditRetention)(nil)

func newAuditRetention(st *store.Store, retention time.Duration) *AuditRetention {
	if retention <= 0 {
		retention = defaultAuditRetention
	}
	return &AuditRetention{store: st, retention: retention}
}

func (r *AuditRetention) ListPins() ([]bypassadmin.AuditPin, error) {
	rows, err := r.store.ListBypassAuditPins()
	if err != nil {
		return nil, err
	}
	out := make([]bypassadmin.AuditPin, len(rows))
	for i, row := range rows {
		out[i] = bypassadmin.AuditPin{
			ConversationID: row.ConversationID,
			PinnedAt:       row.PinnedAt,
			Reason:         row.Reason,
		}
	}
	return out, nil
}

func (r *AuditRetention) Pin(conversationID, reason string) error {
	return r.store.UpsertBypassAuditPin(conversationID, time.Now().Unix(), reason)
}

func (r *AuditRetention) Unpin(conversationID string) error {
	return r.store.DeleteBypassAuditPin(conversationID)
}

func (r *AuditRetention) DryRun(ctx context.Context) (int, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	cutoff := time.Now().Add(-r.retention).Unix()
	exempt, err := r.store.PinnedConversationIDs()
	if err != nil {
		return 0, 0, err
	}
	candidates, err := r.store.CountBypassAuditOlderThan(cutoff, exempt)
	if err != nil {
		return 0, 0, err
	}
	freed, err := r.store.SizeBypassAuditBodiesOlderThan(cutoff, exempt)
	if err != nil {
		return 0, 0, err
	}
	return candidates, freed, nil
}

func (r *AuditRetention) Purge(ctx context.Context) (int, int64, error) {
	candidates, freed, err := r.DryRun(ctx)
	if err != nil {
		return 0, 0, err
	}
	if candidates == 0 {
		return 0, freed, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	cutoff := time.Now().Add(-r.retention).Unix()
	exempt, err := r.store.PinnedConversationIDs()
	if err != nil {
		return 0, 0, err
	}
	purged, err := r.store.PurgeBypassAuditOlderThan(cutoff, exempt)
	if err != nil {
		return 0, 0, err
	}
	return purged, freed, nil
}

func (r *AuditRetention) startPurgeJob(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _, _ = r.Purge(ctx)
			}
		}
	}()
	return done
}
