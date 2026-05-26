// SPDX-License-Identifier: MIT
package boundary

import (
	"context"
	"errors"
)

// ErrCapabilityUnavailable is returned by capability-gated methods when the
// underlying Hermes version lacks the expected API. Consumers SHOULD
// feature-detect via Is*Supported() helpers before calling capability-gated
// methods.
//
// inv-zen-322: every G2/G3/G5 capability-gated method MUST return this
// sentinel when feature-detection finds the underlying API absent. The
// boundary lint (scripts/verify_no_direct_hermes_imports.sh) enforces
// no consumer touches Hermes directly; the Surface contract enforces
// that consumers see ErrCapabilityUnavailable on graceful degrade.
var ErrCapabilityUnavailable = errors.New("boundary: hermes capability unavailable in pinned version")

type Surface interface {
	SendCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	RegisterStatusProvider(provider StatusProvider) error

	IsStatusProviderSupported() bool

	OnSessionStart(handler SessionStartHandler) error

	IsSessionStartHookSupported() bool

	OnPreToolCall(handler PreToolCallHandler) error

	RenderInlinePrompt(ctx context.Context, prompt InlinePrompt) (InlinePromptResponse, error)

	IsInlinePromptSupported() bool

	WrapMCPEnvelope(payload MCPPayload) MCPEnvelope
}

type CompletionRequest struct {
	Model string

	Prompt string

	Headers map[string]string

	SessionID string

	Profile string
}

type CompletionResponse struct {
	Text string

	ModelUsed string

	Status int

	AuditEventID string
}

type StatusProvider func() string

type SessionStartHandler func(ctx context.Context, session SessionInfo)

type SessionInfo struct {
	SessionID string

	ResumedFrom string
}

type PreToolCallHandler func(ctx context.Context, call ToolCall)

type ToolCall struct {
	Name string

	Args map[string]any
}

type InlinePrompt struct {
	Question string

	Options []string
}

type InlinePromptResponse struct {
	Choice string
}

type MCPPayload struct {
	Method string

	Params map[string]any
}

type MCPEnvelope struct {
	Version string

	Payload MCPPayload
}
