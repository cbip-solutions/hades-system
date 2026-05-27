// SPDX-License-Identifier: MIT
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

func TestServerOperatorGateHTTPPersistsThroughGateAdapter(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	srv := New(st, Config{})
	persist := workforceadapter.NewGateAdapter(st)
	operatorGate, err := gate.NewOperatorGate(ctx, persist)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	srv.SetOperatorGate(operatorGate)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/workforce/gate/pause",
		strings.NewReader(`{"mode":"paused_quiet","reason":"budget cap reached"}`),
	)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var paused map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &paused); err != nil {
		t.Fatalf("decode pause: %v", err)
	}
	if paused["state"] != "paused_quiet" {
		t.Fatalf("pause state = %v, want paused_quiet", paused["state"])
	}

	reloaded, err := gate.NewOperatorGate(ctx, workforceadapter.NewGateAdapter(st))
	if err != nil {
		t.Fatalf("reload OperatorGate: %v", err)
	}
	if got := reloaded.State(); got != gate.StatePausedQuiet {
		t.Fatalf("persisted state = %s, want %s", got, gate.StatePausedQuiet)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/resume", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body=%s", rec.Code, rec.Body.String())
	}
	reloaded, err = gate.NewOperatorGate(ctx, workforceadapter.NewGateAdapter(st))
	if err != nil {
		t.Fatalf("reload after resume: %v", err)
	}
	if got := reloaded.State(); got != gate.StateRunning {
		t.Fatalf("persisted state after resume = %s, want %s", got, gate.StateRunning)
	}
}
