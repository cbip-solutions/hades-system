package orchestrator_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeDispatcher struct {
	mu      sync.Mutex
	calls   int
	lastReq providers.TierRequest
	lastCtx context.Context
	respFn  func(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error)
	respPtr *providers.TierResponse
	respErr error
}

func (f *fakeDispatcher) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	f.lastCtx = ctx
	respFn := f.respFn
	respPtr := f.respPtr
	respErr := f.respErr
	f.mu.Unlock()

	if respFn != nil {
		return respFn(ctx, req)
	}
	if respErr != nil {
		return nil, respErr
	}
	if respPtr != nil {
		return respPtr, nil
	}
	return &providers.TierResponse{TierUsed: providers.TierInHouse, Status: 200}, nil
}

func (f *fakeDispatcher) snapshot() (int, providers.TierRequest, context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastReq, f.lastCtx
}

func TestForwarderInterfaceSatisfiedByDispatcher(t *testing.T) {
	t.Parallel()
	var _ orchestrator.Forwarder = (*dispatcher.Dispatcher)(nil)
}

func TestForwardInjectsExplicitProfile(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	resp, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "my-proj",
		SessionID: "sess-9",
		Profile:   "audit-reviewer",
		Body:      []byte(`{"model":"claude-sonnet-4-6"}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response")
	}
	if resp.TierUsed != providers.TierInHouse {
		t.Errorf("TierUsed = %v, want TierInHouse", resp.TierUsed)
	}
	_, _, ctx := fd.snapshot()
	if got := dispatcher.HeadersFromContext(ctx)["X-Zen-Profile"]; got != "audit-reviewer" {
		t.Errorf("X-Zen-Profile = %q, want %q", got, "audit-reviewer")
	}
}

func TestForwardUsesDefaultProfile(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	if _, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "my-proj",
		SessionID: "sess-9",

		Body: []byte(`{}`),
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, _, ctx := fd.snapshot()
	if got := dispatcher.HeadersFromContext(ctx)["X-Zen-Profile"]; got != "default-profile" {
		t.Errorf("X-Zen-Profile = %q, want %q", got, "default-profile")
	}
}

func TestForwardEmptyProfileAndEmptyDefault(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "")

	if _, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "p",
		SessionID: "s",
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, ctx := fd.snapshot()
	if _, present := dispatcher.HeadersFromContext(ctx)["X-Zen-Profile"]; present {
		t.Error("X-Zen-Profile must be omitted when both Call.Profile and defaultProfile are empty")
	}
	if _, present := req.Headers["X-Zen-Profile"]; present {
		t.Error("TierRequest.Headers must not carry an empty X-Zen-Profile")
	}
}

func TestForwardEmptyProjectAndSessionOmitted(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	if _, err := o.Forward(context.Background(), orchestrator.Call{

		Profile: "p",
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if _, present := req.Headers["X-Zen-Project"]; present {
		t.Error("X-Zen-Project must be omitted when Call.Project is empty")
	}
	if _, present := req.Headers["X-Zen-Session"]; present {
		t.Error("X-Zen-Session must be omitted when Call.SessionID is empty")
	}
	if got := req.Headers["X-Zen-Profile"]; got != "p" {
		t.Errorf("X-Zen-Profile = %q, want %q", got, "p")
	}
}

func TestForwardHeadersContainAllThreeWhenAllPopulated(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	if _, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj-x",
		SessionID: "sess-x",
		Profile:   "profile-x",
		Body:      []byte(`{}`),
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	want := map[string]string{
		"X-Zen-Project": "proj-x",
		"X-Zen-Session": "sess-x",
		"X-Zen-Profile": "profile-x",
	}
	for k, v := range want {
		if got := req.Headers[k]; got != v {
			t.Errorf("Headers[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestForwardBodyPassThrough(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)

	if _, err := o.Forward(context.Background(), orchestrator.Call{
		Project: "proj", SessionID: "sess", Profile: "explicit",
		Body: body,
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if !bytes.Equal(req.Body, body) {
		t.Errorf("Body pass-through broken:\n got=%q\nwant=%q", req.Body, body)
	}
}

func TestForwardErrorPropagation(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("dispatcher: synthetic failure for test")
	fd := &fakeDispatcher{respErr: sentinel}
	o := orchestrator.New(fd, "p")

	resp, err := o.Forward(context.Background(), orchestrator.Call{Profile: "x"})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v (errors.Is)", err, sentinel)
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil on error", resp)
	}
}

func TestForwardResponsePointerIdentity(t *testing.T) {
	t.Parallel()
	want := &providers.TierResponse{
		TierUsed:     providers.TierAnthropicPAYG,
		Status:       200,
		InputTokens:  42,
		OutputTokens: 7,
	}
	fd := &fakeDispatcher{respPtr: want}
	o := orchestrator.New(fd, "p")

	got, err := o.Forward(context.Background(), orchestrator.Call{Profile: "x"})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if got != want {
		t.Errorf("response pointer identity lost: got %p, want %p", got, want)
	}
}

func TestNewPanicsOnNilForwarder(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("orchestrator.New(nil, ...) did not panic")
		}
	}()
	_ = orchestrator.New(nil, "default")
}

func TestForwardConcurrent(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := o.Forward(context.Background(), orchestrator.Call{
				Project:   "p",
				SessionID: "s",
				Profile:   "x",
				Body:      []byte(`{}`),
			})
			if err != nil {
				t.Errorf("goroutine %d: Forward err = %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	calls, _, _ := fd.snapshot()
	if calls != n {
		t.Errorf("call count = %d, want %d", calls, n)
	}
}

func TestForwardContextPropagatedToForwarder(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	if _, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "P",
		SessionID: "S",
		Profile:   "PR",
	}); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, _, ctx := fd.snapshot()
	h := dispatcher.HeadersFromContext(ctx)
	if h["X-Zen-Project"] != "P" || h["X-Zen-Session"] != "S" || h["X-Zen-Profile"] != "PR" {
		t.Errorf("ctx headers = %v, want P/S/PR", h)
	}
}

func TestForwardPopulatesTypedRoutingFields(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "default-profile")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj-x",
		SessionID: "sess-x",
		Profile:   "profile-x",
		Body:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.Project != "proj-x" {
		t.Errorf("req.Project = %q, want %q", req.Project, "proj-x")
	}
	if req.SessionID != "sess-x" {
		t.Errorf("req.SessionID = %q, want %q", req.SessionID, "sess-x")
	}
	if req.Profile != "profile-x" {
		t.Errorf("req.Profile = %q, want %q", req.Profile, "profile-x")
	}
}

func TestForwardTypedProfileResolvesDefault(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "resolved-default")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "p",
		SessionID: "s",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.Profile != "resolved-default" {
		t.Errorf("req.Profile = %q, want %q (resolved default)", req.Profile, "resolved-default")
	}
}

func TestForwardPopulatesModelField(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj",
		SessionID: "sess",
		Profile:   "x",
		Model:     "claude-sonnet-4-6",
		Body:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.Model != "claude-sonnet-4-6" {
		t.Errorf("req.Model = %q, want %q", req.Model, "claude-sonnet-4-6")
	}
}

// TestForwardPropagatesNilResponseNilError — orchestrator passes (nil, nil)
// through unchanged. Production *dispatcher.Dispatcher rejects this at the
// backend boundary, so this path is unreachable in production wiring, but
// the orchestrator MUST NOT synthesise an error or response of its own.
func TestForwardPropagatesNilResponseNilError(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{respFn: func(context.Context, providers.TierRequest) (*providers.TierResponse, error) {
		return nil, nil
	}}
	o := orchestrator.New(fd, "p")
	resp, err := o.Forward(context.Background(), orchestrator.Call{Profile: "x"})
	if resp != nil || err != nil {
		t.Errorf("Forward = (%v, %v), want (nil, nil)", resp, err)
	}
}

func TestForwardBodyNilPassThrough(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")
	_, _ = o.Forward(context.Background(), orchestrator.Call{Profile: "x", Body: nil})
	_, req, _ := fd.snapshot()
	if req.Body != nil {
		t.Errorf("Body nil-pass-through broken: got %v, want nil", req.Body)
	}
}

func TestForwardPopulatesHTTPMethodAndPath(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj",
		SessionID: "sess",
		Profile:   "x",
		Method:    "POST",
		Path:      "/v1/messages",
		Body:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.Method != "POST" {
		t.Errorf("req.Method = %q, want %q", req.Method, "POST")
	}
	if req.Path != "/v1/messages" {
		t.Errorf("req.Path = %q, want %q", req.Path, "/v1/messages")
	}
}

// TestForwardPopulatesIdempotencyKey — Call.IdempotencyKey is propagated to
// req.IdempotencyKey verbatim. Plan 2's dedup contract (inv-zen-058) MUST
// survive the orchestrator hop end-to-end; B-8 is the wiring that proves it.
func TestForwardPopulatesIdempotencyKey(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:        "proj",
		SessionID:      "sess",
		Profile:        "x",
		IdempotencyKey: "client-idem-12345",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.IdempotencyKey != "client-idem-12345" {
		t.Errorf("req.IdempotencyKey = %q, want %q", req.IdempotencyKey, "client-idem-12345")
	}
}

// TestForwardPopulatesConversationID — Call.ConversationID groups turns in the
// canonical store (Phase E). MUST flow through to TierRequest.ConversationID
// so backends and the audit pipeline see the same correlation key.
func TestForwardPopulatesConversationID(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:        "proj",
		SessionID:      "sess",
		Profile:        "x",
		ConversationID: "conv_abc",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if req.ConversationID != "conv_abc" {
		t.Errorf("req.ConversationID = %q, want %q", req.ConversationID, "conv_abc")
	}
}

func TestForwardMergesCallerHeadersWithContextHeaders(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj",
		SessionID: "sess",
		Profile:   "x",
		Headers: map[string]string{
			"Anthropic-Beta":  "messages-2024-12-15",
			"User-Agent":      "zen-swarm/0.2.0",
			"X-Custom-Client": "abc",
		},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()

	if got := req.Headers["Anthropic-Beta"]; got != "messages-2024-12-15" {
		t.Errorf("Anthropic-Beta = %q, want %q", got, "messages-2024-12-15")
	}
	if got := req.Headers["User-Agent"]; got != "zen-swarm/0.2.0" {
		t.Errorf("User-Agent = %q, want %q", got, "zen-swarm/0.2.0")
	}
	if got := req.Headers["X-Custom-Client"]; got != "abc" {
		t.Errorf("X-Custom-Client = %q, want %q", got, "abc")
	}

	if got := req.Headers["X-Zen-Project"]; got != "proj" {
		t.Errorf("X-Zen-Project = %q, want %q", got, "proj")
	}
	if got := req.Headers["X-Zen-Session"]; got != "sess" {
		t.Errorf("X-Zen-Session = %q, want %q", got, "sess")
	}
	if got := req.Headers["X-Zen-Profile"]; got != "x" {
		t.Errorf("X-Zen-Profile = %q, want %q", got, "x")
	}
}

func TestForwardCallerExplicitXZenWinsOnConflict(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "ctx-proj",
		SessionID: "ctx-sess",
		Profile:   "ctx-prof",
		Headers: map[string]string{
			"X-Zen-Project": "explicit-proj",
		},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if got := req.Headers["X-Zen-Project"]; got != "explicit-proj" {
		t.Errorf("explicit X-Zen-Project must win: got %q, want %q", got, "explicit-proj")
	}

	if got := req.Headers["X-Zen-Session"]; got != "ctx-sess" {
		t.Errorf("X-Zen-Session = %q, want %q", got, "ctx-sess")
	}
}

func TestForwardNilCallerHeadersOK(t *testing.T) {
	t.Parallel()
	fd := &fakeDispatcher{}
	o := orchestrator.New(fd, "p")

	_, err := o.Forward(context.Background(), orchestrator.Call{
		Project:   "proj",
		SessionID: "sess",
		Profile:   "x",
		Headers:   nil,
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	_, req, _ := fd.snapshot()
	if got := req.Headers["X-Zen-Project"]; got != "proj" {
		t.Errorf("X-Zen-Project = %q, want %q", got, "proj")
	}
	if len(req.Headers) != 3 {
		t.Errorf("req.Headers len = %d, want 3 (only X-Zen-Project/Session/Profile)", len(req.Headers))
	}
}
