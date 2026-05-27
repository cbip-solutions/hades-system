package dispatcher_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

type fakeBackend struct {
	mu           sync.Mutex
	tier         providers.Tier
	name         string
	calls        int
	lastReq      providers.TierRequest
	respFn       func(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error)
	probeFn      func(ctx context.Context) error
	capabilities providers.TierCapabilities
}

func (f *fakeBackend) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	f.mu.Unlock()
	if f.respFn == nil {
		return &providers.TierResponse{TierUsed: f.tier, Status: 200}, nil
	}
	return f.respFn(ctx, req)
}

func (f *fakeBackend) Probe(ctx context.Context) error {
	if f.probeFn == nil {
		return nil
	}
	return f.probeFn(ctx)
}

func (f *fakeBackend) Close() error                             { return nil }
func (f *fakeBackend) Name() string                             { return f.name }
func (f *fakeBackend) Capabilities() providers.TierCapabilities { return f.capabilities }
func (f *fakeBackend) Tier() providers.Tier                     { return f.tier }

func (f *fakeBackend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type recordingEmitter struct {
	mu     sync.Mutex
	events []dispatcher.CostEvent
	err    error
}

func (r *recordingEmitter) Emit(_ context.Context, evt dispatcher.CostEvent) error {
	r.mu.Lock()
	r.events = append(r.events, evt)
	r.mu.Unlock()
	return r.err
}

func (r *recordingEmitter) snapshot() []dispatcher.CostEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]dispatcher.CostEvent, len(r.events))
	copy(out, r.events)
	return out
}

type stubBreaker struct {
	mu        sync.Mutex
	permit    map[string]bool
	successes map[string]int
	failures  map[string]int
}

func newStubBreaker() *stubBreaker {
	return &stubBreaker{
		permit:    map[string]bool{},
		successes: map[string]int{},
		failures:  map[string]int{},
	}
}

func (s *stubBreaker) Permit(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.permit[name]
	if !ok {
		return true
	}
	return v
}

func (s *stubBreaker) RecordSuccess(name string) {
	s.mu.Lock()
	s.successes[name]++
	s.mu.Unlock()
}

func (s *stubBreaker) RecordFailure(name string) {
	s.mu.Lock()
	s.failures[name]++
	s.mu.Unlock()
}

func (s *stubBreaker) RecordRateLimited(_ string, _ time.Duration) {}

func (s *stubBreaker) setPermit(name string, allow bool) {
	s.mu.Lock()
	s.permit[name] = allow
	s.mu.Unlock()
}

func (s *stubBreaker) successCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successes[name]
}

func (s *stubBreaker) failureCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures[name]
}

type fakeBreaker struct {
	mu        sync.Mutex
	deny      map[string]bool
	successes []string
	failures  []string
}

func newFakeBreaker() *fakeBreaker {
	return &fakeBreaker{deny: map[string]bool{}}
}

func (f *fakeBreaker) Permit(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.deny[name]
}

func (f *fakeBreaker) RecordSuccess(name string) {
	f.mu.Lock()
	f.successes = append(f.successes, name)
	f.mu.Unlock()
}

func (f *fakeBreaker) RecordFailure(name string) {
	f.mu.Lock()
	f.failures = append(f.failures, name)
	f.mu.Unlock()
}

func (f *fakeBreaker) RecordRateLimited(name string, _ time.Duration) {
	f.mu.Lock()

	_ = name
	f.mu.Unlock()
}

type fakeRegistry struct {
	backends map[string]providers.TierBackend
}

func (r *fakeRegistry) Get(name string) (providers.TierBackend, error) {
	b, ok := r.backends[name]
	if !ok {
		return nil, fmt.Errorf("fakeRegistry: %q not registered", name)
	}
	return b, nil
}

type fakeResolver struct {
	cascades       map[string][]string
	defaultCascade []string
	err            error
}

func (r *fakeResolver) Resolve(profile, project string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	if c, ok := r.cascades[profile]; ok {
		return c, nil
	}
	if r.defaultCascade != nil {
		return r.defaultCascade, nil
	}
	return nil, fmt.Errorf("fakeResolver: no cascade for profile %q", profile)
}

func newRecordingEmitter() *recordingEmitter {
	return &recordingEmitter{}
}

func newTwoBackendDispatcher(
	tier1, tier2 providers.TierBackend,
	emitter dispatcher.CostEmitter,
	breaker dispatcher.BreakerState,
) *dispatcher.Dispatcher {
	reg := &fakeRegistry{
		backends: map[string]providers.TierBackend{
			"in-house":   tier1,
			"openclaude": tier2,
		},
	}
	res := &fakeResolver{
		cascades: map[string][]string{
			"": {"in-house", "openclaude"},
		},

		defaultCascade: []string{"in-house", "openclaude"},
	}
	return dispatcher.New(reg, res, emitter, breaker)
}

func TestNew_RequiresAllDeps(t *testing.T) {
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{}}
	res := &fakeResolver{cascades: map[string][]string{}}
	emt := newRecordingEmitter()
	brk := newFakeBreaker()

	var _ dispatcher.BackendRegistry = reg
	var _ dispatcher.ProfileResolver = res

	d := dispatcher.New(reg, res, emt, brk)
	if d == nil {
		t.Fatal("New returned nil for valid deps")
	}
	for _, tc := range []struct {
		name string
		reg  dispatcher.BackendRegistry
		res  dispatcher.ProfileResolver
		emt  dispatcher.CostEmitter
		brk  dispatcher.BreakerState
	}{
		{"nil registry", nil, res, emt, brk},
		{"nil resolver", reg, nil, emt, brk},
		{"nil emitter", reg, res, nil, brk},
		{"nil breaker", reg, res, emt, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%s): expected panic, got none", tc.name)
				}
			}()
			_ = dispatcher.New(tc.reg, tc.res, tc.emt, tc.brk)
		})
	}
}

func TestForward_Tier1Success(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse,
		name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{
				Status:       200,
				Body:         []byte("tier1 response"),
				TierUsed:     providers.TierInHouse,
				ModelUsed:    "claude-sonnet-4-6",
				InputTokens:  5,
				OutputTokens: 2,
				LatencyMs:    7,
			}, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude,
		name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("Tier 2+ MUST NOT be called when Tier 1 succeeds")
			return nil, nil
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)
	resp, err := d.Forward(context.Background(), providers.TierRequest{
		Method: "POST",
		Path:   "/v1/messages",
		Body:   []byte(`{"model":"claude-sonnet-4-6"}`),
		Headers: map[string]string{
			"X-Zen-Profile": "tier-test",
			"X-Zen-Project": "test-project",
		},
		Profile: "tier-test",
		Project: "test-project",
		Model:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("Forward: unexpected error %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response")
	}
	if resp.TierUsed != providers.TierInHouse {
		t.Errorf("TierUsed = %v, want TierInHouse", resp.TierUsed)
	}
	if string(resp.Body) != "tier1 response" {
		t.Errorf("Body = %q, want %q", resp.Body, "tier1 response")
	}
	if tier1.callCount() != 1 {
		t.Errorf("tier1 call count = %d, want 1", tier1.callCount())
	}
	if tier2.callCount() != 0 {
		t.Errorf("tier2 call count = %d, want 0 (no failover on success)", tier2.callCount())
	}
	if breaker.successCount(providers.TierInHouse.String()) != 1 {
		t.Errorf("breaker successes for TierInHouse = %d, want 1", breaker.successCount(providers.TierInHouse.String()))
	}
	if breaker.failureCount(providers.TierInHouse.String()) != 0 {
		t.Errorf("breaker failures for TierInHouse = %d, want 0", breaker.failureCount(providers.TierInHouse.String()))
	}

	events := emitter.snapshot()
	if len(events) != 1 {
		t.Fatalf("emitter events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Tier != providers.TierInHouse {
		t.Errorf("CostEvent.Tier = %v, want TierInHouse", ev.Tier)
	}
	if ev.Profile != "tier-test" {
		t.Errorf("CostEvent.Profile = %q, want tier-test", ev.Profile)
	}
	if ev.Project != "test-project" {
		t.Errorf("CostEvent.Project = %q, want test-project", ev.Project)
	}
	if ev.Model != "claude-sonnet-4-6" {
		t.Errorf("CostEvent.Model = %q, want claude-sonnet-4-6", ev.Model)
	}
	if ev.InputTokens != 5 || ev.OutputTokens != 2 {
		t.Errorf("CostEvent token counts = (%d,%d), want (5,2)", ev.InputTokens, ev.OutputTokens)
	}
	if ev.Status != 200 {
		t.Errorf("CostEvent.Status = %d, want 200", ev.Status)
	}
	if ev.Err != "" {
		t.Errorf("CostEvent.Err = %q, want empty", ev.Err)
	}
}

func TestForward_FailoverToTier2(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse,
		name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return nil, providers.ErrTierUnavailable
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude,
		name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{
				Status:       200,
				Body:         []byte("tier2 response"),
				TierUsed:     providers.TierOpenClaude,
				ModelUsed:    "claude-sonnet-4-6",
				InputTokens:  3,
				OutputTokens: 4,
			}, nil
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)
	resp, err := d.Forward(context.Background(), providers.TierRequest{
		Body:    []byte("{}"),
		Profile: "tier-test",
		Project: "test-project",
		Model:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("Forward: unexpected error %v", err)
	}
	if resp.TierUsed != providers.TierOpenClaude {
		t.Errorf("TierUsed = %v, want TierOpenClaude (post-failover)", resp.TierUsed)
	}
	if string(resp.Body) != "tier2 response" {
		t.Errorf("Body = %q, want tier2 response", resp.Body)
	}
	if tier1.callCount() != 1 {
		t.Errorf("tier1 call count = %d, want 1", tier1.callCount())
	}
	if tier2.callCount() != 1 {
		t.Errorf("tier2 call count = %d, want 1", tier2.callCount())
	}
	if breaker.failureCount(providers.TierInHouse.String()) != 1 {
		t.Errorf("breaker failures for TierInHouse = %d, want 1", breaker.failureCount(providers.TierInHouse.String()))
	}
	if breaker.successCount(providers.TierOpenClaude.String()) != 1 {
		t.Errorf("breaker successes for TierOpenClaude = %d, want 1", breaker.successCount(providers.TierOpenClaude.String()))
	}

	events := emitter.snapshot()
	if len(events) != 2 {
		t.Fatalf("emitter events = %d, want 2 (failure + success)", len(events))
	}
	if events[0].Tier != providers.TierInHouse || events[0].Err == "" {
		t.Errorf("first event tier=%v err=%q, want TierInHouse + non-empty Err", events[0].Tier, events[0].Err)
	}
	if events[1].Tier != providers.TierOpenClaude || events[1].Err != "" {
		t.Errorf("second event tier=%v err=%q, want TierOpenClaude + empty Err", events[1].Tier, events[1].Err)
	}
	// LatencyMS is a wall-clock measurement; cannot reliably bound it > 0
	// under fast mocks (sub-microsecond elapsed rounds to 0 ms), but it
	// MUST never go negative — that would imply the time.Since math broke.
	if events[0].LatencyMS < 0 {
		t.Errorf("Tier1 LatencyMS = %d, want >= 0", events[0].LatencyMS)
	}
	if events[1].LatencyMS < 0 {
		t.Errorf("Tier2 LatencyMS = %d, want >= 0", events[1].LatencyMS)
	}
}

func TestForward_BreakerVetoesTier1(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse,
		name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("Tier 1 MUST NOT be called when breaker vetoes it")
			return nil, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude,
		name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{Status: 200, Body: []byte("ok"), TierUsed: providers.TierOpenClaude}, nil
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()
	breaker.setPermit(providers.TierInHouse.String(), false)

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)
	resp, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.TierUsed != providers.TierOpenClaude {
		t.Errorf("TierUsed = %v, want TierOpenClaude", resp.TierUsed)
	}
	if tier1.callCount() != 0 {
		t.Errorf("tier1 call count = %d, want 0 (breaker veto)", tier1.callCount())
	}
	if tier2.callCount() != 1 {
		t.Errorf("tier2 call count = %d, want 1", tier2.callCount())
	}
	// Breaker veto MUST NOT generate a cost event for the skipped tier.
	events := emitter.snapshot()
	if len(events) != 1 {
		t.Fatalf("emitter events = %d, want 1 (Tier 2 success only)", len(events))
	}
	if events[0].Tier != providers.TierOpenClaude {
		t.Errorf("event tier = %v, want TierOpenClaude", events[0].Tier)
	}
}

func TestForward_AllTiersUnavailable(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse,
		name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return nil, providers.ErrTierUnavailable
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude,
		name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return nil, errors.New("openclaude 503")
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)
	resp, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if !errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
		t.Fatalf("err = %v, want ErrAllTiersUnavailable", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil on terminal failure", resp)
	}
	if breaker.failureCount(providers.TierInHouse.String()) != 1 {
		t.Errorf("TierInHouse failures = %d, want 1", breaker.failureCount(providers.TierInHouse.String()))
	}
	if breaker.failureCount(providers.TierOpenClaude.String()) != 1 {
		t.Errorf("TierOpenClaude failures = %d, want 1", breaker.failureCount(providers.TierOpenClaude.String()))
	}
	events := emitter.snapshot()
	if len(events) != 2 {
		t.Fatalf("emitter events = %d, want 2 (both failures)", len(events))
	}
	if events[0].Err == "" || events[1].Err == "" {
		t.Errorf("expected both events to carry non-empty Err, got %+v", events)
	}
}

func TestForward_AllTiersBreakerVetoed(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("must not call Tier 1 when vetoed")
			return nil, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("must not call Tier 2 when vetoed")
			return nil, nil
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()
	breaker.setPermit(providers.TierInHouse.String(), false)
	breaker.setPermit(providers.TierOpenClaude.String(), false)

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)
	_, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if !errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
		t.Fatalf("err = %v, want ErrAllTiersUnavailable", err)
	}
	if len(emitter.snapshot()) != 0 {
		t.Errorf("expected zero cost events when both tiers vetoed, got %d", len(emitter.snapshot()))
	}
}

func TestForward_PassesRequestUnchanged(t *testing.T) {
	cred := redact.NewSecret("sk-test-12345")
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{Status: 200, Body: []byte("ok"), TierUsed: providers.TierInHouse}, nil
		},
	}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}

	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())
	original := providers.TierRequest{
		Method:         "POST",
		Path:           "/v1/messages",
		Headers:        map[string]string{"X-Zen-Profile": "p", "Content-Type": "application/json"},
		Body:           []byte(`{"hi":"there"}`),
		Credentials:    map[string]redact.Secret{"x-api-key": cred},
		ConversationID: "conv-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-1",
		Profile:        "p",
		Project:        "proj",
		Model:          "claude-sonnet-4-6",
	}
	if _, err := d.Forward(context.Background(), original); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	got := tier1.lastReq
	if got.Method != "POST" || got.Path != "/v1/messages" {
		t.Errorf("method/path mutated: %q %q", got.Method, got.Path)
	}
	if string(got.Body) != `{"hi":"there"}` {
		t.Errorf("Body mutated: %q", got.Body)
	}
	if got.Headers["X-Zen-Profile"] != "p" {
		t.Errorf("X-Zen-Profile dropped/mutated: %q", got.Headers["X-Zen-Profile"])
	}
	if _, ok := got.Credentials["x-api-key"]; !ok {
		t.Errorf("credential dropped by dispatcher")
	}
	// And the credential value MUST still reveal correctly (no double-wrap).
	if revealed := string(got.Credentials["x-api-key"].Reveal()); revealed != "sk-test-12345" {
		t.Errorf("credential corrupted: reveal = %q", revealed)
	}
	if got.ConversationID != "conv-1" || got.SessionID != "sess-1" || got.IdempotencyKey != "idem-1" {
		t.Errorf("routing context mutated: %+v", got)
	}
}

// ----------------------------------------------------------------------------
// Forward — emitter error MUST NOT shadow a successful upstream response.
// The cost-emit path is best-effort.
// ----------------------------------------------------------------------------

func TestForward_EmitterErrorDoesNotShadowSuccess(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{Status: 200, Body: []byte("ok"), TierUsed: providers.TierInHouse}, nil
		},
	}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	emitter := &recordingEmitter{err: errors.New("ledger down")}

	d := newTwoBackendDispatcher(tier1, tier2, emitter, newStubBreaker())
	resp, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp == nil || string(resp.Body) != "ok" {
		t.Errorf("response should pass through despite emitter error; got %+v", resp)
	}
}

// ----------------------------------------------------------------------------
// New — every required dependency individually MUST trigger a panic when nil.
// Single-argument coverage is what makes the per-arg OR clauses meaningful;
// without it, only the first nil-check is exercised.
// ----------------------------------------------------------------------------

func TestNew_NilDependencyPanics(t *testing.T) {
	realReg := &fakeRegistry{backends: map[string]providers.TierBackend{}}
	realRes := &fakeResolver{cascades: map[string][]string{}}
	realEmit := &recordingEmitter{}
	realBreaker := newStubBreaker()

	cases := []struct {
		name    string
		reg     dispatcher.BackendRegistry
		res     dispatcher.ProfileResolver
		emitter dispatcher.CostEmitter
		breaker dispatcher.BreakerState
	}{
		{"nil registry", nil, realRes, realEmit, realBreaker},
		{"nil resolver", realReg, nil, realEmit, realBreaker},
		{"nil emitter", realReg, realRes, nil, realBreaker},
		{"nil breaker", realReg, realRes, realEmit, nil},
		{"all nil", nil, nil, nil, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for %s, got none", tc.name)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value not a string: %v", r)
				}
				if !strings.Contains(msg, "dispatcher.New") {
					t.Errorf("panic message %q missing 'dispatcher.New' tag", msg)
				}
			}()
			_ = dispatcher.New(tc.reg, tc.res, tc.emitter, tc.breaker)
		})
	}
}

// ----------------------------------------------------------------------------
// Forward — concurrent invocation MUST be safe. Documented in dispatcher.go
// as "Forward is safe for concurrent invocation". Run under -race to catch
// data races; assert per-tier counts equal N and emitter received N events.
// ----------------------------------------------------------------------------

func TestForward_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	const N = 64
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{Status: 200, TierUsed: providers.TierInHouse, Body: []byte("ok")}, nil
		},
	}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()

	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)

	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			if _, err := d.Forward(context.Background(), providers.TierRequest{
				Body:    []byte("{}"),
				Profile: "p", Project: "proj", Model: "m",
			}); err != nil {
				t.Errorf("Forward: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := tier1.callCount(); got != N {
		t.Errorf("tier1 calls = %d, want %d", got, N)
	}
	if got := tier2.callCount(); got != 0 {
		t.Errorf("tier2 calls = %d, want 0 (no failover on success)", got)
	}
	if got := breaker.successCount(providers.TierInHouse.String()); got != N {
		t.Errorf("Tier1 successes = %d, want %d", got, N)
	}
	if got := breaker.failureCount(providers.TierInHouse.String()); got != 0 {
		t.Errorf("Tier1 failures = %d, want 0", got)
	}
	if got := len(emitter.snapshot()); got != N {
		t.Errorf("emitted events = %d, want %d", got, N)
	}
}

// ----------------------------------------------------------------------------
// Forward — request with nil Headers/Credentials maps MUST NOT cause NPE.
// invariant: dispatcher does not synthesise or rewrite these maps.
// ----------------------------------------------------------------------------

func TestForward_AcceptsNilMapsInRequest(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
			if req.Headers != nil {
				t.Errorf("backend Headers = %v, want nil", req.Headers)
			}
			if req.Credentials != nil {
				t.Errorf("backend Credentials = %v, want nil", req.Credentials)
			}
			return &providers.TierResponse{Status: 200, TierUsed: providers.TierInHouse, Body: []byte("ok")}, nil
		},
	}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())

	resp, err := d.Forward(context.Background(), providers.TierRequest{

		Body:    []byte("{}"),
		Profile: "p", Project: "proj", Model: "m",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp == nil || resp.Status != 200 {
		t.Errorf("resp = %+v, want status 200", resp)
	}
}

// ----------------------------------------------------------------------------
// Forward — caller cancels ctx after Tier 1 starts. Dispatcher MUST NOT then
// roll over to Tier 2; it MUST short-circuit and surface ctx.Err() so callers
// see context.Canceled (not a misleading 503 ErrAllTiersUnavailable). Tier 1
// failure event still emits (caller can audit what happened pre-cancel).
// ----------------------------------------------------------------------------

func TestForward_CancelledContextSkipsFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {

			cancel()
			return nil, context.Canceled
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("Tier 2 MUST NOT be called once ctx is cancelled")
			return nil, nil
		},
	}
	emitter := &recordingEmitter{}
	d := newTwoBackendDispatcher(tier1, tier2, emitter, newStubBreaker())

	resp, err := d.Forward(ctx, providers.TierRequest{Body: []byte("{}")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil on cancel", resp)
	}
	if tier1.callCount() != 1 {
		t.Errorf("tier1 calls = %d, want 1 (cancelled mid-call)", tier1.callCount())
	}
	if tier2.callCount() != 0 {
		t.Errorf("tier2 calls = %d, want 0 (no failover after cancel)", tier2.callCount())
	}
	// The Tier 1 failure event MUST still be recorded (audit fidelity).
	events := emitter.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (Tier 1 failure pre-cancel)", len(events))
	}
	if events[0].Tier != providers.TierInHouse || events[0].Err == "" {
		t.Errorf("event = %+v, want TierInHouse + non-empty Err", events[0])
	}
}

// ----------------------------------------------------------------------------
// Forward — ctx already past deadline when Forward called. Tier 1 fails with
// deadline-exceeded; dispatcher MUST return ctx.Err() (not
// ErrAllTiersUnavailable), and MUST NOT call Tier 2.
// ----------------------------------------------------------------------------

func TestForward_DeadlineExpiredReturnsCtxErr(t *testing.T) {

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("Tier 2 MUST NOT be called once ctx deadline expired")
			return nil, nil
		},
	}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())

	_, err := d.Forward(ctx, providers.TierRequest{Body: []byte("{}")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if tier2.callCount() != 0 {
		t.Errorf("tier2 calls = %d, want 0 (no failover after deadline)", tier2.callCount())
	}
}

// ----------------------------------------------------------------------------
// Forward — Tier 1 vetoed by breaker; Tier 2 fails AND caller ctx is done at
// terminal time. Dispatcher MUST surface ctx.Err() (not ErrAllTiersUnavailable),
// covering the post-Tier-2-failure ctx-err short-circuit at lines 223-225 of
// dispatcher.go (introduced in the I-3 fix). Documented behaviour: when Tier 1
// is vetoed, the ctx-cancel check after Tier 1 is never reached, so the only
// way to cover the terminal ctx-err guard is via this Tier-1-vetoed path.
// ----------------------------------------------------------------------------

func TestForward_Tier1VetoedTier2FailsCtxDoneReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("Tier 1 MUST NOT be called when breaker vetoes it")
			return nil, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {

			cancel()
			return nil, errors.New("openclaude transport error")
		},
	}
	breaker := newStubBreaker()
	breaker.setPermit(providers.TierInHouse.String(), false)

	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, breaker)
	_, err := d.Forward(ctx, providers.TierRequest{Body: []byte("{}")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (terminal ctx-err short-circuit)", err)
	}
	if errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
		t.Fatalf("err must not be ErrAllTiersUnavailable when ctx is done at terminal check")
	}
	if tier1.callCount() != 0 {
		t.Errorf("tier1 calls = %d, want 0 (breaker veto)", tier1.callCount())
	}
	if tier2.callCount() != 1 {
		t.Errorf("tier2 calls = %d, want 1", tier2.callCount())
	}
}

// ----------------------------------------------------------------------------
// Forward — backend contract violation: Forward returns (nil resp, nil err).
// Dispatcher MUST NOT NPE; it treats the violation as a tier failure (records
// breaker failure, emits CostEvent with explanatory Err) and falls over to
// Tier 2+. drift audit catches the violation in the cost ledger.
// ----------------------------------------------------------------------------

func TestForward_BackendReturnsNilNilTreatedAsFailure(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {

			return nil, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			return &providers.TierResponse{Status: 200, Body: []byte("tier2"), TierUsed: providers.TierOpenClaude}, nil
		},
	}
	emitter := &recordingEmitter{}
	breaker := newStubBreaker()
	d := newTwoBackendDispatcher(tier1, tier2, emitter, breaker)

	resp, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if err != nil {
		t.Fatalf("Forward: unexpected error %v", err)
	}
	if resp == nil || string(resp.Body) != "tier2" {
		t.Errorf("resp = %+v, want Tier 2 response", resp)
	}

	if breaker.failureCount(providers.TierInHouse.String()) != 1 {
		t.Errorf("Tier1 failures = %d, want 1", breaker.failureCount(providers.TierInHouse.String()))
	}
	if breaker.successCount(providers.TierOpenClaude.String()) != 1 {
		t.Errorf("Tier2 successes = %d, want 1", breaker.successCount(providers.TierOpenClaude.String()))
	}
	events := emitter.snapshot()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (Tier1 violation + Tier2 success)", len(events))
	}
	if events[0].Tier != providers.TierInHouse || events[0].Err == "" {
		t.Errorf("first event = %+v, want TierInHouse with non-empty Err", events[0])
	}
	if !strings.Contains(events[0].Err, "nil") {
		t.Errorf("Tier1 violation Err = %q, want mention of nil response/err", events[0].Err)
	}
	if events[1].Tier != providers.TierOpenClaude || events[1].Err != "" {
		t.Errorf("second event = %+v, want TierOpenClaude with empty Err", events[1])
	}
}

type fakeOverrideStore struct {
	mu   sync.Mutex
	rows map[string]quota.Override
}

func newFakeOverrideStore() *fakeOverrideStore {
	return &fakeOverrideStore{rows: map[string]quota.Override{}}
}

func (f *fakeOverrideStore) Get(_ context.Context, alias string) (*quota.Override, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[alias]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeOverrideStore) Set(_ context.Context, alias string, mult float64, exp time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[alias] = quota.Override{
		Alias:      alias,
		Multiplier: mult,
		ExpiresAt:  exp,
		Reason:     reason,
		CreatedAt:  time.Now(),
	}
	return nil
}

func (f *fakeOverrideStore) Reset(_ context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, alias)
	return nil
}

func (f *fakeOverrideStore) List(_ context.Context) ([]quota.Override, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]quota.Override, 0, len(f.rows))
	for _, v := range f.rows {
		out = append(out, v)
	}
	return out, nil
}

func newSeamDispatcher(t *testing.T, opts dispatcher.QuotaSeam) *dispatcher.Dispatcher {
	t.Helper()
	tier1 := &fakeBackend{tier: providers.TierInHouse, name: "in-house"}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())
	d.SetQuotaSeam(opts)
	return d
}

func TestSetQuotaSeamWiresAccessors(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"internal-platform-x": 1.0})
	store := newFakeOverrideStore()
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: store,
		Wfq:           wfq,
	})
	if d.OverrideStore() != store {
		t.Errorf("OverrideStore() did not return the wired store")
	}
	if d.Wfq() != wfq {
		t.Errorf("Wfq() did not return the wired queue")
	}
}

func TestSetQuotaSeamUnwiredDispatcherReturnsNilAccessors(t *testing.T) {
	tier1 := &fakeBackend{tier: providers.TierInHouse, name: "in-house"}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())
	if d.OverrideStore() != nil {
		t.Errorf("OverrideStore() should be nil before SetQuotaSeam")
	}
	if d.Wfq() != nil {
		t.Errorf("Wfq() should be nil before SetQuotaSeam")
	}
}

func TestPreFlightCheckDefaultDelegatesToQuotaPreFlight(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"internal-platform-x": 1.0})
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
	})
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(doctrine.NameDefault),
		Used:       0,
		Cap:        10000,
		Wfq:        wfq,
	}
	dec, err := d.PreFlightCheck(context.Background(), "internal-platform-x", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlightCheck: %v", err)
	}
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true (zero usage, no override needed)")
	}
}

func TestPreFlightCheckUnwiredDispatcherUsesDefault(t *testing.T) {
	// A Dispatcher constructed via New() (no SetQuotaSeam) MUST still serve
	// PreFlightCheck via the package default — defensive fall-through so
	// callers that build the dispatcher before wiring the seam still get
	// correct behaviour. Verifies the d.preflight nil-guard branch.
	tier1 := &fakeBackend{tier: providers.TierInHouse, name: "in-house"}
	tier2 := &fakeBackend{tier: providers.TierOpenClaude, name: "openclaude"}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())

	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(doctrine.NameDefault),
		Used:       0,
		Cap:        10000,
		Wfq:        wfq,
	}
	dec, err := d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlightCheck: %v", err)
	}
	if !dec.Allowed {
		t.Errorf("Allowed = false; want true via default quota.PreFlight")
	}
}

func TestPreFlightCheckUsesInjectedFunc(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	called := false
	injected := func(_ context.Context, _ string, _ doctrine.Name, _ quota.PreFlightDeps) (quota.PreFlightDecision, error) {
		called = true
		return quota.PreFlightDecision{Allowed: false, Reason: "test-injected"}, nil
	}
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
		PreFlight:     injected,
	})
	dec, err := d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, quota.PreFlightDeps{Wfq: wfq})
	if err != nil {
		t.Fatalf("PreFlightCheck: %v", err)
	}
	if !called {
		t.Error("injected PreFlight not called")
	}
	if dec.Reason != "test-injected" {
		t.Errorf("Reason = %q, want test-injected", dec.Reason)
	}
	if dec.Allowed {
		t.Errorf("Allowed = true; want false (injected denied)")
	}
}

func TestPreFlightCheckPropagatesError(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	sentinel := errors.New("synthetic")
	injected := func(_ context.Context, _ string, _ doctrine.Name, _ quota.PreFlightDeps) (quota.PreFlightDecision, error) {
		return quota.PreFlightDecision{}, sentinel
	}
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
		PreFlight:     injected,
	})
	_, err := d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, quota.PreFlightDeps{Wfq: wfq})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}

func TestPreFlightCheckEmptyAliasErrors(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
	})
	_, err := d.PreFlightCheck(context.Background(), "", doctrine.NameDefault, quota.PreFlightDeps{Wfq: wfq})
	if err == nil {
		t.Error("PreFlightCheck with empty alias should error")
	}
}

func TestPreFlightCheckDoesNotMutateWfqDepth(t *testing.T) {
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
	})
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(doctrine.NameDefault),
		Cap:        100,
		Used:       50,
		Wfq:        wfq,
	}
	preDepth := wfq.Depth("a")
	_, _ = d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, deps)
	postDepth := wfq.Depth("a")
	if preDepth != postDepth {
		t.Errorf("PreFlightCheck mutated WFQ depth: %d → %d (must be decision-only)", preDepth, postDepth)
	}
}

// ----------------------------------------------------------------------------
// PreFlightCheck — runs without invoking ANY provider (compile-time +
// behavioural check). The dispatcher's tier1/tier2 backends MUST NOT be
// called by PreFlightCheck — that is the whole point of invariant's
// "pre-flight runs before any provider call" contract.
// ----------------------------------------------------------------------------

func TestPreFlightCheckNeverCallsProviders(t *testing.T) {
	tier1 := &fakeBackend{
		tier: providers.TierInHouse, name: "in-house",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("PreFlightCheck MUST NOT call tier1 (decision-only seam)")
			return nil, nil
		},
	}
	tier2 := &fakeBackend{
		tier: providers.TierOpenClaude, name: "openclaude",
		respFn: func(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
			t.Fatal("PreFlightCheck MUST NOT call tier2 (decision-only seam)")
			return nil, nil
		},
	}
	d := newTwoBackendDispatcher(tier1, tier2, &recordingEmitter{}, newStubBreaker())
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	d.SetQuotaSeam(dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
	})
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(doctrine.NameDefault),
		Cap:        100,
		Used:       0,
		Wfq:        wfq,
	}
	_, err := d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, deps)
	if err != nil {
		t.Fatalf("PreFlightCheck: %v", err)
	}
	if tier1.callCount() != 0 {
		t.Errorf("tier1 invocations = %d, want 0", tier1.callCount())
	}
	if tier2.callCount() != 0 {
		t.Errorf("tier2 invocations = %d, want 0", tier2.callCount())
	}
}

// ----------------------------------------------------------------------------
// PreFlightCheck — concurrent invocation MUST be safe. The seam holds no
// mutable state per call; the underlying quota.PreFlight is pure.
// ----------------------------------------------------------------------------

func TestPreFlightCheckConcurrentSafe(t *testing.T) {
	t.Parallel()
	const N = 64
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: newFakeOverrideStore(),
		Wfq:           wfq,
	})
	deps := quota.PreFlightDeps{
		Thresholds: quota.DoctrineDefaults(doctrine.NameDefault),
		Cap:        10000,
		Used:       0,
		Wfq:        wfq,
	}
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := d.PreFlightCheck(context.Background(), "a", doctrine.NameDefault, deps)
			if err != nil {
				t.Errorf("PreFlightCheck: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestSetQuotaSeamRewires(t *testing.T) {
	wfqA := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	wfqB := quota.NewWfqQueue(map[string]quota.Weight{"b": 1.0})
	storeA := newFakeOverrideStore()
	storeB := newFakeOverrideStore()
	d := newSeamDispatcher(t, dispatcher.QuotaSeam{
		OverrideStore: storeA,
		Wfq:           wfqA,
	})
	d.SetQuotaSeam(dispatcher.QuotaSeam{
		OverrideStore: storeB,
		Wfq:           wfqB,
	})
	if d.OverrideStore() != storeB {
		t.Errorf("OverrideStore() did not pick up the re-wired store")
	}
	if d.Wfq() != wfqB {
		t.Errorf("Wfq() did not pick up the re-wired queue")
	}
}

func TestPreFlightFuncSignatureParity(t *testing.T) {
	var _ dispatcher.PreFlightFunc = quota.PreFlight
}

// ----------------------------------------------------------------------------
// Tasks 5–6 cascade refactor verbatim. The QuotaSeam struct, SetQuotaSeam,
// PreFlightCheck, OverrideStore, Wfq, PreFlightFunc and the underlying
// atomic seam pointer are orthogonal to the cascade and MUST keep working
// unchanged on a cascade-shaped Dispatcher (master plan C6: "the
// quota seam is preserved verbatim"). This test pins that contract.
// ----------------------------------------------------------------------------

func TestQuotaSeam_SurvivesCascadeRefactor(t *testing.T) {
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{}}
	res := &fakeResolver{cascades: map[string][]string{}}
	d := dispatcher.New(reg, res, newRecordingEmitter(), newFakeBreaker())

	if d.OverrideStore() != nil {
		t.Error("OverrideStore() should be nil before SetQuotaSeam")
	}
	if d.Wfq() != nil {
		t.Error("Wfq() should be nil before SetQuotaSeam")
	}

	called := false
	d.SetQuotaSeam(dispatcher.QuotaSeam{
		PreFlight: func(ctx context.Context, alias string, dn doctrine.Name, deps quota.PreFlightDeps) (quota.PreFlightDecision, error) {
			called = true
			return quota.PreFlightDecision{Allowed: true}, nil
		},
	})
	dec, err := d.PreFlightCheck(context.Background(), "internal-platform-x", doctrine.NameMaxScope, quota.PreFlightDeps{})
	if err != nil {
		t.Fatalf("PreFlightCheck: unexpected error: %v", err)
	}
	if !called {
		t.Error("injected PreFlight delegate was not invoked")
	}
	if !dec.Allowed {
		t.Error("decision should propagate verbatim from the delegate")
	}

	called = false
	if _, err := d.PreFlightCheck(context.Background(), "", doctrine.NameMaxScope, quota.PreFlightDeps{}); err == nil {
		t.Error("PreFlightCheck with empty alias: want error, got nil")
	}
	if called {
		t.Error("delegate must NOT be invoked for empty alias — dispatcher's guard must short-circuit")
	}
}

func TestBreakerState_KeyedByName(t *testing.T) {
	var _ dispatcher.BreakerState = (*fakeBreaker)(nil)
	fb := newFakeBreaker()
	fb.deny["deepseek-direct"] = true
	if fb.Permit("deepseek-direct") {
		t.Error("Permit should honour the per-name deny set")
	}
	if !fb.Permit("gemini-flash") {
		t.Error("unrelated provider must stay permitted")
	}
}

type scriptedBackend struct {
	name  string
	tier  providers.Tier
	resp  *providers.TierResponse
	err   error
	calls atomic.Int64
}

func (b *scriptedBackend) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	b.calls.Add(1)
	if b.err != nil {
		return nil, b.err
	}
	return b.resp, nil
}
func (b *scriptedBackend) Probe(context.Context) error { return nil }
func (b *scriptedBackend) Close() error                { return nil }
func (b *scriptedBackend) Name() string                { return b.name }
func (b *scriptedBackend) Tier() providers.Tier        { return b.tier }
func (b *scriptedBackend) Capabilities() providers.TierCapabilities {
	return providers.TierCapabilities{}
}

func okResp(tier providers.Tier) *providers.TierResponse {
	return &providers.TierResponse{Status: 200, TierUsed: tier, InputTokens: 3, OutputTokens: 5}
}

func TestForward_FirstProviderSucceeds(t *testing.T) {
	p1 := &scriptedBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"deepseek-direct": p1, "siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	emt := newRecordingEmitter()
	d := dispatcher.New(reg, res, emt, newFakeBreaker())

	resp, err := d.Forward(context.Background(), providers.TierRequest{Profile: "worker-code", Project: "internal-platform-x"})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("resp.Status = %d, want 200", resp.Status)
	}
	if p1.calls.Load() != 1 {
		t.Errorf("provider 1 calls = %d, want 1", p1.calls.Load())
	}
	if p2.calls.Load() != 0 {
		t.Errorf("provider 2 calls = %d, want 0 (first provider succeeded)", p2.calls.Load())
	}
	if len(emt.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(emt.events))
	}
	if emt.events[0].Provider != "deepseek-direct" {
		t.Errorf("CostEvent.Provider = %q, want deepseek-direct", emt.events[0].Provider)
	}
}

func TestForward_FailoverToSecondProvider(t *testing.T) {
	p1 := &scriptedBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat, err: errors.New("transport boom")}
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"deepseek-direct": p1, "siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	emt := newRecordingEmitter()
	brk := newFakeBreaker()
	d := dispatcher.New(reg, res, emt, brk)

	resp, err := d.Forward(context.Background(), providers.TierRequest{Profile: "worker-code", Project: "p"})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("resp.Status = %d, want 200", resp.Status)
	}
	if p1.calls.Load() != 1 || p2.calls.Load() != 1 {
		t.Errorf("calls = (%d,%d), want (1,1)", p1.calls.Load(), p2.calls.Load())
	}
	if len(emt.events) != 2 {
		t.Fatalf("emitted %d events, want 2 (one failure + one success)", len(emt.events))
	}
	if emt.events[0].Err == "" {
		t.Error("first CostEvent should record the failure")
	}
	if emt.events[1].Provider != "siliconflow-deepseek" {
		t.Errorf("second CostEvent.Provider = %q, want siliconflow-deepseek", emt.events[1].Provider)
	}
	wantFail := []string{"deepseek-direct"}
	if len(brk.failures) != 1 || brk.failures[0] != wantFail[0] {
		t.Errorf("breaker failures = %v, want %v", brk.failures, wantFail)
	}
}

func TestForward_BreakerVetoSkipsProviderNoEmit(t *testing.T) {
	p1 := &scriptedBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"deepseek-direct": p1, "siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	emt := newRecordingEmitter()
	brk := newFakeBreaker()
	brk.deny["deepseek-direct"] = true
	d := dispatcher.New(reg, res, emt, brk)

	resp, err := d.Forward(context.Background(), providers.TierRequest{Profile: "worker-code", Project: "p"})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("resp.Status = %d, want 200", resp.Status)
	}
	if p1.calls.Load() != 0 {
		t.Errorf("vetoed provider 1 calls = %d, want 0", p1.calls.Load())
	}
	if len(emt.events) != 1 {
		t.Errorf("emitted %d events, want 1 (veto MUST NOT emit)", len(emt.events))
	}
}

func TestForward_CascadeExhaustedReturnsErrAllTiersUnavailable(t *testing.T) {
	p1 := &scriptedBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat, err: errors.New("boom1")}
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, err: errors.New("boom2")}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"deepseek-direct": p1, "siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	d := dispatcher.New(reg, res, newRecordingEmitter(), newFakeBreaker())

	_, err := d.Forward(context.Background(), providers.TierRequest{Profile: "worker-code", Project: "p"})
	if !errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
		t.Errorf("err = %v, want ErrAllTiersUnavailable", err)
	}
}

func TestForward_ResolverErrorIsReturned(t *testing.T) {
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{}}
	res := &fakeResolver{err: errors.New("unknown profile")}
	d := dispatcher.New(reg, res, newRecordingEmitter(), newFakeBreaker())
	_, err := d.Forward(context.Background(), providers.TierRequest{Profile: "nope", Project: "p"})
	if err == nil {
		t.Fatal("Forward with resolver error: want error, got nil")
	}
}

func TestForward_UnknownProviderInCascadeSkips(t *testing.T) {
	// Cascade names a provider absent from the registry: skip it, do NOT
	// emit a CostEvent (no call happened), continue to the next.
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	emt := newRecordingEmitter()
	d := dispatcher.New(reg, res, emt, newFakeBreaker())

	resp, err := d.Forward(context.Background(), providers.TierRequest{Profile: "worker-code", Project: "p"})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("resp.Status = %d, want 200", resp.Status)
	}
	if len(emt.events) != 1 {
		t.Errorf("emitted %d events, want 1 (missing provider MUST NOT emit)", len(emt.events))
	}
}

func TestForward_CtxCancelledMidCascadeStops(t *testing.T) {
	cancelCtx, cancel := context.WithCancel(context.Background())
	p1 := &scriptedBackend{name: "deepseek-direct", tier: providers.TierGenericOpenAICompat}
	p1.err = errors.New("fail then cancel")
	p2 := &scriptedBackend{name: "siliconflow-deepseek", tier: providers.TierGenericOpenAICompat, resp: okResp(providers.TierGenericOpenAICompat)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"deepseek-direct": p1, "siliconflow-deepseek": p2}}
	res := &fakeResolver{cascades: map[string][]string{"worker-code": {"deepseek-direct", "siliconflow-deepseek"}}}
	emt := newRecordingEmitter()
	d := dispatcher.New(reg, res, emt, newFakeBreaker())

	cancel()
	_, err := d.Forward(cancelCtx, providers.TierRequest{Profile: "worker-code", Project: "p"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if p2.calls.Load() != 0 {
		t.Errorf("provider 2 calls = %d, want 0 (ctx cancelled — no further attempts)", p2.calls.Load())
	}
}

// ----------------------------------------------------------------------------
// B-5 — dispatcher routes *providers.RateLimitedError → RecordRateLimited
// ----------------------------------------------------------------------------
//
// A backend returning *providers.RateLimitedError MUST cause
// breaker.RecordRateLimited (→ StateRateLimited), NOT RecordFailure
// (no Suspect/Open), and Forward MUST still fail over to the next provider.
//
// Uses the real *orchestrator.CircuitBreaker so the State assertion is
// meaningful (not just a counter check). *CircuitBreaker satisfies
// dispatcher.BreakerState after B-5 widens the interface.
// =============================================================================
// Plan v0.20.0 — ErrToolsUnsupported breaker-skip
//
// providers.ErrToolsUnsupported is returned by openai-compat / gemini /
// ollama backends when the canonical request carries a tools field. The
// dispatcher MUST treat it as a CAPABILITY-MISMATCH signal (cascade-skip
// to the next backend) — NOT a health degradation. The breaker.RecordFailure
// path is SKIPPED so a healthy backend stays healthy for non-tools traffic.
// Mirror the *RateLimitedError short-circuit at attempt() ~line 402-407.
// =============================================================================

func TestDispatcher_ToolsUnsupportedSkipsBreakerFailure(t *testing.T) {
	primary := &scriptedBackend{
		name: "deepseek-direct",
		tier: providers.TierGenericOpenAICompat,
		err:  providers.ErrToolsUnsupported,
	}
	fallback := &scriptedBackend{
		name: "anthropic-paygo",
		tier: providers.TierAnthropicPAYG,
		resp: okResp(providers.TierAnthropicPAYG),
	}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{
		"deepseek-direct": primary,
		"anthropic-paygo": fallback,
	}}
	res := &fakeResolver{cascades: map[string][]string{
		"tool-using": {"deepseek-direct", "anthropic-paygo"},
	}}
	br := newFakeBreaker()
	em := newRecordingEmitter()
	d := dispatcher.New(reg, res, em, br)

	resp, err := d.Forward(context.Background(), providers.TierRequest{
		Profile: "tool-using",
		Body:    []byte(`{"model":"x","max_tokens":1,"tools":[{"name":"t"}],"messages":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward must cascade to fallback; got error %v", err)
	}
	if resp == nil || resp.Status != 200 || resp.TierUsed != providers.TierAnthropicPAYG {
		t.Fatalf("expected fallback (anthropic-paygo) 200; got %+v", resp)
	}
	if primary.calls.Load() != 1 {
		t.Errorf("primary call count = %d, want 1", primary.calls.Load())
	}
	if fallback.calls.Load() != 1 {
		t.Errorf("fallback call count = %d, want 1", fallback.calls.Load())
	}
	// CRITICAL the breaker MUST NOT have RecordFailure called on the
	// primary — capability mismatch is not health degradation.
	br.mu.Lock()
	failures := append([]string{}, br.failures...)
	successes := append([]string{}, br.successes...)
	br.mu.Unlock()
	for _, f := range failures {
		if f == "deepseek-direct" {
			t.Errorf("breaker.RecordFailure was called on deepseek-direct (got %v); ErrToolsUnsupported must NOT degrade health", failures)
		}
	}

	if !br.Permit("deepseek-direct") {
		t.Error("breaker should still Permit deepseek-direct after ErrToolsUnsupported (no health degradation)")
	}
	// Fallback was a success → its name MUST appear in successes.
	foundFallbackSuccess := false
	for _, s := range successes {
		if s == "anthropic-paygo" {
			foundFallbackSuccess = true
			break
		}
	}
	if !foundFallbackSuccess {
		t.Errorf("fallback success not recorded; successes = %v", successes)
	}

	events := em.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 CostEvents (skip + success); got %d: %+v", len(events), events)
	}
	if events[0].Provider != "deepseek-direct" || events[0].Err == "" {
		t.Errorf("first event = %+v, want Provider=deepseek-direct + non-empty Err", events[0])
	}
	if !strings.Contains(events[0].Err, "tools field not yet supported") {
		t.Errorf("first event Err = %q, want it to reference ErrToolsUnsupported", events[0].Err)
	}
	if events[1].Provider != "anthropic-paygo" || events[1].Err != "" {
		t.Errorf("second event = %+v, want Provider=anthropic-paygo + empty Err", events[1])
	}
}

func TestDispatcher_ToolsUnsupportedAllBackendsCascadeExhausted(t *testing.T) {
	b1 := &scriptedBackend{name: "p1", tier: providers.TierGenericOpenAICompat, err: providers.ErrToolsUnsupported}
	b2 := &scriptedBackend{name: "p2", tier: providers.TierGemini, err: providers.ErrToolsUnsupported}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"p1": b1, "p2": b2}}
	res := &fakeResolver{cascades: map[string][]string{"tool-only-cascade": {"p1", "p2"}}}
	br := newFakeBreaker()
	em := newRecordingEmitter()
	d := dispatcher.New(reg, res, em, br)

	_, err := d.Forward(context.Background(), providers.TierRequest{Profile: "tool-only-cascade", Body: []byte("{}")})
	if !errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
		t.Fatalf("expected ErrAllTiersUnavailable when every backend returns ErrToolsUnsupported; got %v", err)
	}
	if b1.calls.Load() != 1 || b2.calls.Load() != 1 {
		t.Errorf("both backends must be attempted; calls = (%d, %d)", b1.calls.Load(), b2.calls.Load())
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	if len(br.failures) != 0 {
		t.Errorf("ErrToolsUnsupported must NOT record breaker failures; got %v", br.failures)
	}
}

func TestDispatcher_ToolsUnsupportedRealBreakerStaysClosed(t *testing.T) {
	br := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{FailureThreshold: 1})

	primary := &scriptedBackend{name: "p1", tier: providers.TierGenericOpenAICompat, err: providers.ErrToolsUnsupported}
	fallback := &scriptedBackend{name: "p2", tier: providers.TierAnthropicPAYG, resp: okResp(providers.TierAnthropicPAYG)}
	reg := &fakeRegistry{backends: map[string]providers.TierBackend{"p1": primary, "p2": fallback}}
	res := &fakeResolver{cascades: map[string][]string{"": {"p1", "p2"}}}
	d := dispatcher.New(reg, res, newRecordingEmitter(), br)

	_, err := d.Forward(context.Background(), providers.TierRequest{Body: []byte("{}")})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if got := br.State("p1"); got != orchestrator.StateClosed {
		t.Errorf("p1 state after ErrToolsUnsupported = %v, want StateClosed (capability mismatch != health)", got)
	}
}

func TestDispatcher_429RoutesToRateLimitedNotFailure(t *testing.T) {

	br := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{FailureThreshold: 1})

	var _ dispatcher.BreakerState = br

	primary := &scriptedBackend{
		name: "bypass",
		tier: providers.TierInHouse,
		err:  &providers.RateLimitedError{Provider: "bypass", RetryAfter: time.Second},
	}
	fallback := &scriptedBackend{
		name: "fallback",
		tier: providers.TierGenericOpenAICompat,
		resp: okResp(providers.TierGenericOpenAICompat),
	}

	reg := &fakeRegistry{backends: map[string]providers.TierBackend{
		"bypass":   primary,
		"fallback": fallback,
	}}
	res := &fakeResolver{cascades: map[string][]string{
		"": {"bypass", "fallback"},
	}}
	d := dispatcher.New(reg, res, newRecordingEmitter(), br)

	resp, err := d.Forward(context.Background(), providers.TierRequest{})
	if err != nil {
		t.Fatalf("Forward must fail over to fallback: %v", err)
	}
	if resp == nil || resp.Status != 200 {
		t.Errorf("expected fallback 200 response; got %+v", resp)
	}

	if got := br.State("bypass"); got != orchestrator.StateRateLimited {
		t.Fatalf("bypass state = %v, want StateRateLimited", got)
	}

	if got := br.State("fallback"); got != orchestrator.StateClosed {
		t.Fatalf("fallback state = %v, want StateClosed", got)
	}

	if got := br.State("bypass"); got == orchestrator.StateSuspect || got == orchestrator.StateOpen {
		t.Errorf("429 must not push bypass to Suspect/Open; got %v", got)
	}
}
