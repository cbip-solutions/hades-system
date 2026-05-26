package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func makePhaseHFake(t *testing.T, capStatusJSON string) (*client.Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if capStatusJSON == "" {
			capStatusJSON = `{"axis":"x","value":"y","allowed":true,"remaining_usd":1.0,"blocked_scope":""}`
		}
		_, _ = w.Write([]byte(capStatusJSON))
	})
	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/v1/audit/emit", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "research",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c, srv
}

func newFakeBudgetClient(c *client.Client) *client.BudgetClient {
	return client.NewBudgetClient(c)
}

func newFakeEmitClient(c *client.Client, bufDir string) *client.EmitClient {
	return client.NewEmitClient(c, bufDir)
}

func TestBudgetAdapterNilClientAllows(t *testing.T) {
	a := NewBudgetAdapter(nil)
	allowed, blocked, err := a.PreCall(context.Background(), "x", "y", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("nil client should allow")
	}
	if blocked != "" {
		t.Errorf("blocked = %q", blocked)
	}
}

func TestBudgetAdapterNilRecord(t *testing.T) {
	a := NewBudgetAdapter(nil)
	if err := a.Record(context.Background(), "x", map[string]string{"a": "b"}); err != nil {
		t.Errorf("nil client Record returned error: %v", err)
	}
}

func TestAuditAdapterNilClientNoOp(t *testing.T) {
	a := NewAuditAdapter(nil)
	if err := a.Emit(context.Background(), "type", []byte("payload")); err != nil {
		t.Errorf("nil client Emit returned error: %v", err)
	}
}

func TestBudgetAdapterRoundTripUsesPhaseHClient(t *testing.T) {

	t.Log("BudgetAdapter wrapping behaviour exercised via nil-client paths above; full integration is in the daemon e2e suite (Plan 4 Phase L).")
}

func TestBudgetAdapterPreCallReal(t *testing.T) {
	hc, srv := makePhaseHFake(t,
		`{"axis":"x","value":"y","allowed":true,"remaining_usd":1.0,"blocked_scope":""}`)
	defer srv.Close()
	a := NewBudgetAdapter(newFakeBudgetClient(hc))
	allowed, blocked, err := a.PreCall(context.Background(), "x", "y", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("expected allowed")
	}
	if blocked != "" {
		t.Errorf("blocked = %q", blocked)
	}
}

func TestBudgetAdapterRecordReal(t *testing.T) {
	hc, srv := makePhaseHFake(t, "")
	defer srv.Close()
	a := NewBudgetAdapter(newFakeBudgetClient(hc))
	if err := a.Record(context.Background(), "cost-1",
		map[string]string{"project": "internal-platform-x", "stage": "design"}); err != nil {
		t.Errorf("Record: %v", err)
	}
}

func TestAuditAdapterEmitReal(t *testing.T) {
	hc, srv := makePhaseHFake(t, "")
	defer srv.Close()
	a := NewAuditAdapter(newFakeEmitClient(hc, t.TempDir()))
	if err := a.Emit(context.Background(), "test.event", []byte("payload")); err != nil {
		t.Errorf("Emit: %v", err)
	}
}
