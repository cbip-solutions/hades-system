// SPDX-License-Identifier: MIT
// Package providers declares the common TierBackend interface, value
// types, and registry that all LLM tier backends satisfy.
//
// of: bypass.Client (Tier 1, declared in private-tier1-module),
// anthropic-paygo native client (Tier 2), Gemini native client (Tier 3),
// Ollama OpenAI-compat client (Tier 4), generic OpenAI-compat client
// (operator-extensible Tier 4 slot), or the PauseBackend sentinel
// (Tier 5, returns a descriptive error so the operator can act).
//
// # Compile-time interface guard
//
// Every concrete backend in Phases B-D and MUST include a
// compile-time guard at package scope:
//
// var _ TierBackend = (*MyBackend)(nil)
//
// This line is the runtime-zero, compile-fail proof that the type
// satisfies the interface declared here. The compliance test in
// tests/compliance/dispatcher_invariants_test.go scans the source for
// this pattern across every file in internal/providers/ and the
// bypass adapter.
//
// # Credential routing
//
// TierRequest carries credentials in two shapes:
//
// - Headers map[string]string — for non-secret HTTP headers
// (Content-Type, X-Zen-Profile, X-Zen-Project, etc.).
// - Credentials map[string]redact.Secret — for any header whose
// value is a token, API key, or refresh credential. Backends
// unwrap with.Reveal() at the exact moment of HTTP transmission
// and never log the unwrapped value. The redact.Secret type
// intercepts every Format verb to emit "[REDACTED]" instead of
// plaintext (see internal/redact/secret.go).
//
// # Provider extensibility
//
// Operator-declared providers (providers.toml) flow through
// Registry.RegisterFromConfig, which validates the schema (required
// fields, type whitelist) BEFORE constructing the backend.
// extends validation with endpoint reachability + rate card presence
// checks at startup.
//
// # Boundary
//
// This package does NOT import internal/store. Cost ledger writes
// flow via internal/daemon/dispatcheradapter. The package
// also does NOT import private-tier1-module — bypass.Client is
// adapted into a TierBackend by, not by this layer, so the
// dependency arrow points the right way (high-level orchestrator
// depends on low-level providers, not the reverse).
package providers
