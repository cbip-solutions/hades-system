package cli

import (
	"context"
	"testing"
)

func TestRunAuditTesseraProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		TesseraProber: &fakeTesseraProberP9{
			results: []ProbeResult{
				{Name: "audit.tessera.witness_key_health", Status: ProbeOK, Message: "fresh (age 10d; rotation cadence 90d)"},
				{Name: "audit.tessera.last_sth_age_per_project", Status: ProbeOK, Message: "all 1 projects fresh"},
				{Name: "audit.tessera.daemon_global_checkpoint_freshness", Status: ProbeOK, Message: "fresh 30s ago"},
			},
		},
	}
	probes, err := RunAuditTesseraProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditTesseraProbe error = %v", err)
	}
	if len(probes) != 3 {
		t.Errorf("len(probes) = %d, want 3", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", ExitCode(probes, false))
	}
}

func TestRunAuditTesseraProbeWarn(t *testing.T) {
	deps := DoctorDeps{
		TesseraProber: &fakeTesseraProberP9{
			results: []ProbeResult{
				{Name: "audit.tessera.witness_key_health", Status: ProbeWarn, Message: "witness key age 80d nearing rotation cadence 90d (>=80%)"},
				{Name: "audit.tessera.last_sth_age_per_project", Status: ProbeOK, Message: "fresh"},
				{Name: "audit.tessera.daemon_global_checkpoint_freshness", Status: ProbeOK, Message: "fresh"},
			},
		},
	}
	probes, err := RunAuditTesseraProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditTesseraProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunAuditTesseraProbeFail(t *testing.T) {
	deps := DoctorDeps{
		TesseraProber: &fakeTesseraProberP9{
			results: []ProbeResult{
				{Name: "audit.tessera.witness_key_health", Status: ProbeFail, Message: "witness key missing", Hint: "zen audit witness rotate"},
				{Name: "audit.tessera.last_sth_age_per_project", Status: ProbeOK, Message: "fresh"},
				{Name: "audit.tessera.daemon_global_checkpoint_freshness", Status: ProbeOK, Message: "fresh"},
			},
		},
	}
	probes, _ := RunAuditTesseraProbe(context.Background(), deps)
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunAuditTesseraProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunAuditTesseraProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil TesseraProber, got nil")
	}
}

func TestRunAuditBackupProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		LitestreamProber: &fakeLitestreamProberP9{
			results: []ProbeResult{
				{Name: "audit.backup.litestream_lag_per_project", Status: ProbeOK, Message: "lag 2s"},
				{Name: "audit.backup.cold_archive_last_rsync", Status: ProbeOK, Message: "1d ago"},
				{Name: "audit.backup.s3_reachability", Status: ProbeOK, Message: "200"},
			},
		},
	}
	probes, err := RunAuditBackupProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditBackupProbe error = %v", err)
	}
	if len(probes) != 3 {
		t.Errorf("len(probes) = %d, want 3", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0", ExitCode(probes, false))
	}
}

func TestRunAuditBackupProbeWarnColdArchive(t *testing.T) {
	deps := DoctorDeps{
		LitestreamProber: &fakeLitestreamProberP9{
			results: []ProbeResult{
				{Name: "audit.backup.litestream_lag_per_project", Status: ProbeOK, Message: "lag 2s"},
				{Name: "audit.backup.cold_archive_last_rsync", Status: ProbeWarn, Message: "12d ago — nearing 2× cadence"},
				{Name: "audit.backup.s3_reachability", Status: ProbeOK, Message: "200"},
			},
		},
	}
	probes, err := RunAuditBackupProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditBackupProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn-only (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn-only (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunAuditBackupProbeFailS3Unreachable(t *testing.T) {
	deps := DoctorDeps{
		LitestreamProber: &fakeLitestreamProberP9{
			results: []ProbeResult{
				{Name: "audit.backup.litestream_lag_per_project", Status: ProbeOK, Message: "lag 1s"},
				{Name: "audit.backup.cold_archive_last_rsync", Status: ProbeOK, Message: "1d ago"},
				{Name: "audit.backup.s3_reachability", Status: ProbeFail, Message: "connection refused", Hint: "check S3 credentials and bucket policy"},
			},
		},
	}
	probes, err := RunAuditBackupProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditBackupProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode S3-fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunAuditBackupProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunAuditBackupProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil LitestreamProber, got nil")
	}
}

func TestRunAuditChainIntegrityProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		ChainProber: &fakeChainProberP9{
			results: []ProbeResult{
				{Name: "audit.chain.last_verify_age", Status: ProbeOK, Message: "4h ago"},
				{Name: "audit.chain.tamper_events_count", Status: ProbeOK, Message: "0 in last 7d"},
			},
		},
		RecoveryProber: &fakeRecoveryProberP9{
			results: []ProbeResult{
				{Name: "audit.recovery.last_dispatch_status", Status: ProbeOK, Message: "no recovery in 30d"},
			},
		},
	}
	probes, err := RunAuditChainIntegrityProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditChainIntegrityProbe error = %v", err)
	}
	if len(probes) != 3 {
		t.Errorf("len(probes) = %d, want 3", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0", ExitCode(probes, false))
	}
}

func TestRunAuditChainIntegrityProbeWarnStaleVerify(t *testing.T) {
	deps := DoctorDeps{
		ChainProber: &fakeChainProberP9{
			results: []ProbeResult{
				{Name: "audit.chain.last_verify_age", Status: ProbeWarn, Message: "30h ago — within 2× cadence"},
				{Name: "audit.chain.tamper_events_count", Status: ProbeOK, Message: "0 in last 7d"},
			},
		},
		RecoveryProber: &fakeRecoveryProberP9{
			results: []ProbeResult{
				{Name: "audit.recovery.last_dispatch_status", Status: ProbeOK, Message: "no recovery in 30d"},
			},
		},
	}
	probes, err := RunAuditChainIntegrityProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditChainIntegrityProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
}

func TestRunAuditChainIntegrityProbeFailTamper(t *testing.T) {
	deps := DoctorDeps{
		ChainProber: &fakeChainProberP9{
			results: []ProbeResult{
				{Name: "audit.chain.last_verify_age", Status: ProbeOK, Message: "2h ago"},
				{Name: "audit.chain.tamper_events_count", Status: ProbeFail, Message: "5 tamper events in last 7d", Hint: "zen audit verify-chain --project foo"},
			},
		},
		RecoveryProber: &fakeRecoveryProberP9{
			results: []ProbeResult{
				{Name: "audit.recovery.last_dispatch_status", Status: ProbeOK, Message: "no recovery in 30d"},
			},
		},
	}
	probes, err := RunAuditChainIntegrityProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAuditChainIntegrityProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode tamper-fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunAuditChainIntegrityProbeNilChainError(t *testing.T) {
	deps := DoctorDeps{
		RecoveryProber: &fakeRecoveryProberP9{},
	}
	_, err := RunAuditChainIntegrityProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil ChainProber, got nil")
	}
}

func TestRunAuditChainIntegrityProbeNilRecoveryError(t *testing.T) {
	deps := DoctorDeps{
		ChainProber: &fakeChainProberP9{},
	}
	_, err := RunAuditChainIntegrityProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil RecoveryProber, got nil")
	}
}

func TestNewDoctorAuditCmdHelp(t *testing.T) {
	cmd := NewDoctorAuditCmd()
	if cmd.Use != "audit" {
		t.Errorf("Use = %q, want \"audit\"", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short empty")
	}
	subs := cmd.Commands()
	if len(subs) != 3 {
		t.Errorf("subcommands = %d, want 3 (tessera, backup, chain-integrity)", len(subs))
	}
	expectedNames := map[string]bool{"tessera": true, "backup": true, "chain-integrity": true}
	for _, s := range subs {
		if !expectedNames[s.Use] {
			t.Errorf("unexpected subcommand %q", s.Use)
		}
	}
}

type fakeTesseraProberP9 struct{ results []ProbeResult }

func (f *fakeTesseraProberP9) Probe(_ context.Context) []ProbeResult { return f.results }

type fakeLitestreamProberP9 struct{ results []ProbeResult }

func (f *fakeLitestreamProberP9) Probe(_ context.Context) []ProbeResult { return f.results }

type fakeChainProberP9 struct{ results []ProbeResult }

func (f *fakeChainProberP9) Probe(_ context.Context) []ProbeResult { return f.results }

type fakeRecoveryProberP9 struct{ results []ProbeResult }

func (f *fakeRecoveryProberP9) Probe(_ context.Context) []ProbeResult { return f.results }

var (
	_ TesseraProber    = (*fakeTesseraProberP9)(nil)
	_ LitestreamProber = (*fakeLitestreamProberP9)(nil)
	_ ChainProber      = (*fakeChainProberP9)(nil)
	_ RecoveryProber   = (*fakeRecoveryProberP9)(nil)
)
