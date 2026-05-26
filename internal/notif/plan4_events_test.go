package notif_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/notif"
)

func TestEventType_String(t *testing.T) {
	types := []notif.EventType{
		notif.EventSSHExecInteractiveBlocked,
		notif.EventAuditFamilyDisjointPoolEmpty,
		notif.EventDoctrineInvalidOnReload,
		notif.EventSubprocessCrashChain,
		notif.EventBudgetAnomalyZScore,
		notif.EventAuditEmitHTTPFailSustained,
		notif.EventResearchCitationHallucinated,
		notif.EventBudgetCapExhausted,
		notif.EventSSHExecAllowlistDenied,
		notif.EventSubprocessTTLEvicted,
		notif.EventWorkerCompleted,
		notif.EventWorkerFailed,
	}
	seen := make(map[string]int)
	for _, et := range types {
		s := et.String()
		if s == "" {
			t.Errorf("EventType %d has empty String()", int(et))
		}
		seen[s]++
	}
	for s, count := range seen {
		if count > 1 {
			t.Errorf("duplicate EventType string %q appears %d times", s, count)
		}
	}

	const unknown notif.EventType = 9999
	if got := unknown.String(); !strings.HasPrefix(got, "unknown_event_type_") {
		t.Errorf("unknown EventType: got %q, want prefix unknown_event_type_", got)
	}
}

func TestEventSeverity(t *testing.T) {
	tests := []struct {
		ev   notif.EventType
		want notif.Severity
	}{
		{notif.EventSSHExecInteractiveBlocked, notif.SeveritySecurityAlert},
		{notif.EventAuditFamilyDisjointPoolEmpty, notif.SeveritySecurityAlert},
		{notif.EventDoctrineInvalidOnReload, notif.SeverityCritical},
		{notif.EventSubprocessCrashChain, notif.SeverityWarn},
		{notif.EventBudgetAnomalyZScore, notif.SeverityCritical},
		{notif.EventAuditEmitHTTPFailSustained, notif.SeverityCritical},
		{notif.EventWorkerCompleted, notif.SeverityInfo},
		{notif.EventWorkerFailed, notif.SeverityWarn},
		{notif.EventSSHExecAllowlistDenied, notif.SeverityWarn},
		{notif.EventSubprocessTTLEvicted, notif.SeverityInfo},
		{notif.EventBudgetCapExhausted, notif.SeverityCritical},
		{notif.EventResearchCitationHallucinated, notif.SeverityWarn},
	}
	for _, tt := range tests {
		got := tt.ev.DefaultSeverity()
		if got != tt.want {
			t.Errorf("EventType %s: severity got %s, want %s",
				tt.ev, got, tt.want)
		}
	}

	const unknown notif.EventType = 9999
	if got := unknown.DefaultSeverity(); got != notif.SeverityInfo {
		t.Errorf("unknown EventType severity: got %s, want %s", got, notif.SeverityInfo)
	}
}

func TestNewSSHExecInteractiveBlockedEvent(t *testing.T) {
	ev := notif.NewSSHExecInteractiveBlockedEvent("vps", "sudo bash", "worktree-42", "task-007")
	if ev.Type != notif.EventSSHExecInteractiveBlocked {
		t.Errorf("type: got %v, want EventSSHExecInteractiveBlocked", ev.Type)
	}
	if ev.Severity != notif.SeveritySecurityAlert {
		t.Errorf("severity: got %v, want SeveritySecurityAlert", ev.Severity)
	}
	if ev.Payload == nil {
		t.Fatal("payload is nil")
	}
	p, ok := ev.Payload.(*notif.SSHExecInteractiveBlockedPayload)
	if !ok {
		t.Fatalf("payload type: got %T, want *SSHExecInteractiveBlockedPayload", ev.Payload)
	}
	if p.Host != "vps" {
		t.Errorf("host: got %q, want vps", p.Host)
	}
	if p.WorkerID != "worktree-42" {
		t.Errorf("worker_id: got %q, want worktree-42", p.WorkerID)
	}
	if p.TaskID != "task-007" {
		t.Errorf("task_id: got %q, want task-007", p.TaskID)
	}
	if p.CmdPrefix != "sudo bash" {
		t.Errorf("cmd_prefix: got %q, want %q", p.CmdPrefix, "sudo bash")
	}
	if p.DetectedAt == "" {
		t.Error("detected_at is empty; should be RFC3339")
	}
}

func TestNewSSHExecInteractiveBlockedEvent_TruncatesCmdPrefix(t *testing.T) {
	long := strings.Repeat("A", 200)
	ev := notif.NewSSHExecInteractiveBlockedEvent("vps", long, "w", "t")
	p := ev.Payload.(*notif.SSHExecInteractiveBlockedPayload)
	if len(p.CmdPrefix) != 128 {
		t.Errorf("cmd_prefix length: got %d, want 128", len(p.CmdPrefix))
	}
}

func TestNewSSHExecAllowlistDeniedEvent_TruncatesCmdPrefix(t *testing.T) {
	long := strings.Repeat("B", 200)
	ev := notif.NewSSHExecAllowlistDeniedEvent("vps", long, "no_prefix_match", "w-1")
	p := ev.Payload.(*notif.SSHExecAllowlistDeniedPayload)
	if len(p.CmdPrefix) != 128 {
		t.Errorf("cmd_prefix length: got %d, want 128", len(p.CmdPrefix))
	}
	if p.DenyReason != "no_prefix_match" {
		t.Errorf("deny_reason: got %q", p.DenyReason)
	}
}

func TestNewWorkerFailedEvent_TruncatesError(t *testing.T) {
	long := strings.Repeat("E", 500)
	ev := notif.NewWorkerFailedEvent("w-1", "t-1", long)
	p := ev.Payload.(*notif.WorkerFailedPayload)
	if len(p.ErrorSummary) != 256 {
		t.Errorf("error_summary length: got %d, want 256", len(p.ErrorSummary))
	}
}

func TestVerifyNoPII(t *testing.T) {
	events := []notif.Event{
		notif.NewSSHExecInteractiveBlockedEvent("vps", "alembic upgrade head", "w-01", "t-01"),
		notif.NewBudgetAnomalyEvent("project-x", "stage", "design", 6.5, 4.0),
		notif.NewSubprocessCrashChainEvent("spec-abc", 3, "signal: killed"),
		notif.NewDoctrineInvalidEvent("max-scope", "unknown field: research.forbidden"),
		notif.NewAuditEmitHTTPFailEvent("/v1/audit/emit", 502, 90*time.Second),
		notif.NewAuditFamilyDisjointPoolEmptyEvent("max-scope", "anthropic"),
		notif.NewSSHExecAllowlistDeniedEvent("vps", "rm -rf /", "forbidden_chars", "w-02"),
		notif.NewSubprocessTTLEvictedEvent("spec-xyz", "8h", "9h"),
		notif.NewWorkerCompletedEvent("w-1", "t-1", 12*time.Second),
		notif.NewWorkerFailedEvent("w-2", "t-2", "boom"),
		notif.NewBudgetCapExhaustedEvent("project", "internal-platform-x", 50.0, 50.0),
		notif.NewResearchCitationHallucinatedEvent("ab12", 10, 6),
	}

	forbidden := []string{"token", "key", "password", "secret", "credential", "auth"}

	for _, ev := range events {
		b, err := json.Marshal(ev.Payload)
		if err != nil {
			t.Errorf("marshal %s: %v", ev.Type, err)
			continue
		}
		lower := strings.ToLower(string(b))
		for _, f := range forbidden {
			if strings.Contains(lower, f) {
				t.Errorf("PII leak in %s payload: JSON contains %q:\n%s",
					ev.Type, f, string(b))
			}
		}
	}
}

func TestEvent_JSONRoundtrip(t *testing.T) {
	orig := notif.NewSSHExecInteractiveBlockedEvent("vps", "cmd", "w-01", "t-01")
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "ssh_exec.interactive_blocked") {
		t.Errorf("event_type missing in JSON: %s", string(b))
	}
	if !strings.Contains(string(b), "SECURITY_ALERT") {
		t.Errorf("severity missing in JSON: %s", string(b))
	}
}

// TestNotifyCondition_AlwaysNotify verifies the security-grade types
// bypass aggregation, while info/warn-tier types do not.
func TestNotifyCondition_AlwaysNotify(t *testing.T) {
	alwaysNotify := []notif.EventType{
		notif.EventSSHExecInteractiveBlocked,
		notif.EventAuditFamilyDisjointPoolEmpty,
		notif.EventDoctrineInvalidOnReload,
		notif.EventBudgetAnomalyZScore,
		notif.EventAuditEmitHTTPFailSustained,
	}
	for _, et := range alwaysNotify {
		if !et.IsAlwaysNotify() {
			t.Errorf("EventType %s should be always-notify", et)
		}
	}
	notAlways := []notif.EventType{
		notif.EventWorkerCompleted,
		notif.EventWorkerFailed,
		notif.EventSubprocessCrashChain,
		notif.EventBudgetCapExhausted,
		notif.EventSSHExecAllowlistDenied,
		notif.EventSubprocessTTLEvicted,
		notif.EventResearchCitationHallucinated,
	}
	for _, et := range notAlways {
		if et.IsAlwaysNotify() {
			t.Errorf("EventType %s should not be always-notify", et)
		}
	}
}

func TestEventConstructors_FillEmittedAt(t *testing.T) {
	events := []notif.Event{
		notif.NewSSHExecInteractiveBlockedEvent("h", "c", "w", "t"),
		notif.NewBudgetAnomalyEvent("v", "stage", "design", 5, 3),
		notif.NewSubprocessCrashChainEvent("s", 3, "exit"),
		notif.NewDoctrineInvalidEvent("d", "e"),
		notif.NewAuditEmitHTTPFailEvent("/p", 500, time.Minute),
		notif.NewAuditFamilyDisjointPoolEmptyEvent("d", "f"),
		notif.NewSSHExecAllowlistDeniedEvent("h", "c", "r", "w"),
		notif.NewSubprocessTTLEvictedEvent("s", "8h", "9h"),
		notif.NewWorkerCompletedEvent("w", "t", time.Second),
		notif.NewWorkerFailedEvent("w", "t", "err"),
		notif.NewBudgetCapExhaustedEvent("p", "v", 1, 1),
		notif.NewResearchCitationHallucinatedEvent("h", 10, 5),
	}
	for _, ev := range events {
		if ev.EmittedAt.IsZero() {
			t.Errorf("event %s: EmittedAt is zero", ev.Type)
		}
		if ev.EmittedAt.Location() != time.UTC {
			t.Errorf("event %s: EmittedAt timezone got %v, want UTC", ev.Type, ev.EmittedAt.Location())
		}
	}
}

func TestSeverityValues(t *testing.T) {
	if string(notif.SeverityInfo) != "INFO" {
		t.Errorf("SeverityInfo: got %q, want INFO", string(notif.SeverityInfo))
	}
	if string(notif.SeverityWarn) != "WARN" {
		t.Errorf("SeverityWarn: got %q, want WARN", string(notif.SeverityWarn))
	}
	if string(notif.SeverityCritical) != "CRITICAL" {
		t.Errorf("SeverityCritical: got %q, want CRITICAL", string(notif.SeverityCritical))
	}
	if string(notif.SeveritySecurityAlert) != "SECURITY_ALERT" {
		t.Errorf("SeveritySecurityAlert: got %q, want SECURITY_ALERT", string(notif.SeveritySecurityAlert))
	}
}
