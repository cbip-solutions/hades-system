// SPDX-License-Identifier: MIT
package boundary_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/hermes/boundary"
)

type fakeHermesCli struct {
	cap                      boundary.Capabilities
	sendErr                  error
	sendResp                 boundary.CompletionResponse
	statusRegistered         int
	sessionStartRegistered   int
	preToolRegistered        int
	inlinePromptCalled       int
	envelopeCalls            int
	registerStatusReturn     error
	registerSessionReturn    error
	registerPreToolReturn    error
	renderInlinePromptReturn error
	renderInlinePromptOutput boundary.InlinePromptResponse
	envelopeOutputOverride   *boundary.MCPEnvelope
}

func (f *fakeHermesCli) SendCompletion(_ context.Context, _ boundary.CompletionRequest) (boundary.CompletionResponse, error) {
	if f.sendErr != nil {
		return boundary.CompletionResponse{}, f.sendErr
	}
	return f.sendResp, nil
}

func (f *fakeHermesCli) RegisterStatusProvider(_ boundary.StatusProvider) error {
	f.statusRegistered++
	return f.registerStatusReturn
}

func (f *fakeHermesCli) OnSessionStart(_ boundary.SessionStartHandler) error {
	f.sessionStartRegistered++
	return f.registerSessionReturn
}

func (f *fakeHermesCli) OnPreToolCall(_ boundary.PreToolCallHandler) error {
	f.preToolRegistered++
	return f.registerPreToolReturn
}

func (f *fakeHermesCli) RenderInlinePrompt(_ context.Context, _ boundary.InlinePrompt) (boundary.InlinePromptResponse, error) {
	f.inlinePromptCalled++
	return f.renderInlinePromptOutput, f.renderInlinePromptReturn
}

func (f *fakeHermesCli) WrapMCPEnvelope(payload boundary.MCPPayload) boundary.MCPEnvelope {
	f.envelopeCalls++
	if f.envelopeOutputOverride != nil {
		return *f.envelopeOutputOverride
	}
	return boundary.WrapMCPEnvelope(payload)
}

func (f *fakeHermesCli) Capabilities() boundary.Capabilities { return f.cap }

func TestSurfaceInterfaceShape(t *testing.T) {
	t.Parallel()
	var _ boundary.Surface = (*boundary.Adapter)(nil)
}

func TestNewAdapterPanicsOnNilCli(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil cli")
		}
	}()
	_ = boundary.NewAdapter(nil)
}

func TestAdapterCapabilitiesSnapshot(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{cap: boundary.Capabilities{
		StatusProvider:   true,
		SessionStartHook: false,
		InlinePrompt:     true,
	}}
	a := boundary.NewAdapter(cli)
	snap := a.CapabilitiesSnapshot()
	if !snap.StatusProvider || snap.SessionStartHook || !snap.InlinePrompt {
		t.Errorf("snapshot mismatch: %+v", snap)
	}
}

func TestAdapterSendCompletionHappyPath(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{sendResp: boundary.CompletionResponse{
		Text:      "hello",
		ModelUsed: "test-model",
		Status:    200,
	}}
	a := boundary.NewAdapter(cli)
	resp, err := a.SendCompletion(context.Background(), boundary.CompletionRequest{Model: "test", Prompt: "hi"})
	if err != nil {
		t.Fatalf("SendCompletion: %v", err)
	}
	if resp.Text != "hello" || resp.Status != 200 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestAdapterSendCompletionErrorPropagated(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("dispatcher down")
	cli := &fakeHermesCli{sendErr: wantErr}
	a := boundary.NewAdapter(cli)
	_, err := a.SendCompletion(context.Background(), boundary.CompletionRequest{Model: "test"})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected errors.Is(err, wantErr); got %v", err)
	}
}

func TestAdapterFeatureDetectGracefulDegrade(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{cap: boundary.Capabilities{}}
	a := boundary.NewAdapter(cli)

	if a.IsStatusProviderSupported() {
		t.Error("IsStatusProviderSupported should be false (G2 absent)")
	}
	if a.IsSessionStartHookSupported() {
		t.Error("IsSessionStartHookSupported should be false (G5 inert)")
	}
	if a.IsInlinePromptSupported() {
		t.Error("IsInlinePromptSupported should be false (G3 absent)")
	}

	if err := a.RegisterStatusProvider(func() string { return "" }); err != boundary.ErrCapabilityUnavailable {
		t.Errorf("RegisterStatusProvider expected ErrCapabilityUnavailable; got %v", err)
	}
	if err := a.OnSessionStart(func(context.Context, boundary.SessionInfo) {}); err != boundary.ErrCapabilityUnavailable {
		t.Errorf("OnSessionStart expected ErrCapabilityUnavailable; got %v", err)
	}
	_, err := a.RenderInlinePrompt(context.Background(), boundary.InlinePrompt{Question: "q"})
	if err != boundary.ErrCapabilityUnavailable {
		t.Errorf("RenderInlinePrompt expected ErrCapabilityUnavailable; got %v", err)
	}

	// Capability-gated cli methods MUST NOT be called when feature absent.
	if cli.statusRegistered != 0 {
		t.Errorf("cli.RegisterStatusProvider should NOT be called when G2 absent; got %d calls", cli.statusRegistered)
	}
	if cli.sessionStartRegistered != 0 {
		t.Errorf("cli.OnSessionStart should NOT be called when G5 absent; got %d calls", cli.sessionStartRegistered)
	}
	if cli.inlinePromptCalled != 0 {
		t.Errorf("cli.RenderInlinePrompt should NOT be called when G3 absent; got %d calls", cli.inlinePromptCalled)
	}
}

func TestAdapterFeatureDetectAvailable(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{cap: boundary.Capabilities{
		StatusProvider:   true,
		SessionStartHook: true,
		InlinePrompt:     true,
	}}
	a := boundary.NewAdapter(cli)

	if !a.IsStatusProviderSupported() {
		t.Error("IsStatusProviderSupported should be true")
	}
	if !a.IsSessionStartHookSupported() {
		t.Error("IsSessionStartHookSupported should be true")
	}
	if !a.IsInlinePromptSupported() {
		t.Error("IsInlinePromptSupported should be true")
	}

	if err := a.RegisterStatusProvider(func() string { return "" }); err != nil {
		t.Errorf("RegisterStatusProvider with G2 present: %v", err)
	}
	if err := a.OnSessionStart(func(context.Context, boundary.SessionInfo) {}); err != nil {
		t.Errorf("OnSessionStart with G5 present: %v", err)
	}
	_, err := a.RenderInlinePrompt(context.Background(), boundary.InlinePrompt{Question: "q"})
	if err != nil {
		t.Errorf("RenderInlinePrompt with G3 present: %v", err)
	}

	if cli.statusRegistered != 1 {
		t.Errorf("expected 1 cli.RegisterStatusProvider call; got %d", cli.statusRegistered)
	}
	if cli.sessionStartRegistered != 1 {
		t.Errorf("expected 1 cli.OnSessionStart call; got %d", cli.sessionStartRegistered)
	}
	if cli.inlinePromptCalled != 1 {
		t.Errorf("expected 1 cli.RenderInlinePrompt call; got %d", cli.inlinePromptCalled)
	}
}

func TestAdapterRejectNilProvider(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{cap: boundary.Capabilities{StatusProvider: true, SessionStartHook: true}}
	a := boundary.NewAdapter(cli)

	if err := a.RegisterStatusProvider(nil); err == nil {
		t.Error("expected error on nil provider")
	}
	if err := a.OnSessionStart(nil); err == nil {
		t.Error("expected error on nil handler")
	}
	if err := a.OnPreToolCall(nil); err == nil {
		t.Error("expected error on nil pre-tool handler")
	}
}

func TestAdapterOnPreToolCallAlwaysSupported(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{cap: boundary.Capabilities{}}
	a := boundary.NewAdapter(cli)
	if err := a.OnPreToolCall(func(context.Context, boundary.ToolCall) {}); err != nil {
		t.Errorf("OnPreToolCall should always be supported; got %v", err)
	}
	if cli.preToolRegistered != 1 {
		t.Errorf("expected 1 cli.OnPreToolCall call; got %d", cli.preToolRegistered)
	}
}

func TestAdapterWrapMCPEnvelope(t *testing.T) {
	t.Parallel()
	cli := &fakeHermesCli{}
	a := boundary.NewAdapter(cli)
	env := a.WrapMCPEnvelope(boundary.MCPPayload{Method: "tools/call"})
	if env.Version != boundary.MCPProtocolVersion {
		t.Errorf("envelope version %q; want %q", env.Version, boundary.MCPProtocolVersion)
	}
	if env.Payload.Method != "tools/call" {
		t.Errorf("envelope payload.Method %q; want tools/call", env.Payload.Method)
	}
	if cli.envelopeCalls != 1 {
		t.Errorf("expected 1 cli.WrapMCPEnvelope call; got %d", cli.envelopeCalls)
	}
}

func TestErrCapabilityUnavailableSentinel(t *testing.T) {
	t.Parallel()
	wrapped := errors.Join(errors.New("outer"), boundary.ErrCapabilityUnavailable)
	if !errors.Is(wrapped, boundary.ErrCapabilityUnavailable) {
		t.Error("wrapped error should match ErrCapabilityUnavailable via errors.Is")
	}
}
