package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func TestUnavailableRelayFailsLoud(t *testing.T) {
	r := worker.NewUnavailableRelay()
	got, err := r.Dispatch(context.Background(), "research_dispatch", json.RawMessage(`{"q":"x"}`))
	if err == nil {
		t.Fatal("expected error from unavailableRelay.Dispatch")
	}
	if got != nil {
		t.Errorf("unavailableRelay returned payload %s; want nil", got)
	}
	if !errors.Is(err, worker.ErrToolNotAvailable) {
		t.Errorf("err = %v, want errors.Is(ErrToolNotAvailable)", err)
	}
	if !strings.Contains(err.Error(), "research_dispatch") {
		t.Errorf("err message = %q, want substring 'research_dispatch'", err)
	}
}

func TestToolRelayFuncAdapter(t *testing.T) {
	called := false
	var gotName string
	var gotIn json.RawMessage
	var r worker.ToolRelay = worker.ToolRelayFunc(func(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
		called = true
		gotName = name
		gotIn = input
		return json.RawMessage(`{"ok":true}`), nil
	})
	out, err := r.Dispatch(context.Background(), "ssh_exec", json.RawMessage(`{"host":"vps"}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !called {
		t.Fatal("ToolRelayFunc was not called")
	}
	if gotName != "ssh_exec" {
		t.Errorf("gotName = %q", gotName)
	}
	if string(gotIn) != `{"host":"vps"}` {
		t.Errorf("gotIn = %q", gotIn)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %q", out)
	}
}
