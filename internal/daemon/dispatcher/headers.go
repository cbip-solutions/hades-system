// SPDX-License-Identifier: MIT
// internal/daemon/dispatcher/headers.go
//
// Header injection for X-Zen-Profile/Project/Session. Context-keyed values
// flow from the daemon entry point (anthropic_proxy / orchestrator) down to
// dispatcher.Forward, which merges them with explicit per-request headers
// before tier dispatch.
//
// Design notes:
//   - Keys are unexported typed ints (ctxKey) to avoid cross-package collision.
//   - Empty values are omitted from HeadersFromContext output so callers never
//     need to filter zero-value headers before forwarding to the upstream tier.
//   - MergeHeaders: explicit wins on conflict, including explicit empty string —
//     the caller's deliberate choice always overrides the ambient context default.
//   - Boundary (inv-zen-031): this file only imports "context" from stdlib.
//     It MUST NOT import internal/store or any other internal package.

package dispatcher

import "context"

type ctxKey int

const (
	ctxProjectKey ctxKey = iota
	ctxSessionKey
	ctxProfileKey
)

func WithProject(ctx context.Context, project string) context.Context {
	return context.WithValue(ctx, ctxProjectKey, project)
}

func WithSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, ctxSessionKey, sessionID)
}

func WithProfile(ctx context.Context, profile string) context.Context {
	return context.WithValue(ctx, ctxProfileKey, profile)
}

func HeadersFromContext(ctx context.Context) map[string]string {
	out := make(map[string]string)
	if v, ok := ctx.Value(ctxProjectKey).(string); ok && v != "" {
		out["X-Zen-Project"] = v
	}
	if v, ok := ctx.Value(ctxSessionKey).(string); ok && v != "" {
		out["X-Zen-Session"] = v
	}
	if v, ok := ctx.Value(ctxProfileKey).(string); ok && v != "" {
		out["X-Zen-Profile"] = v
	}
	return out
}

func MergeHeaders(ctx context.Context, explicit map[string]string) map[string]string {
	out := HeadersFromContext(ctx)
	for k, v := range explicit {
		out[k] = v
	}
	return out
}
