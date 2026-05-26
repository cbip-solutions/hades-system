package zenday_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeInboxNotifier struct {
	notified []zenday.InboxEvent
	failWith error
}

func (f *fakeInboxNotifier) Notify(_ context.Context, ev zenday.InboxEvent) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.notified = append(f.notified, ev)
	return nil
}

func TestDispatchInboxSeverityUrgent_TamperDetected(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{"project_id":"zen-swarm","record_id":847239}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityUrgent {
		t.Errorf("severity = %q, want SeverityUrgent", sev)
	}
	if len(n.notified) != 1 {
		t.Errorf("notifier called %d times, want 1", len(n.notified))
	}
	if n.notified[0].Severity != inbox.SeverityUrgent {
		t.Errorf("notified.Severity = %q, want SeverityUrgent", n.notified[0].Severity)
	}
}

func TestDispatchInboxSeverityUrgent_WitnessKeyCompromised(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtDaemonWitnessKeyCompromised,
		[]byte(`{"old_pubkey":"abc","reason":"audit"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityUrgent {
		t.Errorf("severity = %q, want SeverityUrgent", sev)
	}
}

func TestDispatchInboxSeverityUrgent_PartitionSealFailed(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditPartitionSealFailed,
		[]byte(`{"project_id":"zen-swarm","partition_id":"2026_05"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityUrgent {
		t.Errorf("severity = %q, want SeverityUrgent", sev)
	}
}

func TestDispatchInboxSeverityHigh_LitestreamLag(t *testing.T) {

	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditLitestreamLag,
		[]byte(`{"project_id":"zen-swarm","lag_seconds":3700}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityActionNeeded {
		t.Errorf("lag>1h severity = %q, want SeverityActionNeeded", sev)
	}
}

func TestDispatchInboxSeverityHigh_ColdArchiveFailed(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditColdArchiveFailed,
		[]byte(`{"project_id":"zen-swarm","consecutive_failures":3}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityActionNeeded {
		t.Errorf("severity = %q, want SeverityActionNeeded", sev)
	}
}

func TestDispatchInboxSeverityMedium_CacheRevalidationStuck(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		zenday.EvtResearchCacheRevalidationStuck,
		[]byte(`{"project_id":"zen-swarm","stuck_duration_s":300}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoImmediate {
		t.Errorf("severity = %q, want SeverityInfoImmediate", sev)
	}
}

func TestDispatchInboxSeverityMedium_EmbedWorkerDegraded(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		zenday.EvtKnowledgeEmbedWorkerDegraded,
		[]byte(`{"project_id":"zen-swarm","error":"OOM"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoImmediate {
		t.Errorf("severity = %q, want SeverityInfoImmediate", sev)
	}
}

func TestDispatchInboxSeverityLow_RecoveryCompleted(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditRecoveryCompleted,
		[]byte(`{"project_id":"zen-swarm"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoDigest {
		t.Errorf("severity = %q, want SeverityInfoDigest", sev)
	}
}

func TestDispatchInboxSeverityLow_AdrTransitioned(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtADRTransitionAccepted,
		[]byte(`{"adr_id":"ADR-0070","from":"proposed","to":"accepted"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoDigest {
		t.Errorf("severity = %q, want SeverityInfoDigest", sev)
	}
}

func TestDispatchInboxSeverityLow_StateRegenerated(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtStateRegenerated,
		[]byte(`{"project_id":"zen-swarm"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoDigest {
		t.Errorf("severity = %q, want SeverityInfoDigest", sev)
	}
}

func TestDispatchInboxSeverityLow_VaultNotePromoted(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtVaultNotePromotedToGlobal,
		[]byte(`{"project_id":"zen-swarm","note_id":"note-42"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoDigest {
		t.Errorf("severity = %q, want SeverityInfoDigest", sev)
	}
}

func TestDispatchInboxSeverityLow_LitestreamLagUnderHour(t *testing.T) {

	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditLitestreamLag,
		[]byte(`{"project_id":"zen-swarm","lag_seconds":300}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}

	if sev != inbox.SeverityInfoImmediate {
		t.Errorf("lag<1h severity = %q, want SeverityInfoImmediate", sev)
	}
}

func TestDispatchInboxSeverityDefault_UnmappedEvent_Info(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		"unknown.event_type_future_plan",
		[]byte(`{}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("DispatchInboxSeverity error = %v", err)
	}
	if sev != inbox.SeverityInfoDigest {
		t.Errorf("unmapped event severity = %q, want SeverityInfoDigest", sev)
	}
}

func TestDispatchInboxSeverityNotifyError_Propagated(t *testing.T) {
	wantErr := errors.New("notifier: connection refused")
	n := &fakeInboxNotifier{failWith: wantErr}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	_, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{"project_id":"zen-swarm"}`),
		time.Now(),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want to wrap %v", err, wantErr)
	}
}

func TestDispatchInboxSeverityNilNotifier_Error(t *testing.T) {
	deps := zenday.InboxSeverityDispatchDeps{Notifier: nil}

	_, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{}`),
		time.Now(),
	)
	if err == nil {
		t.Fatal("expected error for nil Notifier, got nil")
	}
}

func TestDispatchInboxSeverityContextCancelled_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	_, err := zenday.DispatchInboxSeverity(ctx, deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{}`),
		time.Now(),
	)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestDispatchInboxSeverity_AllADRTransitions_InfoDigest(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
	}{
		{"proposed", eventlog.EvtADRTransitionProposed},
		{"accepted", eventlog.EvtADRTransitionAccepted},
		{"rejected", eventlog.EvtADRTransitionRejected},
		{"superseded", eventlog.EvtADRTransitionSuperseded},
		{"deprecated", eventlog.EvtADRTransitionDeprecated},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			n := &fakeInboxNotifier{}
			deps := zenday.InboxSeverityDispatchDeps{Notifier: n}
			sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
				tc.eventType,
				[]byte(`{"adr_id":"ADR-0070"}`),
				time.Now(),
			)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if sev != inbox.SeverityInfoDigest {
				t.Errorf("%s: severity = %q, want SeverityInfoDigest", tc.name, sev)
			}
		})
	}
}

func TestDispatchInboxSeverity_WitnessEvents_MixedSeverity(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		want      inbox.Severity
	}{
		{"witness_co_signed", eventlog.EvtDaemonWitnessCoSigned, inbox.SeverityInfoDigest},
		{"witness_rotated", eventlog.EvtDaemonWitnessRotated, inbox.SeverityInfoDigest},
		{"witness_key_compromised", eventlog.EvtDaemonWitnessKeyCompromised, inbox.SeverityUrgent},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			n := &fakeInboxNotifier{}
			deps := zenday.InboxSeverityDispatchDeps{Notifier: n}
			sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
				tc.eventType,
				[]byte(`{}`),
				time.Now(),
			)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if sev != tc.want {
				t.Errorf("%s: severity = %q, want %q", tc.name, sev, tc.want)
			}
		})
	}
}

func TestDispatchInboxSeverity_ResearchCacheEvents_InfoDigest(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
	}{
		{"cache_hit_exact", eventlog.EvtResearchCacheHitExact},
		{"cache_hit_semantic", eventlog.EvtResearchCacheHitSemantic},
		{"cache_revalidated_fresh", eventlog.EvtResearchCacheRevalidatedFresh},
		{"cache_revalidated_stale", eventlog.EvtResearchCacheRevalidatedStaleRefetched},
		{"findings_returned", eventlog.EvtResearchFindingsReturned},
		{"dispatch_initiated", eventlog.EvtResearchDispatchInitiated},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			n := &fakeInboxNotifier{}
			deps := zenday.InboxSeverityDispatchDeps{Notifier: n}
			sev, err := zenday.DispatchInboxSeverity(context.Background(), deps,
				tc.eventType,
				[]byte(`{}`),
				time.Now(),
			)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if sev != inbox.SeverityInfoDigest {
				t.Errorf("%s: severity = %q, want SeverityInfoDigest", tc.name, sev)
			}
		})
	}
}

func TestDispatchInboxSeverity_NotificationTitle_ContainsEventType(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}

	_, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{"project_id":"zen-swarm"}`),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(n.notified) != 1 {
		t.Fatalf("notified count = %d, want 1", len(n.notified))
	}
	if n.notified[0].Title == "" {
		t.Error("notified.Title is empty, want non-empty")
	}
}

func TestDispatchInboxSeverity_NotificationAt_PreservesTimestamp(t *testing.T) {
	n := &fakeInboxNotifier{}
	deps := zenday.InboxSeverityDispatchDeps{Notifier: n}
	at := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)

	_, err := zenday.DispatchInboxSeverity(context.Background(), deps,
		eventlog.EvtAuditTamperDetected,
		[]byte(`{}`),
		at,
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(n.notified) != 1 {
		t.Fatalf("notified count = %d, want 1", len(n.notified))
	}
	if !n.notified[0].At.Equal(at) {
		t.Errorf("notified.At = %v, want %v", n.notified[0].At, at)
	}
}
