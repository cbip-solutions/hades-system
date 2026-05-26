// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/tool_registry.go
//
// ToolRegistry — dedup-enforcing canonical-tool-name registry. Implements
// inv-zen-165 (no two downstream MCPs may register the same canonical
// name). Lookup is the routing primitive Dispatcher (A-5) consults on
// every CallRequest; List is the response payload for the gateway's
// `tools/list` MCP method (server.go A-6).
package mcpgateway

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

type ToolMeta struct {
	Description string
	InputSchema map[string]any
}

type ToolEntry struct {
	Name    ToolName
	Handler Handler
	Meta    ToolMeta
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolEntry
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]ToolEntry)}
}

func (r *ToolRegistry) Register(name ToolName, h Handler, meta ToolMeta) error {
	if name.IsZero() {
		return fmt.Errorf("%w: zero ToolName", ErrToolNameInvalid)
	}
	if h == nil {
		return errors.New("mcpgateway: nil Handler in Register")
	}
	key := name.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[key]; exists {
		return fmt.Errorf("%w: %q already registered", ErrToolNameCollision, key)
	}
	r.tools[key] = ToolEntry{Name: name, Handler: h, Meta: copyToolMeta(meta)}
	return nil
}

func (r *ToolRegistry) Lookup(name ToolName) (ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name.String()]
	if !ok {
		return ToolEntry{}, false
	}
	return ToolEntry{Name: e.Name, Handler: e.Handler, Meta: copyToolMeta(e.Meta)}, true
}

func (r *ToolRegistry) Has(name ToolName) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name.String()]
	return ok
}

func (r *ToolRegistry) List() []ToolEntry {
	r.mu.RLock()
	out := make([]ToolEntry, 0, len(r.tools))
	for _, e := range r.tools {
		out = append(out, ToolEntry{
			Name:    e.Name,
			Handler: e.Handler,
			Meta:    copyToolMeta(e.Meta),
		})
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name.String() < out[j].Name.String()
	})
	return out
}

func copyToolMeta(m ToolMeta) ToolMeta {
	return ToolMeta{
		Description: m.Description,
		InputSchema: copyAnyMap(m.InputSchema),
	}
}

func copyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = copyAny(v)
	}
	return out
}

func copyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return copyAnyMap(t)
	case []any:
		if t == nil {
			return nil
		}
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = copyAny(e)
		}
		return out
	default:

		return v
	}
}

func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
