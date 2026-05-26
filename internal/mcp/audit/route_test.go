package audit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeLLMResponse(verdictJSON string) []byte {

	resp := map[string]interface{}{
		"id":    "msg_test_001",
		"type":  "message",
		"role":  "assistant",
		"model": "gemini-2.6-pro",
		"content": []map[string]interface{}{
			{"type": "text", "text": verdictJSON},
		},
		"stop_reason": "end_turn",
		"usage": map[string]int{
			"input_tokens":  120,
			"output_tokens": 80,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// TestRouteCallSetsModelHintHeader verifies the C-2 fix: RouteCall MUST
// emit the X-Zen-Model-Hint header carrying req.ReviewerModel. Pre-fix
// the route doc-comment claimed "Passed via X-Zen-Model-Hint" but the
// header was never set — only Content-Type, Authorization, X-Zen-Profile,
// and X-Zen-Family-Constraint were emitted. Documented intent must match
// wire reality (review C-2).
func TestRouteCallSetsModelHintHeader(t *testing.T) {
	verdictJSON := `{"classification":"clean","concerns":[],"suggestions":[]}`
	gotHint := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHint = r.Header.Get("X-Zen-Model-Hint")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeLLMResponse(verdictJSON))
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "gemini-2.6-pro")
	if _, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "prompt",
		ReviewerFamily: "google",
		ReviewerModel:  "gemini-2.6-pro-preview-0501",
	}); err != nil {
		t.Fatalf("RouteCall: %v", err)
	}
	if gotHint != "gemini-2.6-pro-preview-0501" {
		t.Errorf("X-Zen-Model-Hint = %q, want gemini-2.6-pro-preview-0501", gotHint)
	}
}

func TestRouteCallRejectsEmptyReviewerModel(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "tok", "model")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "prompt",
		ReviewerFamily: "google",
		ReviewerModel:  "",
	})
	if err == nil {
		t.Error("expected error for empty ReviewerModel, got nil")
	}
	if called {
		t.Error("HTTP server should not be reached when ReviewerModel is empty")
	}
}

func TestRouteCallExtractsVerdictFromLLMResponse(t *testing.T) {
	verdictJSON := `{"classification":"minor","concerns":["missing docstring on exported type"],"suggestions":["add godoc comment"]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Header.Get("X-Zen-Profile") != "audit-reviewer" {
			t.Errorf("X-Zen-Profile = %q, want audit-reviewer", r.Header.Get("X-Zen-Profile"))
		}
		if !strings.HasPrefix(r.Header.Get("X-Zen-Family-Constraint"), "google") {

			t.Errorf("X-Zen-Family-Constraint = %q, want google", r.Header.Get("X-Zen-Family-Constraint"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeLLMResponse(verdictJSON))
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "gemini-2.6-pro")
	verdict, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+// Foo does bar.",
		CriteriaPrompt: defaultTemplates()["default"],
		ReviewerFamily: "google",
		ReviewerModel:  "gemini-2.6-pro",
	})
	if err != nil {
		t.Fatalf("RouteCall: %v", err)
	}
	if verdict.Classification != ClassificationMinor {
		t.Errorf("Classification = %q, want minor", verdict.Classification)
	}
	if len(verdict.Concerns) != 1 {
		t.Errorf("Concerns len = %d, want 1", len(verdict.Concerns))
	}
	if verdict.ReviewerProvider != "google" {
		t.Errorf("ReviewerProvider = %q, want google", verdict.ReviewerProvider)
	}
}

func TestRouteCallHandlesDaemon503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"bypass-config not extracted"}`))
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "gemini-2.6-pro")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "criteria",
		ReviewerFamily: "google",
		ReviewerModel:  "gemini-2.6-pro",
	})
	if err == nil {
		t.Error("expected error from 503 response, got nil")
	}
}

func TestRouteCallHandlesMalformedVerdictJSON(t *testing.T) {
	malformed := `I cannot review this diff because it is too large.`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeLLMResponse(malformed))
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "gemini-2.6-pro")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "criteria",
		ReviewerFamily: "google",
		ReviewerModel:  "gemini-2.6-pro",
	})
	if err == nil {
		t.Error("expected error from malformed verdict JSON, got nil")
	}
}

func TestRouteCallContextCancellation(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "gemini-2.6-pro")
	_, err := router.RouteCall(ctx, RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "criteria",
		ReviewerFamily: "google",
		ReviewerModel:  "gemini-2.6-pro",
	})
	if err == nil {
		t.Error("expected error after context cancellation, got nil")
	}
}

func TestRouteCallRejectsEmptyFields(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "test-token", "model")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "",
		CriteriaPrompt: "criteria",
		ReviewerFamily: "google",
		ReviewerModel:  "model",
	})
	if err == nil {
		t.Error("expected error for empty Diff")
	}
	if called {
		t.Error("HTTP server should not be called for invalid request")
	}
}

func TestErrEmptyContentBlock(t *testing.T) {
	emptyContent := map[string]interface{}{
		"id":          "msg_empty_sentinel",
		"type":        "message",
		"role":        "assistant",
		"model":       "gemini-2.6-pro",
		"content":     []map[string]interface{}{},
		"stop_reason": "end_turn",
	}
	b, _ := json.Marshal(emptyContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "tok", "model")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "prompt",
		ReviewerFamily: "google",
		ReviewerModel:  "model",
	})
	if err == nil {
		t.Fatal("expected ErrEmptyContentBlock, got nil")
	}
	if !errors.Is(err, ErrEmptyContentBlock) {
		t.Errorf("err = %v, want errors.Is == ErrEmptyContentBlock", err)
	}
}

func TestRouteCallHandlesEmptyContentBlock(t *testing.T) {

	emptyContent := map[string]interface{}{
		"id":          "msg_empty",
		"type":        "message",
		"role":        "assistant",
		"model":       "gemini-2.6-pro",
		"content":     []map[string]interface{}{},
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 10, "output_tokens": 0},
	}
	b, _ := json.Marshal(emptyContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "tok", "model")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "criteria",
		ReviewerFamily: "google",
		ReviewerModel:  "model",
	})
	if err == nil {
		t.Error("expected error for empty content block, got nil")
	}
}

func TestParseVerdictFromTextMarkdownFence(t *testing.T) {
	fenced := "```json\n{\"classification\":\"major\",\"concerns\":[\"bad\"],\"suggestions\":[\"fix\"]}\n```"
	v, err := parseVerdictFromText(fenced, "google", "gemini-2.6-pro")
	if err != nil {
		t.Fatalf("parseVerdictFromText with fence: %v", err)
	}
	if v.Classification != ClassificationMajor {
		t.Errorf("Classification = %q, want major", v.Classification)
	}
}

func TestParseVerdictFromTextMarkdownFenceNoNewline(t *testing.T) {
	noNewline := "```" + `{"classification":"clean","concerns":[],"suggestions":[]}` + "```"
	v, err := parseVerdictFromText(noNewline, "google", "model")
	if err != nil {
		t.Fatalf("parseVerdictFromText with no-newline fence: %v", err)
	}
	if v.Classification != ClassificationClean {
		t.Errorf("Classification = %q, want clean", v.Classification)
	}
}

func TestTruncateLongString(t *testing.T) {
	s := "abcdefghij"
	got := truncate(s, 5)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(%q, 5) = %q, want suffix '…'", s, got)
	}
	if len([]rune(got)) > 6 {
		t.Errorf("truncated string too long: %q", got)
	}
}

func TestTruncateShortString(t *testing.T) {
	s := "abc"
	got := truncate(s, 100)
	if got != s {
		t.Errorf("truncate short: got %q, want %q", got, s)
	}
}

type brokenReader struct{}

func (b brokenReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read error: connection reset")
}

func TestRouteCallHandlesBodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)

		hj, ok := w.(http.Hijacker)
		if !ok {

			w.Write([]byte("partial"))
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "tok", "model")
	router.httpClient.Transport = &errorOnReadTransport{delegate: router.httpClient.Transport}
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "prompt",
		ReviewerFamily: "google",
		ReviewerModel:  "model",
	})
	if err == nil {
		t.Error("expected error from body read failure, got nil")
	}
}

type errorOnReadTransport struct {
	delegate http.RoundTripper
}

func (e *errorOnReadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if e.delegate == nil {
		e.delegate = http.DefaultTransport
	}
	resp, err := e.delegate.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	resp.Body = io.NopCloser(brokenReader{})
	return resp, nil
}

func TestRouteCallHandlesInvalidDispatcherResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json-at-all"))
	}))
	defer srv.Close()

	router := NewRouter(srv.URL, "tok", "model")
	_, err := router.RouteCall(context.Background(), RouteRequest{
		Diff:           "diff",
		CriteriaPrompt: "prompt",
		ReviewerFamily: "google",
		ReviewerModel:  "model",
	})
	if err == nil {
		t.Error("expected error for non-JSON dispatcher response, got nil")
	}
}

func TestParseVerdictFromTextInvalidClassification(t *testing.T) {
	badClassJSON := `{"classification":"approved","concerns":[],"suggestions":[]}`
	_, err := parseVerdictFromText(badClassJSON, "google", "model")
	if err == nil {
		t.Error("expected error for invalid classification, got nil")
	}
}

func TestExtractJSONObjectHappyPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"bare", `{"a":1}`, `{"a":1}`},
		{"with-prose-prefix", `Here is my review: {"a":1}`, `{"a":1}`},
		{"with-prose-prefix-and-suffix", `Sure! {"a":1} That's my answer.`, `{"a":1}`},
		{"nested", `prose {"outer":{"inner":42}} trailing`, `{"outer":{"inner":42}}`},
		{"deeply-nested", `prefix {"a":{"b":{"c":{"d":1}}}} suffix`, `{"a":{"b":{"c":{"d":1}}}}`},
		{"string-with-brace", `{"text":"contains } close brace"}`, `{"text":"contains } close brace"}`},
		{"string-with-open-and-close", `{"text":"{ both } here"}`, `{"text":"{ both } here"}`},
		{"escaped-quote-then-brace", `{"text":"escaped \" then } here"}`, `{"text":"escaped \" then } here"}`},
		{"escaped-backslash-then-quote", `{"text":"path \\\"quoted\\\""}`, `{"text":"path \\\"quoted\\\""}`},
		{"first-of-multiple", `{"first":1} ignored {"second":2}`, `{"first":1}`},
		{"markdown-fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"markdown-fence-no-language", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"markdown-fence-with-prose", "Sure!\n```json\n{\"a\":1}\n```\nDone.", `{"a":1}`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractJSONObject(tc.in)
			if err != nil {
				t.Fatalf("extractJSONObject(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("extractJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractJSONObjectErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		wantErr error
	}{
		{"no-brace", "no json here at all", errNoJSON},
		{"unbalanced-open-only", `prefix { "a": 1 still open`, errUnbalancedJSON},
		{"unbalanced-string-eats-close", `{"text":"never ends`, errUnbalancedJSON},
		{"empty-string", "", errNoJSON},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractJSONObject(tc.in)
			if err == nil {
				t.Fatalf("extractJSONObject(%q): expected error %v, got nil", tc.in, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("extractJSONObject(%q): err = %v, want errors.Is == %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestParseVerdictFromTextRobust(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{
			"prose-prefix",
			`Sure, here is my review: {"classification":"clean","concerns":[],"suggestions":[]}`,
		},
		{
			"prose-prefix-and-suffix",
			`Here is the verdict: {"classification":"minor","concerns":["x"],"suggestions":["y"]} - that is all.`,
		},
		{
			"fence-with-prose-around",
			"Reviewing now.\n```json\n{\"classification\":\"major\",\"concerns\":[\"a\"],\"suggestions\":[\"b\"]}\n```\nDone.",
		},
		{
			"nested-json-inside-string",
			`{"classification":"reject","concerns":["found {bad: example} inside"],"suggestions":[]}`,
		},
		{
			"multiple-fenced-blocks",
			"```json\n{\"classification\":\"clean\",\"concerns\":[],\"suggestions\":[]}\n```\n\n```json\n{\"ignored\":true}\n```",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseVerdictFromText(tc.in, "google", "model")
			if err != nil {
				t.Fatalf("parseVerdictFromText(%q): %v", tc.in, err)
			}
			if v.Classification == "" {
				t.Errorf("parseVerdictFromText(%q) returned empty classification", tc.in)
			}
		})
	}
}

func TestParseVerdictFromTextExtractedButInvalidJSON(t *testing.T) {

	bad := `prose {this is not json} trailing`
	_, err := parseVerdictFromText(bad, "google", "model")
	if err == nil {
		t.Fatal("expected error when extracted braces are not valid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "LLM output is not valid verdict JSON") {
		t.Errorf("err = %v, want wrapping 'LLM output is not valid verdict JSON'", err)
	}
}

func TestRouterWithTimeoutOption(t *testing.T) {
	r := NewRouter("http://example", "tok", "model", WithTimeout(5*time.Second))
	if r.httpClient.Timeout != 5*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 5s", r.httpClient.Timeout)
	}
}

func TestRouterDefaultTimeout(t *testing.T) {
	r := NewRouter("http://example", "tok", "model")
	if r.httpClient.Timeout != DefaultRouterTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v", r.httpClient.Timeout, DefaultRouterTimeout)
	}
}

func TestRouterWithTimeoutPanicsOnNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -1 * time.Second} {
		d := d
		t.Run(d.String(), func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("WithTimeout(%v) did not panic", d)
				}
			}()
			_ = NewRouter("http://example", "tok", "model", WithTimeout(d))
		})
	}
}

func TestParseVerdictFromTextNilConcernsSuggestions(t *testing.T) {

	minimalJSON := `{"classification":"clean"}`
	v, err := parseVerdictFromText(minimalJSON, "google", "model")
	if err != nil {
		t.Fatalf("parseVerdictFromText with nil slices: %v", err)
	}
	if v.Concerns == nil {
		t.Error("Concerns should be empty slice, not nil")
	}
	if v.Suggestions == nil {
		t.Error("Suggestions should be empty slice, not nil")
	}
	if len(v.Concerns) != 0 || len(v.Suggestions) != 0 {
		t.Errorf("Concerns=%v Suggestions=%v, want both empty", v.Concerns, v.Suggestions)
	}
}
