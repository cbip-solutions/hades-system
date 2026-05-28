// SPDX-License-Identifier: MIT
package redact

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
)

// RedactingTransport is an http.RoundTripper that:
//
// 1. Sets the Authorization header from a Secret at the moment of
// transmission, never storing the plaintext on the caller's
// request struct.
// 2. Replaces every Authorization header value in dump output with
// Marker, so accidental httputil.DumpRequestOut calls cannot leak.
// 3. Provides SafeDumpRequest / SafeDumpResponse helpers that callers
// MUST use instead of httputil.DumpRequest{,Out}.
//
// invariant (compile-checked): every HTTP path in
// private-tier1-module that issues outbound requests must use a
// client whose Transport is a *RedactingTransport. The compile-check
// symbol in compile_check.go enforces this by referencing the
// constructor, so removing the wrap call surfaces in `make
// verify-invariants` output.
type RedactingTransport struct {
	Inner http.RoundTripper

	Bearer Secret
}

func NewRedactingTransport(inner http.RoundTripper, bearer Secret) *RedactingTransport {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &RedactingTransport{Inner: inner, Bearer: bearer}
}

func (rt *RedactingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	if len(rt.Bearer) > 0 {
		cloned.Header.Set("Authorization", string(rt.Bearer.Reveal()))
	}
	return rt.Inner.RoundTrip(cloned)
}

func (rt *RedactingTransport) SafeDumpRequest(req *http.Request, withBody bool) ([]byte, error) {

	cloned := req.Clone(req.Context())

	if cloned.Header.Get("Authorization") != "" {
		cloned.Header.Set("Authorization", Marker)
	}

	if cloned.Header.Get("Authorization") == "" && len(rt.Bearer) > 0 {
		cloned.Header.Set("Authorization", Marker)
	}
	dump, err := httputil.DumpRequestOut(cloned, withBody)
	if err != nil {

		dump2, err2 := httputil.DumpRequest(cloned, withBody)
		if err2 != nil {

			return nil, fmt.Errorf("dump request: %w (fallback: %w)", err, err2)
		}
		dump = append([]byte("# fallback: server-side format (DumpRequestOut failed: "+err.Error()+")\n"), dump2...)
	}
	return ScrubBytes(dump), nil
}

func (rt *RedactingTransport) SafeDumpResponse(resp *http.Response, withBody bool) ([]byte, error) {
	if withBody && resp.Body != nil {

		original := resp.Body
		buf, err := io.ReadAll(original)
		_ = original.Close()
		if err != nil {

			resp.Body = io.NopCloser(bytes.NewReader(nil))
			return nil, err
		}

		resp.Body = io.NopCloser(bytes.NewReader(ScrubBytes(buf)))
	}
	dump, err := httputil.DumpResponse(resp, withBody)
	if err != nil {
		return nil, err
	}
	return ScrubBytes(dump), nil
}

func (rt *RedactingTransport) Unwrap() http.RoundTripper {
	return rt.Inner
}

var _ http.RoundTripper = (*RedactingTransport)(nil)
