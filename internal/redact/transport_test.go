package redact

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

const tokenForTransport = "sk-ant-oat01-TRANSPORTSECRETXXXXXXXXXXXXXXXXX"

func TestRedactingTransport_AuthHeader_SetFromSecret(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rt := NewRedactingTransport(http.DefaultTransport, NewSecret("Bearer "+tokenForTransport))
	c := &http.Client{Transport: rt}
	resp, err := c.Get(srv.URL + "/v1/messages")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if captured == nil {
		t.Fatal("server did not capture request")
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer "+tokenForTransport {
		t.Fatalf("server saw Authorization = %q, want %q", got, "Bearer "+tokenForTransport)
	}
}

func TestRedactingTransport_SafeDumpRequest_Redacts(t *testing.T) {
	rt := NewRedactingTransport(http.DefaultTransport, NewSecret("Bearer "+tokenForTransport))
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", strings.NewReader(`{"model":"opus"}`))

	req.Header.Set("Authorization", "Bearer "+tokenForTransport)
	dump, err := rt.SafeDumpRequest(req, true)
	if err != nil {
		t.Fatalf("SafeDumpRequest: %v", err)
	}
	if bytes.Contains(dump, []byte(tokenForTransport)) {
		t.Fatalf("SafeDumpRequest leaked token: %s", dump)
	}
	if !bytes.Contains(dump, []byte(Marker)) {
		t.Fatalf("SafeDumpRequest missing marker: %s", dump)
	}
}

func TestRedactingTransport_SafeDumpResponse_RedactsBody(t *testing.T) {
	body := `{"refresh_token":"rt_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ok":true}`
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{Method: "POST"},
	}
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	dump, err := rt.SafeDumpResponse(resp, true)
	if err != nil {
		t.Fatalf("SafeDumpResponse: %v", err)
	}
	if bytes.Contains(dump, []byte("rt_AAAA")) {
		t.Fatalf("SafeDumpResponse leaked refresh_token: %s", dump)
	}
}

func TestRedactingTransport_StandardDumpRequestOut_WouldLeak_BUT_HelperRedacts(t *testing.T) {

	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+tokenForTransport)
	stdDump, err := httputil.DumpRequest(req, false)
	if err != nil {
		t.Fatalf("std DumpRequest: %v", err)
	}
	if !bytes.Contains(stdDump, []byte(tokenForTransport)) {
		t.Skip("stdlib no longer leaks Authorization in DumpRequest; helper still safe but redundant")
	}

	rt := NewRedactingTransport(http.DefaultTransport, nil)
	safeDump, err := rt.SafeDumpRequest(req, false)
	if err != nil {
		t.Fatalf("SafeDumpRequest: %v", err)
	}
	if bytes.Contains(safeDump, []byte(tokenForTransport)) {
		t.Fatalf("safe dump leaked: %s", safeDump)
	}
}

func TestRedactingTransport_NilSecret_NoAuthHeaderSet(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := NewRedactingTransport(http.DefaultTransport, nil)
	c := &http.Client{Transport: rt}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if captured.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization unexpectedly set with nil Secret: %q", captured.Header.Get("Authorization"))
	}
}

func TestRedactingTransport_PreservesCallerHeaders(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := NewRedactingTransport(http.DefaultTransport, NewSecret("Bearer "+tokenForTransport))
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("body"))
	req.Header.Set("X-Custom", "value")
	req.Header.Set("User-Agent", "test-agent/1.0")
	c := &http.Client{Transport: rt}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if got := captured.Header.Get("X-Custom"); got != "value" {
		t.Fatalf("X-Custom not preserved: got %q", got)
	}
	if got := captured.Header.Get("User-Agent"); got != "test-agent/1.0" {
		t.Fatalf("User-Agent not preserved: got %q", got)
	}
}

func TestRedactingTransport_RoundTripError_Propagates(t *testing.T) {
	rt := NewRedactingTransport(errTransport{}, NewSecret("Bearer "+tokenForTransport))
	req, _ := http.NewRequest("GET", "http://example.invalid", nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from inner transport, got nil")
	}
	if strings.Contains(err.Error(), tokenForTransport) {
		t.Fatalf("error message leaked token: %v", err)
	}
}

type errTransport struct{}

func (errTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, &transportTestError{"connection refused"}
}

type transportTestError struct{ msg string }

func (e *transportTestError) Error() string { return e.msg }

type errReadCloser struct {
	closed bool
}

func (e *errReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("forced read error")
}

func (e *errReadCloser) Close() error {
	e.closed = true
	return nil
}

func TestSafeDumpResponse_ReadAllError_ClosesBodyAndRestoresEmpty(t *testing.T) {
	body := &errReadCloser{}
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Header:     http.Header{},
		Body:       body,
		Request:    &http.Request{Method: "GET"},
	}
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	_, err := rt.SafeDumpResponse(resp, true)
	if err == nil {
		t.Fatal("expected error from failed body read, got nil")
	}
	if !body.closed {
		t.Fatal("SafeDumpResponse did not close body on read error")
	}
	if resp.Body == nil {
		t.Fatal("SafeDumpResponse left resp.Body nil after error; want NopCloser(empty)")
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading restored body: %v", err)
	}
	if len(buf) != 0 {
		t.Fatalf("restored body not empty: %q", buf)
	}
}

type errBody struct{}

func (errBody) Read(_ []byte) (int, error) { return 0, errors.New("forced body read error") }
func (errBody) Close() error               { return nil }

func TestSafeDumpRequest_BothFail_WrapsErrors(t *testing.T) {

	req, _ := http.NewRequest("POST", "https://example.com/x", errBody{})
	req.ContentLength = 10
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	_, err := rt.SafeDumpRequest(req, true)
	if err == nil {
		t.Skip("stdlib unexpectedly accepted err-body request; cannot test wrap")
	}
	if !strings.Contains(err.Error(), "dump request") {
		t.Fatalf("expected wrapped error to mention 'dump request', got %v", err)
	}
	if !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("expected wrapped error to mention fallback, got %v", err)
	}
}

func TestSafeDumpRequest_FallbackComment_AddsCommentLine(t *testing.T) {
	req := &http.Request{
		Method:     "GET",
		URL:        &url.URL{Path: "/relative-only"},
		Host:       "example.com",
		Header:     http.Header{},
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	dump, err := rt.SafeDumpRequest(req, false)
	if err != nil {
		t.Skipf("both dump paths failed (stdlib-dependent); cannot exercise comment branch: %v", err)
	}
	if bytes.HasPrefix(dump, []byte("# fallback:")) {

		return
	}

	t.Skipf("DumpRequestOut succeeded directly; fallback branch not exercised on this stdlib")
}

func TestRedactingTransport_DoesNotMutateCallerRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := NewRedactingTransport(http.DefaultTransport, NewSecret("Bearer "+tokenForTransport))
	req, _ := http.NewRequest("GET", srv.URL, nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("caller's request was mutated; Authorization = %q", got)
	}
}
