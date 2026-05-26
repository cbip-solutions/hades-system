package cleanup_test

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/state/cleanup"
)

func TestDefaultPolicyValues(t *testing.T) {
	p := cleanup.DefaultPolicy()
	if p.DoctorBackupsTTL != 30*24*time.Hour {
		t.Errorf("DoctorBackupsTTL = %v, want 30d", p.DoctorBackupsTTL)
	}
	if p.MigrateBackupsTTL != 30*24*time.Hour {
		t.Errorf("MigrateBackupsTTL = %v, want 30d", p.MigrateBackupsTTL)
	}
	if p.SpikeArtifactsTTL != 0 {
		t.Errorf("SpikeArtifactsTTL = %v, want 0 (indefinite)", p.SpikeArtifactsTTL)
	}
	if p.CacheTTL != 7*24*time.Hour {
		t.Errorf("CacheTTL = %v, want 7d", p.CacheTTL)
	}
}

func TestMergeOverrideAllFields(t *testing.T) {
	base := cleanup.DefaultPolicy()
	override := cleanup.Override{
		DoctorBackupsDays:  90,
		MigrateBackupsDays: 60,
		SpikeArtifactsDays: 365,
		CacheDays:          14,
	}
	merged := base.MergeOverride(override)
	if merged.DoctorBackupsTTL != 90*24*time.Hour {
		t.Errorf("DoctorBackupsTTL = %v, want 90d", merged.DoctorBackupsTTL)
	}
	if merged.MigrateBackupsTTL != 60*24*time.Hour {
		t.Errorf("MigrateBackupsTTL = %v, want 60d", merged.MigrateBackupsTTL)
	}
	if merged.SpikeArtifactsTTL != 365*24*time.Hour {
		t.Errorf("SpikeArtifactsTTL = %v, want 365d", merged.SpikeArtifactsTTL)
	}
	if merged.CacheTTL != 14*24*time.Hour {
		t.Errorf("CacheTTL = %v, want 14d", merged.CacheTTL)
	}
}

func TestMergeOverrideSpikeIndefiniteTakesPrecedence(t *testing.T) {
	base := cleanup.DefaultPolicy()
	override := cleanup.Override{
		SpikeArtifactsIndefinite: true,
		SpikeArtifactsDays:       42,
	}
	merged := base.MergeOverride(override)
	if merged.SpikeArtifactsTTL != 0 {
		t.Errorf("SpikeArtifactsTTL = %v, want 0 (Indefinite=true)", merged.SpikeArtifactsTTL)
	}
}

func TestMergeOverrideZeroValuesKeepBase(t *testing.T) {
	base := cleanup.DefaultPolicy()
	merged := base.MergeOverride(cleanup.Override{})
	if merged.DoctorBackupsTTL != base.DoctorBackupsTTL {
		t.Errorf("DoctorBackupsTTL drifted: %v vs %v", merged.DoctorBackupsTTL, base.DoctorBackupsTTL)
	}
	if merged.MigrateBackupsTTL != base.MigrateBackupsTTL {
		t.Errorf("MigrateBackupsTTL drifted: %v vs %v", merged.MigrateBackupsTTL, base.MigrateBackupsTTL)
	}
	if merged.SpikeArtifactsTTL != base.SpikeArtifactsTTL {
		t.Errorf("SpikeArtifactsTTL drifted: %v vs %v", merged.SpikeArtifactsTTL, base.SpikeArtifactsTTL)
	}
	if merged.CacheTTL != base.CacheTTL {
		t.Errorf("CacheTTL drifted: %v vs %v", merged.CacheTTL, base.CacheTTL)
	}
}

func TestMergeOverrideSpikeDaysOnlyWhenNotIndefinite(t *testing.T) {
	base := cleanup.DefaultPolicy()
	merged := base.MergeOverride(cleanup.Override{
		SpikeArtifactsIndefinite: false,
		SpikeArtifactsDays:       10,
	})
	if merged.SpikeArtifactsTTL != 10*24*time.Hour {
		t.Errorf("SpikeArtifactsTTL = %v, want 10d", merged.SpikeArtifactsTTL)
	}
}
