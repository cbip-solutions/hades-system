package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestClassify_Table(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want FailureClass
	}{

		{"http_500_server_error", &HTTPStatusError{Code: 500, Endpoint: "anthropic"}, FailureTransientLLM},
		{"http_502_bad_gateway", &HTTPStatusError{Code: 502, Endpoint: "anthropic"}, FailureTransientLLM},
		{"http_503_service_unavailable", &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, FailureTransientLLM},
		{"http_429_rate_limit", &HTTPStatusError{Code: 429, Endpoint: "anthropic"}, FailureTransientLLM},

		{"network_timeout_to_llm", &net.OpError{Op: "dial", Err: errTimeout}, FailureTransientLLM},
		{"context_deadline_llm_call", &LLMCallError{Inner: context.DeadlineExceeded}, FailureTransientLLM},

		{"worker_subprocess_panic", &WorkerSubprocessError{Reason: "panic"}, FailureTransientInfra},
		{"queue_write_fail", &QueueWriteError{Inner: errors.New("disk i/o transient")}, FailureTransientInfra},
		{"oom_kill", &WorkerSubprocessError{Reason: "oom_kill", ExitCode: 137}, FailureTransientInfra},

		{"heartbeat_timeout", ErrHeartbeatTimeout, FailureTransientInfra},

		{"http_400_client_error", &HTTPStatusError{Code: 400, Endpoint: "anthropic"}, FailurePermanentTask},

		{"http_401_unauth", &HTTPStatusError{Code: 401, Endpoint: "anthropic"}, FailurePermanentInfra},
		{"http_403_forbidden", &HTTPStatusError{Code: 403, Endpoint: "anthropic"}, FailurePermanentInfra},

		{"disk_full", &DiskFullError{Path: "/var/lib/zen/worktrees"}, FailurePermanentInfra},
		{"repo_corrupt", &RepoCorruptError{Repo: "/home/u/proj"}, FailurePermanentInfra},
		{"audit_write_fail", &AuditWriteError{Inner: errors.New("sqlite_corrupt")}, FailurePermanentInfra},

		{"all_tiers_down", ErrAllTiersDown, FailurePermanentInfra},
		{"unknown_classify_default_permanent_infra", errors.New("totally unknown"), FailurePermanentInfra},

		{"nil_err_default_permanent_infra", nil, FailurePermanentInfra},

		{"wrapped_heartbeat_timeout", fmt.Errorf("worker X: %w", ErrHeartbeatTimeout), FailureTransientInfra},

		{"wrapped_http_5xx", fmt.Errorf("dial: %w", &HTTPStatusError{Code: 503}), FailureTransientLLM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.err)
			if got != tc.want {
				t.Fatalf("Classify(%T)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

var errTimeout = &timeoutError{}

type timeoutError struct{}

func (*timeoutError) Error() string   { return "i/o timeout" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }

func TestFailureClass_String(t *testing.T) {
	cases := []struct {
		fc   FailureClass
		want string
	}{
		{FailureTransientLLM, "TRANSIENT_LLM"},
		{FailureTransientInfra, "TRANSIENT_INFRA"},
		{FailurePermanentTask, "PERMANENT_TASK"},
		{FailurePermanentInfra, "PERMANENT_INFRA"},
	}
	for _, tc := range cases {
		if got := tc.fc.String(); got != tc.want {
			t.Fatalf("%d.String()=%q want %q", tc.fc, got, tc.want)
		}
	}
}

func TestFailureClass_String_Unknown(t *testing.T) {
	if got := FailureClass(99).String(); got != "UNKNOWN" {
		t.Fatalf("FailureClass(99).String()=%q want %q", got, "UNKNOWN")
	}
}

func TestErrorTypes_StringRoundTrip(t *testing.T) {
	t.Run("HTTPStatusError", func(t *testing.T) {
		e := &HTTPStatusError{Code: 503, Endpoint: "anthropic"}
		s := e.Error()
		if !contains(s, "503") {
			t.Fatalf("HTTPStatusError.Error()=%q missing code 503", s)
		}
		if !contains(s, "anthropic") {
			t.Fatalf("HTTPStatusError.Error()=%q missing endpoint", s)
		}
	})
	t.Run("LLMCallError", func(t *testing.T) {
		inner := errors.New("deadline exceeded")
		e := &LLMCallError{Inner: inner}
		s := e.Error()
		if !contains(s, "deadline exceeded") {
			t.Fatalf("LLMCallError.Error()=%q missing inner", s)
		}

		if !errors.Is(e, inner) {
			t.Fatal("LLMCallError.Unwrap() not surfacing inner")
		}
	})
	t.Run("WorkerSubprocessError", func(t *testing.T) {
		e := &WorkerSubprocessError{Reason: "panic", ExitCode: 1}
		s := e.Error()
		if !contains(s, "panic") {
			t.Fatalf("WorkerSubprocessError.Error()=%q missing reason", s)
		}
	})
	t.Run("QueueWriteError", func(t *testing.T) {
		inner := errors.New("io error")
		e := &QueueWriteError{Inner: inner}
		s := e.Error()
		if !contains(s, "io error") {
			t.Fatalf("QueueWriteError.Error()=%q missing inner", s)
		}
		if !errors.Is(e, inner) {
			t.Fatal("QueueWriteError.Unwrap() not surfacing inner")
		}
	})
	t.Run("DiskFullError", func(t *testing.T) {
		e := &DiskFullError{Path: "/var/lib/zen"}
		s := e.Error()
		if !contains(s, "/var/lib/zen") {
			t.Fatalf("DiskFullError.Error()=%q missing path", s)
		}
	})
	t.Run("RepoCorruptError", func(t *testing.T) {
		e := &RepoCorruptError{Repo: "/home/user/proj"}
		s := e.Error()
		if !contains(s, "/home/user/proj") {
			t.Fatalf("RepoCorruptError.Error()=%q missing repo", s)
		}
	})
	t.Run("AuditWriteError", func(t *testing.T) {
		inner := errors.New("sqlite corrupt")
		e := &AuditWriteError{Inner: inner}
		s := e.Error()
		if !contains(s, "sqlite corrupt") {
			t.Fatalf("AuditWriteError.Error()=%q missing inner", s)
		}
		if !errors.Is(e, inner) {
			t.Fatal("AuditWriteError.Unwrap() not surfacing inner")
		}
	})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type fakeDoctrine struct {
	name           string
	llmRetries     int
	infraRetries   int
	permanentAfter int
	onExhaust      map[FailureClass]RecoveryAction
	tierPolicy     TierFallbackPolicy
}

func (d *fakeDoctrine) Name() string                { return d.name }
func (d *fakeDoctrine) TransientLLMRetries() int    { return d.llmRetries }
func (d *fakeDoctrine) TransientInfraRetries() int  { return d.infraRetries }
func (d *fakeDoctrine) PermanentAfterNRetries() int { return d.permanentAfter }
func (d *fakeDoctrine) OnExhaustAction(c FailureClass) RecoveryAction {
	return d.onExhaust[c]
}
func (d *fakeDoctrine) TierFallbackPolicy() TierFallbackPolicy { return d.tierPolicy }

func loadFakeDoctrine(t *testing.T, name string) DoctrineView {
	t.Helper()
	switch name {
	case "max-scope":

		return &fakeDoctrine{
			name: "max-scope", llmRetries: 3, infraRetries: 2, permanentAfter: 3,
			onExhaust: map[FailureClass]RecoveryAction{
				FailurePermanentTask:  RecoveryActionEscalateL4,
				FailureTransientLLM:   RecoveryActionEscalateL4,
				FailureTransientInfra: RecoveryActionEscalateL4,
			},
			tierPolicy: TierFallbackFullChain,
		}
	case "default":
		return &fakeDoctrine{
			name: "default", llmRetries: 1, infraRetries: 1, permanentAfter: 3,
			onExhaust: map[FailureClass]RecoveryAction{
				FailurePermanentTask:  RecoveryActionSkipTask,
				FailureTransientLLM:   RecoveryActionSkipTask,
				FailureTransientInfra: RecoveryActionSkipTask,
			},
			tierPolicy: TierFallbackPartial,
		}
	case "capa-firewall":
		return &fakeDoctrine{
			name: "capa-firewall", llmRetries: 0, infraRetries: 0, permanentAfter: 1,
			onExhaust: map[FailureClass]RecoveryAction{
				FailurePermanentTask:  RecoveryActionWaitForConfirmation,
				FailureTransientLLM:   RecoveryActionWaitForConfirmation,
				FailureTransientInfra: RecoveryActionWaitForConfirmation,
			},
			tierPolicy: TierFallbackNone,
		}
	}
	t.Fatalf("unknown doctrine: %s", name)
	return nil
}

func canonicalTierSlice(doctrineName string) ([]string, int) {
	switch doctrineName {
	case "max-scope":

		return []string{"t1_bypass", "t2_anthropic_paygo", "t3_gemini", "t4_local"}, 0
	case "default":

		return []string{"t1_bypass", "t2_anthropic_paygo", "t3_gemini"}, 2
	case "capa-firewall":

		return []string{}, 0
	}
	return []string{"t0"}, 0
}

type recoveryFixture struct {
	t            *testing.T
	ctx          context.Context
	eng          *RecoveryEngine
	evlog        *eventlog.Log
	doctrine     string
	tierChainLen int
}

func newRecoveryFixture(t *testing.T, doctrineName string) *recoveryFixture {
	t.Helper()
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	doc := loadFakeDoctrine(t, doctrineName)
	evlog := eventlog.NewMemory(fc)
	tiers, stopBefore := canonicalTierSlice(doctrineName)
	tier := AdaptTierChain(tiers, stopBefore)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine:  doc,
		EventLog:  evlog,
		TierChain: tier,
		Clock:     fc,
		ProjectID: "p-test",
		SessionID: "s-test-" + doctrineName,
	})
	if err != nil {
		t.Fatalf("NewRecoveryEngine: %v", err)
	}
	return &recoveryFixture{
		t:            t,
		ctx:          context.Background(),
		eng:          eng,
		evlog:        evlog,
		doctrine:     doctrineName,
		tierChainLen: tier.Len(),
	}
}

func countByEventType(records []eventlog.Record, et eventlog.EventType) int {
	n := 0
	for _, r := range records {
		if r.EventType == et {
			n++
		}
	}
	return n
}

func (f *recoveryFixture) records(t *testing.T) []eventlog.Record {
	t.Helper()
	recs, err := f.evlog.Query(f.ctx, "s-test-"+f.doctrine, 0)
	if err != nil {
		t.Fatalf("eventlog.Query: %v", err)
	}
	return recs
}

func TestRecoveryEngine_HandleWorkerDeath_MaxScope_TransientLLM_Retries3(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	taskID := "task-001"

	for i := 1; i <= 3; i++ {
		decision, derr := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
			TaskID:    taskID,
			WorkerID:  "w1",
			Err:       &HTTPStatusError{Code: 503, Endpoint: "anthropic"},
			TierIndex: 0,
		})
		if derr != nil {
			t.Fatalf("retry %d: %v", i, derr)
		}
		if decision.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("retry %d action=%v want RedispatchSameTier", i, decision.Action)
		}
		if decision.NewTierIndex != 0 {
			t.Fatalf("retry %d tier=%d want 0", i, decision.NewTierIndex)
		}
		if decision.Class != FailureTransientLLM {
			t.Fatalf("retry %d class=%v want TRANSIENT_LLM", i, decision.Class)
		}
		if decision.RetryCount != i {
			t.Fatalf("retry %d retry_count=%d want %d", i, decision.RetryCount, i)
		}
	}

	dec, err := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID:    taskID,
		WorkerID:  "w1",
		Err:       &HTTPStatusError{Code: 503, Endpoint: "anthropic"},
		TierIndex: 0,
	})
	if err != nil {
		t.Fatalf("4th: %v", err)
	}
	if dec.Action != RecoveryActionRedispatchNextTier {
		t.Fatalf("4th action=%v want RedispatchNextTier", dec.Action)
	}
	if dec.NewTierIndex != 1 {
		t.Fatalf("4th tier=%d want 1", dec.NewTierIndex)
	}

	recs := fx.records(t)
	if got := countByEventType(recs, eventlog.EvtWorkerRedispatched); got != 4 {
		t.Fatalf("WorkerRedispatched count=%d want 4 (recs=%d)", got, len(recs))
	}
}

func TestRecoveryEngine_HandleWorkerDeath_Default_TransientLLM_Retries1_ThenSkip(t *testing.T) {
	fx := newRecoveryFixture(t, "default")
	taskID := "task-002"

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("retry 1 action=%v want RedispatchSameTier", dec.Action)
	}

	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if dec.Action != RecoveryActionRedispatchNextTier {
		t.Fatalf("retry 2 action=%v want RedispatchNextTier (default partial fallback)", dec.Action)
	}
	if dec.NewTierIndex != 1 {
		t.Fatalf("retry 2 tier=%d want 1", dec.NewTierIndex)
	}

	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 1,
	})
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("tier1 retry1 action=%v want RedispatchSameTier", dec.Action)
	}

	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 1,
	})
	if dec.Action != RecoveryActionSkipTask {
		t.Fatalf("exhaust action=%v want SkipTask (default On exhaust)", dec.Action)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_CapaFirewall_TransientLLM_Retries0_WaitConfirmation(t *testing.T) {
	fx := newRecoveryFixture(t, "capa-firewall")
	taskID := "task-003"

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if dec.Action != RecoveryActionWaitForConfirmation {
		t.Fatalf("capa-firewall action=%v want WaitForConfirmation (retries=0)", dec.Action)
	}
	if dec.NewTierIndex != 0 {
		t.Fatalf("capa-firewall must NOT advance tier (chain=none); got %d", dec.NewTierIndex)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_TransientInfra_RetriesPerDoctrine(t *testing.T) {
	cases := []struct {
		doctrine string
		budget   int
		exhaust  RecoveryAction
	}{
		{"max-scope", 2, RecoveryActionEscalateL4},
		{"default", 1, RecoveryActionSkipTask},
		{"capa-firewall", 0, RecoveryActionWaitForConfirmation},
	}
	for _, tc := range cases {
		t.Run(tc.doctrine, func(t *testing.T) {
			fx := newRecoveryFixture(t, tc.doctrine)
			taskID := "tinfra-" + tc.doctrine
			for i := 1; i <= tc.budget; i++ {
				dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
					TaskID: taskID, WorkerID: "w1",
					Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
				})
				if dec.Action != RecoveryActionRedispatchSameTier {
					t.Fatalf("retry %d/%d action=%v want RedispatchSameTier",
						i, tc.budget, dec.Action)
				}
			}

			dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
				TaskID: taskID, WorkerID: "w1",
				Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
			})
			if dec.Action != tc.exhaust {
				t.Fatalf("%s exhaust action=%v want %v", tc.doctrine, dec.Action, tc.exhaust)
			}
		})
	}
}

func TestRecoveryEngine_HandleWorkerDeath_PermanentInfra_AlwaysHardPause(t *testing.T) {
	for _, doc := range []string{"max-scope", "default", "capa-firewall"} {
		t.Run(doc, func(t *testing.T) {
			fx := newRecoveryFixture(t, doc)
			dec, derr := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
				TaskID: "tperm", WorkerID: "w1",
				Err: &DiskFullError{Path: "/x"}, TierIndex: 0,
			})
			if derr != nil {
				t.Fatalf("HandleWorkerDeath: %v", derr)
			}
			if dec.Action != RecoveryActionHardPause {
				t.Fatalf("permanent-infra under %q action=%v want HardPause", doc, dec.Action)
			}
			if dec.Class != FailurePermanentInfra {
				t.Fatalf("permanent-infra under %q class=%v want PERMANENT_INFRA", doc, dec.Class)
			}

			recs := fx.records(t)
			if got := countByEventType(recs, eventlog.EvtWorkerRedispatched); got != 0 {
				t.Fatalf("HardPause emitted %d WorkerRedispatched events; want 0", got)
			}
		})
	}
}

func TestRecoveryEngine_HandleWorkerDeath_PermanentTask_OnExhaustAction(t *testing.T) {
	cases := []struct {
		doctrine string
		want     RecoveryAction
	}{
		{"max-scope", RecoveryActionEscalateL4},
		{"default", RecoveryActionSkipTask},
		{"capa-firewall", RecoveryActionWaitForConfirmation},
	}
	for _, tc := range cases {
		t.Run(tc.doctrine, func(t *testing.T) {
			fx := newRecoveryFixture(t, tc.doctrine)
			dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
				TaskID: "tperm-task", WorkerID: "w1",
				Err: &HTTPStatusError{Code: 400, Endpoint: "anthropic"}, TierIndex: 0,
			})
			if dec.Action != tc.want {
				t.Fatalf("%s permanent_task action=%v want %v", tc.doctrine, dec.Action, tc.want)
			}
			if dec.Class != FailurePermanentTask {
				t.Fatalf("%s permanent_task class=%v want PERMANENT_TASK", tc.doctrine, dec.Class)
			}

			recs := fx.records(t)
			if got := countByEventType(recs, eventlog.EvtWorkerRedispatched); got != 0 {
				t.Fatalf("permanent_task emitted %d WorkerRedispatched events; want 0", got)
			}
		})
	}
}

func TestRecoveryEngine_OnCorruption_BoundedN5(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	for i := 1; i <= 5; i++ {
		got := fx.eng.OnCorruption(fx.ctx)
		if got != RecoveryActionRedispatchSameTier {
			t.Fatalf("hit %d action=%v want RedispatchSameTier", i, got)
		}
	}
	got := fx.eng.OnCorruption(fx.ctx)
	if got != RecoveryActionHardPause {
		t.Fatalf("hit 6 action=%v want HardPause (inv-zen-095)", got)
	}
}

func TestNewRecoveryEngine_NilDoctrine_Errors(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	evlog := eventlog.NewMemory(fc)
	_, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: nil, EventLog: evlog, Clock: fc,
		ProjectID: "p", SessionID: "s",
	})
	if err == nil {
		t.Fatal("nil doctrine: want wrapped ErrInvalidConfig")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil doctrine err=%v want wrapped ErrInvalidConfig", err)
	}
}

func TestNewRecoveryEngine_NilEventLog_Errors(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	doc := loadFakeDoctrine(t, "max-scope")
	_, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: doc, EventLog: nil, Clock: fc,
		ProjectID: "p", SessionID: "s",
	})
	if err == nil {
		t.Fatal("nil eventlog: want wrapped ErrInvalidConfig")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil eventlog err=%v want wrapped ErrInvalidConfig", err)
	}
}

func TestNewRecoveryEngine_NilClock_DefaultsToReal(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	doc := loadFakeDoctrine(t, "max-scope")
	evlog := eventlog.NewMemory(fc)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: doc, EventLog: evlog, Clock: nil,
		ProjectID: "p", SessionID: "s",
	})
	if err != nil {
		t.Fatalf("nil clock should default to clock.Real{}, got err: %v", err)
	}
	if eng == nil {
		t.Fatal("engine is nil despite no error")
	}

	if eng.clk == nil {
		t.Fatal("eng.clk nil; expected clock.Real{} default")
	}
	if _, ok := eng.clk.(clock.Real); !ok {

		if _, okPtr := eng.clk.(*clock.Real); !okPtr {
			t.Fatalf("eng.clk type=%T; want clock.Real or *clock.Real", eng.clk)
		}
	}

	dec, derr := eng.HandleWorkerDeath(context.Background(), WorkerDeathInput{
		TaskID: "t1", WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503}, TierIndex: 0,
	})
	if derr != nil {
		t.Fatalf("HandleWorkerDeath with real clock: %v", derr)
	}
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("dec.Action=%v want RedispatchSameTier", dec.Action)
	}
}

// TestRecoveryEngine_HandleWorkerDeath_NonRedispatchEmitsNoEvent verifies
// terminal actions (HardPause, EscalateL4, SkipTask, WaitForConfirmation)
// do not emit WorkerRedispatched events — the caller's state machine
// handles terminal transitions.
func TestRecoveryEngine_HandleWorkerDeath_NonRedispatchEmitsNoEvent(t *testing.T) {
	fx := newRecoveryFixture(t, "capa-firewall")

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: "noemit", WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if dec.Action != RecoveryActionWaitForConfirmation {
		t.Fatalf("setup precondition broken: action=%v want WaitForConfirmation", dec.Action)
	}
	recs := fx.records(t)
	if got := countByEventType(recs, eventlog.EvtWorkerRedispatched); got != 0 {
		t.Fatalf("WaitForConfirmation emitted %d WorkerRedispatched events; want 0", got)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_AuditSurvivesCancelledCtx(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	cctx, cancel := context.WithCancel(fx.ctx)
	cancel()

	dec, derr := fx.eng.HandleWorkerDeath(cctx, WorkerDeathInput{
		TaskID:    "cancel-task",
		WorkerID:  "w1",
		Err:       &HTTPStatusError{Code: 503, Endpoint: "anthropic"},
		TierIndex: 0,
	})
	if derr != nil {
		t.Fatalf("HandleWorkerDeath returned err on cancelled ctx: %v", derr)
	}
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("cancelled ctx action=%v want RedispatchSameTier", dec.Action)
	}
	recs := fx.records(t)
	if got := countByEventType(recs, eventlog.EvtWorkerRedispatched); got != 1 {
		t.Fatalf("cancelled ctx WorkerRedispatched count=%d want 1 (forensic-trace must survive cancel)", got)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_NilErr_HardPause(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	dec, derr := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: "tnil", WorkerID: "w1",
		Err: nil, TierIndex: 0,
	})
	if derr != nil {
		t.Fatalf("HandleWorkerDeath(nil err): %v", derr)
	}
	if dec.Action != RecoveryActionHardPause {
		t.Fatalf("nil err action=%v want HardPause", dec.Action)
	}
	if dec.Class != FailurePermanentInfra {
		t.Fatalf("nil err class=%v want PERMANENT_INFRA", dec.Class)
	}
	if !contains(dec.Reason, "<nil>") {
		t.Fatalf("nil err reason=%q want contains <nil>", dec.Reason)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_MaxScope_LLM_CumulativeReclassify(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))

	doc := &fakeDoctrine{
		name: "max-scope-cum", llmRetries: 3, infraRetries: 2,
		permanentAfter: 5,
		onExhaust: map[FailureClass]RecoveryAction{
			FailurePermanentTask:  RecoveryActionEscalateL4,
			FailureTransientLLM:   RecoveryActionEscalateL4,
			FailureTransientInfra: RecoveryActionEscalateL4,
		},
		tierPolicy: TierFallbackFullChain,
	}
	evlog := eventlog.NewMemory(fc)

	tier := AdaptTierChain([]string{"t0", "t1", "t2", "t3"}, 0)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: doc, EventLog: evlog, TierChain: tier, Clock: fc,
		ProjectID: "p", SessionID: "s-cum",
	})
	if err != nil {
		t.Fatalf("NewRecoveryEngine: %v", err)
	}
	taskID := "tcum"
	ctx := context.Background()
	mkErr := func() error { return &HTTPStatusError{Code: 503, Endpoint: "anthropic"} }

	for i := 1; i <= 3; i++ {
		dec, _ := eng.HandleWorkerDeath(ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 0,
		})
		if dec.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("call %d action=%v want SameTier", i, dec.Action)
		}
		if dec.RetryCount != i {
			t.Fatalf("call %d cumulative RetryCount=%d want %d", i, dec.RetryCount, i)
		}
	}

	dec, _ := eng.HandleWorkerDeath(ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 0,
	})
	if dec.Action != RecoveryActionRedispatchNextTier {
		t.Fatalf("call 4 action=%v want NextTier", dec.Action)
	}
	if dec.NewTierIndex != 1 {
		t.Fatalf("call 4 tier=%d want 1", dec.NewTierIndex)
	}
	if dec.RetryCount != 4 {
		t.Fatalf("call 4 cumulative RetryCount=%d want 4 (cumulative MUST NOT reset on tier fallback)", dec.RetryCount)
	}

	dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 1,
	})
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("call 5 action=%v want SameTier (per-tier 1 <= budget 3)", dec.Action)
	}
	if dec.RetryCount != 5 {
		t.Fatalf("call 5 cumulative RetryCount=%d want 5", dec.RetryCount)
	}

	for i := 6; i <= 7; i++ {
		dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 1,
		})
		if dec.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("call %d action=%v want SameTier", i, dec.Action)
		}
	}

	dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 1,
	})
	if dec.Action != RecoveryActionRedispatchNextTier || dec.NewTierIndex != 2 {
		t.Fatalf("call 8 action=%v tier=%d want NextTier→2", dec.Action, dec.NewTierIndex)
	}

	for i := 9; i <= 11; i++ {
		dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 2,
		})
		if dec.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("call %d action=%v want SameTier", i, dec.Action)
		}
	}
	dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 2,
	})
	if dec.Action != RecoveryActionRedispatchNextTier || dec.NewTierIndex != 3 {
		t.Fatalf("call 12 action=%v tier=%d want NextTier→3", dec.Action, dec.NewTierIndex)
	}

	for i := 13; i <= 15; i++ {
		dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 3,
		})
		if dec.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("call %d action=%v want SameTier", i, dec.Action)
		}
	}
	dec, _ = eng.HandleWorkerDeath(ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w", Err: mkErr(), TierIndex: 3,
	})
	if dec.Class != FailurePermanentTask {
		t.Fatalf("final class=%v want PermanentTask (cumulative reclassify)", dec.Class)
	}
	if dec.Action != RecoveryActionEscalateL4 {
		t.Fatalf("final action=%v want EscalateL4 (max-scope OnExhaust(PERMANENT_TASK))", dec.Action)
	}
	if dec.Reason != "reclassified_permanent_task" {
		t.Fatalf("final reason=%q want reclassified_permanent_task", dec.Reason)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_TypedPayloadRoundTrip(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	dec, err := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID:    "trt-1",
		WorkerID:  "w-rt-1",
		Err:       &HTTPStatusError{Code: 503, Endpoint: "anthropic"},
		TierIndex: 0,
	})
	if err != nil {
		t.Fatalf("HandleWorkerDeath: %v", err)
	}
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("setup: action=%v want SameTier", dec.Action)
	}

	recs := fx.records(t)
	var redispatch *eventlog.Record
	for i := range recs {
		if recs[i].EventType == eventlog.EvtWorkerRedispatched {
			redispatch = &recs[i]
			break
		}
	}
	if redispatch == nil {
		t.Fatalf("no EvtWorkerRedispatched in event log; got %d records", len(recs))
	}

	decoded, decErr := eventlog.Decode(redispatch.EventType, redispatch.Payload)
	if decErr != nil {
		t.Fatalf("Decode WorkerRedispatched: %v", decErr)
	}
	wr, ok := decoded.(eventlog.WorkerRedispatched)
	if !ok {
		t.Fatalf("Decode returned %T want eventlog.WorkerRedispatched", decoded)
	}
	if wr.TaskID != "trt-1" {
		t.Fatalf("decoded TaskID=%q want %q", wr.TaskID, "trt-1")
	}
	if wr.WorkerID != "w-rt-1" {
		t.Fatalf("decoded WorkerID=%q want %q", wr.WorkerID, "w-rt-1")
	}
	if wr.Class != "TRANSIENT_LLM" {
		t.Fatalf("decoded Class=%q want %q", wr.Class, "TRANSIENT_LLM")
	}
	if wr.Action != "redispatch_same_tier" {
		t.Fatalf("decoded Action=%q want %q", wr.Action, "redispatch_same_tier")
	}
	if wr.NewTierIndex != 0 {
		t.Fatalf("decoded NewTierIndex=%d want 0", wr.NewTierIndex)
	}
	if wr.RetryCount != 1 {
		t.Fatalf("decoded RetryCount=%d want 1", wr.RetryCount)
	}
	if wr.Reason != "within_budget" {
		t.Fatalf("decoded Reason=%q want within_budget", wr.Reason)
	}
}

func TestRecoveryEngine_HandleWorkerDeath_ConcurrentSameTask_Race(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	const N = 100
	var wg sync.WaitGroup
	var redispatchOK int64
	var nonRedispatch int64
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
				TaskID:    "tconc",
				WorkerID:  "w-conc",
				Err:       &HTTPStatusError{Code: 503, Endpoint: "anthropic"},
				TierIndex: 0,
			})
			if dec.Action == RecoveryActionRedispatchSameTier ||
				dec.Action == RecoveryActionRedispatchNextTier {
				atomic.AddInt64(&redispatchOK, 1)
			} else {
				atomic.AddInt64(&nonRedispatch, 1)
			}
		}()
	}
	wg.Wait()

	if redispatchOK+nonRedispatch != N {
		t.Fatalf("decisions sum=%d want %d (no path should error)",
			redispatchOK+nonRedispatch, N)
	}

	// The audit log must contain exactly one EvtWorkerRedispatched per
	// redispatch decision (the redispatch path is the ONLY path that
	// emits via Append; terminal actions do not).
	recs := fx.records(t)
	got := int64(countByEventType(recs, eventlog.EvtWorkerRedispatched))
	if got != redispatchOK {
		t.Fatalf("EvtWorkerRedispatched=%d want %d (redispatch decisions)",
			got, redispatchOK)
	}

	fx.eng.mu.Lock()
	cum := fx.eng.cumulative[retryKey{taskID: "tconc", class: FailureTransientLLM}]
	fx.eng.mu.Unlock()
	if cum != N {
		t.Fatalf("cumulative retries=%d want %d (every concurrent call must increment exactly once)", cum, N)
	}
}

func TestRecoveryEngine_TierFallback_FullChain_MaxScope(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	taskID := "e3-full-chain"
	mkErr := func() error { return &HTTPStatusError{Code: 503, Endpoint: "anthropic"} }

	for tier := 0; tier < 3; tier++ {
		for i := 1; i <= 3; i++ {
			dec, derr := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
				TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: tier,
			})
			if derr != nil {
				t.Fatalf("tier %d retry %d: %v", tier, i, derr)
			}
			if dec.Action != RecoveryActionRedispatchSameTier {
				t.Fatalf("tier %d retry %d: action=%v want SameTier", tier, i, dec.Action)
			}
		}

		dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: tier,
		})
		if dec.Action != RecoveryActionRedispatchNextTier {
			t.Fatalf("tier %d 4th: action=%v want NextTier", tier, dec.Action)
		}
		if dec.NewTierIndex != tier+1 {
			t.Fatalf("tier %d 4th: NewTierIndex=%d want %d", tier, dec.NewTierIndex, tier+1)
		}
	}

	for i := 1; i <= 3; i++ {
		dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 3,
		})
		if dec.Action != RecoveryActionRedispatchSameTier {
			t.Fatalf("tier3 retry %d: action=%v want SameTier", i, dec.Action)
		}
	}
	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 3,
	})

	if dec.Action != RecoveryActionEscalateL4 {
		t.Fatalf("last tier exhausted: action=%v want EscalateL4 (max-scope OnExhaust)", dec.Action)
	}
	if dec.Class != FailurePermanentTask {
		t.Fatalf("last tier exhausted: class=%v want PermanentTask", dec.Class)
	}
}

func TestRecoveryEngine_TierFallback_Partial_Default(t *testing.T) {
	fx := newRecoveryFixture(t, "default")
	taskID := "e3-partial"
	mkErr := func() error { return &HTTPStatusError{Code: 503, Endpoint: "anthropic"} }

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 0,
	})
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("tier0 retry1: action=%v want SameTier", dec.Action)
	}
	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 0,
	})
	if dec.Action != RecoveryActionRedispatchNextTier || dec.NewTierIndex != 1 {
		t.Fatalf("tier0→1: action=%v tier=%d want NextTier→1", dec.Action, dec.NewTierIndex)
	}

	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 1,
	})
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("tier1 retry1: action=%v want SameTier", dec.Action)
	}
	dec, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1", Err: mkErr(), TierIndex: 1,
	})
	if dec.Action != RecoveryActionSkipTask {
		t.Fatalf("tier1 exhaust: action=%v want SkipTask (default OnExhaust)", dec.Action)
	}

	if dec.NewTierIndex == 2 {
		t.Fatalf("tier1 exhaust: NewTierIndex=%d; tier 2 must not be reached under partial policy", dec.NewTierIndex)
	}
}

func TestRecoveryEngine_TierFallback_None_CapaFirewall(t *testing.T) {
	fx := newRecoveryFixture(t, "capa-firewall")
	taskID := "e3-none"

	dec, derr := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w1",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if derr != nil {
		t.Fatalf("HandleWorkerDeath: %v", derr)
	}
	if dec.Action != RecoveryActionWaitForConfirmation {
		t.Fatalf("none policy: action=%v want WaitForConfirmation", dec.Action)
	}
	if dec.NewTierIndex != 0 {
		t.Fatalf("none policy: NewTierIndex=%d want 0 (no tier advance)", dec.NewTierIndex)
	}
}

func TestAdaptTierChain_LenAndNextTier_FullChain(t *testing.T) {
	tiers := []string{"t1_bypass", "t2_anthropic_paygo", "t3_gemini", "t4_local"}
	chain := AdaptTierChain(tiers, 0)

	if chain.Len() != 4 {
		t.Fatalf("Len()=%d want 4", chain.Len())
	}

	fullCases := []struct {
		cur  int
		next int
		ok   bool
	}{
		{0, 1, true},
		{1, 2, true},
		{2, 3, true},
		{3, 3, false},
	}
	for _, tc := range fullCases {
		got, gotOK := chain.NextTier(tc.cur, TierFallbackFullChain)
		if gotOK != tc.ok {
			t.Fatalf("FullChain NextTier(%d): ok=%v want %v", tc.cur, gotOK, tc.ok)
		}
		if gotOK && got != tc.next {
			t.Fatalf("FullChain NextTier(%d): next=%d want %d", tc.cur, got, tc.next)
		}
	}

	for _, cur := range []int{0, 1, 2, 3} {
		got, ok := chain.NextTier(cur, TierFallbackNone)
		if ok {
			t.Fatalf("None policy NextTier(%d) on non-empty chain: ok=true want false", cur)
		}
		if got != cur {
			t.Fatalf("None policy NextTier(%d): next=%d want %d (current)", cur, got, cur)
		}
	}
}

func TestAdaptTierChain_PartialStopBoundaryNormalization(t *testing.T) {
	tiers := []string{"t0", "t1", "t2", "t3"}

	for _, stopBefore := range []int{0, 99} {
		label := fmt.Sprintf("partialStopBefore=%d", stopBefore)
		chain := AdaptTierChain(tiers, stopBefore)

		cases := []struct {
			cur  int
			next int
			ok   bool
		}{
			{0, 1, true},
			{1, 2, true},
			{2, 3, true},
			{3, 3, false},
		}
		for _, tc := range cases {
			got, gotOK := chain.NextTier(tc.cur, TierFallbackPartial)
			if gotOK != tc.ok {
				t.Fatalf("%s Partial NextTier(%d): ok=%v want %v", label, tc.cur, gotOK, tc.ok)
			}
			if gotOK && got != tc.next {
				t.Fatalf("%s Partial NextTier(%d): next=%d want %d", label, tc.cur, got, tc.next)
			}
		}
	}
}

func TestAdaptTierChain_EmptyChain(t *testing.T) {
	chain := AdaptTierChain([]string{}, 0)

	if chain.Len() != 0 {
		t.Fatalf("empty chain Len()=%d want 0", chain.Len())
	}

	for _, policy := range []TierFallbackPolicy{TierFallbackNone, TierFallbackFullChain, TierFallbackPartial} {
		for _, cur := range []int{0, 1, 5} {
			got, ok := chain.NextTier(cur, policy)
			if ok {
				t.Fatalf("empty chain NextTier(%d, %v): ok=true want false", cur, policy)
			}
			if got != 0 {
				t.Fatalf("empty chain NextTier(%d, %v): next=%d want 0", cur, policy, got)
			}
		}
	}
}

func TestAdaptTierChain_UnknownPolicy(t *testing.T) {
	tiers := []string{"t0", "t1", "t2"}
	chain := AdaptTierChain(tiers, 0)

	unknownPolicy := TierFallbackPolicy(99)
	for _, cur := range []int{0, 1, 2} {
		got, ok := chain.NextTier(cur, unknownPolicy)
		if ok {
			t.Fatalf("unknown policy NextTier(%d): ok=true want false", cur)
		}
		if got != cur {
			t.Fatalf("unknown policy NextTier(%d): next=%d want %d (current)", cur, got, cur)
		}
	}
}

func TestRecoveryAction_String(t *testing.T) {
	cases := []struct {
		a    RecoveryAction
		want string
	}{
		{RecoveryActionRedispatchSameTier, "redispatch_same_tier"},
		{RecoveryActionRedispatchNextTier, "redispatch_next_tier"},
		{RecoveryActionEscalateL4, "escalate_l4"},
		{RecoveryActionSkipTask, "skip_task"},
		{RecoveryActionWaitForConfirmation, "wait_for_confirmation"},
		{RecoveryActionHardPause, "hard_pause"},
		{RecoveryAction(99), "unknown"},
		{RecoveryAction(0), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.a.String(); got != tc.want {
			t.Fatalf("%d.String()=%q want %q", tc.a, got, tc.want)
		}
	}
}

func TestRecoveryEngine_Reclassify_TransientToPermanent_AtThreshold(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	taskID := "treclassify"
	mkInfra := func() WorkerDeathInput {
		return WorkerDeathInput{
			TaskID: taskID, WorkerID: "w",
			Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
		}
	}

	dec1, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec1.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("call 1 action=%v want RedispatchSameTier", dec1.Action)
	}

	dec2, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec2.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("call 2 action=%v want RedispatchSameTier", dec2.Action)
	}

	dec3, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec3.Class != FailurePermanentTask {
		t.Fatalf("call 3 class=%v want FailurePermanentTask (reclassified after cumulative=3 >= permanent_after=3)", dec3.Class)
	}
	if dec3.Action != RecoveryActionEscalateL4 {
		t.Fatalf("call 3 action=%v want RecoveryActionEscalateL4 (max-scope OnExhaust(PERMANENT_TASK))", dec3.Action)
	}
	if dec3.Reason != "reclassified_permanent_task" {
		t.Fatalf("call 3 reason=%q want reclassified_permanent_task", dec3.Reason)
	}
}

func TestRecoveryEngine_Fingerprint_DistinctClassDoesNotShareCounter(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	taskID := "tfp"

	for i := 0; i < 2; i++ {
		_, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
			TaskID: taskID, WorkerID: "w",
			Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
		})
	}

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w",
		Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
	})
	if dec.RetryCount != 1 {
		t.Fatalf("INFRA first death RetryCount=%d want 1 (distinct (task,class) fingerprint must not share LLM counter)", dec.RetryCount)
	}
	if dec.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("INFRA first death action=%v want RecoveryActionRedispatchSameTier (within INFRA budget=2)", dec.Action)
	}

	fx.eng.mu.Lock()
	llmCum := fx.eng.cumulative[retryKey{taskID: taskID, class: FailureTransientLLM}]
	infraCum := fx.eng.cumulative[retryKey{taskID: taskID, class: FailureTransientInfra}]
	fx.eng.mu.Unlock()
	if llmCum != 2 {
		t.Fatalf("LLM cumulative=%d want 2 (INFRA call must not mutate LLM counter)", llmCum)
	}
	if infraCum != 1 {
		t.Fatalf("INFRA cumulative=%d want 1 (fresh fingerprint)", infraCum)
	}
}

func TestRecoveryEngine_Fingerprint_DistinctTaskDoesNotShareCounter(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")

	for i := 0; i < 3; i++ {
		_, _ = fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
			TaskID: "ta", WorkerID: "w",
			Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
		})
	}

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: "tb", WorkerID: "w",
		Err: &HTTPStatusError{Code: 503, Endpoint: "anthropic"}, TierIndex: 0,
	})
	if dec.RetryCount != 1 {
		t.Fatalf("tb first death RetryCount=%d want 1 (distinct task_id must not share counter with ta)", dec.RetryCount)
	}

	fx.eng.mu.Lock()
	taCum := fx.eng.cumulative[retryKey{taskID: "ta", class: FailureTransientLLM}]
	tbCum := fx.eng.cumulative[retryKey{taskID: "tb", class: FailureTransientLLM}]
	fx.eng.mu.Unlock()
	if taCum != 3 {
		t.Fatalf("ta cumulative=%d want 3 (tb call must not mutate ta counter)", taCum)
	}
	if tbCum != 1 {
		t.Fatalf("tb cumulative=%d want 1 (fresh fingerprint)", tbCum)
	}
}

func TestRecoveryEngine_Reclassify_BoundaryAtExactThreshold(t *testing.T) {

	fx := newRecoveryFixture(t, "max-scope")
	taskID := "tboundary"
	mkInfra := func() WorkerDeathInput {
		return WorkerDeathInput{
			TaskID: taskID, WorkerID: "w",
			Err: &WorkerSubprocessError{Reason: "oom"}, TierIndex: 0,
		}
	}

	for i := 1; i <= 2; i++ {
		dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
		if dec.Class == FailurePermanentTask {
			t.Fatalf("call %d: premature reclassify (cumulative=%d < permanent_after=3)", i, i)
		}
	}

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec.Class != FailurePermanentTask {
		t.Fatalf("call 3 class=%v want FailurePermanentTask (cumulative=3 == permanent_after=3; >= must fire, not just >)", dec.Class)
	}
	if dec.Action != RecoveryActionEscalateL4 {
		t.Fatalf("call 3 action=%v want RecoveryActionEscalateL4", dec.Action)
	}
}

func TestRecoveryEngine_Reclassify_DefaultDoctrine_SkipTask(t *testing.T) {
	fx := newRecoveryFixture(t, "default")
	taskID := "tdefault-reclassify"
	mkInfra := func() WorkerDeathInput {
		return WorkerDeathInput{
			TaskID: taskID, WorkerID: "w",
			Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
		}
	}

	dec1, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec1.Action != RecoveryActionRedispatchSameTier {
		t.Fatalf("call 1 action=%v want RedispatchSameTier", dec1.Action)
	}

	dec2, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec2.Class != FailureTransientInfra {
		t.Fatalf("call 2 class=%v want FailureTransientInfra (cumulative=2 < permanent_after=3, not yet reclassified)", dec2.Class)
	}

	dec3, _ := fx.eng.HandleWorkerDeath(fx.ctx, mkInfra())
	if dec3.Class != FailurePermanentTask {
		t.Fatalf("call 3 class=%v want FailurePermanentTask (cumulative=3 >= permanent_after=3 → reclassify)", dec3.Class)
	}
	if dec3.Action != RecoveryActionSkipTask {
		t.Fatalf("call 3 action=%v want RecoveryActionSkipTask (default OnExhaust(PERMANENT_TASK))", dec3.Action)
	}
	if dec3.Reason != "reclassified_permanent_task" {
		t.Fatalf("call 3 reason=%q want reclassified_permanent_task", dec3.Reason)
	}
}

func TestRecoveryEngine_Reclassify_CapaFirewallDoctrine_WaitConfirmation(t *testing.T) {
	fx := newRecoveryFixture(t, "capa-firewall")
	taskID := "tcapa-reclassify"

	dec, _ := fx.eng.HandleWorkerDeath(fx.ctx, WorkerDeathInput{
		TaskID: taskID, WorkerID: "w",
		Err: &WorkerSubprocessError{Reason: "panic"}, TierIndex: 0,
	})
	if dec.Class != FailurePermanentTask {
		t.Fatalf("capa-firewall class=%v want FailurePermanentTask (cumulative=1 >= permanent_after=1 → reclassify)", dec.Class)
	}
	if dec.Action != RecoveryActionWaitForConfirmation {
		t.Fatalf("capa-firewall action=%v want RecoveryActionWaitForConfirmation (OnExhaust(PERMANENT_TASK))", dec.Action)
	}
	if dec.Reason != "reclassified_permanent_task" {
		t.Fatalf("capa-firewall reason=%q want reclassified_permanent_task", dec.Reason)
	}
}

func TestRecoveryEngine_LastAssignmentFor(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	ctx := context.Background()
	now := fx.eng.clk.Now()

	for i, ev := range []eventlog.Event{
		{
			Type: eventlog.EvtWorkerDispatched, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now,
			Payload: map[string]any{"worker_id": "w1", "task_id": "task-1", "tier": "t1"},
		},
		{
			Type: eventlog.EvtWorkerDispatched, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now.Add(time.Second),
			Payload: map[string]any{"worker_id": "w2", "task_id": "task-2", "tier": "t1"},
		},
		{
			Type: eventlog.EvtWorkerDispatched, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now.Add(2 * time.Second),
			Payload: map[string]any{"worker_id": "w1", "task_id": "task-3", "tier": "t2"},
		},
	} {
		if _, err := fx.evlog.Append(ctx, ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	if _, err := fx.evlog.Append(ctx, eventlog.Event{
		Type: eventlog.EvtWorkerDispatched, SessionID: "s-other",
		ProjectID: "p-test", Timestamp: now.Add(3 * time.Second),
		Payload: map[string]any{"worker_id": "w1", "task_id": "task-other", "tier": "t1"},
	}); err != nil {
		t.Fatalf("append other-session: %v", err)
	}

	if got := fx.eng.LastAssignmentFor(ctx, "w1"); got != "task-3" {
		t.Errorf("LastAssignmentFor(w1)=%q want task-3 (most recent)", got)
	}
	if got := fx.eng.LastAssignmentFor(ctx, "w2"); got != "task-2" {
		t.Errorf("LastAssignmentFor(w2)=%q want task-2", got)
	}
	if got := fx.eng.LastAssignmentFor(ctx, "w-unknown"); got != "" {
		t.Errorf("LastAssignmentFor(unknown)=%q want \"\"", got)
	}
}

func TestRecoveryEngine_LastAssignmentFor_NoEvents(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	if got := fx.eng.LastAssignmentFor(context.Background(), "w-any"); got != "" {
		t.Errorf("LastAssignmentFor on empty log=%q want \"\"", got)
	}
}

type corruptingEmitter struct {
	rows []eventlog.Record
}

func (c *corruptingEmitter) EmitRaw(_ context.Context, _, _ string, _ int, _ []byte, _ int64) (int64, error) {

	return 0, errors.New("corruptingEmitter.EmitRaw not implemented")
}

func (c *corruptingEmitter) QueryRaw(_ context.Context, _ string, _ int64) ([]eventlog.Record, error) {
	return c.rows, nil
}

func TestRecoveryEngine_LastAssignmentFor_DecodeError(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	emitter := &corruptingEmitter{
		rows: []eventlog.Record{
			{
				EventID:   1,
				SessionID: "s-corrupt",
				ProjectID: "p",
				EventType: eventlog.EvtWorkerDispatched,
				Payload:   []byte("{not-json"),
				Timestamp: fc.Now().UnixNano(),
			},
		},
	}
	evlog := eventlog.New(emitter, fc)
	doc := loadFakeDoctrine(t, "max-scope")
	tier := AdaptTierChain([]string{"t0"}, 0)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: doc, EventLog: evlog, TierChain: tier, Clock: fc,
		ProjectID: "p", SessionID: "s-corrupt",
	})
	if err != nil {
		t.Fatalf("NewRecoveryEngine: %v", err)
	}
	if got := eng.LastAssignmentFor(context.Background(), "w1"); got != "" {
		t.Errorf("LastAssignmentFor on corrupt row=%q want \"\" (skip-and-fail-soft)", got)
	}
}

type erroringEmitter struct{}

func (erroringEmitter) EmitRaw(_ context.Context, _, _ string, _ int, _ []byte, _ int64) (int64, error) {
	return 0, errors.New("erroringEmitter.EmitRaw not implemented")
}
func (erroringEmitter) QueryRaw(_ context.Context, _ string, _ int64) ([]eventlog.Record, error) {
	return nil, errors.New("simulated query failure")
}

func TestRecoveryEngine_LastAssignmentFor_QueryError(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	evlog := eventlog.New(erroringEmitter{}, fc)
	doc := loadFakeDoctrine(t, "max-scope")
	tier := AdaptTierChain([]string{"t0"}, 0)
	eng, err := NewRecoveryEngine(RecoveryEngineConfig{
		Doctrine: doc, EventLog: evlog, TierChain: tier, Clock: fc,
		ProjectID: "p", SessionID: "s-err",
	})
	if err != nil {
		t.Fatalf("NewRecoveryEngine: %v", err)
	}
	if got := eng.LastAssignmentFor(context.Background(), "w1"); got != "" {
		t.Errorf("LastAssignmentFor on Query error=%q want \"\"", got)
	}
}

func TestRecoveryEngine_LastAssignmentFor_EmptyWorkerID(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")

	if _, err := fx.evlog.Append(context.Background(), eventlog.Event{
		Type: eventlog.EvtWorkerDispatched, SessionID: "s-test-max-scope",
		ProjectID: "p-test", Timestamp: fx.eng.clk.Now(),
		Payload: map[string]any{"worker_id": "w1", "task_id": "t1", "tier": "t1"},
	}); err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}
	if got := fx.eng.LastAssignmentFor(context.Background(), ""); got != "" {
		t.Errorf("LastAssignmentFor(\"\")=%q want \"\" (fast-path guard)", got)
	}
}

func TestRecoveryEngine_LastAssignmentFor_IgnoresOtherEventTypes(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	ctx := context.Background()
	now := fx.eng.clk.Now()

	for _, ev := range []eventlog.Event{
		{
			Type: eventlog.EvtWorkerDispatched, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now,
			Payload: map[string]any{"worker_id": "w1", "task_id": "task-real", "tier": "t1"},
		},
		{
			Type: eventlog.EvtWorkerCheckpoint, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now.Add(time.Second),
			Payload: map[string]any{"worker_id": "w1", "checkpoint_sha": "abc", "summary": "step"},
		},
		{
			Type: eventlog.EvtWorkerDeath, SessionID: "s-test-max-scope",
			ProjectID: "p-test", Timestamp: now.Add(2 * time.Second),
			Payload: map[string]any{
				"worker_id": "w1", "task_id": "task-bogus",
				"class": "TRANSIENT_INFRA", "reason": "x", "retry_count": 0,
			},
		},
	} {
		if _, err := fx.evlog.Append(ctx, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	if got := fx.eng.LastAssignmentFor(ctx, "w1"); got != "task-real" {
		t.Errorf("LastAssignmentFor(w1)=%q want task-real (death/checkpoint must be ignored)", got)
	}
}
