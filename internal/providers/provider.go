// SPDX-License-Identifier: MIT
// internal/providers/provider.go
package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

// Tier identifies which LLM tier handled (or will handle) a request.
//
// Order matters: TierInHouse..TierPAYG preserve the numeric values used
// 's bypass.Tier (so audit rows written before release remain
// readable when re-decoded), and release appends new tiers after.
//
// String() values are the canonical lowercase form persisted in
// cost_ledger.tier, tier_health_samples.tier, pin_overrides.tier, and
// emitted in audit logs / morning brief / operator notifications. These
// strings MUST NOT change across releases (operator dashboards depend
// on them).
type Tier int

const (
	TierInHouse Tier = iota

	TierCommunity

	TierPAYG

	TierGemini

	TierOllama

	TierGenericOpenAICompat
	// TierOpenClaude is a TOMBSTONE tier. The routing-layer backend that
	// formerly reported this tier was removed in release — the
	// providers.toml-driven cascade superseded it (ADR-0093). This
	// constant is KEPT, not deleted: its String() value "openclaude"
	// is persisted in cost_ledger.tier for pre-release historical rows,
	// and Tier String() values MUST NOT change across releases. No
	// backend reports Tier() == TierOpenClaude anymore. invariant
	// enforces the backend's removal while permitting this enum remnant.
	// (The substrate-layer OpenClaude in internal/workforce/ is an
	// unrelated concern — ADR-0080 territory — not governed here.)
	TierOpenClaude

	TierPause
)

const TierAnthropicPAYG = TierPAYG

// String returns the canonical lowercase name. UNKNOWN values render
// as "unknown(N)" so log lines do not become ambiguous; the compliance
// test asserts every declared constant has a non-unknown rendering.
func (t Tier) String() string {
	switch t {
	case TierInHouse:
		return "in-house"
	case TierCommunity:
		return "community"
	case TierPAYG:
		return "anthropic-paygo"
	case TierGemini:
		return "gemini"
	case TierOllama:
		return "ollama"
	case TierGenericOpenAICompat:
		return "openai-compat"
	case TierOpenClaude:
		return "openclaude"
	case TierPause:
		return "pause"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

func ParseTier(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "in-house":
		return TierInHouse, nil
	case "community":
		return TierCommunity, nil
	case "anthropic-paygo", "payg", "paygo":
		return TierAnthropicPAYG, nil
	case "gemini":
		return TierGemini, nil
	case "ollama":
		return TierOllama, nil
	case "openai-compat", "openai":
		return TierGenericOpenAICompat, nil
	case "openclaude":
		return TierOpenClaude, nil
	case "pause":
		return TierPause, nil
	default:
		return Tier(-1), fmt.Errorf("providers: unknown tier %q", s)
	}
}

var (
	ErrBackendNotConfigured = errors.New("providers: backend not configured")

	ErrTierUnavailable = errors.New("providers: tier unavailable")

	ErrPaused = errors.New("providers: tier is paused (Tier 5 sentinel)")

	ErrRateMissing = errors.New("providers: rate card missing for provider/model")

	ErrCapacityExceeded = errors.New("providers: per-window capacity exceeded")
)

type RateLimitedError struct {
	Provider   string
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("provider %q rate-limited (429); retry-after=%s", e.Provider, e.RetryAfter)
}

// TierRequest is the input every TierBackend.Forward consumes. It
// is provider-agnostic: each backend translates the canonical fields
// into its own wire format (anthropic-paygo, gemini, openai-compat,
// etc.). The orchestrator builds this struct from the
// daemon's incoming HTTP request plus the resolved profile/pin/project
// context.
//
// Body is the canonical-encoded request body ( supplies the
// per-tier encoder; only carries the bytes through). Backends
// MUST NOT mutate Body.
//
// Credentials carries values whose printed form would expose secrets.
// Backends call cred.Reveal() at the exact moment of HTTP header
// injection and never store the unwrapped value. invariant.
type TierRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte

	// Credentials — secret headers. Backends unwrap with.Reveal().
	// The map is nil for backends that do not need a credential
	// (e.g. Ollama on localhost with no auth).
	Credentials map[string]redact.Secret

	ConversationID string
	SessionID      string
	IdempotencyKey string
	Profile        string
	Project        string
	Model          string
}

type TierResponse struct {
	Status  int
	Body    []byte
	Headers map[string]string

	LatencyMs int64
	TierUsed  Tier
	ModelUsed string

	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUSD             float64

	ErrorCode    string
	ErrorPattern string
}

type TierCapabilities struct {
	SupportsStreaming     bool
	SupportsToolUse       bool
	SupportsVision        bool
	SupportsPromptCaching bool
	MaxContextTokens      int
	MaxOutputTokens       int
}

// TierBackend is the contract every release backend satisfies.
//
// Phases B/C/D/H attach a compile-time guard at package scope:
//
// var _ TierBackend = (*MyBackend)(nil)
//
// invariant (compliance test scans for this pattern).
//
// Forward MUST be safe for concurrent invocation (the dispatcher fans
// out up to 200 in-flight requests). Backends use their own mutexes
// for refresh / rotation; the interface itself imposes no locking.
//
// Probe is called by the active probe scheduler every ~10min
// for tiers idle ≥30min. It executes a 1-token canonical "hi" request
// (invariant: never user content). On success it returns nil; on
// failure it returns the wrapped error so the scheduler can record it
// in tier_health_samples.
//
// Close releases backend resources (HTTP transports, refresh tickers,
// keychain handles). Called by Registry.Close() at daemon shutdown.
//
// Name returns the backend's unique registry key (e.g. "anthropic-paygo",
// "gemini", "ollama", "deepseek-r1"). MUST be stable across daemon
// restarts because cost_ledger.tier and pin_overrides.provider hold
// it as text.
//
// Capabilities returns the static capability matrix. Backends that
// dynamically degrade (e.g., Gemini disables tool_use under safety
// filter) MUST still advertise the maximum here — runtime degradation
// is reported via TierResponse.ErrorCode.
//
// Tier returns the broad tier classification (one of the Tier enum
// constants). Multiple backends can share the same Tier (e.g.,
// "deepseek-r1" and "kimi-k2" both have Tier() == TierGenericOpenAICompat),
// so the orchestrator distinguishes them by Name() inside the same
// tier when iterating fallback chains.
type TierBackend interface {
	Forward(ctx context.Context, req TierRequest) (*TierResponse, error)
	Probe(ctx context.Context) error
	Close() error
	Name() string
	Capabilities() TierCapabilities
	Tier() Tier
}
