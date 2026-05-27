// go:build adversarial
package adversarial

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type countingDispatcher struct {
	count atomic.Int32
}

func (c *countingDispatcher) Forward(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
	c.count.Add(1)
	return &providers.TierResponse{}, nil
}

type noopAnchor struct{}

func (noopAnchor) Emit(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "evt-anchor", nil
}

func TestAdversarial_SingleEgress_AllRequestsReachDispatcher(t *testing.T) {
	disp := &countingDispatcher{}
	handler := transport.NewMessagesHandler(disp, noopAnchor{})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	const n = 50
	for i := 0; i < n; i++ {
		body, _ := json.Marshal(map[string]any{
			"model": "anthropic/claude-opus-4-7",
			"messages": []map[string]any{
				{"role": "user", "content": "ping"},
			},
		})
		req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %d: %v", i, err)
		}
		resp.Body.Close()
	}

	got := int(disp.count.Load())
	if got != n {
		t.Errorf("dispatcher saw %d Forward calls, want %d (single-egress bypass detected)", got, n)
	}
}

func TestAdversarial_SingleEgress_NilDispatcherPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewMessagesHandler(nil, ...) did not panic")
		}
	}()
	_ = transport.NewMessagesHandler(nil, noopAnchor{})
}

func TestAdversarial_SingleEgress_NilAnchorTolerated(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewMessagesHandler(disp, nil) panicked unexpectedly: %v", r)
		}
	}()
	h := transport.NewMessagesHandler(&countingDispatcher{}, nil)
	if h == nil {
		t.Error("NewMessagesHandler returned nil")
	}
}
