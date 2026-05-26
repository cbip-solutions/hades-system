package budget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewServerRegistersAllTools(t *testing.T) {
	srv := NewServer(nil)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	want := []string{
		"rollup",
		"cap_status",
		"tag",
		"anomaly_check",
		"pause",
		"resume",
		"events",
	}

	got := srv.ToolNames()
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}

	for _, name := range want {
		if !gotSet[name] {
			t.Errorf("tool %q not registered in server", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("tool count: got %d, want %d; registered: %v", len(got), len(want), got)
	}
}

func TestNewServerNilClientDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewServer(nil) panicked: %v", r)
		}
	}()
	_ = NewServer(nil)
}

func TestBudgetAxisEnumIncludesAugmentation(t *testing.T) {
	found := false
	for _, a := range budgetAxisEnum {
		if a == "augmentation" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("budgetAxisEnum=%v missing \"augmentation\" (Plan 11 Phase C 5th axis)", budgetAxisEnum)
	}

	if len(budgetAxisEnum) != 6 {
		t.Errorf("budgetAxisEnum len=%d; want 6 (4 required + operation + augmentation)", len(budgetAxisEnum))
	}
}

func TestServerHasRunMethod(t *testing.T) {
	srv := NewServer(nil)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	var _ interface{ Run() error } = srv
}

func TestAssertStdioCanonical(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AssertStdioCanonical panicked: %v", r)
		}
	}()
	AssertStdioCanonical()
}

func TestAssertBoundaryPreserved(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AssertBoundaryPreserved panicked: %v", r)
		}
	}()
	AssertBoundaryPreserved()
}

func TestRunReturnsOnStdinClose(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	srv := NewServer(nil)
	done := make(chan error, 1)
	go func() {
		done <- srv.Run()
	}()

	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}

	select {
	case <-done:

	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after stdin EOF — possible hang regression")
	}
}

func serveOverInMemory(t *testing.T, srv *Server, clientName string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.sdk.Run(ctx, serverTransport)
	}()

	cli := mcp.NewClient(&mcp.Implementation{Name: clientName, Version: "0"}, nil)
	session, err := cli.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		<-serveErr
		t.Fatalf("client.Connect: %v", err)
	}

	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		<-serveErr
	})
	return ctx, session
}

func TestServeOverInMemoryTransportWithNilClient(t *testing.T) {
	srv := NewServer(nil)
	ctx, session := serveOverInMemory(t, srv, "test-budget")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "cap_status",
		Arguments: map[string]any{"axis": "stage", "value": "design"},
	})

	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error result from nil-client handler, got success")
	}
}

func TestServeOverInMemoryTransportCallTool(t *testing.T) {

	fakeDaemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/budget/cap_status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"remaining_usd":5.0,"allowed":true,"blocked_scope":""}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(fakeDaemon.Close)

	srv := newTestServer(t, fakeDaemon.URL)
	ctx, session := serveOverInMemory(t, srv, "test-budget-2")

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 7 {
		t.Errorf("ListTools: got %d tools, want 7", len(tools.Tools))
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "cap_status",
		Arguments: map[string]any{"axis": "stage", "value": "design"},
	})
	if err != nil {
		t.Fatalf("CallTool cap_status: %v", err)
	}
	if res == nil {
		t.Fatal("CallTool: nil result")
	}
	if res.IsError {
		t.Errorf("CallTool: unexpected error result: %v", res.Content)
	}
}

func TestJSONStringHelper(t *testing.T) {
	r := RollupResponse{TotalUSD: 4.2, Breakdown: map[string]float64{"design": 1.1}}
	s := jsonString(r)
	if s == "" {
		t.Fatal("jsonString returned empty string")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Errorf("jsonString produced invalid JSON: %v", err)
	}
}

func TestJSONStringHelperUnmarshalable(t *testing.T) {
	ch := make(chan int)
	s := jsonString(ch)

	if s == "" {
		t.Fatal("jsonString returned empty string for unmarshalable type")
	}
}

func TestToolSchemas_HaveProperties(t *testing.T) {
	srv := NewServer(nil)
	ctx, session := serveOverInMemory(t, srv, "test-schema-check")

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	wantRequired := map[string][]string{
		"rollup":        {"axis", "value"},
		"cap_status":    {"axis", "value"},
		"tag":           {"cost_id"},
		"anomaly_check": {"scope"},
		"pause":         {"scope", "reason"},
		"resume":        {"scope"},
		"events":        nil,
	}

	if len(tools.Tools) != len(wantRequired) {
		t.Fatalf("tool count = %d, want %d", len(tools.Tools), len(wantRequired))
	}

	for _, tool := range tools.Tools {
		want, ok := wantRequired[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q registered", tool.Name)
			continue
		}

		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("%s: marshal InputSchema: %v", tool.Name, err)
			continue
		}
		var sch struct {
			Type       string                    `json:"type"`
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		}
		if err := json.Unmarshal(raw, &sch); err != nil {
			t.Errorf("%s: unmarshal InputSchema: %v (raw=%s)", tool.Name, err, raw)
			continue
		}

		if sch.Type != "object" {
			t.Errorf("%s: schema.type = %q, want \"object\"", tool.Name, sch.Type)
		}
		if len(sch.Properties) == 0 {
			t.Errorf("%s: schema.properties is empty — must declare concrete fields (review I-5)", tool.Name)
		}

		for fieldName, prop := range sch.Properties {
			if desc, _ := prop["description"].(string); desc == "" {
				t.Errorf("%s.%s: missing description on property", tool.Name, fieldName)
			}
			if ptype, _ := prop["type"].(string); ptype == "" {
				t.Errorf("%s.%s: missing type on property", tool.Name, fieldName)
			}
		}

		gotRequired := append([]string(nil), sch.Required...)
		sort.Strings(gotRequired)
		wantSorted := append([]string(nil), want...)
		sort.Strings(wantSorted)
		if !equalStringSlice(gotRequired, wantSorted) {
			t.Errorf("%s: required = %v, want %v", tool.Name, gotRequired, wantSorted)
		}
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBindToMCPServer_PanicsOnMissingHandler verifies the defensive panic
// in bindToMCPServer (review NIT M-1). A toolSpec without a handler is a
// programmer error — registerHandlers and toolSpecs MUST stay in lock-step.
// This is a structural test that drains the handlers map mid-construction
// and re-runs bindToMCPServer to confirm the panic fires (vs silently
// dropping the registration).
func TestBindToMCPServer_PanicsOnMissingHandler(t *testing.T) {
	srv := NewServer(nil)

	srv.handlers = map[string]toolHandler{}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("bindToMCPServer: expected panic for missing handler, got none")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "tool spec without handler") {
			t.Errorf("panic message %q lacks expected 'tool spec without handler' phrase", msg)
		}
	}()

	dummy := mcp.NewServer(&mcp.Implementation{Name: "panic-test", Version: "0"}, nil)
	srv.bindToMCPServer(dummy)
}
