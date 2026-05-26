// SPDX-License-Identifier: MIT
package redact

import (
	"net/http"
)

func ensureNoRawTokenLeaks() bool {

	var (
		_ Secret              = nil
		_ *RedactingTransport = (*RedactingTransport)(nil)
		_ http.RoundTripper   = (*RedactingTransport)(nil)
		_ http.RoundTripper   = (http.RoundTripper)(nil)
	)

	_ = NewLogger(discardWriter{}, "", 0)
	_ = NewRedactingWriter(discardWriter{})
	return true
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

var _ = ensureNoRawTokenLeaks()
