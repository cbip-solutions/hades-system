// SPDX-License-Identifier: MIT
// internal/providers/errors.go
//
// Package-level sentinel errors used across multiple backends. Each
// sentinel documents how the dispatcher routes it (RecordFailure vs.
// RecordRateLimited vs. cascade-skip) so a future contributor can wire
// a new backend's failure mode without re-reading the dispatcher.
package providers

import "errors"

// ErrToolsUnsupported is returned by the openai-compat, gemini, and
// ollama backends when the canonical request carries a `tools` field.
// is the candidate to extend Anthropic-tools → OpenAI-function-calling
// translation. Until then explicit rejection beats silently dropping
// the caller's tools schema.
//
// Dispatcher contract: errors.Is(err, ErrToolsUnsupported) is a
// CAPABILITY-MISMATCH signal — NOT a health degradation. The
// dispatcher (internal/daemon/dispatcher/dispatcher.go) MUST skip
// breaker.RecordFailure on this error and continue the cascade. The
// breaker.RecordRateLimited short-circuit at attempt() line ~402-407 is
// the structural sibling; the tools-unsupported branch sits parallel
// and emits a CostEvent without touching the breaker state — a healthy
// backend stays healthy for non-tools traffic.
var ErrToolsUnsupported = errors.New("providers: tools field not yet supported on this backend")
