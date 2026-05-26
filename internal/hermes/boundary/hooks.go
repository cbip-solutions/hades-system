// SPDX-License-Identifier: MIT
package boundary

import "context"

type PreCompletionHook interface {
	PreCompletion(ctx context.Context, req *CompletionRequest) error
}

type PostCompletionHook interface {
	PostCompletion(ctx context.Context, req CompletionRequest, resp *CompletionResponse) error
}

type HookChain[H any] struct {
	hooks []H
}

// NewHookChain constructs a HookChain bound to the given hook slice. The
// slice is captured by reference for zero-copy iteration (do NOT mutate
// after construction; tests + production both treat chains as immutable
// once registered).
func NewHookChain[H any](hooks []H) *HookChain[H] {
	return &HookChain[H]{hooks: hooks}
}

// Hooks returns the underlying hook slice (read-only; do not mutate).
func (c *HookChain[H]) Hooks() []H { return c.hooks }

func (c *HookChain[H]) Len() int { return len(c.hooks) }
