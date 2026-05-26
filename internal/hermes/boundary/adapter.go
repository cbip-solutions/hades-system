// SPDX-License-Identifier: MIT
package boundary

import (
	"context"
	"fmt"
)

type HermesCli interface {
	SendCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	RegisterStatusProvider(provider StatusProvider) error

	OnSessionStart(handler SessionStartHandler) error

	OnPreToolCall(handler PreToolCallHandler) error

	RenderInlinePrompt(ctx context.Context, prompt InlinePrompt) (InlinePromptResponse, error)

	WrapMCPEnvelope(payload MCPPayload) MCPEnvelope

	Capabilities() Capabilities
}

type Capabilities struct {
	StatusProvider bool

	SessionStartHook bool

	InlinePrompt bool
}

// Adapter is the production implementation of Surface. Delegates every
// method to the injected HermesCli; short-circuits capability-gated methods
// with ErrCapabilityUnavailable when feature-detection finds the API absent.
//
// Construct via NewAdapter; do NOT zero-value (the embedded Capabilities
// snapshot must be populated for feature-detection to work).
type Adapter struct {
	cli HermesCli
	cap Capabilities
}

var _ Surface = (*Adapter)(nil)

// NewAdapter constructs an Adapter wired to the given HermesCli implementation.
// cli MUST NOT be nil — passing nil is a wiring bug at daemon bootstrap
// that fails fast here rather than at first call.
//
// Capabilities are captured once at construction; they reflect the pinned
// Hermes version's behaviour for the lifetime of the Adapter.
func NewAdapter(cli HermesCli) *Adapter {
	if cli == nil {
		panic("boundary.NewAdapter: cli is required")
	}
	return &Adapter{
		cli: cli,
		cap: cli.Capabilities(),
	}
}

func (a *Adapter) CapabilitiesSnapshot() Capabilities { return a.cap }

func (a *Adapter) SendCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	return a.cli.SendCompletion(ctx, req)
}

func (a *Adapter) RegisterStatusProvider(provider StatusProvider) error {
	if provider == nil {
		return fmt.Errorf("boundary.RegisterStatusProvider: provider is required")
	}
	if !a.cap.StatusProvider {
		return ErrCapabilityUnavailable
	}
	return a.cli.RegisterStatusProvider(provider)
}

func (a *Adapter) IsStatusProviderSupported() bool { return a.cap.StatusProvider }

func (a *Adapter) OnSessionStart(handler SessionStartHandler) error {
	if handler == nil {
		return fmt.Errorf("boundary.OnSessionStart: handler is required")
	}
	if !a.cap.SessionStartHook {
		return ErrCapabilityUnavailable
	}
	return a.cli.OnSessionStart(handler)
}

func (a *Adapter) IsSessionStartHookSupported() bool { return a.cap.SessionStartHook }

func (a *Adapter) OnPreToolCall(handler PreToolCallHandler) error {
	if handler == nil {
		return fmt.Errorf("boundary.OnPreToolCall: handler is required")
	}
	return a.cli.OnPreToolCall(handler)
}

func (a *Adapter) RenderInlinePrompt(ctx context.Context, prompt InlinePrompt) (InlinePromptResponse, error) {
	if !a.cap.InlinePrompt {
		return InlinePromptResponse{}, ErrCapabilityUnavailable
	}
	return a.cli.RenderInlinePrompt(ctx, prompt)
}

func (a *Adapter) IsInlinePromptSupported() bool { return a.cap.InlinePrompt }

func (a *Adapter) WrapMCPEnvelope(payload MCPPayload) MCPEnvelope {
	return a.cli.WrapMCPEnvelope(payload)
}
