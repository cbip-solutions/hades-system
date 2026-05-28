// SPDX-License-Identifier: MIT
// internal/providers/registry.go
package providers

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	backends map[string]TierBackend

	families map[string]string
	closed   bool

	ctorMu       sync.RWMutex
	constructors map[string]BackendConstructor
}

func NewRegistry() *Registry {
	return &Registry{
		backends: make(map[string]TierBackend),
		families: make(map[string]string),
	}
}

// Register attaches a backend under the given name. The name MUST be
// non-empty AND match backend.Name() — mismatch is rejected because
// downstream code looks backends up by both name and Backend.Name()
// interchangeably.
//
// Duplicate registration returns an error rather than overwriting:
// silent overwrite would mask provider-config typos that map two
// declarations to the same name.
func (r *Registry) Register(name string, b TierBackend) error {
	if name == "" {
		return errors.New("providers.Registry.Register: name is empty")
	}
	if b == nil {
		return fmt.Errorf("providers.Registry.Register(%q): backend is nil", name)
	}
	if b.Name() != name {
		return fmt.Errorf("providers.Registry.Register: name=%q but backend.Name()=%q (mismatch)",
			name, b.Name())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("providers.Registry.Register(%q): registry is closed", name)
	}
	if _, exists := r.backends[name]; exists {
		return fmt.Errorf("providers.Registry.Register(%q): already registered", name)
	}
	r.backends[name] = b
	return nil
}

func (r *Registry) Get(name string) (TierBackend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, fmt.Errorf("providers.Registry.Get(%q): %w (registry closed)",
			name, ErrBackendNotConfigured)
	}
	b, ok := r.backends[name]
	if !ok {
		return nil, fmt.Errorf("providers.Registry.Get(%q): %w", name, ErrBackendNotConfigured)
	}
	return b, nil
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.backends))
	for k := range r.backends {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var firstErr error
	for name, b := range r.backends {
		if err := b.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("providers.Registry.Close: backend %q: %w", name, err)
		}
	}
	r.backends = nil
	r.families = nil
	return firstErr
}

var validProviderTypes = map[string]struct{}{
	"anthropic-paygo": {},
	"gemini":          {},
	"ollama":          {},
	"openai-compat":   {},
}

var providerTypesNeedingAPIKey = map[string]struct{}{
	"anthropic-paygo": {},
	"gemini":          {},
	"openai-compat":   {},
}

type ProviderConfig struct {
	Name           string            `toml:"name"`
	Type           string            `toml:"type"`
	Endpoint       string            `toml:"endpoint"`
	Model          string            `toml:"model"`
	Family         string            `toml:"family"`
	APIKeyKeychain string            `toml:"api_key_keychain"`
	Headers        map[string]string `toml:"headers"`
	RateCardName   string            `toml:"rate_card"`
}

func (c ProviderConfig) Validate() error {
	if c.Name == "" {
		return errors.New("providers.ProviderConfig: name is empty")
	}
	if c.Type == "" {
		return fmt.Errorf("providers.ProviderConfig(%q): type is empty", c.Name)
	}
	if _, ok := validProviderTypes[c.Type]; !ok {
		return fmt.Errorf("providers.ProviderConfig(%q): type %q invalid (want one of: anthropic-paygo, gemini, ollama, openai-compat)",
			c.Name, c.Type)
	}
	if c.Endpoint == "" {
		return fmt.Errorf("providers.ProviderConfig(%q): endpoint is empty", c.Name)
	}

	if !(strings.HasPrefix(c.Endpoint, "http://") || strings.HasPrefix(c.Endpoint, "https://")) {
		return fmt.Errorf("providers.ProviderConfig(%q): endpoint scheme must be http:// or https:// (got %q)",
			c.Name, c.Endpoint)
	}
	if c.Model == "" {
		return fmt.Errorf("providers.ProviderConfig(%q): model is empty", c.Name)
	}
	if c.Family == "" {
		return fmt.Errorf("providers.ProviderConfig(%q): family is empty (invariant family-disjoint key)", c.Name)
	}
	if _, needs := providerTypesNeedingAPIKey[c.Type]; needs && c.APIKeyKeychain == "" {
		return fmt.Errorf("providers.ProviderConfig(%q): api_key_keychain is required for type=%q",
			c.Name, c.Type)
	}
	return nil
}

type BackendConstructor func(cfg ProviderConfig) (TierBackend, error)

func (r *Registry) RegisterConstructor(providerType string, ctor BackendConstructor) error {
	if _, ok := validProviderTypes[providerType]; !ok {
		return fmt.Errorf("providers.Registry.RegisterConstructor: unknown type %q", providerType)
	}
	if ctor == nil {
		return fmt.Errorf("providers.Registry.RegisterConstructor(%q): ctor is nil", providerType)
	}
	r.ctorMu.Lock()
	defer r.ctorMu.Unlock()
	if r.constructors == nil {
		r.constructors = make(map[string]BackendConstructor)
	}
	r.constructors[providerType] = ctor
	return nil
}

func (r *Registry) RegisterFromConfig(cfg ProviderConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("providers.Registry.RegisterFromConfig: %w", err)
	}
	r.ctorMu.RLock()
	ctor, ok := r.constructors[cfg.Type]
	r.ctorMu.RUnlock()
	if !ok {
		return fmt.Errorf("providers.Registry.RegisterFromConfig(%q): no constructor for type=%q", cfg.Name, cfg.Type)
	}
	b, err := ctor(cfg)
	if err != nil {
		return fmt.Errorf("providers.Registry.RegisterFromConfig(%q): constructor: %w", cfg.Name, err)
	}
	if err := r.Register(cfg.Name, b); err != nil {
		return err
	}

	r.mu.Lock()
	if r.families != nil {
		r.families[cfg.Name] = cfg.Family
	}
	r.mu.Unlock()
	return nil
}

// Families returns a defensive copy of the providerName -> family map for
// every backend registered via RegisterFromConfig. Backends registered via
// the plain Register path (no ProviderConfig — e.g. the bypass-client
// adapter) are absent from the map; callers treat an absent name as
// "family unknown".
//
// invariant: the audit MCP's family-disjoint reviewer pool is built from
// this live map (was a static doctrine TOML list). A reviewer<->worker
// pair sharing a family is rejected at pair-selection.
//
// Concurrency takes r.mu.RLock for the duration of the copy; concurrent
// readers do not block each other. The returned map is a fresh allocation;
// callers may mutate it freely without affecting the registry. After
// Close, returns an empty (non-nil) map.
func (r *Registry) Families() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.families))
	for k, v := range r.families {
		out[k] = v
	}
	return out
}
