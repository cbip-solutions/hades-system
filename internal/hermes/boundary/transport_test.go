// SPDX-License-Identifier: MIT
package boundary_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/hermes/boundary"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type recordingDispatcher struct {
	resp    *providers.TierResponse
	err     error
	callCnt int
	lastReq providers.TierRequest
}

func (d *recordingDispatcher) Forward(_ context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	d.callCnt++
	d.lastReq = req
	if d.err != nil {
		return nil, d.err
	}
	return d.resp, nil
}

func TestNewHermesCliFromZenSwarmTransportPanicsOnNil(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil ZenSwarmTransport")
		}
	}()
	_ = boundary.NewHermesCliFromZenSwarmTransport(nil, boundary.HermesV0_13_2)
}

func TestZenSwarmHermesCliSendCompletionRoutesThroughTransport(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`hello`)},
	}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	resp, err := cli.SendCompletion(context.Background(), boundary.CompletionRequest{
		Model:  "claude-test",
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("SendCompletion: %v", err)
	}
	if resp.Status != 200 || resp.Text != "hello" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if disp.callCnt != 1 {
		t.Errorf("dispatcher.Forward called %d times; want 1 (single-egress preserved)", disp.callCnt)
	}
	if string(disp.lastReq.Body) != "hi" {
		t.Errorf("dispatcher saw body %q; want hi", string(disp.lastReq.Body))
	}
}

func TestZenSwarmHermesCliSendCompletionRequiresModel(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{resp: &providers.TierResponse{Status: 200}}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	_, err := cli.SendCompletion(context.Background(), boundary.CompletionRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on missing Model")
	}
	if disp.callCnt != 0 {
		t.Errorf("transport should NOT be called with invalid input; got %d call(s)", disp.callCnt)
	}
}

func TestZenSwarmHermesCliSendCompletionPropagatesTransportError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("dispatcher offline")
	disp := &recordingDispatcher{err: wantErr}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	_, err := cli.SendCompletion(context.Background(), boundary.CompletionRequest{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected errors.Is(err, wantErr); got %v", err)
	}
}

func TestZenSwarmHermesCliCapabilitiesMatchPin(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	cap := cli.Capabilities()
	if cap.StatusProvider || cap.SessionStartHook || cap.InlinePrompt {
		t.Errorf("v0.13.2 capabilities should all be false; got %+v", cap)
	}
}

func TestZenSwarmHermesCliCapabilityGatedReturnSentinel(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	if err := cli.RegisterStatusProvider(func() string { return "" }); err != boundary.ErrCapabilityUnavailable {
		t.Errorf("RegisterStatusProvider: got %v; want ErrCapabilityUnavailable", err)
	}
	if err := cli.OnSessionStart(func(context.Context, boundary.SessionInfo) {}); err != boundary.ErrCapabilityUnavailable {
		t.Errorf("OnSessionStart: got %v; want ErrCapabilityUnavailable", err)
	}
	_, err := cli.RenderInlinePrompt(context.Background(), boundary.InlinePrompt{})
	if err != boundary.ErrCapabilityUnavailable {
		t.Errorf("RenderInlinePrompt: got %v; want ErrCapabilityUnavailable", err)
	}
}

func TestZenSwarmHermesCliOnPreToolCallAccepts(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	handler := func(context.Context, boundary.ToolCall) {}
	if err := cli.OnPreToolCall(handler); err != nil {
		t.Errorf("OnPreToolCall: %v", err)
	}
	if err := cli.OnPreToolCall(handler); err != nil {
		t.Errorf("second OnPreToolCall: %v", err)
	}

	type preToolAccessor interface {
		PreToolHooks() []boundary.PreToolCallHandler
	}
	acc, ok := cli.(preToolAccessor)
	if !ok {
		t.Fatal("zenSwarmHermesCli should expose PreToolHooks() accessor for fan-out")
	}
	hooks := acc.PreToolHooks()
	if len(hooks) != 2 {
		t.Errorf("PreToolHooks() len = %d; want 2", len(hooks))
	}
}

func TestZenSwarmHermesCliWrapMCPEnvelope(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	env := cli.WrapMCPEnvelope(boundary.MCPPayload{Method: "tools/call"})
	if env.Version != boundary.MCPProtocolVersion {
		t.Errorf("envelope version %q; want %q", env.Version, boundary.MCPProtocolVersion)
	}
	if env.Payload.Method != "tools/call" {
		t.Errorf("envelope payload.Method %q; want tools/call", env.Payload.Method)
	}
}

func TestZenSwarmHermesCliNilResponseFromTransport(t *testing.T) {
	t.Parallel()
	disp := &recordingDispatcher{resp: nil, err: nil}
	zt := transport.NewZenSwarmTransport(disp, nil)
	cli := boundary.NewHermesCliFromZenSwarmTransport(zt, boundary.HermesV0_13_2)

	_, err := cli.SendCompletion(context.Background(), boundary.CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("expected error on nil-with-nil-err transport response")
	}
}
