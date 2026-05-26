package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeDispatcher struct {
	lastReq providers.TierRequest
	resp    *providers.TierResponse
	err     error
	calls   int
}

func (f *fakeDispatcher) Forward(_ context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	f.lastReq = req
	f.calls++
	return f.resp, f.err
}

func TestZenSwarmTransportForwardSuccess(t *testing.T) {
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{
			Status:       200,
			Body:         []byte(`{"id":"msg_TEST","content":[{"type":"text","text":"hi"}]}`),
			TierUsed:     providers.TierInHouse,
			ModelUsed:    "claude-sonnet-4-6",
			InputTokens:  10,
			OutputTokens: 5,
		},
	}
	zt := transport.NewZenSwarmTransport(disp, nil)

	req := providers.TierRequest{
		Method: "POST",
		Path:   "/v1/messages",
		Body:   []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`),
		Model:  "claude-sonnet-4-6",
	}
	resp, err := zt.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response with nil error (contract violation)")
	}
	if resp.Status != 200 {
		t.Errorf("Status = %d, want 200", resp.Status)
	}
	if disp.calls != 1 {
		t.Errorf("dispatcher.Forward called %d times, want 1", disp.calls)
	}
	if string(disp.lastReq.Body) != string(req.Body) {
		t.Errorf("body forwarded = %q, want %q", string(disp.lastReq.Body), string(req.Body))
	}
}

func TestZenSwarmTransportForwardDispatcherError(t *testing.T) {
	wantErr := errors.New("upstream-fail")
	disp := &fakeDispatcher{err: wantErr}
	zt := transport.NewZenSwarmTransport(disp, nil)

	_, err := zt.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want wrapping of %v", err, wantErr)
	}
}

func TestZenSwarmTransportNilDispatcherPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewZenSwarmTransport(nil, _) must panic")
		}
	}()
	_ = transport.NewZenSwarmTransport(nil, nil)
}

func TestZenSwarmTransportName(t *testing.T) {
	zt := transport.NewZenSwarmTransport(&fakeDispatcher{}, nil)
	if got := zt.Name(); got != "zenswarm-transport" {
		t.Errorf("Name() = %q, want %q", got, "zenswarm-transport")
	}
}

func TestZenSwarmTransportTier(t *testing.T) {
	zt := transport.NewZenSwarmTransport(&fakeDispatcher{}, nil)
	if got := zt.Tier(); got != providers.TierInHouse {
		t.Errorf("Tier() = %v, want TierInHouse (advertised primary)", got)
	}
}

func TestZenSwarmTransportProbeAlwaysNil(t *testing.T) {
	disp := &fakeDispatcher{err: errors.New("dispatcher-down")}
	zt := transport.NewZenSwarmTransport(disp, nil)
	if err := zt.Probe(context.Background()); err != nil {
		t.Errorf("Probe must return nil (pass-through); got %v", err)
	}
	if disp.calls != 0 {
		t.Errorf("Probe must NOT call dispatcher; got %d calls", disp.calls)
	}
}

func TestZenSwarmTransportCloseAlwaysNil(t *testing.T) {
	zt := transport.NewZenSwarmTransport(&fakeDispatcher{}, nil)
	if err := zt.Close(); err != nil {
		t.Errorf("Close must return nil; got %v", err)
	}
}

func TestZenSwarmTransportCapabilities(t *testing.T) {
	zt := transport.NewZenSwarmTransport(&fakeDispatcher{}, nil)
	caps := zt.Capabilities()
	if caps.SupportsStreaming {
		t.Error("Phase B Capabilities.SupportsStreaming must be false (Phase C flips)")
	}
	if !caps.SupportsToolUse {
		t.Error("Capabilities.SupportsToolUse must be true")
	}
	if !caps.SupportsVision {
		t.Error("Capabilities.SupportsVision must be true")
	}
	if !caps.SupportsPromptCaching {
		t.Error("Capabilities.SupportsPromptCaching must be true")
	}
	if caps.MaxContextTokens <= 0 {
		t.Errorf("Capabilities.MaxContextTokens = %d, want > 0", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens <= 0 {
		t.Errorf("Capabilities.MaxOutputTokens = %d, want > 0", caps.MaxOutputTokens)
	}
}

func TestZenSwarmTransportSatisfiesTierBackend(t *testing.T) {

	var _ providers.TierBackend = (*transport.ZenSwarmTransport)(nil)
	zt := transport.NewZenSwarmTransport(&fakeDispatcher{}, nil)
	var b providers.TierBackend = zt
	if b == nil {
		t.Fatal("ZenSwarmTransport must satisfy providers.TierBackend")
	}
}
