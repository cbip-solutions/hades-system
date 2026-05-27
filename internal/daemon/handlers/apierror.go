// SPDX-License-Identifier: MIT
// Package handlers — apierror.go.
//
// Canonical JSON error/response shape across every handler.
// Wire format:
//
// {"error":"<human-readable>","code":"<stable-id>","request_id":"<8-hex>"}
//
// Operator's CLI parses.code for stable error identification (e.g.,
// "alias_unknown", "schedule_not_found", "bad_json", "validation_failed",
// "unauthorized"). The X-Request-ID response header surfaces the same
// id for log-correlation when the body is consumed by a streaming
// pipe.
//
// Both RenderError + RenderJSON are intentionally tolerant: a missing
// request_id in ctx is auto-filled with a fresh crypto/rand 8-byte hex
// id (same encoding everywhere, never empty in the response).
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type requestIDCtxKey struct{}

func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDCtxKey{}).(string)
	return v
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

func generateRequestID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

type APIError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

func RenderError(ctx context.Context, w http.ResponseWriter, status int, code, msg string) {
	rid := RequestIDFromContext(ctx)
	if rid == "" {
		rid = generateRequestID()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", rid)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{
		Error:     msg,
		Code:      code,
		RequestID: rid,
	})
}

func RenderJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	rid := RequestIDFromContext(ctx)
	if rid == "" {
		rid = generateRequestID()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", rid)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
