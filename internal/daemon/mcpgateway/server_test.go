package mcpgateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func newTestDispatcher(t *testing.T) *mcpgateway.Dispatcher {
	t.Helper()
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{
			Content:   []mcpgateway.CallContentItem{{Type: "text", Text: "ok:" + req.Tool.Tool()}},
			Subsystem: "audit",
		}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
		entryFor(t, "audit", "query", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	return d
}

func TestServerInitialize(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", resp["jsonrpc"])
	}
	if _, ok := resp["result"]; !ok {
		t.Errorf("missing result; body = %s", w.Body.String())
	}
}

func TestServerToolsListReturnsRegistry(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Errorf("tools len = %d, want 2; body = %s", len(resp.Result.Tools), w.Body.String())
	}
	wantNames := map[string]bool{
		"mcp_zen-swarm_audit_emit":  true,
		"mcp_zen-swarm_audit_query": true,
	}
	for _, tn := range resp.Result.Tools {
		if !wantNames[tn.Name] {
			t.Errorf("unexpected tool name %q", tn.Name)
		}
	}
}

func TestServerToolsListWithNilInputSchema(t *testing.T) {

	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	tn := mcpgateway.MustToolName("audit", "emit")
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{{
		Name: tn, Handler: h, Meta: mcpgateway.ToolMeta{
			Description: "nil schema test",
			InputSchema: nil,
		},
	}}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Result.Tools) != 1 {
		t.Fatalf("tools len = %d", len(resp.Result.Tools))
	}
	if resp.Result.Tools[0].InputSchema["type"] != "object" {
		t.Errorf("inputSchema fallback missing type=object: %v", resp.Result.Tools[0].InputSchema)
	}
}

func TestServerToolsCallSuccess(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 3,
		"method": "tools/call",
		"params": {
			"name": "mcp_zen-swarm_audit_emit",
			"arguments": {"type": "test", "payload": {}}
		}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Result.IsError {
		t.Errorf("isError = true; want false")
	}
	if len(resp.Result.Content) == 0 {
		t.Fatal("content empty")
	}
	if !strings.Contains(resp.Result.Content[0].Text, "emit") {
		t.Errorf("text = %q; expected to contain tool name", resp.Result.Content[0].Text)
	}
}

func TestServerToolsCallUnknownReturnsRPCError(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_ghost", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("error null; expected JSON-RPC error envelope")
	}
}

func TestServerInvalidJSONReturns400(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServerNonPostReturns405(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestServerUnknownMethodReturnsRPCError(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"2.0","id":5,"method":"nonsense","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("body = %s; expected error envelope", w.Body.String())
	}
}

func TestServerWrongJSONRPCVersionRejected(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"1.0","id":5,"method":"tools/list","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32600`) {
		t.Errorf("body = %s; expected -32600 invalid request", w.Body.String())
	}
}

func TestServerMissingMethodRejected(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"2.0","id":5,"params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32600`) {
		t.Errorf("body = %s; expected -32600 invalid request", w.Body.String())
	}
}

func TestServerToolsCallMissingNameReturnsRPCError(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("body = %s; expected error envelope", w.Body.String())
	}
}

func TestServerToolsCallMalformedToolName(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	body := []byte(`{
		"jsonrpc":"2.0","id":7,
		"method":"tools/call",
		"params":{"name":"not_canonical","arguments":{}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params", w.Body.String())
	}
}

func TestServerToolsCallMalformedParams(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))

	body := []byte(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":42}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params", w.Body.String())
	}
}

func TestServerHeadersForwardedToCallRequest(t *testing.T) {
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 7,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Doctrine", "capa-firewall")
	r.Header.Set("X-Zen-Mode", "afk")
	r.Header.Set("X-Zen-Session-ID", "s-1")
	r.Header.Set("X-Zen-Project-ID", "internal-platform-x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got := <-captured
	if got.Doctrine != mcpgateway.DoctrineCapaFirewall {
		t.Errorf("Doctrine = %q want capa-firewall", got.Doctrine)
	}
	if got.Mode != mcpgateway.ModeAFK {
		t.Errorf("Mode = %v want AFK", got.Mode)
	}
	if got.SessionID != "s-1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectID = %q", got.ProjectID)
	}
}

func TestServerHeaderModeAutonomyAndInteractive(t *testing.T) {

	cases := []struct {
		header string
		want   mcpgateway.Mode
	}{
		{"interactive", mcpgateway.ModeInteractive},
		{"autonomy", mcpgateway.ModeAutonomy},
		{"afk", mcpgateway.ModeAFK},
		{"", mcpgateway.ModeUnspecified},
	}
	for _, c := range cases {
		captured := make(chan mcpgateway.CallRequest, 1)
		h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
			captured <- req
			return mcpgateway.CallResponse{Subsystem: "audit"}, nil
		}
		sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
			entryFor(t, "audit", "emit", h),
		}}
		d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
			Audit: mcpgateway.NopAuditEmitter(),
		})
		if err := d.RegisterSubsystem(sub); err != nil {
			t.Fatalf("RegisterSubsystem: %v", err)
		}
		srv := mcpgateway.NewServer(d)
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{}}}`)
		r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
		if c.header != "" {
			r.Header.Set("X-Zen-Mode", c.header)
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		got := <-captured
		if got.Mode != c.want {
			t.Errorf("header=%q got Mode=%v want %v", c.header, got.Mode, c.want)
		}
	}
}

func TestServerHeaderInvalidDoctrineRejected(t *testing.T) {
	tn := mcpgateway.MustToolName("audit", "emit")
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		t.Error("handler must not be reached when doctrine header is invalid")
		return mcpgateway.CallResponse{}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		{Name: tn, Handler: h, Meta: mcpgateway.ToolMeta{Description: "test"}},
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Doctrine", "max_scope")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "max_scope") {
		t.Errorf("body = %s; expected offending header value 'max_scope' in error", w.Body.String())
	}
}

func TestServerHeaderEmptyDoctrineAccepted(t *testing.T) {
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	got := <-captured
	if got.Doctrine != "" {
		t.Errorf("empty header should yield empty Doctrine; got %q", got.Doctrine)
	}
	if got.Doctrine.Resolved() != mcpgateway.DoctrineDefault {
		t.Errorf("Resolved() = %q, want default", got.Doctrine.Resolved())
	}
}

func TestServerHeaderInvalidModeRejected(t *testing.T) {
	tn := mcpgateway.MustToolName("audit", "emit")
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		t.Error("handler must not be reached when mode header is invalid")
		return mcpgateway.CallResponse{}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		{Name: tn, Handler: h, Meta: mcpgateway.ToolMeta{Description: "test"}},
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Mode", "autonmy")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "autonmy") {
		t.Errorf("body = %s; expected offending header value 'autonmy' in error", w.Body.String())
	}
}

func TestServerNewServerNilDispatcherPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = mcpgateway.NewServer(nil)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic read error")
}

func TestServerReadBodyError(t *testing.T) {
	srv := mcpgateway.NewServer(newTestDispatcher(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", errReader{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "read body") {
		t.Errorf("body = %s; expected 'read body' message", w.Body.String())
	}
}

type erroringHandlerSubsystem struct {
	tn  mcpgateway.ToolName
	err error
}

func (e *erroringHandlerSubsystem) Name() string { return e.tn.Subsystem() }
func (e *erroringHandlerSubsystem) Tools() []mcpgateway.ToolEntry {
	return []mcpgateway.ToolEntry{{
		Name: e.tn,
		Handler: func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
			return mcpgateway.CallResponse{}, e.err
		},
		Meta: mcpgateway.ToolMeta{Description: "test"},
	}}
}

func TestServerToolsCallMapsErrToolNotRegisteredToMethodNotFound(t *testing.T) {

	tn := mcpgateway.MustToolName("caronte", "unknownop")
	sub := &erroringHandlerSubsystem{
		tn:  tn,
		err: mcpgateway.ErrToolNotRegistered,
	}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"mcp_zen-swarm_caronte_unknownop","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32601`) {
		t.Errorf("body = %s; expected -32601 method not found", w.Body.String())
	}
}

func TestServerToolsCallUnknownToolNameYieldsMethodNotFound(t *testing.T) {

	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_ghost","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32601`) {
		t.Errorf("body = %s; expected -32601 method-not-found for unknown tool", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"code":-32000`) {
		t.Errorf("body = %s; must NOT map to -32000 (pre-flight registry check fix)", w.Body.String())
	}
}

type fakeResolver struct {
	mu        sync.Mutex
	calls     []string
	mapTo     map[string]string
	hardErr   error
	resolveID string
}

func (f *fakeResolver) Resolve(_ context.Context, idOrAlias string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, idOrAlias)
	if f.hardErr != nil {
		return "", f.hardErr
	}
	if f.mapTo != nil {
		if got, ok := f.mapTo[idOrAlias]; ok {
			return got, nil
		}
		return "", mcpgateway.ErrAliasNotFound
	}
	if f.resolveID == "" {
		return idOrAlias, nil
	}
	return f.resolveID, nil
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeResolver) lastCall() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1]
}

func newToolsCallServerWithResolver(t *testing.T, r mcpgateway.ProjectsAliasResolver) (*mcpgateway.Server, chan mcpgateway.CallRequest) {
	t.Helper()
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	srv.SetAliasResolver(r)
	return srv, captured
}

func TestServerToolsCallHeaderPrecedence(t *testing.T) {
	resolver := &fakeResolver{mapTo: map[string]string{
		"header-alias": "canonical-from-header",
		"body-alias":   "canonical-from-body",
	}}
	srv, captured := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 200,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {"project_id": "body-alias"}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Project-ID", "header-alias")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got := <-captured
	if got.ProjectID != "canonical-from-header" {
		t.Errorf("ProjectID = %q, want canonical-from-header (header takes precedence over body args)", got.ProjectID)
	}
	if resolver.lastCall() != "header-alias" {
		t.Errorf("resolver was invoked with %q; want header-alias", resolver.lastCall())
	}
}

func TestServerToolsCallBodyFallback(t *testing.T) {
	resolver := &fakeResolver{mapTo: map[string]string{
		"body-alias": "canonical-from-body",
	}}
	srv, captured := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 201,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {"project_id": "body-alias"}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got := <-captured
	if got.ProjectID != "canonical-from-body" {
		t.Errorf("ProjectID = %q, want canonical-from-body (body args fallback)", got.ProjectID)
	}
	if resolver.lastCall() != "body-alias" {
		t.Errorf("resolver was invoked with %q; want body-alias", resolver.lastCall())
	}
}

func TestServerToolsCallMissingProjectIDReturnsError(t *testing.T) {
	resolver := &fakeResolver{}
	srv, _ := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 202,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params for missing project_id", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "project_id required") {
		t.Errorf("body = %s; expected 'project_id required' error message", w.Body.String())
	}
	if resolver.callCount() != 0 {
		t.Errorf("resolver was invoked %d times; expected 0 (request rejected before resolution)", resolver.callCount())
	}
}

func TestServerToolsCallAliasResolved(t *testing.T) {
	const canonical = "3572a35b596db245956622561aa35f3a0000000000000000000000000000abcd"
	resolver := &fakeResolver{mapTo: map[string]string{
		"zen-swarm-3572a35b": canonical,
	}}
	srv, captured := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 203,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Project-ID", "zen-swarm-3572a35b")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got := <-captured
	if got.ProjectID != canonical {
		t.Errorf("ProjectID = %q, want canonical id_sha256 %q (alias → canonical resolution)", got.ProjectID, canonical)
	}
}

// TestServerToolsCallAliasNotFoundReturnsError — when the resolver
// returns ErrAliasNotFound, the wire layer maps to JSON-RPC -32000
// (server error) with operator-readable detail citing the unresolved id.
// The downstream handler MUST NOT be invoked.
func TestServerToolsCallAliasNotFoundReturnsError(t *testing.T) {
	resolver := &fakeResolver{}
	resolver.mapTo = map[string]string{}
	srv, captured := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 204,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Project-ID", "ghost-project")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32000`) {
		t.Errorf("body = %s; expected -32000 server error for unresolved alias", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ghost-project") {
		t.Errorf("body = %s; expected unresolved alias in error message for operator triage", w.Body.String())
	}
	// Downstream handler MUST NOT have been invoked.
	select {
	case req := <-captured:
		t.Errorf("downstream handler was invoked with %+v; expected resolver-error short-circuit", req)
	default:

	}
}

func TestServerToolsCallResolverFailureReturnsError(t *testing.T) {
	resolver := &fakeResolver{hardErr: errors.New("synthetic DB failure")}
	srv, _ := newToolsCallServerWithResolver(t, resolver)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 205,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Project-ID", "any-id")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32000`) {
		t.Errorf("body = %s; expected -32000 server error for infra failure", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "resolution failed") {
		t.Errorf("body = %s; expected 'resolution failed' marker (distinguishes from alias-not-found)", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "not found in projects_alias") {
		t.Errorf("body = %s; resolver failure must NOT alias to 'not found' message", w.Body.String())
	}
}

// TestServerSetAliasResolverNilFallback — when no resolver is wired (a
// daemon-construction bug or a test path that skips SetAliasResolver),
// the server MUST NOT crash on tools/call. A nil resolver means
// "ingress runs in legacy mode": the raw header value is forwarded
// AS-IS to ProjectID (matching pre-v0.20.0 behaviour). This preserves
// daemon-boot recovery on a partially-wired build.
func TestServerSetAliasResolverNilFallback(t *testing.T) {
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{
		"jsonrpc": "2.0",
		"id": 206,
		"method": "tools/call",
		"params": {"name": "mcp_zen-swarm_audit_emit", "arguments": {}}
	}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Project-ID", "raw-id-passthrough")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got := <-captured
	if got.ProjectID != "raw-id-passthrough" {
		t.Errorf("ProjectID = %q, want raw-id-passthrough (legacy header pass-through when resolver nil)", got.ProjectID)
	}
}

func TestServerAliasResolverAccessor(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	srv := mcpgateway.NewServer(d)
	if srv.AliasResolver() != nil {
		t.Errorf("AliasResolver() = %v; want nil when never set", srv.AliasResolver())
	}
	resolver := &fakeResolver{}
	srv.SetAliasResolver(resolver)
	if srv.AliasResolver() != resolver {
		t.Errorf("AliasResolver() = %v; want %v (post-SetAliasResolver round-trip)", srv.AliasResolver(), resolver)
	}
}

func TestServerToolsCallRBACDeniedYieldsServerError(t *testing.T) {
	tn := mcpgateway.MustToolName("caronte", "query")
	sub := &erroringHandlerSubsystem{
		tn:  tn,
		err: nil,
	}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
		RBACCfg: mcpgateway.RBACConfig{
			DoctrineDisabled: map[mcpgateway.Doctrine][]string{
				mcpgateway.DoctrineCapaFirewall: {tn.String()},
			},
		},
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	srv := mcpgateway.NewServer(d)
	body := []byte(`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"mcp_zen-swarm_caronte_query","arguments":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	r.Header.Set("X-Zen-Doctrine", "capa-firewall")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"code":-32000`) {
		t.Errorf("body = %s; expected -32000 server error for RBAC deny", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"code":-32601`) {
		t.Errorf("body = %s; RBAC deny must NOT alias method-not-found", w.Body.String())
	}
}
