package eventlog_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestEventTypeStringer(t *testing.T) {
	cases := []struct {
		et   eventlog.EventType
		want string
	}{
		{eventlog.EvtOrchestratorStarted, "OrchestratorStarted"},
		{eventlog.EvtWorkerDispatched, "WorkerDispatched"},
		{eventlog.EvtVotingDecisionMade, "VotingDecisionMade"},
		{eventlog.EvtReplayCorruptionDetected, "ReplayCorruptionDetected"},
		{eventlog.EvtFMVDegradedToPlurality, "FMVDegradedToPlurality"},
		{eventlog.EvtApplyFixStarted, "ApplyFixStarted"},
		{eventlog.EvtApplyFixSucceeded, "ApplyFixSucceeded"},
		{eventlog.EvtApplyFixReverted, "ApplyFixReverted"},
		{eventlog.EvtADRRangeExhausted, "ADRRangeExhausted"},
		{eventlog.EvtHandoffPosted, "HandoffPosted"},
		{eventlog.EvtMorningBriefReady, "MorningBriefReady"},
		{eventlog.EvtEODDigestReady, "EODDigestReady"},
	}
	for _, c := range cases {
		if got := c.et.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q want %q", int(c.et), got, c.want)
		}
	}
}

func TestEventTypeAllCovered(t *testing.T) {
	all := eventlog.AllEventTypes()

	if len(all) != 78 {
		t.Fatalf("AllEventTypes() len = %d, want 78 (39 Phase A + 2 Plan 5 G-2 cost-gating failure events + 1 Plan 5 G-4 atomicity timeout + 2 Plan 5 G-6 recovery-walk events + 1 Plan 5 H-8 phase-boundary trigger + 1 Plan 5 I-5 FMV degradation event + 3 Plan 5 J-2 apply-engine canonical wire codes + 1 Plan 5 K-7 ADR-range-exhausted + 1 Plan 7 F-1 HandoffPosted + 2 Plan 7 F-2 zen day brief/digest + 17 Plan 8 Phase H Task H-1 doctrine domain events + 8 Plan 14 Phase A-1 ecosystem RAG events)", len(all))
	}
	seen := make(map[eventlog.EventType]bool)
	for _, et := range all {
		if seen[et] {
			t.Errorf("duplicate EventType in AllEventTypes: %v", et)
		}
		seen[et] = true
		if et.String() == "" || et.String() == "Unknown" {
			t.Errorf("EventType %d has empty/Unknown string", int(et))
		}
	}
}

func TestWorkerDispatchedRoundTrip(t *testing.T) {
	in := eventlog.WorkerDispatched{
		WorkerID: "w-42",
		TaskID:   "task-7",
		Tier:     "t1_bypass",
	}
	raw, err := in.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	var out eventlog.WorkerDispatched
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestDecodeEventDispatch(t *testing.T) {
	in := eventlog.WorkerDispatched{WorkerID: "w-1", TaskID: "t-1", Tier: "t1_bypass"}
	raw, _ := in.Payload()
	got, err := eventlog.Decode(eventlog.EvtWorkerDispatched, raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	wd, ok := got.(eventlog.WorkerDispatched)
	if !ok {
		t.Fatalf("Decode returned %T, want WorkerDispatched", got)
	}
	if wd != in {
		t.Errorf("decoded mismatch: %+v vs %+v", wd, in)
	}
}

func TestDecodeUnknownTypeIsError(t *testing.T) {
	_, err := eventlog.Decode(eventlog.EventType(9999), []byte("{}"))
	if err == nil {
		t.Fatalf("Decode unknown type returned nil error")
	}
}

func TestDecodeMalformedPayloadIsError(t *testing.T) {
	_, err := eventlog.Decode(eventlog.EvtWorkerDispatched, []byte("not-json"))
	if err == nil {
		t.Fatalf("Decode malformed payload returned nil error")
	}
}

func TestEventTypeStringerUnknown(t *testing.T) {
	if got := eventlog.EventType(9999).String(); got != "Unknown" {
		t.Errorf("EventType(9999).String() = %q, want \"Unknown\"", got)
	}

	if got := eventlog.EvtUnknown.String(); got != "Unknown" {
		t.Errorf("EvtUnknown.String() = %q, want \"Unknown\"", got)
	}
}

var exhaustiveCases = []struct {
	name string
	evt  eventlog.PayloadEncoder
	want eventlog.EventType
}{
	{"OrchestratorStarted", eventlog.OrchestratorStarted{SessionID: "s1", ProjectID: "p1", AutonomyMode: "auto"}, eventlog.EvtOrchestratorStarted},
	{"OrchestratorStateTransition", eventlog.OrchestratorStateTransition{From: "IDLE", To: "RUNNING", Reason: "boot"}, eventlog.EvtOrchestratorStateTransition},
	{"OrchestratorRestarting", eventlog.OrchestratorRestarting{LastSeenState: "RUNNING"}, eventlog.EvtOrchestratorRestarting},
	{"OrchestratorRestoreFromReplay_Legacy", eventlog.OrchestratorRestoreFromReplay{SessionID: "s1", RecoveryMs: 42, EventsReplayed: 100, EventsCorrupted: 0}, eventlog.EvtOrchestratorRestoreFromReplay},
	{"OrchestratorRestoreFromReplay_Structured", eventlog.OrchestratorRestoreFromReplay{SessionID: "s1", EventsReplayed: 100, EventsCorrupted: 0, RecoveredTaskCount: 3, HardPause: false, Reason: "recovered 3 in-flight tasks"}, eventlog.EvtOrchestratorRestoreFromReplay},
	{"OrchestratorStopped", eventlog.OrchestratorStopped{Outcome: "success"}, eventlog.EvtOrchestratorStopped},
	{"WorkerDispatched", eventlog.WorkerDispatched{WorkerID: "w1", TaskID: "t1", Tier: "t1_bypass"}, eventlog.EvtWorkerDispatched},
	{"WorkerCheckpoint_Legacy", eventlog.WorkerCheckpoint{WorkerID: "w1", CheckpointSHA: "abc123", Summary: "step done"}, eventlog.EvtWorkerCheckpoint},
	{"WorkerCheckpoint_Structured", eventlog.WorkerCheckpoint{WorkerID: "w1", TaskID: "t-42", CheckpointSHA: "abc123", Summary: "step done"}, eventlog.EvtWorkerCheckpoint},
	{"WorkerDeath_Legacy", eventlog.WorkerDeath{WorkerID: "w1", Cause: "panic", RetryCount: 1}, eventlog.EvtWorkerDeath},
	{"WorkerDeath_Structured", eventlog.WorkerDeath{WorkerID: "w1", TaskID: "t-42", Class: "TRANSIENT_INFRA", Reason: "heartbeat_timeout", RetryCount: 0}, eventlog.EvtWorkerDeath},
	{"WorkerRedispatched", eventlog.WorkerRedispatched{TaskID: "t1", WorkerID: "w2", Class: "TRANSIENT_LLM", Action: "redispatch_same_tier", NewTierIndex: 0, RetryCount: 1, Reason: "within_budget"}, eventlog.EvtWorkerRedispatched},
	{"ReviewerWaveStarted", eventlog.ReviewerWaveStarted{Layer: "T", Reviewers: []string{"r1", "r2"}}, eventlog.EvtReviewerWaveStarted},
	{"ReviewerWaveComplete", eventlog.ReviewerWaveComplete{Layer: "T", Verdict: "pass"}, eventlog.EvtReviewerWaveComplete},
	{"TacticalAggregation", eventlog.TacticalAggregation{WaveID: "wv1", Verdict: "pass"}, eventlog.EvtTacticalAggregation},
	{"StrategicAggregation", eventlog.StrategicAggregation{WaveID: "wv1", Verdict: "pass"}, eventlog.EvtStrategicAggregation},
	{"ArchitecturalReview", eventlog.ArchitecturalReview{PhaseID: "ph1", Verdict: "approve"}, eventlog.EvtArchitecturalReview},
	{"EscalationDecision", eventlog.EscalationDecision{FromLayer: "T", ToLayer: "S", Reason: "split"}, eventlog.EvtEscalationDecision},
	{"VotingDecisionMade", eventlog.VotingDecisionMade{Mechanism: "plurality", Winner: "candidate-A"}, eventlog.EvtVotingDecisionMade},
	{"FMVAllFailed", eventlog.FMVAllFailed{CandidateCount: 5, TestFailures: 5}, eventlog.EvtFMVAllFailed},
	{"EMSConvergenceFailed", eventlog.EMSConvergenceFailed{SampledCount: 7}, eventlog.EvtEMSConvergenceFailed},
	{"CostThresholdCrossed", eventlog.CostThresholdCrossed{ThresholdPct: 80, ObservedPct: 82}, eventlog.EvtCostThresholdCrossed},
	{"BudgetDegradationApplied_Legacy", eventlog.BudgetDegradationApplied{Threshold: 90, Action: "downshift"}, eventlog.EvtBudgetDegradationApplied},
	{"BudgetDegradationApplied_Structured", eventlog.BudgetDegradationApplied{ThresholdPct: 80, Action: "tier_degrade_l2", PriorAction: "drop_l3_strategic", Doctrine: "max-scope", ProjectID: "internal-platform-x", CumulativeUSD: 85.0, DailyCapUSD: 100.0, ProjectedEODUSD: 110.0, PAYGActive: false}, eventlog.EvtBudgetDegradationApplied},
	{"BudgetRecovered_Legacy", eventlog.BudgetRecovered{RestoredToPct: 70}, eventlog.EvtBudgetRecovered},
	{"BudgetRecovered_Structured", eventlog.BudgetRecovered{UndoneAction: "tier_degrade_l2", NextAction: "drop_l3_strategic", NextPct: 60, Doctrine: "max-scope", ProjectID: "internal-platform-x"}, eventlog.EvtBudgetRecovered},
	{"BudgetFullyRecovered", eventlog.BudgetFullyRecovered{Doctrine: "max-scope", ProjectID: "internal-platform-x"}, eventlog.EvtBudgetFullyRecovered},
	{"BudgetRecoveryHeld", eventlog.BudgetRecoveryHeld{HeldAction: "waiting_for_confirmation", Reason: "capa-firewall: operator confirmation required to release", Doctrine: "capa-firewall", ProjectID: "research-ai"}, eventlog.EvtBudgetRecoveryHeld},
	{"EmergencyTierActivated", eventlog.EmergencyTierActivated{Reason: "tier1-down"}, eventlog.EvtEmergencyTierActivated},
	{"ConfirmationRequested_Legacy", eventlog.ConfirmationRequested{EventID: "e1", DecisionClass: "doctrine-amend"}, eventlog.EvtConfirmationRequested},
	{"ConfirmationRequested_Structured", eventlog.ConfirmationRequested{EventID: "req-7", DecisionClass: "invariant_violation", RequestSeq: 7, Summary: "inv-zen-091 violated", Alternatives: []string{"abort", "continue"}}, eventlog.EvtConfirmationRequested},
	{"OperatorConfirmation_Legacy", eventlog.OperatorConfirmation{EventID: "e1", Decision: "ack", Rationale: "looks good"}, eventlog.EvtOperatorConfirmation},
	{"OperatorConfirmation_Structured", eventlog.OperatorConfirmation{EventID: "req-1", Decision: "ack", Rationale: "approved", RequestSeq: 1, OperatorUID: 501}, eventlog.EvtOperatorConfirmation},
	{"OperatorAmendmentDeny", eventlog.OperatorAmendmentDeny{ADRID: "ADR-0042", Rationale: "nope"}, eventlog.EvtOperatorAmendmentDeny},
	{"DoctrineAmendmentProposed", eventlog.DoctrineAmendmentProposed{ADRID: "ADR-0042", DiffSummary: "+1 -1"}, eventlog.EvtDoctrineAmendmentProposed},
	{"DoctrineAmendmentApplied", eventlog.DoctrineAmendmentApplied{ADRID: "ADR-0042", CommitSHA: "deadbeef"}, eventlog.EvtDoctrineAmendmentApplied},
	{"DoctrineAmendmentReverted", eventlog.DoctrineAmendmentReverted{ADRID: "ADR-0042"}, eventlog.EvtDoctrineAmendmentReverted},
	{"DoctrineAmendmentSuppressed", eventlog.DoctrineAmendmentSuppressed{ADRID: "ADR-0042", CooldownRemaining: "24h"}, eventlog.EvtDoctrineAmendmentSuppressed},
	{"SubstrateDriftDetected", eventlog.SubstrateDriftDetected{Severity: "hard", Findings: "go.mod drift"}, eventlog.EvtSubstrateDriftDetected},
	{"ConfigDivergenceDetected", eventlog.ConfigDivergenceDetected{DiffSummary: "+key x"}, eventlog.EvtConfigDivergenceDetected},
	{"RegressionBySelfAlarm", eventlog.RegressionBySelfAlarm{ObservedPassRate: 0.7, BaselinePassRate: 0.95}, eventlog.EvtRegressionBySelfAlarm},
	{"SafetynetPrevMissing", eventlog.SafetynetPrevMissing{Reason: "first-event"}, eventlog.EvtSafetynetPrevMissing},
	{"WorktreePoolDegraded", eventlog.WorktreePoolDegraded{Floor: 4, Used: 7, Target: 8}, eventlog.EvtWorktreePoolDegraded},
	{"WorktreePoolExhausted", eventlog.WorktreePoolExhausted{Requested: 9, Available: 0}, eventlog.EvtWorktreePoolExhausted},
	{"OperatorOverrideApplied_Legacy", eventlog.OperatorOverrideApplied{OverrideClass: "budget-bypass", Rationale: "p0 incident"}, eventlog.EvtOperatorOverrideApplied},
	{"OperatorOverrideApplied_Structured", eventlog.OperatorOverrideApplied{OperatorUID: 501, OperatorReason: "approved", OverrideKind: "confirmation_ack"}, eventlog.EvtOperatorOverrideApplied},
	{"ResearchCompleted", eventlog.ResearchCompleted{FindingsSummary: "options A/B/C", CostUSD: 0.42}, eventlog.EvtResearchCompleted},
	{"DepthWidthDecided", eventlog.DepthWidthDecided{Depth: 3, Width: 5, Rationale: "spec §22.7"}, eventlog.EvtDepthWidthDecided},
	{"ReplayCorruptionDetected", eventlog.ReplayCorruptionDetected{EventOffset: 17, Reason: "json"}, eventlog.EvtReplayCorruptionDetected},
	{"BudgetSnapshotError", eventlog.BudgetSnapshotError{Error: "transient: read budget"}, eventlog.EvtBudgetSnapshotError},
	{"BudgetDegradationFailed", eventlog.BudgetDegradationFailed{Action: "drop_l3_strategic", Error: "actuator: SetTier failed"}, eventlog.EvtBudgetDegradationFailed},
	{"CostGatingAtomicityTimeout", eventlog.CostGatingAtomicityTimeout{TimeoutSec: 30.0, Action: ""}, eventlog.EvtCostGatingAtomicityTimeout},
	{"PhaseBoundaryRecorded", eventlog.PhaseBoundaryRecorded{PhaseID: "phase-D", Trigger: "phase_boundary"}, eventlog.EvtPhaseBoundaryRecorded},
	{"FMVDegradedToPlurality", eventlog.FMVDegradedToPlurality{Reason: "pool_exhausted", CandidateCount: 3, CompletedCount: 1, WinnerID: "Z", Tie: false}, eventlog.EvtFMVDegradedToPlurality},
	{"ApplyFixStarted", eventlog.ApplyFixStarted{Branch: "worker/W1", FixID: "fix-1"}, eventlog.EvtApplyFixStarted},
	{"ApplyFixSucceeded", eventlog.ApplyFixSucceeded{Branch: "worker/W1", FixID: "fix-1", CommitSHA: "deadbeef", Files: []string{"hello.txt"}}, eventlog.EvtApplyFixSucceeded},
	{"ApplyFixReverted", eventlog.ApplyFixReverted{Branch: "worker/W1", FixID: "fix-1", CommitSHA: "cafebabe", Files: []string{"hello.txt"}, Stderr: "regression detected"}, eventlog.EvtApplyFixReverted},
	{"ADRRangeExhausted", eventlog.ADRRangeExhausted{Min: 20, Max: 29}, eventlog.EvtADRRangeExhausted},
	{"HandoffPosted", eventlog.HandoffPostedEvent{
		ProjectID:       "a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7",
		ProjectAlias:    "internal-platform-x",
		Timestamp:       time.Date(2026, 5, 1, 18, 30, 0, 0, time.UTC),
		Summary:         "Stage 4 Build phase 12 complete (47 commits, 0 critical)",
		RecentCommits:   []string{"abc123 feat: add HRA L4 detector", "def456 test: cover edge cases"},
		AutonomousState: "paused",
		Blockers:        []string{"HRA L4 alert raised"},
		NextSession:     "review L4 finding + resume autonomous",
	}, eventlog.EvtHandoffPosted},
	{"MorningBriefReady", eventlog.MorningBriefReadyEvent{
		Date:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ItemCount:    5,
		ProjectCount: 3,
		FilePath:     "/path/to/home/.config/zen-swarm/zen-day-2026-05-01.md",
	}, eventlog.EvtMorningBriefReady},
	{"EODDigestReady", eventlog.EODDigestReadyEvent{
		Date:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ProjectCount: 3,
		TotalCostUSD: 0.84,
		FilePath:     "/path/to/home/.config/zen-swarm/zen-day-2026-05-01-eod.md",
	}, eventlog.EvtEODDigestReady},

	{"DoctrineLoaded", eventlog.DoctrineLoaded{
		Path: "/etc/zen-swarm/doctrines/max-scope.toml", SchemaVersion: "1.0",
		DoctrineVersion: "1.0.0", Source: "embed", ProjectID: "",
	}, eventlog.EvtDoctrineLoaded},
	{"DoctrineReloaded", eventlog.DoctrineReloaded{
		Path: "/etc/zen-swarm/doctrines/max-scope.toml", ProjectID: "internal-platform-x",
		DoctrineName: "max-scope", FromDoctrineVersion: "1.0.0", ToDoctrineVersion: "1.0.1",
		Source: "operator-edit", At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineReloaded},
	{"DoctrineReloadFailed", eventlog.DoctrineReloadFailed{
		Path: "/etc/zen-swarm/doctrines/max-scope.toml", ProjectID: "internal-platform-x",
		Phase: "validate", Errors: []string{"workforce.max_depth tighten=decrease violated"},
		Reason: "validation-failed", Detail: "field workforce.max_depth", KeptVer: "1.0.0",
		At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineReloadFailed},
	{"DoctrineSchemaDeprecated", eventlog.DoctrineSchemaDeprecated{
		Path: "/etc/zen-swarm/doctrines/max-scope.toml", ProjectID: "", DoctrineName: "max-scope",
		OnDiskVersion: "0.9", CurrentVersion: "1.0", Action: "warn",
		At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineSchemaDeprecated},
	{"DoctrineSchemaMigrated", eventlog.DoctrineSchemaMigrated{
		Path:        "/etc/zen-swarm/doctrines/max-scope.toml",
		FromVersion: "0.9", ToVersion: "1.0",
		BackupPath: "/etc/zen-swarm/doctrines/max-scope.toml.v0_9.bak",
	}, eventlog.EvtDoctrineSchemaMigrated},
	{"DoctrineLoadFailed", eventlog.DoctrineLoadFailed{
		Path: "/etc/zen-swarm/doctrines/custom.toml", Source: "user-default",
		Reason: "parse-failed: line 7: unclosed string literal",
	}, eventlog.EvtDoctrineLoadFailed},
	{"DoctrineOverrideLoadFailed", eventlog.DoctrineOverrideLoadFailed{
		Path:      "/path/to/projects/internal-platform-x/.zen/doctrine-override.toml",
		ProjectID: "internal-platform-x",
		Reason:    "tighten-violation: field hra.cadence_strategic_min loosened",
	}, eventlog.EvtDoctrineOverrideLoadFailed},
	{"DoctrineLoadRefused", eventlog.DoctrineLoadRefused{
		Path: "/etc/zen-swarm/doctrines/legacy.toml", Reason: "schema-version-too-old",
		SchemaVersion: "0.5", MinSupported: "0.9",
	}, eventlog.EvtDoctrineLoadRefused},
	{"DoctrineSchemaMigrationFailed", eventlog.DoctrineSchemaMigrationFailed{
		Path:        "/etc/zen-swarm/doctrines/max-scope.toml",
		FromVersion: "0.9", ToVersion: "1.0",
		Reason: "converter-bug: field rename autonomy.mode → autonomy.kind missing",
	}, eventlog.EvtDoctrineSchemaMigrationFailed},
	{"DoctrineAutonomousReverted", eventlog.DoctrineAutonomousReverted{
		ADRID: "ADR-0024", RulePath: "autonomy.cost_degradation.soft_check_usd",
		TelemetryCategory: "cost", ThresholdBreached: 0.62, WindowSessions: 20,
		Reason: "BypassedSoftCheckEvent rate exceeded revert_threshold_pct=0.7 in 20-session window",
	}, eventlog.EvtDoctrineAutonomousReverted},
	{"DoctrineRevertSuppressedCooldown", eventlog.DoctrineRevertSuppressedCooldown{
		Path:  "/etc/zen-swarm/doctrines/max-scope.toml",
		ADRID: "ADR-0024", RulePath: "autonomy.cost_degradation.soft_check_usd",
		TelemetryCategory: "cost", FailureCount: 0, WindowSec: 0,
		AttemptedAtUnix: 1746230400, LastRevertedAtUnix: 1746229200,
		CooldownUntil:          time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
		CooldownRemainingHours: 23,
		At:                     time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineRevertSuppressedCooldown},
	{"DoctrineTightenViolationRejected", eventlog.DoctrineTightenViolationRejected{
		Path:      "/etc/zen-swarm/doctrines/max-scope.toml",
		ProjectID: "", DoctrineName: "max-scope",
		Source: "amendment-apply", ADRID: "ADR-0025", RulePath: "workforce.max_depth",
		AttemptedValue: "6", BaselineValue: "3", Direction: "decrease",
		At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineTightenViolationRejected},
	{"DoctrineAmendmentApplyFailed", eventlog.DoctrineAmendmentApplyFailed{
		ADRID: "ADR-0025", Path: "/etc/zen-swarm/doctrines/max-scope.toml",
		Stage: "atomic-rename", Reason: "filesystem ENOSPC during rename",
	}, eventlog.EvtDoctrineAmendmentApplyFailed},
	{"DoctrineWatcherStalled", eventlog.DoctrineWatcherStalled{
		Path:            "/etc/zen-swarm/doctrines",
		LastEventAt:     time.Date(2026, 5, 3, 11, 50, 0, 0, time.UTC),
		StallTimeoutSec: 300, StaleSec: 600,
		At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineWatcherStalled},
	{"DoctrineWatcherRestarted", eventlog.DoctrineWatcherRestarted{
		Path: "/etc/zen-swarm/doctrines", Reason: "stalled-detected",
		At: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineWatcherRestarted},
	{"DoctrineWatcherOverflow", eventlog.DoctrineWatcherOverflow{
		Path: "/etc/zen-swarm/doctrines", ReReadAllPaths: 3,
		QueueDepth: 1024, Action: "force-reload-all",
		AffectedFiles: []string{"max-scope.toml", "default.toml", "capa-firewall.toml"},
		At:            time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	}, eventlog.EvtDoctrineWatcherOverflow},
	{"DoctrineAccessorAuditPassed", eventlog.DoctrineAccessorAuditPassed{
		AuditedAtUnix: 1746229200, PackagesScanned: 32, Violations: 0,
	}, eventlog.EvtDoctrineAccessorAuditPassed},
}

func TestExhaustiveTypeAndPayload(t *testing.T) {
	if len(exhaustiveCases) != 78 {
		t.Fatalf("exhaustive table size = %d, want 78 (53 unique pre-Phase-H types + 8 dual-form rows for E-5/E-6/F-2/F-3/F-5/G-5/G-6 schema extensions + 17 Plan 8 Phase H Task H-1 doctrine domain events)", len(exhaustiveCases))
	}

	for _, c := range exhaustiveCases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.evt.Type(); got != c.want {
				t.Errorf("Type() = %v, want %v", got, c.want)
			}
			raw, err := c.evt.Payload()
			if err != nil {
				t.Fatalf("Payload(): %v", err)
			}
			decoded, err := eventlog.Decode(c.want, raw)
			if err != nil {
				t.Fatalf("Decode(%v): %v", c.want, err)
			}
			if decoded.Type() != c.want {
				t.Errorf("decoded.Type() = %v, want %v", decoded.Type(), c.want)
			}

			reraw, err := decoded.Payload()
			if err != nil {
				t.Fatalf("decoded.Payload(): %v", err)
			}
			if string(raw) != string(reraw) {
				t.Errorf("round-trip payload drift:\n  in:  %s\n  out: %s", string(raw), string(reraw))
			}

			got := c.want.String()
			switch c.name {
			case "WorkerDeath_Legacy", "WorkerDeath_Structured":
				if got != "WorkerDeath" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "WorkerDeath")
				}
			case "WorkerCheckpoint_Legacy", "WorkerCheckpoint_Structured":
				if got != "WorkerCheckpoint" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "WorkerCheckpoint")
				}
			case "OrchestratorRestoreFromReplay_Legacy", "OrchestratorRestoreFromReplay_Structured":
				if got != "OrchestratorRestoreFromReplay" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "OrchestratorRestoreFromReplay")
				}
			case "ConfirmationRequested_Legacy", "ConfirmationRequested_Structured":
				if got != "ConfirmationRequested" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "ConfirmationRequested")
				}
			case "OperatorConfirmation_Legacy", "OperatorConfirmation_Structured":
				if got != "OperatorConfirmation" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "OperatorConfirmation")
				}
			case "OperatorOverrideApplied_Legacy", "OperatorOverrideApplied_Structured":
				if got != "OperatorOverrideApplied" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "OperatorOverrideApplied")
				}
			case "BudgetDegradationApplied_Legacy", "BudgetDegradationApplied_Structured":
				if got != "BudgetDegradationApplied" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "BudgetDegradationApplied")
				}
			case "BudgetRecovered_Legacy", "BudgetRecovered_Structured":
				if got != "BudgetRecovered" {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, "BudgetRecovered")
				}
			default:
				if got != c.name {
					t.Errorf("EventType(%d).String() = %q, want %q", int(c.want), got, c.name)
				}
			}
		})
	}
}

func TestExhaustiveTypeAndPayloadMatchesAllEventTypes(t *testing.T) {

	plan14RAGTypes := map[eventlog.EventType]bool{
		eventlog.EvtRAGQuery: true, eventlog.EvtRAGRetrieval: true,
		eventlog.EvtRAGCitation: true, eventlog.EvtRAGVerify: true,
		eventlog.EvtRAGAbstain: true, eventlog.EvtRAGAnswer: true,
		eventlog.EvtRAGIngestPackage: true, eventlog.EvtRAGIngestJoinKey: true,
	}

	covered := make(map[eventlog.EventType]int, len(exhaustiveCases))
	for _, c := range exhaustiveCases {
		covered[c.want]++
	}
	for _, et := range eventlog.AllEventTypes() {
		if plan14RAGTypes[et] {
			continue
		}
		if count := covered[et]; count == 0 {
			t.Errorf("AllEventTypes() includes %v but exhaustiveCases has no row for it", et)
		}
	}

	wantCovered := len(eventlog.AllEventTypes()) - len(plan14RAGTypes)
	if len(covered) != wantCovered {
		t.Errorf("unique EventTypes in table %d != AllEventTypes() size %d minus %d deferred RAG types = %d (event added to one but not the other)",
			len(covered), len(eventlog.AllEventTypes()), len(plan14RAGTypes), wantCovered)
	}
}

func TestEventTypeIsValidForAll(t *testing.T) {
	for _, et := range eventlog.AllEventTypes() {
		if !et.IsValid() {
			t.Errorf("AllEventTypes() includes %v but IsValid() == false", et)
		}
	}
}

func TestEventTypeIsValidForUnknown(t *testing.T) {
	if eventlog.EvtUnknown.IsValid() {
		t.Errorf("EvtUnknown.IsValid() = true; expected false (zero-value guard)")
	}
}

func TestEventTypeIsValidForOutOfRange(t *testing.T) {
	if eventlog.EventType(9999).IsValid() {
		t.Errorf("EventType(9999).IsValid() = true; expected false")
	}
}

func TestDecodeMalformedAllArms(t *testing.T) {
	bad := []byte("not-json")
	for _, et := range eventlog.AllEventTypes() {
		_, err := eventlog.Decode(et, bad)
		if err == nil {
			t.Errorf("Decode(%v, %q) returned nil error; expected json error", et, bad)
		}
	}
}

func TestHandoffPostedEventTypeAndPayload(t *testing.T) {
	ts := time.Date(2026, 5, 1, 18, 30, 0, 0, time.UTC)
	evt := eventlog.HandoffPostedEvent{
		ProjectID:       "a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7a3f5b2c8d4e1f9b7",
		ProjectAlias:    "internal-platform-x",
		Timestamp:       ts,
		Summary:         "Stage 4 Build phase 12 complete (47 commits, 0 critical)",
		RecentCommits:   []string{"abc123 feat: add HRA L4 detector", "def456 test: cover edge cases"},
		AutonomousState: "paused",
		Blockers:        []string{"HRA L4 alert raised"},
		NextSession:     "review L4 finding + resume autonomous",
	}
	if got := evt.Type(); got != eventlog.EvtHandoffPosted {
		t.Errorf("Type() = %v, want EvtHandoffPosted", got)
	}
	body, err := evt.Payload()
	if err != nil {
		t.Fatalf("Payload() error = %v", err)
	}
	// Round-trip MUST preserve every field byte-identically.
	var back eventlog.HandoffPostedEvent
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if back.ProjectID != evt.ProjectID {
		t.Errorf("ProjectID = %q, want %q", back.ProjectID, evt.ProjectID)
	}
	if back.ProjectAlias != evt.ProjectAlias {
		t.Errorf("ProjectAlias = %q, want %q", back.ProjectAlias, evt.ProjectAlias)
	}
	if !back.Timestamp.Equal(evt.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", back.Timestamp, evt.Timestamp)
	}
	if back.Summary != evt.Summary {
		t.Errorf("Summary = %q, want %q", back.Summary, evt.Summary)
	}
	if !slicesEqualHandoff(back.RecentCommits, evt.RecentCommits) {
		t.Errorf("RecentCommits = %v, want %v", back.RecentCommits, evt.RecentCommits)
	}
	if back.AutonomousState != evt.AutonomousState {
		t.Errorf("AutonomousState = %q, want %q", back.AutonomousState, evt.AutonomousState)
	}
	if !slicesEqualHandoff(back.Blockers, evt.Blockers) {
		t.Errorf("Blockers = %v, want %v", back.Blockers, evt.Blockers)
	}
	if back.NextSession != evt.NextSession {
		t.Errorf("NextSession = %q, want %q", back.NextSession, evt.NextSession)
	}
}

func TestEventTypeStringHandoffPosted(t *testing.T) {
	if got := eventlog.EvtHandoffPosted.String(); got != "HandoffPosted" {
		t.Errorf("EvtHandoffPosted.String() = %q, want %q", got, "HandoffPosted")
	}
}

func TestDecodeHandoffPosted(t *testing.T) {
	body := []byte(`{"project_id":"abc","project_alias":"internal-platform-x","timestamp":"2026-05-01T18:30:00Z","summary":"done","recent_commits":["aaa","bbb"],"autonomous_state":"paused","blockers":["b1"],"next_session_action":"resume"}`)
	got, err := eventlog.Decode(eventlog.EvtHandoffPosted, body)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	hp, ok := got.(eventlog.HandoffPostedEvent)
	if !ok {
		t.Fatalf("Decode returned %T, want HandoffPostedEvent", got)
	}
	if hp.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want internal-platform-x", hp.ProjectAlias)
	}
	if hp.AutonomousState != "paused" {
		t.Errorf("AutonomousState = %q, want paused", hp.AutonomousState)
	}
	if len(hp.RecentCommits) != 2 {
		t.Errorf("RecentCommits len = %d, want 2", len(hp.RecentCommits))
	}
}

func TestAllEventTypesUniqueIncludesHandoffPosted(t *testing.T) {
	all := eventlog.AllEventTypes()
	found := false
	for _, et := range all {
		if et == eventlog.EvtHandoffPosted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("EvtHandoffPosted absent from AllEventTypes()")
	}
}

func slicesEqualHandoff(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMorningBriefReadyEventTypeAndPayload(t *testing.T) {
	date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	evt := eventlog.MorningBriefReadyEvent{
		Date:         date,
		ItemCount:    5,
		ProjectCount: 3,
		FilePath:     "/path/to/home/.config/zen-swarm/zen-day-2026-05-01.md",
	}
	if got := evt.Type(); got != eventlog.EvtMorningBriefReady {
		t.Errorf("Type() = %v, want EvtMorningBriefReady", got)
	}
	body, err := evt.Payload()
	if err != nil {
		t.Fatalf("Payload() error = %v", err)
	}
	var back eventlog.MorningBriefReadyEvent
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !back.Date.Equal(evt.Date) {
		t.Errorf("Date = %v, want %v", back.Date, evt.Date)
	}
	if back.ItemCount != 5 {
		t.Errorf("ItemCount = %d, want 5", back.ItemCount)
	}
	if back.ProjectCount != 3 {
		t.Errorf("ProjectCount = %d, want 3", back.ProjectCount)
	}
	if back.FilePath != evt.FilePath {
		t.Errorf("FilePath = %q, want %q", back.FilePath, evt.FilePath)
	}
}

func TestEODDigestReadyEventTypeAndPayload(t *testing.T) {
	date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	evt := eventlog.EODDigestReadyEvent{
		Date:         date,
		ProjectCount: 3,
		TotalCostUSD: 0.84,
		FilePath:     "/path/to/home/.config/zen-swarm/zen-day-2026-05-01-eod.md",
	}
	if got := evt.Type(); got != eventlog.EvtEODDigestReady {
		t.Errorf("Type() = %v, want EvtEODDigestReady", got)
	}
	body, err := evt.Payload()
	if err != nil {
		t.Fatalf("Payload() error = %v", err)
	}
	var back eventlog.EODDigestReadyEvent
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !back.Date.Equal(evt.Date) {
		t.Errorf("Date = %v, want %v", back.Date, evt.Date)
	}
	if back.ProjectCount != 3 {
		t.Errorf("ProjectCount = %d, want 3", back.ProjectCount)
	}
	if back.TotalCostUSD != 0.84 {
		t.Errorf("TotalCostUSD = %f, want 0.84", back.TotalCostUSD)
	}
	if back.FilePath != evt.FilePath {
		t.Errorf("FilePath = %q, want %q", back.FilePath, evt.FilePath)
	}
}

func TestEventTypeStringMorningAndEOD(t *testing.T) {
	if got := eventlog.EvtMorningBriefReady.String(); got != "MorningBriefReady" {
		t.Errorf("EvtMorningBriefReady.String() = %q, want MorningBriefReady", got)
	}
	if got := eventlog.EvtEODDigestReady.String(); got != "EODDigestReady" {
		t.Errorf("EvtEODDigestReady.String() = %q, want EODDigestReady", got)
	}
}

func TestDecodeMorningBriefReady(t *testing.T) {
	body := []byte(`{"date":"2026-05-01T00:00:00Z","item_count":5,"project_count":3,"file_path":"/tmp/zen-day.md"}`)
	got, err := eventlog.Decode(eventlog.EvtMorningBriefReady, body)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	mb, ok := got.(eventlog.MorningBriefReadyEvent)
	if !ok {
		t.Fatalf("Decode returned %T, want MorningBriefReadyEvent", got)
	}
	if mb.ItemCount != 5 {
		t.Errorf("ItemCount = %d, want 5", mb.ItemCount)
	}
	if mb.ProjectCount != 3 {
		t.Errorf("ProjectCount = %d, want 3", mb.ProjectCount)
	}
	if mb.FilePath != "/tmp/zen-day.md" {
		t.Errorf("FilePath = %q, want /tmp/zen-day.md", mb.FilePath)
	}
}

func TestDecodeEODDigestReady(t *testing.T) {
	body := []byte(`{"date":"2026-05-01T00:00:00Z","project_count":3,"total_cost_usd":0.84,"file_path":"/tmp/zen-day-eod.md"}`)
	got, err := eventlog.Decode(eventlog.EvtEODDigestReady, body)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	eod, ok := got.(eventlog.EODDigestReadyEvent)
	if !ok {
		t.Fatalf("Decode returned %T, want EODDigestReadyEvent", got)
	}
	if eod.TotalCostUSD != 0.84 {
		t.Errorf("TotalCostUSD = %f, want 0.84", eod.TotalCostUSD)
	}
	if eod.ProjectCount != 3 {
		t.Errorf("ProjectCount = %d, want 3", eod.ProjectCount)
	}
	if eod.FilePath != "/tmp/zen-day-eod.md" {
		t.Errorf("FilePath = %q, want /tmp/zen-day-eod.md", eod.FilePath)
	}
}

func TestPlan7EventTypesAllPresentInAllEventTypes(t *testing.T) {
	want := []eventlog.EventType{
		eventlog.EvtHandoffPosted,
		eventlog.EvtMorningBriefReady,
		eventlog.EvtEODDigestReady,
	}
	got := eventlog.AllEventTypes()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EventType %v missing from AllEventTypes()", w)
		}
	}
}

func TestPhase8DoctrineEventTypeStringerCovers17NewCategories(t *testing.T) {
	want := []eventlog.EventType{
		eventlog.EvtDoctrineLoaded,
		eventlog.EvtDoctrineReloaded,
		eventlog.EvtDoctrineReloadFailed,
		eventlog.EvtDoctrineSchemaDeprecated,
		eventlog.EvtDoctrineSchemaMigrated,
		eventlog.EvtDoctrineLoadFailed,
		eventlog.EvtDoctrineOverrideLoadFailed,
		eventlog.EvtDoctrineLoadRefused,
		eventlog.EvtDoctrineSchemaMigrationFailed,
		eventlog.EvtDoctrineAutonomousReverted,
		eventlog.EvtDoctrineRevertSuppressedCooldown,
		eventlog.EvtDoctrineTightenViolationRejected,
		eventlog.EvtDoctrineAmendmentApplyFailed,
		eventlog.EvtDoctrineWatcherStalled,
		eventlog.EvtDoctrineWatcherRestarted,
		eventlog.EvtDoctrineWatcherOverflow,
		eventlog.EvtDoctrineAccessorAuditPassed,
	}
	if len(want) != 17 {
		t.Fatalf("Phase H must add exactly 17 event types; the test list has %d", len(want))
	}
	for _, et := range want {
		s := et.String()
		if s == "" || s == "Unknown" {
			t.Errorf("EventType(%d).String() = %q; must be human-readable", int(et), s)
		}
	}
}

func TestPhase8DoctrineEventTypesIncludedInAllEventTypes(t *testing.T) {
	all := eventlog.AllEventTypes()
	seen := make(map[eventlog.EventType]bool, len(all))
	for _, et := range all {
		seen[et] = true
	}
	want := []eventlog.EventType{
		eventlog.EvtDoctrineLoaded, eventlog.EvtDoctrineReloaded,
		eventlog.EvtDoctrineReloadFailed, eventlog.EvtDoctrineSchemaDeprecated,
		eventlog.EvtDoctrineSchemaMigrated, eventlog.EvtDoctrineLoadFailed,
		eventlog.EvtDoctrineOverrideLoadFailed, eventlog.EvtDoctrineLoadRefused,
		eventlog.EvtDoctrineSchemaMigrationFailed, eventlog.EvtDoctrineAutonomousReverted,
		eventlog.EvtDoctrineRevertSuppressedCooldown, eventlog.EvtDoctrineTightenViolationRejected,
		eventlog.EvtDoctrineAmendmentApplyFailed, eventlog.EvtDoctrineWatcherStalled,
		eventlog.EvtDoctrineWatcherRestarted, eventlog.EvtDoctrineWatcherOverflow,
		eventlog.EvtDoctrineAccessorAuditPassed,
	}
	for _, et := range want {
		if !seen[et] {
			t.Errorf("AllEventTypes() missing Phase H event type %v", et)
		}
	}
}

func TestPhase8DoctrineAutonomousRevertedRoundTrip(t *testing.T) {
	in := eventlog.DoctrineAutonomousReverted{
		ADRID:             "ADR-0024",
		RulePath:          "autonomy.cost_degradation.soft_check_usd",
		TelemetryCategory: "cost",
		ThresholdBreached: 0.62,
		WindowSessions:    20,
		Reason:            "BypassedSoftCheckEvent rate exceeded revert_threshold_pct=0.7",
	}
	raw, err := in.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	got, err := eventlog.Decode(eventlog.EvtDoctrineAutonomousReverted, raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	out, ok := got.(eventlog.DoctrineAutonomousReverted)
	if !ok {
		t.Fatalf("Decode returned %T, want DoctrineAutonomousReverted", got)
	}
	if out != in {
		t.Errorf("round-trip mismatch: in=%+v out=%+v", in, out)
	}
}

func TestEvtMorningBriefReadyAndEODDigestReadyAreValid(t *testing.T) {
	if !eventlog.EvtMorningBriefReady.IsValid() {
		t.Errorf("EvtMorningBriefReady.IsValid() = false; expected true")
	}
	if !eventlog.EvtEODDigestReady.IsValid() {
		t.Errorf("EvtEODDigestReady.IsValid() = false; expected true")
	}
}

func TestPlan14EventTypeSlotsExist(t *testing.T) {
	cases := []struct {
		got  eventlog.EventType
		want int
		name string
	}{
		{eventlog.EvtRAGQuery, 92, "EvtRAGQuery"},
		{eventlog.EvtRAGRetrieval, 93, "EvtRAGRetrieval"},
		{eventlog.EvtRAGCitation, 94, "EvtRAGCitation"},
		{eventlog.EvtRAGVerify, 95, "EvtRAGVerify"},
		{eventlog.EvtRAGAbstain, 96, "EvtRAGAbstain"},
		{eventlog.EvtRAGAnswer, 97, "EvtRAGAnswer"},
		{eventlog.EvtRAGIngestPackage, 98, "EvtRAGIngestPackage"},
		{eventlog.EvtRAGIngestJoinKey, 99, "EvtRAGIngestJoinKey"},
	}
	for _, c := range cases {
		if int(c.got) != c.want {
			t.Errorf("%s = %d; want %d (master §3.6 contract violation; APPEND-ONLY rule)",
				c.name, int(c.got), c.want)
		}
	}
}

func TestPlan14EventTypeAllEventTypesIncludesRAG(t *testing.T) {
	all := eventlog.AllEventTypes()

	wantSeq := []eventlog.EventType{
		eventlog.EvtRAGQuery, eventlog.EvtRAGRetrieval, eventlog.EvtRAGCitation, eventlog.EvtRAGVerify,
		eventlog.EvtRAGAbstain, eventlog.EvtRAGAnswer, eventlog.EvtRAGIngestPackage, eventlog.EvtRAGIngestJoinKey,
	}

	startIdx := -1
	for i, e := range all {
		if e == eventlog.EvtRAGQuery {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		t.Fatalf("EvtRAGQuery missing from AllEventTypes(); 8 Plan 14 slots must all appear")
	}

	if startIdx+len(wantSeq) > len(all) {
		t.Fatalf("AllEventTypes() truncated at idx=%d; expected 8 contiguous RAG entries starting from EvtRAGQuery",
			startIdx)
	}
	for off, want := range wantSeq {
		if all[startIdx+off] != want {
			t.Errorf("AllEventTypes()[%d] = %d (%s); want %d (%s) — canonical order violated (inv-zen-197)",
				startIdx+off, all[startIdx+off], all[startIdx+off].String(),
				want, want.String())
		}
	}
}

func TestPlan14EventTypeStringMapping(t *testing.T) {
	cases := []struct {
		evt  eventlog.EventType
		want string
	}{
		{eventlog.EvtRAGQuery, "EvtRAGQuery"},
		{eventlog.EvtRAGRetrieval, "EvtRAGRetrieval"},
		{eventlog.EvtRAGCitation, "EvtRAGCitation"},
		{eventlog.EvtRAGVerify, "EvtRAGVerify"},
		{eventlog.EvtRAGAbstain, "EvtRAGAbstain"},
		{eventlog.EvtRAGAnswer, "EvtRAGAnswer"},
		{eventlog.EvtRAGIngestPackage, "EvtRAGIngestPackage"},
		{eventlog.EvtRAGIngestJoinKey, "EvtRAGIngestJoinKey"},
	}
	for _, c := range cases {
		if got := c.evt.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q; want %q",
				int(c.evt), got, c.want)
		}
	}
}

func TestPlan14EventTypeNoCollision(t *testing.T) {
	plan14Slots := map[eventlog.EventType]string{
		eventlog.EvtRAGQuery:         "EvtRAGQuery",
		eventlog.EvtRAGRetrieval:     "EvtRAGRetrieval",
		eventlog.EvtRAGCitation:      "EvtRAGCitation",
		eventlog.EvtRAGVerify:        "EvtRAGVerify",
		eventlog.EvtRAGAbstain:       "EvtRAGAbstain",
		eventlog.EvtRAGAnswer:        "EvtRAGAnswer",
		eventlog.EvtRAGIngestPackage: "EvtRAGIngestPackage",
		eventlog.EvtRAGIngestJoinKey: "EvtRAGIngestJoinKey",
	}

	seen := make(map[eventlog.EventType][]string)
	for _, e := range eventlog.AllEventTypes() {
		seen[e] = append(seen[e], e.String())
	}

	for slot, name := range plan14Slots {
		owners := seen[slot]
		if len(owners) == 0 {
			t.Errorf("slot %d (%s) missing from AllEventTypes()", int(slot), name)
			continue
		}
		if len(owners) > 1 {
			t.Errorf("slot %d collision: %d EventType constants claim it: %v",
				int(slot), len(owners), owners)
		}
		if owners[0] != name {
			t.Errorf("slot %d AllEventTypes() reports name %q; want %q",
				int(slot), owners[0], name)
		}
	}
}

func TestPlan14EventTypeAppendOnly(t *testing.T) {
	rag := map[eventlog.EventType]bool{
		eventlog.EvtRAGQuery: true, eventlog.EvtRAGRetrieval: true, eventlog.EvtRAGCitation: true,
		eventlog.EvtRAGVerify: true, eventlog.EvtRAGAbstain: true, eventlog.EvtRAGAnswer: true,
		eventlog.EvtRAGIngestPackage: true, eventlog.EvtRAGIngestJoinKey: true,
	}

	maxNonRAG := eventlog.EventType(0)
	for _, e := range eventlog.AllEventTypes() {
		if !rag[e] && e > maxNonRAG {
			maxNonRAG = e
		}
	}

	minRAG := eventlog.EvtRAGQuery
	for e := range rag {
		if e < minRAG {
			minRAG = e
		}
	}

	if int(minRAG) <= int(maxNonRAG) {
		t.Errorf("APPEND-ONLY violated: min Plan 14 slot %d <= max non-RAG slot %d "+
			"(inv-zen-197; doc.go lines 31-46)",
			int(minRAG), int(maxNonRAG))
	}
}
