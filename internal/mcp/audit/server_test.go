package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func fakeDispatcherForServer(verdictJSON, model string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":    "msg_server_test",
			"type":  "message",
			"role":  "assistant",
			"model": model,
			"content": []map[string]interface{}{
				{"type": "text", "text": verdictJSON},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 60},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
}

func TestServerAuditReviewToolHappyPath(t *testing.T) {
	verdictJSON := `{"classification":"clean","concerns":[],"suggestions":[]}`
	dSrv := fakeDispatcherForServer(verdictJSON, "gemini-2.6-pro")
	defer dSrv.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        dSrv.URL,
		AuthToken:            "test-token",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek", "local-qwen"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp, err := srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+func main() {}",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Fatalf("handleAuditReview: %v", err)
	}
	if resp.Verdict.Classification != ClassificationClean {
		t.Errorf("Classification = %q, want clean", resp.Verdict.Classification)
	}
	if resp.CriteriaUsed != "default" {
		t.Errorf("CriteriaUsed = %q, want default", resp.CriteriaUsed)
	}
	if resp.GeneratorFamily != "anthropic" {
		t.Errorf("GeneratorFamily = %q, want anthropic", resp.GeneratorFamily)
	}
	if resp.Verdict.ReviewerProvider == "anthropic" {
		t.Error("ReviewerProvider must not equal GeneratorFamily (inv-zen-080 violation)")
	}
}

func TestServerNewPoolEnforcesMinSize(t *testing.T) {
	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gpt-4o",
	}
	_, err := NewServer(cfg)

	if err == nil {
		t.Error("expected error when pool too small for any generator exclusion")
	}
}

func TestServerRejectsEmptyDiff(t *testing.T) {
	called := false
	dSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer dSrv.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        dSrv.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err == nil {
		t.Error("expected error for empty diff")
	}
	if called {
		t.Error("dispatcher should not be called for invalid request")
	}
}

func TestServerToolRegistered(t *testing.T) {
	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	tools := srv.ListTools()
	if len(tools) != 1 {
		t.Errorf("expected exactly 1 registered tool, got %d: %v", len(tools), tools)
	}
	if tools[0] != "audit_review" {
		t.Errorf("registered tool = %q, want audit_review", tools[0])
	}
}

func TestMaxScopeHardStopOnEmptyPool(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "claude-3")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "extra-family"},
		MinPoolSize:          1,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolHardStop,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "default",
		GeneratorProviderFamily: "extra-family",
	})
	if err != nil {
		t.Errorf("unexpected error when 1 disjoint family remains: %v", err)
	}
}

// TestCriteriaResolvedFlag verifies the S-7 fix: AuditResponse.CriteriaResolved
// is true when the requested criteria name matches a registered template
// AND false when the registry falls back to "default" because the requested
// name was unknown. Pre-fix the operator could not distinguish "ran the
// requested criteria" from "ran default because requested was unknown"
// without parsing the warning log (review S-7).
func TestCriteriaResolvedFlag(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       map[string]string{"internal-platform-x-specific": "operator template"},
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	for _, name := range []string{"default", "security", "performance", "doctrine-violation", "internal-platform-x-specific"} {
		name := name
		t.Run("resolved/"+name, func(t *testing.T) {
			resp, err := srv.handleAuditReview(context.Background(), AuditRequest{
				Diff:                    "diff",
				CriteriaName:            name,
				GeneratorProviderFamily: "anthropic",
			})
			if err != nil {
				t.Fatalf("handleAuditReview: %v", err)
			}
			if !resp.CriteriaResolved {
				t.Errorf("CriteriaResolved = false for known name %q, want true", name)
			}
			if resp.CriteriaUsed != name {
				t.Errorf("CriteriaUsed = %q, want %q (always echoes original)", resp.CriteriaUsed, name)
			}
		})
	}

	t.Run("unresolved", func(t *testing.T) {
		resp, err := srv.handleAuditReview(context.Background(), AuditRequest{
			Diff:                    "diff",
			CriteriaName:            "completely-unknown-xyz",
			GeneratorProviderFamily: "anthropic",
		})
		if err != nil {
			t.Fatalf("handleAuditReview: %v", err)
		}
		if resp.CriteriaResolved {
			t.Error("CriteriaResolved = true for unknown name, want false")
		}
		if resp.CriteriaUsed != "completely-unknown-xyz" {
			t.Errorf("CriteriaUsed = %q, want original (echoed even on fallback)", resp.CriteriaUsed)
		}
	})
}

func TestDefaultPolicyWarnOnCriteriaFallback(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"minor","concerns":["test"],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp, err := srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "completely-unknown-criteria-xyz",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Errorf("expected no error for unknown criteria with default policy: %v", err)
	}

	if resp.CriteriaUsed != "completely-unknown-criteria-xyz" {
		t.Errorf("CriteriaUsed = %q, want original request name", resp.CriteriaUsed)
	}
}

func TestWarnAndDegradePoolFallback(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google"},
		MinPoolSize:          1,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Errorf("WarnAndDegrade: unexpected error: %v", err)
	}
}

func TestAssertFunctions(t *testing.T) {
	if !AssertStdioCanonical() {
		t.Error("AssertStdioCanonical() returned false")
	}
	if !AssertBoundaryPreserved() {
		t.Error("AssertBoundaryPreserved() returned false")
	}
}

func TestHandleAuditReviewForTest(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp, err := srv.HandleAuditReviewForTest(context.Background(), AuditRequest{
		Diff:                    "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n+// foo",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Fatalf("HandleAuditReviewForTest: %v", err)
	}
	if resp.Verdict.Classification != ClassificationClean {
		t.Errorf("Classification = %q, want clean", resp.Verdict.Classification)
	}
}

func TestParseAuditRequestFromParamsAllPaths(t *testing.T) {

	got, err := parseAuditRequestFromParams(map[string]any{
		"diff":                      "some diff",
		"criteria":                  "security",
		"generator_provider_family": "anthropic",
	})
	if err != nil {
		t.Fatalf("parseAuditRequestFromParams happy path: %v", err)
	}
	if got.Diff != "some diff" {
		t.Errorf("Diff = %q", got.Diff)
	}
	if got.CriteriaName != "security" {
		t.Errorf("CriteriaName = %q", got.CriteriaName)
	}

	_, err = parseAuditRequestFromParams(map[string]any{
		"generator_provider_family": "anthropic",
	})
	if err == nil {
		t.Error("expected error for missing diff")
	}

	_, err = parseAuditRequestFromParams(map[string]any{
		"diff": "some diff",
	})
	if err == nil {
		t.Error("expected error for missing generator_provider_family")
	}

	got, err = parseAuditRequestFromParams(map[string]any{
		"diff":                      "diff",
		"generator_provider_family": "google",
	})
	if err != nil {
		t.Fatalf("parseAuditRequestFromParams default criteria: %v", err)
	}
	if got.CriteriaName != "default" {
		t.Errorf("default criteria: got %q, want default", got.CriteriaName)
	}
}

func TestArgCoercionSurfacesTypeErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    map[string]any
		wantSub string
	}{
		{
			name:    "diff-bool",
			args:    map[string]any{"diff": true, "generator_provider_family": "anthropic"},
			wantSub: "diff",
		},
		{
			name:    "diff-number",
			args:    map[string]any{"diff": 42, "generator_provider_family": "anthropic"},
			wantSub: "diff",
		},
		{
			name:    "diff-object",
			args:    map[string]any{"diff": map[string]any{"x": 1}, "generator_provider_family": "anthropic"},
			wantSub: "diff",
		},
		{
			name:    "criteria-bool",
			args:    map[string]any{"diff": "x", "criteria": true, "generator_provider_family": "anthropic"},
			wantSub: "criteria",
		},
		{
			name:    "generator-number",
			args:    map[string]any{"diff": "x", "generator_provider_family": 7},
			wantSub: "generator_provider_family",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAuditRequestFromParams(tc.args)
			if err == nil {
				t.Fatalf("expected error naming bad-type field, got nil; args=%v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSub)
			}
			if !strings.Contains(err.Error(), "expected string") {
				t.Errorf("err = %v, want 'expected string' substring", err)
			}
		})
	}
}

func TestArgCoercionAcceptsNilOptional(t *testing.T) {
	got, err := parseAuditRequestFromParams(map[string]any{
		"diff":                      "diff text",
		"criteria":                  nil,
		"generator_provider_family": "anthropic",
	})
	if err != nil {
		t.Fatalf("explicit-nil criteria: %v", err)
	}
	if got.CriteriaName != "default" {
		t.Errorf("CriteriaName = %q, want default (nil → absent)", got.CriteriaName)
	}
}

func TestNewServerDefaultMinPoolSize(t *testing.T) {

	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          0,
		CustomCriteria:       nil,
		DefaultReviewerModel: "model",
	}
	_, err := NewServer(cfg)
	if err != nil {
		t.Errorf("expected success with 3-family pool and normalised MinPoolSize=2: %v", err)
	}
}

// TestAuditReviewInputSchemaIsTight verifies the I-5 fix: the audit_review
// input schema MUST set additionalProperties=false so that LLM-supplied
// extra arguments (e.g. typos like "diff_text" instead of "diff") are
// rejected at the wire layer rather than silently dropped, AND the
// "criteria" field MUST publish an enum listing the four built-in
// criteria names so callers see them in tooling without having to grep
// the source (review I-5).
//
// Pre-fix the schema accepted any extra fields and the criteria field
// had only a free-form description; LLM tool-callers had to memorise
// the four names ("default", "security", "performance", "doctrine-violation")
// from the description string, and any typo silently degraded to the
// default criteria via the registry's graceful-fallback path.
func TestAuditReviewInputSchemaIsTight(t *testing.T) {

	schema := auditReviewInputSchema(nil)

	addl, ok := schema["additionalProperties"]
	if !ok {
		t.Fatal("schema missing 'additionalProperties' key")
	}
	addlBool, ok := addl.(bool)
	if !ok {
		t.Fatalf("additionalProperties type = %T, want bool", addl)
	}
	if addlBool {
		t.Errorf("additionalProperties = true, want false")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema.properties not a map")
	}
	criteria, ok := props["criteria"].(map[string]any)
	if !ok {
		t.Fatal("schema.properties.criteria not a map")
	}
	enumRaw, ok := criteria["enum"]
	if !ok {
		t.Fatal("criteria field missing 'enum'")
	}
	enum, ok := enumRaw.([]string)
	if !ok {
		t.Fatalf("criteria.enum type = %T, want []string", enumRaw)
	}
	wantSet := map[string]bool{
		"default":            true,
		"security":           true,
		"performance":        true,
		"doctrine-violation": true,
	}
	gotSet := make(map[string]bool, len(enum))
	for _, v := range enum {
		gotSet[v] = true
	}
	for w := range wantSet {
		if !gotSet[w] {
			t.Errorf("criteria.enum missing %q", w)
		}
	}
	if len(enum) != len(wantSet) {
		t.Errorf("criteria.enum has %d entries (%v), want exactly %d (%v)",
			len(enum), enum, len(wantSet), wantSet)
	}
}

func TestAuditReviewInputSchemaMergesOperatorCriteria(t *testing.T) {
	registry := NewCriteriaRegistry(map[string]string{
		"internal-platform-x-specific": "operator template",
		"hotel-domain":                 "another operator template",
	})
	schema := auditReviewInputSchema(registry.Names())
	props := schema["properties"].(map[string]any)
	criteria := props["criteria"].(map[string]any)
	enum := criteria["enum"].([]string)
	gotSet := map[string]bool{}
	for _, e := range enum {
		gotSet[e] = true
	}
	for _, want := range []string{"default", "security", "performance", "doctrine-violation", "internal-platform-x-specific", "hotel-domain"} {
		if !gotSet[want] {
			t.Errorf("merged enum missing %q (got %v)", want, enum)
		}
	}
}

func TestAuditReviewInputSchemaEmptyArgFallsBackToBuiltins(t *testing.T) {
	schema := auditReviewInputSchema([]string{})
	props := schema["properties"].(map[string]any)
	criteria := props["criteria"].(map[string]any)
	enum := criteria["enum"].([]string)
	if len(enum) != 4 {
		t.Errorf("empty-slice arg: got enum %v, want 4 built-ins", enum)
	}
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

	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}

	select {
	case <-done:

	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after stdin EOF")
	}
}

func serveAuditOverInMemory(t *testing.T, srv *Server) (context.Context, *mcp.ClientSession) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.mcpSrv.Run(ctx, serverTransport)
	}()

	cli := mcp.NewClient(&mcp.Implementation{Name: "test-audit-client", Version: "0"}, nil)
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

func TestInMemoryTransportCallToolHappyPath(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, session := serveAuditOverInMemory(t, srv)

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "audit_review" {
		t.Errorf("unexpected tools: %v", tools.Tools)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "audit_review",
		Arguments: map[string]any{
			"diff":                      "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n+// x",
			"generator_provider_family": "anthropic",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Errorf("CallTool returned error: %v", res.Content)
	}
}

func TestInMemoryTransportCallToolInvalidParams(t *testing.T) {
	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, session := serveAuditOverInMemory(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "audit_review",
		Arguments: map[string]any{
			"diff":                      "",
			"generator_provider_family": "anthropic",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error for empty diff, got success")
	}
}

func TestInMemoryTransportCallToolHandleAuditReviewError(t *testing.T) {

	fakeD := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, session := serveAuditOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "audit_review",
		Arguments: map[string]any{
			"diff":                      "some diff",
			"generator_provider_family": "anthropic",
		},
	})

	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error result when daemon returns 503, got success")
	}
}

func TestHandleAuditReviewRouterError(t *testing.T) {

	fakeD := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err == nil {
		t.Error("expected error when dispatcher returns 500, got nil")
	}
}

func TestHardStopEmptyPoolTrigger(t *testing.T) {

	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "anthropic", "anthropic"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolHardStop,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v (construction should pass with len=3 >= 3)", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err == nil {
		t.Error("expected hard-stop error when all pool families deduplicate to generator, got nil")
	}
}

// TestWarnAndDegradeDegradedSuccessPath exercises the warn-and-degrade path
// where the initial pool fails MinPoolSize but succeeds with minSize=1.
// This covers the _ = fmt.Sprintf warning log line in handleAuditReview.
func TestWarnAndDegradeDegradedSuccessPath(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "anthropic", "google"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp, err := srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Errorf("WarnAndDegrade degraded path: unexpected error: %v", err)
	}
	if resp.Verdict.ReviewerProvider == "anthropic" {
		t.Error("ReviewerProvider must not equal GeneratorFamily (inv-zen-080)")
	}
}

// TestEmptyPoolWarnEmitsLog verifies the C-1 fix: when the disjoint pool is
// below MinPoolSize and the policy is EmptyPoolWarnAndDegrade, the server
// MUST emit a structured warning to the injected Logger so that operators
// observe a real signal (pre-fix the warning was discarded via _ =
// fmt.Sprintf(...) — silent degradation was indistinguishable from success).
func TestEmptyPoolWarnEmitsLog(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	var logBuf bytes.Buffer
	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "anthropic", "google"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
		Logger:               log.New(&logBuf, "", 0),
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Fatalf("handleAuditReview: %v", err)
	}

	got := logBuf.String()
	if !strings.Contains(got, "[audit WARN]") {
		t.Errorf("expected warning prefix in log output; got %q", got)
	}
	if !strings.Contains(got, "MinPoolSize=2") {
		t.Errorf("expected MinPoolSize=2 in log output; got %q", got)
	}
	if !strings.Contains(got, "1 family") {
		t.Errorf("expected degraded-pool size in log output; got %q", got)
	}
}

// TestEmptyPoolWarnUsesDefaultLoggerWhenNil verifies that when ServerConfig
// leaves Logger nil, the server falls back to log.Default() rather than
// crashing or silently dropping the warning.
func TestEmptyPoolWarnUsesDefaultLoggerWhenNil(t *testing.T) {
	fakeD := fakeDispatcherForServer(`{"classification":"clean","concerns":[],"suggestions":[]}`, "gemini-2.6-pro")
	defer fakeD.Close()

	origWriter := log.Writer()
	origFlags := log.Flags()
	origPrefix := log.Prefix()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
		log.SetPrefix(origPrefix)
	})

	cfg := ServerConfig{
		DaemonBaseURL:        fakeD.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "anthropic", "google"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
		Logger:               nil,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff text",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err != nil {
		t.Fatalf("handleAuditReview: %v", err)
	}
	if !strings.Contains(buf.String(), "[audit WARN]") {
		t.Errorf("expected warning via default logger; got %q", buf.String())
	}
}

// TestInvokeToolUnknown asserts the canonical gateway entry point
// (Server.InvokeTool — Plan 11 mcpgateway uniform seam) returns an
// error for unrecognised tool names. The audit MCP exposes only
// audit_review; any other name MUST be rejected.
func TestInvokeToolUnknown(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	_, err = srv.InvokeTool(context.Background(), "non-existent-tool-name", map[string]any{})
	if err == nil {
		t.Fatal("InvokeTool unknown tool: nil err; expected non-nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("err = %v; expected 'unknown tool' substring", err)
	}
}

func TestInvokeToolInvalidParams(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.InvokeTool(context.Background(), "audit_review", map[string]any{
		"criteria":                  "default",
		"generator_provider_family": "anthropic",
	})
	if err == nil {
		t.Fatal("InvokeTool with missing diff: nil err; expected non-nil")
	}
	if !strings.Contains(err.Error(), "invalid parameters") {
		t.Errorf("err = %v; expected 'invalid parameters' substring", err)
	}
}

func TestInvokeToolAuditReviewRoutesToHandler(t *testing.T) {
	verdictJSON := `{"classification":"clean","concerns":[],"suggestions":[]}`
	dSrv := fakeDispatcherForServer(verdictJSON, "gemini-2.6-pro")
	defer dSrv.Close()

	srv, err := NewServer(ServerConfig{
		DaemonBaseURL:        dSrv.URL,
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "google", "deepseek"},
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	raw, err := srv.InvokeTool(context.Background(), "audit_review", map[string]any{
		"diff":                      "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+func main() {}",
		"criteria":                  "default",
		"generator_provider_family": "anthropic",
	})
	if err != nil {
		t.Fatalf("InvokeTool audit_review: %v", err)
	}
	resp, ok := raw.(AuditResponse)
	if !ok {
		t.Fatalf("response type = %T; expected AuditResponse", raw)
	}
	if resp.Verdict.Classification != ClassificationClean {
		t.Errorf("Classification = %q; want clean", resp.Verdict.Classification)
	}
}

func TestWarnAndDegradeTrulyEmptyPool(t *testing.T) {
	cfg := ServerConfig{
		DaemonBaseURL:        "http://localhost:0",
		AuthToken:            "tok",
		ReviewerFamilyPool:   []string{"anthropic", "anthropic", "anthropic"},
		MinPoolSize:          2,
		CustomCriteria:       nil,
		DefaultReviewerModel: "gemini-2.6-pro",
		EmptyPoolPolicy:      EmptyPoolWarnAndDegrade,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	_, err = srv.handleAuditReview(context.Background(), AuditRequest{
		Diff:                    "diff",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	})
	if err == nil {
		t.Error("expected error when WarnAndDegrade truly-empty pool, got nil")
	}
}

func TestReviewerFamilyPoolFromRegistry_DeDuplicatesAndSorts(t *testing.T) {
	got := ReviewerFamilyPoolFromRegistry(map[string]string{
		"deepseek-direct":      "deepseek",
		"siliconflow-deepseek": "deepseek",
		"gemini-flash":         "google",
		"ollama-qwen-coder":    "local-qwen",
	})
	want := []string{"deepseek", "google", "local-qwen"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReviewerFamilyPoolFromRegistry_DropsEmptyFamily(t *testing.T) {
	got := ReviewerFamilyPoolFromRegistry(map[string]string{
		"with-family": "x",
		"no-family":   "",
	})
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("empty family should be dropped, got %v", got)
	}
}

func TestHandleAuditReview_FamilyPoolFromLiveRegistry(t *testing.T) {
	liveFamilies := []string{"deepseek", "google", "local-qwen"}

	pool, err := NewPool(liveFamilies, "deepseek", 2)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	for _, f := range pool.Families() {
		if f == "deepseek" {
			t.Fatal("inv-zen-213: reviewer pool must not contain the generator family")
		}
	}
	if got := pool.Choose(); got == "deepseek" {
		t.Errorf("Choose() = %q — must be disjoint from generator", got)
	}
}
