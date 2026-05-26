package providers

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

type stubBackend struct {
	name       string
	tier       Tier
	closed     bool
	closeCount int
	closedMu   sync.Mutex
	probeErr   error
	forwardErr error
	closeErr   error
}

func (s *stubBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {
	if s.forwardErr != nil {
		return nil, s.forwardErr
	}
	return &TierResponse{Status: 200, TierUsed: s.tier, ModelUsed: req.Model}, nil
}
func (s *stubBackend) Probe(ctx context.Context) error { return s.probeErr }
func (s *stubBackend) Close() error {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	s.closed = true
	s.closeCount++
	return s.closeErr
}
func (s *stubBackend) Name() string                   { return s.name }
func (s *stubBackend) Capabilities() TierCapabilities { return TierCapabilities{} }
func (s *stubBackend) Tier() Tier                     { return s.tier }

var _ TierBackend = (*stubBackend)(nil)

func TestRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Errorf("empty registry List() = %v, want []", got)
	}
	if _, err := r.Get("missing"); !errors.Is(err, ErrBackendNotConfigured) {
		t.Errorf("Get(missing) err = %v, want ErrBackendNotConfigured", err)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	b := &stubBackend{name: "anthropic-paygo", tier: TierAnthropicPAYG}
	if err := r.Register("anthropic-paygo", b); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Get("anthropic-paygo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "anthropic-paygo" {
		t.Errorf("got.Name() = %q", got.Name())
	}
}

func TestRegistryDuplicateRejected(t *testing.T) {
	r := NewRegistry()
	b1 := &stubBackend{name: "gemini", tier: TierGemini}
	b2 := &stubBackend{name: "gemini", tier: TierGemini}
	if err := r.Register("gemini", b1); err != nil {
		t.Fatal(err)
	}
	err := r.Register("gemini", b2)
	if err == nil {
		t.Fatal("Register dup err = nil, want error")
	}
}

func TestRegistryRegisterEmptyName(t *testing.T) {
	r := NewRegistry()
	err := r.Register("", &stubBackend{tier: TierGemini})
	if err == nil {
		t.Fatal("Register(\"\") err = nil, want error")
	}
}

func TestRegistryRegisterNilBackend(t *testing.T) {
	r := NewRegistry()
	err := r.Register("x", nil)
	if err == nil {
		t.Fatal("Register(nil) err = nil, want error")
	}
}

func TestRegistryRegisterMismatchedName(t *testing.T) {

	r := NewRegistry()
	b := &stubBackend{name: "real-name", tier: TierGemini}
	err := r.Register("different-key", b)
	if err == nil {
		t.Fatal("Register with mismatched name err = nil, want error")
	}
}

func TestRegistryListSorted(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"zeta", "alpha", "mu"} {
		if err := r.Register(name, &stubBackend{name: name, tier: TierGenericOpenAICompat}); err != nil {
			t.Fatal(err)
		}
	}
	got := r.List()
	if !sort.StringsAreSorted(got) {
		t.Errorf("List() = %v, want sorted", got)
	}
	if len(got) != 3 {
		t.Errorf("List() len = %d, want 3", len(got))
	}
}

func TestRegistryClose(t *testing.T) {
	r := NewRegistry()
	b1 := &stubBackend{name: "a", tier: TierGemini}
	b2 := &stubBackend{name: "b", tier: TierOllama}
	for _, b := range []*stubBackend{b1, b2} {
		if err := r.Register(b.name, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !b1.closed || !b2.closed {
		t.Errorf("Close did not propagate: b1=%v b2=%v", b1.closed, b2.closed)
	}

	if _, err := r.Get("a"); !errors.Is(err, ErrBackendNotConfigured) {
		t.Errorf("Get after Close err = %v, want ErrBackendNotConfigured", err)
	}

	if err := r.Register("c", &stubBackend{name: "c", tier: TierGemini}); err == nil {
		t.Errorf("Register after Close err = nil, want error")
	}
}

func TestRegistryCloseIdempotent(t *testing.T) {

	r := NewRegistry()
	b := &stubBackend{name: "a", tier: TierGemini}
	if err := r.Register("a", b); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if got := b.closeCount; got != 1 {
		t.Fatalf("after first Close, b.closeCount = %d, want 1", got)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v, want nil (idempotent)", err)
	}
	// Backend.Close MUST NOT have been called a second time — the
	// early-return prevents re-iteration of the (already nilled) map.
	if got := b.closeCount; got != 1 {
		t.Errorf("after second Close, b.closeCount = %d, want 1 (no double-close)", got)
	}
}

func TestRegistryCloseErrorPropagation(t *testing.T) {

	r := NewRegistry()
	wantErr := errors.New("synthetic backend close failure")
	b1 := &stubBackend{name: "a", tier: TierGemini, closeErr: wantErr}
	b2 := &stubBackend{name: "b", tier: TierOllama}
	for _, b := range []*stubBackend{b1, b2} {
		if err := r.Register(b.name, b); err != nil {
			t.Fatal(err)
		}
	}
	err := r.Close()
	if err == nil {
		t.Fatal("Close() = nil, want wrapped backend error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Close() err = %v, want errors.Is to match wantErr", err)
	}
	// Best-effort propagation: b2 (no error) MUST still be closed even
	// though b1 failed first. Without this, registry shutdown could leak
	// resources on remaining backends after a single Close failure.
	if !b1.closed {
		t.Error("b1 not marked closed despite firing close failure")
	}
	if !b2.closed {
		t.Error("b2 not closed — best-effort propagation broken (first error stopped iteration)")
	}
}

func TestRegistryConcurrentRegister(t *testing.T) {

	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := stringFromInt(i)
			if err := r.Register(name, &stubBackend{name: name, tier: TierGenericOpenAICompat}); err != nil {
				t.Errorf("goroutine %d Register: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := r.List(); len(got) != 50 {
		t.Errorf("List() len = %d, want 50", len(got))
	}

	for i := 0; i < 50; i++ {
		name := stringFromInt(i)
		b, err := r.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if b.Name() != name {
			t.Errorf("Get(%q).Name() = %q", name, b.Name())
		}
		if b.Tier() != TierGenericOpenAICompat {
			t.Errorf("Get(%q).Tier() = %v, want TierGenericOpenAICompat", name, b.Tier())
		}
	}
}

func stringFromInt(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "n0"
	}
	out := []byte{}
	for i > 0 {
		out = append([]byte{digits[i%10]}, out...)
		i /= 10
	}
	return "n" + string(out)
}

func TestProviderConfigValidateOK(t *testing.T) {
	cases := []ProviderConfig{
		{Name: "anthropic-paygo", Type: "anthropic-paygo", Endpoint: "https://api.anthropic.com",
			Model: "claude-opus-4-6", Family: "anthropic", APIKeyKeychain: "zen.anthropic.paygo", RateCardName: "anthropic-opus"},
		{Name: "gemini-3-pro", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
			Model: "gemini-3-pro", Family: "google", APIKeyKeychain: "zen.gemini", RateCardName: "gemini-3-pro"},
		{Name: "ollama-local", Type: "ollama", Endpoint: "http://127.0.0.1:11434", Model: "qwen3:32b", Family: "ollama"},
		{Name: "deepseek-r1", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
			Model: "deepseek-r1", Family: "deepseek", APIKeyKeychain: "zen.deepseek", RateCardName: "deepseek-r1"},
	}
	for _, c := range cases {
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", c, err)
		}
	}
}

func TestProviderConfigValidateFails(t *testing.T) {
	cases := []struct {
		c    ProviderConfig
		want string
	}{
		{ProviderConfig{}, "name"},
		{ProviderConfig{Name: "x"}, "type"},
		{ProviderConfig{Name: "x", Type: "frobnicate"}, "type"},
		{ProviderConfig{Name: "x", Type: "anthropic-paygo"}, "endpoint"},
		{ProviderConfig{Name: "x", Type: "anthropic-paygo", Endpoint: "https://api.anthropic.com"}, "model"},

		{ProviderConfig{Name: "x", Type: "anthropic-paygo", Endpoint: "https://api.anthropic.com",
			Model: "opus", Family: "test-family"}, "api_key_keychain"},

		{ProviderConfig{Name: "x", Type: "ollama", Endpoint: "ftp://nope", Model: "qwen3"}, "endpoint scheme"},
	}
	for _, tc := range cases {
		err := tc.c.Validate()
		if err == nil {
			t.Errorf("Validate(%+v) = nil, want error containing %q", tc.c, tc.want)
			continue
		}
		if !contains(err.Error(), tc.want) {
			t.Errorf("Validate(%+v) err = %v, want substring %q", tc.c, err, tc.want)
		}
	}
}

func TestRegisterFromConfigUnknownType(t *testing.T) {
	r := NewRegistry()
	err := r.RegisterFromConfig(ProviderConfig{
		Name: "x", Type: "frobnicate", Endpoint: "https://x", Model: "m",
	})
	if err == nil {
		t.Fatal("RegisterFromConfig unknown type err = nil")
	}
}

func TestRegisterFromConfigConstructorMissing(t *testing.T) {

	r := NewRegistry()
	cfg := ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo",
		Endpoint: "https://api.anthropic.com", Model: "claude-opus-4-6",
		Family: "anthropic", APIKeyKeychain: "zen.anthropic.paygo", RateCardName: "anthropic-opus",
	}
	err := r.RegisterFromConfig(cfg)
	if err == nil {
		t.Fatal("err = nil, want constructor-missing error")
	}
	if !contains(err.Error(), "no constructor") {
		t.Errorf("err = %v, want substring 'no constructor'", err)
	}
}

func TestRegisterConstructorUnknownType(t *testing.T) {

	r := NewRegistry()
	err := r.RegisterConstructor("frobnicate", func(cfg ProviderConfig) (TierBackend, error) {
		return &stubBackend{name: cfg.Name, tier: TierGemini}, nil
	})
	if err == nil {
		t.Fatal("RegisterConstructor unknown type err = nil, want error")
	}
	if !contains(err.Error(), "unknown type") {
		t.Errorf("err = %v, want substring 'unknown type'", err)
	}
}

func TestRegisterConstructorNilCtor(t *testing.T) {

	r := NewRegistry()
	err := r.RegisterConstructor("anthropic-paygo", nil)
	if err == nil {
		t.Fatal("RegisterConstructor(nil) err = nil, want error")
	}
	if !contains(err.Error(), "ctor is nil") {
		t.Errorf("err = %v, want substring 'ctor is nil'", err)
	}
}

func TestRegisterFromConfigConstructorError(t *testing.T) {

	r := NewRegistry()
	wantErr := errors.New("synthetic ctor failure (keychain unavailable)")
	if err := r.RegisterConstructor("anthropic-paygo", func(cfg ProviderConfig) (TierBackend, error) {
		return nil, wantErr
	}); err != nil {
		t.Fatal(err)
	}
	cfg := ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo",
		Endpoint: "https://api.anthropic.com", Model: "claude-opus-4-6",
		Family: "anthropic", APIKeyKeychain: "zen.anthropic.paygo", RateCardName: "anthropic-opus",
	}
	err := r.RegisterFromConfig(cfg)
	if err == nil {
		t.Fatal("RegisterFromConfig with failing ctor err = nil, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want errors.Is to wrap synthetic ctor error", err)
	}

	if got := r.List(); len(got) != 0 {
		t.Errorf("registry not empty after failed ctor: %v", got)
	}
}

func TestRegistryRegisterConstructor(t *testing.T) {

	r := NewRegistry()
	r.RegisterConstructor("anthropic-paygo", func(cfg ProviderConfig) (TierBackend, error) {
		return &stubBackend{name: cfg.Name, tier: TierAnthropicPAYG}, nil
	})
	cfg := ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo",
		Endpoint: "https://api.anthropic.com", Model: "claude-opus-4-6",
		Family: "anthropic", APIKeyKeychain: "zen.anthropic.paygo", RateCardName: "anthropic-opus",
	}
	if err := r.RegisterFromConfig(cfg); err != nil {
		t.Fatalf("RegisterFromConfig: %v", err)
	}
	b, err := r.Get("anthropic-paygo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if b.Tier() != TierAnthropicPAYG {
		t.Errorf("Tier() = %v, want TierAnthropicPAYG", b.Tier())
	}
}

func TestProviderConfigValidateFamilyRequired(t *testing.T) {
	cfg := ProviderConfig{
		Name:           "deepseek-direct",
		Type:           "openai-compat",
		Endpoint:       "https://api.deepseek.com",
		Model:          "deepseek-chat",
		Family:         "",
		APIKeyKeychain: "zen-swarm/deepseek",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a ProviderConfig with empty Family")
	}
	if !strings.Contains(err.Error(), "family") {
		t.Errorf("error %q does not mention the family field", err.Error())
	}
}

func TestProviderConfigValidateFamilyAccepted(t *testing.T) {
	cfg := ProviderConfig{
		Name:           "deepseek-direct",
		Type:           "openai-compat",
		Endpoint:       "https://api.deepseek.com",
		Model:          "deepseek-chat",
		Family:         "deepseek",
		APIKeyKeychain: "zen-swarm/deepseek",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed config: %v", err)
	}
}

func TestRegistry_FamiliesFromConfig(t *testing.T) {
	reg := NewRegistry()
	if err := reg.RegisterConstructor("ollama", func(cfg ProviderConfig) (TierBackend, error) {
		return NewOllamaBackend(cfg)
	}); err != nil {
		t.Fatalf("RegisterConstructor: %v", err)
	}
	if err := reg.RegisterFromConfig(ProviderConfig{
		Name: "ollama-qwen-coder", Type: "ollama", Endpoint: "http://localhost:11434",
		Model: "qwen2.5-coder:32b", Family: "local-qwen",
	}); err != nil {
		t.Fatalf("RegisterFromConfig: %v", err)
	}
	fams := reg.Families()
	if fams["ollama-qwen-coder"] != "local-qwen" {
		t.Errorf("Families()[ollama-qwen-coder] = %q, want local-qwen", fams["ollama-qwen-coder"])
	}

	fams["ollama-qwen-coder"] = "tampered"
	if reg.Families()["ollama-qwen-coder"] != "local-qwen" {
		t.Error("Families() must return a defensive copy")
	}
}

func TestRegistry_FamiliesAbsentForPlainRegister(t *testing.T) {
	reg := NewRegistry()
	stub := &stubBackend{name: "bypass-stub", tier: TierInHouse}
	if err := reg.Register("bypass-stub", stub); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fams := reg.Families()
	if _, present := fams["bypass-stub"]; present {
		t.Errorf("Families() must not synthesise an entry for plain Register; got map=%v", fams)
	}

	if _, err := reg.Get("bypass-stub"); err != nil {
		t.Fatalf("Get(bypass-stub) after plain Register: %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
