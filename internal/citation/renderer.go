// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

package citation

import (
	"errors"
	"fmt"
	"sync"
)

// ErrNoRendererMatch is returned by Dispatch when no platform-specific
// renderer matches AND no markdown fallback is registered. Substrate
// callers MUST register MarkdownFallback at init time to avoid this
// (daemon bootstrap does so via cmd/hades-ctld).
var ErrNoRendererMatch = errors.New("citation: no renderer registered (register MarkdownFallback as fallback)")

type Registry struct {
	mu        sync.RWMutex
	renderers map[string]Renderer
}

func NewRegistry() *Registry {
	return &Registry{
		renderers: make(map[string]Renderer),
	}
}

func (r *Registry) Register(rend Renderer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	platform := rend.Platform()
	if platform == "" {
		panic("citation: Renderer.Platform() returned empty string")
	}
	if _, exists := r.renderers[platform]; exists {
		panic(fmt.Sprintf("citation: duplicate Register for platform %q", platform))
	}
	r.renderers[platform] = rend
}

func (r *Registry) Lookup(platform string) (Renderer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rend, ok := r.renderers[platform]
	return rend, ok
}

func (r *Registry) Dispatch(env *Envelope, sess SessionContext) (string, error) {
	if env == nil {
		return "", errors.New("citation: Dispatch nil envelope")
	}

	r.mu.RLock()
	var rend Renderer
	var ok bool
	if sess.Platform != "" {
		rend, ok = r.renderers[sess.Platform]
	}
	if !ok {
		rend, ok = r.renderers["markdown"]
	}
	r.mu.RUnlock()

	if !ok {
		return "", ErrNoRendererMatch
	}
	return rend.Render(env, sess)
}
