// SPDX-License-Identifier: MIT
// Package cleanup — apply.go ships the retention-policy enforcement per
// Q12=D + invariant. One audit event evt.state.cleanup.deleted per
// expired path (spec §3.7).
//
// Apply is idempotent: re-running with the same policy + state produces
// the same result (zero expirations on second run). Dry-run mode lets
// operators preview without mutating; --keep IDs except specific entries
// even past retention.
package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const AuditEventType = "evt.state.cleanup.deleted"

type Options struct {
	StateDir string
	CacheDir string
	Policy   Policy
	KeepIDs  []string
	DryRun   bool
	Emitter  Emitter
}

type Emitter interface {
	Emit(ctx context.Context, eventType string, payload []byte) (auditHash string, err error)
}

func Apply(ctx context.Context, opts Options) (int, error) {
	entries, err := Enumerate(ctx, opts.StateDir, opts.CacheDir)
	if err != nil {
		return 0, fmt.Errorf("cleanup.Apply: enumerate: %w", err)
	}

	keep := make(map[string]bool, len(opts.KeepIDs))
	for _, id := range opts.KeepIDs {
		keep[id] = true
	}

	expired := 0
	for _, e := range entries {
		if keep[e.ID] {
			continue
		}
		ttl := ttlForSubsystem(e.Subsystem, opts.Policy)
		if ttl == 0 {
			continue
		}
		if e.Age < ttl {
			continue
		}
		expired++
		if opts.DryRun {
			continue
		}
		if err := os.RemoveAll(e.Path); err != nil {
			return expired, fmt.Errorf("cleanup.Apply: remove %s: %w", e.Path, err)
		}
		if opts.Emitter != nil {
			payload, _ := json.Marshal(map[string]any{
				"path":      e.Path,
				"subsystem": e.Subsystem,
				"id":        e.ID,
				"ageNs":     int64(e.Age),
				"deletedAt": time.Now().UTC(),
			})
			_, _ = opts.Emitter.Emit(ctx, AuditEventType, payload)
		}
	}
	return expired, nil
}

func ttlForSubsystem(subsystem string, policy Policy) time.Duration {
	switch subsystem {
	case "doctor-backups":
		return policy.DoctorBackupsTTL
	case "migrate-backups":
		return policy.MigrateBackupsTTL
	case "spike-artifacts":
		return policy.SpikeArtifactsTTL
	case "cache":
		return policy.CacheTTL
	default:
		return 0 // unknown subsystem: do not expire (forward-compatible)
	}
}
