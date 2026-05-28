// SPDX-License-Identifier: MIT
// Package cleanup ships the HADES design state-retention policy
// enforcement per design choice + spec §2.12 + §4.6 + invariant.
//
// Default retention per design contract:
//
// doctor-backups 30d
// migrate-backups 30d
// spike-artifacts indefinite
// cache 7d LRU
//
// Override doctrine TOML `[state.backup_retention]` (HADES design
// F7 extends the HADES design schema with the override section
// shape). The override merges field-by-field via Policy.MergeOverride.
//
// Boundary (invariant): cleanup consumes ONLY stdlib + caller-injected
// Emitter; MUST NOT import internal/store. State enumeration uses
// os.ReadDir on the XDG state root (no daemon HTTP round-trip).
package cleanup

import "time"

type Policy struct {
	DoctorBackupsTTL  time.Duration
	MigrateBackupsTTL time.Duration
	SpikeArtifactsTTL time.Duration
	CacheTTL          time.Duration
}

func DefaultPolicy() Policy {
	return Policy{
		DoctorBackupsTTL:  30 * 24 * time.Hour,
		MigrateBackupsTTL: 30 * 24 * time.Hour,
		SpikeArtifactsTTL: 0,
		CacheTTL:          7 * 24 * time.Hour,
	}
}

func (p Policy) MergeOverride(override Override) Policy {
	q := p
	if override.DoctorBackupsDays > 0 {
		q.DoctorBackupsTTL = time.Duration(override.DoctorBackupsDays) * 24 * time.Hour
	}
	if override.MigrateBackupsDays > 0 {
		q.MigrateBackupsTTL = time.Duration(override.MigrateBackupsDays) * 24 * time.Hour
	}
	if override.SpikeArtifactsIndefinite {
		q.SpikeArtifactsTTL = 0
	} else if override.SpikeArtifactsDays > 0 {
		q.SpikeArtifactsTTL = time.Duration(override.SpikeArtifactsDays) * 24 * time.Hour
	}
	if override.CacheDays > 0 {
		q.CacheTTL = time.Duration(override.CacheDays) * 24 * time.Hour
	}
	return q
}

type Override struct {
	DoctorBackupsDays        int  `toml:"doctor_backups_days"`
	MigrateBackupsDays       int  `toml:"migrate_backups_days"`
	SpikeArtifactsIndefinite bool `toml:"spike_artifacts_indefinite"`
	SpikeArtifactsDays       int  `toml:"spike_artifacts_days"`
	CacheDays                int  `toml:"cache_days"`
}
