package redact

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// This file holds incremental tests added solely to bring per-file
// coverage to the >=90% security-critical floor required by the master
// plan. Each test names the function it targets so future maintainers
// can match coverage gaps to verifications quickly.

func TestScrubBytes_EmptyInput(t *testing.T) {
	if got := ScrubBytes(nil); got != nil {
		t.Fatalf("ScrubBytes(nil) = %v, want nil", got)
	}
	if got := ScrubBytes([]byte{}); len(got) != 0 {
		t.Fatalf("ScrubBytes(empty) = %v, want empty", got)
	}
}

func TestScrubString_EmptyInput(t *testing.T) {
	if got := ScrubString(""); got != "" {
		t.Fatalf("ScrubString(\"\") = %q, want empty", got)
	}
}

func TestNewSecret_EmptyString(t *testing.T) {
	s := NewSecret("")
	if s != nil {
		t.Fatalf("NewSecret(\"\") = %v, want nil", s)
	}
}

func TestNewSecret_CopiesInput(t *testing.T) {
	in := "abc"
	s := NewSecret(in)
	if string(s) != in {
		t.Fatalf("NewSecret stored %q, want %q", s, in)
	}
}

func TestSecret_GoString_Direct(t *testing.T) {

	s := Secret("anything")
	if got := s.GoString(); got != Marker {
		t.Fatalf("GoString() = %q, want %q", got, Marker)
	}
}

func TestSecret_Format_Hex(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%x", s)
	want := fmt.Sprintf("%x", []byte(Marker))
	if got != want {
		t.Fatalf("%%x = %q, want %q", got, want)
	}
}

func TestSecret_Format_HexUpper(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%X", s)
	want := fmt.Sprintf("%X", []byte(Marker))
	if got != want {
		t.Fatalf("%%X = %q, want %q", got, want)
	}
}

func TestSecret_Format_QuotedAlternate(t *testing.T) {

	s := Secret(sample)
	got := fmt.Sprintf("%q", s)
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("%%q = %q, missing marker", got)
	}
	if strings.Contains(got, sample) {
		t.Fatalf("%%q leaked: %q", got)
	}
}

func TestSecret_Format_WithWidth(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%20v", s)

	if !strings.Contains(got, Marker) {
		t.Fatalf("width-formatted output missing marker: %q", got)
	}
	if len(got) < 20 {
		t.Fatalf("width=20 produced only %d chars: %q", len(got), got)
	}
}

func TestLogger_PrefixFlagAccessors(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "p1 ", 0)
	if got := l.Prefix(); got != "p1 " {
		t.Fatalf("Prefix() = %q, want %q", got, "p1 ")
	}
	l.SetPrefix("p2 ")
	if got := l.Prefix(); got != "p2 " {
		t.Fatalf("after SetPrefix, Prefix() = %q, want %q", got, "p2 ")
	}
	if got := l.Flags(); got != 0 {
		t.Fatalf("Flags() = %d, want 0", got)
	}
	l.SetFlags(1)
	if got := l.Flags(); got != 1 {
		t.Fatalf("after SetFlags(1), Flags() = %d, want 1", got)
	}
}

func TestRedactingWriter_EmptyWrite(t *testing.T) {
	var buf bytes.Buffer
	w := NewRedactingWriter(&buf)
	n, err := w.Write(nil)
	if err != nil {
		t.Fatalf("empty Write: %v", err)
	}
	if n != 0 {
		t.Fatalf("empty Write n = %d, want 0", n)
	}
}

type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, errors.New("forced inner write error") }

func TestRedactingWriter_InnerWriteError(t *testing.T) {
	w := NewRedactingWriter(errWriter{})
	n, err := w.Write([]byte("anything"))
	if err == nil {
		t.Fatal("expected inner error, got nil")
	}
	if n != 0 {
		t.Fatalf("on error n = %d, want 0", n)
	}
}

func TestNewRedactingTransport_NilInnerDefaultsToHTTPDefault(t *testing.T) {
	rt := NewRedactingTransport(nil, NewSecret("Bearer x"))
	if rt.Inner != http.DefaultTransport {
		t.Fatalf("nil inner not defaulted to http.DefaultTransport")
	}
}

func TestSafeDumpRequest_NoAuthHeader_AddsMarkerWhenBearerPresent(t *testing.T) {
	rt := NewRedactingTransport(http.DefaultTransport, NewSecret("Bearer "+tokenForTransport))
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)

	dump, err := rt.SafeDumpRequest(req, false)
	if err != nil {
		t.Fatalf("SafeDumpRequest: %v", err)
	}
	if !bytes.Contains(dump, []byte("Authorization: "+Marker)) {
		t.Fatalf("expected Authorization marker in dump: %s", dump)
	}
	if bytes.Contains(dump, []byte(tokenForTransport)) {
		t.Fatalf("dump leaked token: %s", dump)
	}
}

func TestSafeDumpResponse_WithoutBody(t *testing.T) {
	resp := &http.Response{
		Status:     "204 No Content",
		StatusCode: 204,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("ignored body")),
		Request:    &http.Request{Method: "GET"},
	}
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	dump, err := rt.SafeDumpResponse(resp, false)
	if err != nil {
		t.Fatalf("SafeDumpResponse: %v", err)
	}
	if bytes.Contains(dump, []byte("ignored body")) {
		t.Fatalf("withBody=false leaked body: %s", dump)
	}
}

func TestSafeDumpResponse_NilBody(t *testing.T) {
	resp := &http.Response{
		Status:     "204 No Content",
		StatusCode: 204,
		Header:     http.Header{},
		Body:       nil,
		Request:    &http.Request{Method: "GET"},
	}
	rt := NewRedactingTransport(http.DefaultTransport, nil)
	if _, err := rt.SafeDumpResponse(resp, true); err != nil {
		t.Fatalf("SafeDumpResponse with nil body: %v", err)
	}
}

func TestDiscardWriter_Write(t *testing.T) {
	var w discardWriter
	n, err := w.Write([]byte("ignored"))
	if err != nil {
		t.Fatalf("discardWriter.Write: %v", err)
	}
	if n != len("ignored") {
		t.Fatalf("discardWriter.Write n = %d, want %d", n, len("ignored"))
	}
}
