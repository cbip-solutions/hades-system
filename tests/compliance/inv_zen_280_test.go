// tests/compliance/inv_zen_280_test.go
//
// Compliance gate for inv-zen-280 (Plan v0.20.0 Phase A): mcpgateway
// HTTP ingress accepts project_id from BOTH the X-Zen-Project-ID header
// AND the body arguments.project_id. The header takes precedence; the
// body args is the fallback for clients that cannot set HTTP headers
// (e.g. Hermes' /mcp/ ingress when the upstream client cannot intercept
// headers).
//
// Three anchors per phase plan §Task A-5:
//
//  1. source-regex 1: `internal/daemon/mcpgateway/server.go` contains
//     the `params.Arguments["project_id"]` literal that performs the
//     body-args extraction.
//  2. source-regex 2: the precedence pattern
//     `rawProjectID == "" && params.Arguments != nil` is present —
//     this is the load-bearing branch that consults body args only when
//     the header is absent (NOT a different order, which would
//     accidentally override the header).
//  3. behavioural test: an in-process httptest.Server with the gateway
//     dispatches three requests (header-only / body-only / both); the
//     CallRequest.ProjectID delivered to the downstream handler MUST
//     match the per-test expectation.
//
// Sister-test pattern (feedback_sister_test_pattern): bite-check is to
// revert server.go to header-only ingress; this test MUST fail.
//
// inv-zen-280 (Plan v0.20.0 Phase A Task A-3 + A-5).
package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func readSourceForInv276277(t *testing.T, relPath string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInvZen280SourceRegex_BodyArgsExtraction(t *testing.T) {
	src := readSourceForInv276277(t, "internal/daemon/mcpgateway/server.go")
	const needle = `params.Arguments["project_id"]`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-280 violated: internal/daemon/mcpgateway/server.go missing %q literal; body-args fallback path absent", needle)
	}
}

func TestInvZen280SourceRegex_PrecedencePattern(t *testing.T) {
	src := readSourceForInv276277(t, "internal/daemon/mcpgateway/server.go")
	const needle = `rawProjectID == "" && params.Arguments != nil`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-280 violated: server.go missing precedence guard %q; body-args fallback may run before header check", needle)
	}
}

type invZen276Resolver struct {
	called string
	mapTo  string
}

func (r *invZen276Resolver) Resolve(_ context.Context, idOrAlias string) (string, error) {
	r.called = idOrAlias
	return r.mapTo, nil
}

func invZen276EntryFor(t *testing.T, sub, tool string, h mcpgateway.Handler) mcpgateway.ToolEntry {
	t.Helper()
	tn := mcpgateway.MustToolName(sub, tool)
	return mcpgateway.ToolEntry{
		Name:    tn,
		Handler: h,
		Meta:    mcpgateway.ToolMeta{Description: "inv-zen-280 behavioural test entry"},
	}
}

type invZen276Subsystem struct {
	name  string
	tools []mcpgateway.ToolEntry
}

func (f *invZen276Subsystem) Name() string                    { return f.name }
func (f *invZen276Subsystem) Tools() []mcpgateway.ToolEntry   { return f.tools }
func (f *invZen276Subsystem) Close() error                    { return nil }
func (f *invZen276Subsystem) Subsystem() mcpgateway.Subsystem { return f }

func TestInvZen280Behavioural_HeaderPrecedence(t *testing.T) {
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &invZen276Subsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		invZen276EntryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: mcpgateway.NopAuditEmitter()})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	resolver := &invZen276Resolver{mapTo: "canonical-header"}
	srv := mcpgateway.NewServer(d)
	srv.SetAliasResolver(resolver)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{"project_id":"body-value"}}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
	req.Header.Set("X-Zen-Project-ID", "header-value")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	got := <-captured
	if got.ProjectID != "canonical-header" {
		t.Errorf("CallRequest.ProjectID = %q; want canonical-header (header wins over body)", got.ProjectID)
	}
	if resolver.called != "header-value" {
		t.Errorf("resolver received %q; want header-value (header precedence)", resolver.called)
	}
}

func TestInvZen280Behavioural_BodyFallback(t *testing.T) {
	captured := make(chan mcpgateway.CallRequest, 1)
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		captured <- req
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &invZen276Subsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		invZen276EntryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: mcpgateway.NopAuditEmitter()})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	resolver := &invZen276Resolver{mapTo: "canonical-body"}
	srv := mcpgateway.NewServer(d)
	srv.SetAliasResolver(resolver)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{"project_id":"body-only-value"}}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	got := <-captured
	if got.ProjectID != "canonical-body" {
		t.Errorf("CallRequest.ProjectID = %q; want canonical-body (body args fallback)", got.ProjectID)
	}
	if resolver.called != "body-only-value" {
		t.Errorf("resolver received %q; want body-only-value (body args fallback path)", resolver.called)
	}
}

// TestInvZen280Behavioural_MissingBoth — anchor 3c: neither header nor
// body args carries a project_id; the daemon returns JSON-RPC -32602
// (invalid params) with the operator-readable "project_id required"
// message. The downstream handler MUST NOT be invoked.
func TestInvZen280Behavioural_MissingBoth(t *testing.T) {
	called := false
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called = true
		return mcpgateway.CallResponse{}, nil
	}
	sub := &invZen276Subsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		invZen276EntryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: mcpgateway.NopAuditEmitter()})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	resolver := &invZen276Resolver{}
	srv := mcpgateway.NewServer(d)
	srv.SetAliasResolver(resolver)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{}}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Error == nil {
		t.Fatal("missing-both case returned no error envelope; expected -32602")
	}
	if envelope.Error.Code != -32602 {
		t.Errorf("error.code = %d; want -32602 for missing project_id", envelope.Error.Code)
	}
	if !strings.Contains(envelope.Error.Message, "project_id required") {
		t.Errorf("error.message = %q; expected to contain 'project_id required'", envelope.Error.Message)
	}
	if called {
		t.Error("downstream handler was invoked; expected short-circuit on missing project_id")
	}
}
