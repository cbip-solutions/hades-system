package providers

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRateCardCalculateAnthropicOpus(t *testing.T) {

	c := &RateCard{
		Provider:                   "anthropic-paygo",
		Model:                      "claude-opus-4-6",
		InputUSDPerMillion:         15.0,
		OutputUSDPerMillion:        75.0,
		CacheReadUSDPerMillion:     1.875,
		CacheCreationUSDPerMillion: 18.75,
	}
	usage := TierResponse{
		InputTokens:         100_000,
		OutputTokens:        50_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 80_000,
	}
	got := c.Calculate(usage)

	want := 7.125
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Calculate = %.6f, want %.6f", got, want)
	}
}

func TestRateCardCalculateZeroUsage(t *testing.T) {
	c := &RateCard{
		Provider:            "gemini",
		Model:               "gemini-3-pro",
		InputUSDPerMillion:  1.25,
		OutputUSDPerMillion: 5.00,
	}
	if got := c.Calculate(TierResponse{}); got != 0 {
		t.Errorf("Calculate(zero usage) = %.6f, want 0", got)
	}
}

func TestRateCardCalculateOllamaFree(t *testing.T) {

	c := &RateCard{Provider: "ollama", Model: "qwen3:32b"}
	usage := TierResponse{InputTokens: 1_000_000, OutputTokens: 500_000}
	if got := c.Calculate(usage); got != 0 {
		t.Errorf("Calculate(ollama) = %.6f, want 0", got)
	}
}

func TestRateCardCalculateNegativeRejected(t *testing.T) {

	c := &RateCard{
		Provider:           "x",
		Model:              "y",
		InputUSDPerMillion: -1.0,
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate negative rate err = nil, want error")
	}
}

func TestRateCardValidateOK(t *testing.T) {
	c := &RateCard{
		Provider:            "anthropic-paygo",
		Model:               "claude-opus-4-6",
		InputUSDPerMillion:  15.0,
		OutputUSDPerMillion: 75.0,
		Source:              "config",
		UpdatedAt:           time.Now(),
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate ok rate err = %v", err)
	}
}

func TestRateCardValidateRequiresProviderModel(t *testing.T) {
	for _, c := range []*RateCard{
		{Model: "x", InputUSDPerMillion: 1, OutputUSDPerMillion: 1},
		{Provider: "x", InputUSDPerMillion: 1, OutputUSDPerMillion: 1},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%+v) err = nil, want error", c)
		}
	}
}

func TestRateCardRegistryLookup(t *testing.T) {
	r := NewRateCardRegistry()
	c := &RateCard{
		Provider:            "anthropic-paygo",
		Model:               "claude-opus-4-6",
		InputUSDPerMillion:  15.0,
		OutputUSDPerMillion: 75.0,
	}
	if err := r.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Lookup("anthropic-paygo", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.InputUSDPerMillion != 15.0 {
		t.Errorf("Input rate = %v, want 15", got.InputUSDPerMillion)
	}
}

func TestRateCardRegistryLookupMissing(t *testing.T) {
	r := NewRateCardRegistry()
	_, err := r.Lookup("nonexistent", "model")
	if err == nil {
		t.Fatal("Lookup missing err = nil, want error")
	}
	if !errors.Is(err, ErrRateMissing) {
		t.Errorf("Lookup missing err = %v, want ErrRateMissing", err)
	}
}

func TestRateCardRegistryDuplicateRejected(t *testing.T) {
	r := NewRateCardRegistry()
	c1 := &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: 1, OutputUSDPerMillion: 1}
	c2 := &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: 2, OutputUSDPerMillion: 2}
	if err := r.Register(c1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(c2); err == nil {
		t.Error("Register duplicate err = nil, want error")
	}
}

func TestRateCardRegistryListSorted(t *testing.T) {
	r := NewRateCardRegistry()
	for _, pm := range []struct{ p, m string }{
		{"z-prov", "m1"}, {"a-prov", "m2"}, {"m-prov", "m3"},
	} {
		_ = r.Register(&RateCard{Provider: pm.p, Model: pm.m,
			InputUSDPerMillion: 1, OutputUSDPerMillion: 1})
	}
	got := r.List()
	if len(got) != 3 {
		t.Fatalf("List len = %d, want 3", len(got))
	}

	if got[0] != "a-prov:m2" || got[1] != "m-prov:m3" || got[2] != "z-prov:m1" {
		t.Errorf("List = %v, not sorted by composite key", got)
	}
}

func TestRateCardRegistryLoadFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.toml")
	body := `
[[rate_cards]]
provider = "anthropic-paygo"
model = "claude-opus-4-6"
input_usd_per_million = 15.0
output_usd_per_million = 75.0
cache_read_usd_per_million = 1.875
cache_creation_usd_per_million = 18.75

[[rate_cards]]
provider = "gemini"
model = "gemini-3-pro"
input_usd_per_million = 1.25
output_usd_per_million = 5.00
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRateCardRegistry()
	if err := r.LoadFromConfig(path); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	if got := r.List(); len(got) != 2 {
		t.Errorf("List len = %d, want 2", len(got))
	}
	c, err := r.Lookup("gemini", "gemini-3-pro")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if c.OutputUSDPerMillion != 5.00 {
		t.Errorf("Output rate = %v, want 5.00", c.OutputUSDPerMillion)
	}
	if c.Source != "config" {
		t.Errorf("Source = %q, want 'config'", c.Source)
	}
}

func TestRateCardRegistryLoadFromConfigMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("this is not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRateCardRegistry()
	if err := r.LoadFromConfig(path); err == nil {
		t.Error("LoadFromConfig malformed err = nil, want error")
	}
}

func TestRateCardRegistryLoadFromConfigInvalidEntry(t *testing.T) {

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	body := `
[[rate_cards]]
provider = "x"
model = "y"
input_usd_per_million = -1.0
output_usd_per_million = 1.0
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRateCardRegistry()
	if err := r.LoadFromConfig(path); err == nil {
		t.Error("LoadFromConfig invalid entry err = nil, want error")
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("registry not empty after rejected load: %v", got)
	}
}

func TestRateCardValidateRejectsAllNegativeRates(t *testing.T) {

	cases := []struct {
		name string
		card *RateCard
		want string
	}{
		{
			name: "negative input",
			card: &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: -0.5},
			want: "input rate",
		},
		{
			name: "negative output",
			card: &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: 1, OutputUSDPerMillion: -0.5},
			want: "output rate",
		},
		{
			name: "negative cache read",
			card: &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: 1, OutputUSDPerMillion: 1,
				CacheReadUSDPerMillion: -0.5},
			want: "cache read rate",
		},
		{
			name: "negative cache creation",
			card: &RateCard{Provider: "x", Model: "y", InputUSDPerMillion: 1, OutputUSDPerMillion: 1,
				CacheCreationUSDPerMillion: -0.5},
			want: "cache creation rate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.card.Validate()
			if err == nil {
				t.Fatalf("Validate(%+v) err = nil, want error containing %q", tc.card, tc.want)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestRateCardRegistryLoadFromConfigCollidesWithPriorRegister(t *testing.T) {
	// Atomic load must reject TOML entries that collide with cards
	// already in the registry. Prior entries MUST survive the failed
	// load (atomic semantics: no partial registration).
	r := NewRateCardRegistry()
	prior := &RateCard{
		Provider:            "anthropic-paygo",
		Model:               "claude-opus-4-6",
		InputUSDPerMillion:  15.0,
		OutputUSDPerMillion: 75.0,
		Source:              "hardcoded",
	}
	if err := r.Register(prior); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.toml")
	body := `
[[rate_cards]]
provider = "anthropic-paygo"
model = "claude-opus-4-6"
input_usd_per_million = 99.0
output_usd_per_million = 99.0
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := r.LoadFromConfig(path)
	if err == nil {
		t.Fatal("LoadFromConfig with colliding key err = nil, want error")
	}
	if !contains(err.Error(), "already registered") {
		t.Errorf("err = %v, want substring 'already registered'", err)
	}
	// Atomic semantics: prior Register'd card MUST still be present
	// AND unmodified after the failed LoadFromConfig.
	got, err := r.Lookup("anthropic-paygo", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("prior card lost after failed LoadFromConfig: %v", err)
	}
	if got.InputUSDPerMillion != 15.0 {
		t.Errorf("prior card overwritten: input rate = %v, want 15.0 (atomic broken)", got.InputUSDPerMillion)
	}
	if got.Source != "hardcoded" {
		t.Errorf("prior card metadata changed: Source = %q, want 'hardcoded'", got.Source)
	}
}

func TestRateCardRegistryRegisterNil(t *testing.T) {

	r := NewRateCardRegistry()
	err := r.Register(nil)
	if err == nil {
		t.Fatal("Register(nil) err = nil, want error")
	}
	if !contains(err.Error(), "card is nil") {
		t.Errorf("err = %v, want substring 'card is nil'", err)
	}
}

func TestRateCardRegistryRegisterInvalid(t *testing.T) {
	// Register MUST run Validate before mutating the map. A card with
	// an empty Provider/Model or a negative rate is rejected and the
	// registry remains untouched.
	r := NewRateCardRegistry()
	bad := &RateCard{Provider: "", Model: "y", InputUSDPerMillion: 1, OutputUSDPerMillion: 1}
	err := r.Register(bad)
	if err == nil {
		t.Fatal("Register(invalid) err = nil, want error")
	}
	if !contains(err.Error(), "provider is empty") {
		t.Errorf("err = %v, want substring 'provider is empty'", err)
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("registry not empty after rejected Register: %v", got)
	}
}

func TestRateCardRegistryLoadFromConfigMissingFile(t *testing.T) {
	r := NewRateCardRegistry()
	err := r.LoadFromConfig("/nonexistent/path/providers.toml")
	if err == nil {
		t.Fatal("LoadFromConfig missing file err = nil, want error")
	}
}

func TestRateCardCalculateCheckedMatchingModelSucceeds(t *testing.T) {

	card := &RateCard{
		Provider: "anthropic-paygo", Model: "claude-opus-4-6",
		InputUSDPerMillion: 15.00, OutputUSDPerMillion: 75.00,
	}
	usage := TierResponse{
		ModelUsed:   "claude-opus-4-6",
		InputTokens: 1_000_000, OutputTokens: 500_000,
	}
	got, err := card.CalculateChecked(usage)
	if err != nil {
		t.Fatalf("CalculateChecked: %v", err)
	}

	if got < 52.499 || got > 52.501 {
		t.Errorf("got %f, want 52.5", got)
	}
}

func TestRateCardCalculateCheckedEmptyModelUsedTrusted(t *testing.T) {

	card := &RateCard{
		Provider: "anthropic-paygo", Model: "claude-haiku-4-6",
		InputUSDPerMillion: 1.00, OutputUSDPerMillion: 5.00,
	}
	usage := TierResponse{
		ModelUsed:    "",
		InputTokens:  100_000,
		OutputTokens: 50_000,
	}
	got, err := card.CalculateChecked(usage)
	if err != nil {
		t.Fatalf("CalculateChecked: empty ModelUsed should be trusted: %v", err)
	}

	if got < 0.349 || got > 0.351 {
		t.Errorf("got %f, want 0.35", got)
	}
}

func TestRateCardCalculateCheckedModelMismatch(t *testing.T) {

	card := &RateCard{
		Provider: "anthropic-paygo", Model: "claude-opus-4-6",
		InputUSDPerMillion: 15.00, OutputUSDPerMillion: 75.00,
	}
	usage := TierResponse{
		ModelUsed:   "claude-haiku-4-6",
		InputTokens: 1, OutputTokens: 1,
	}
	_, err := card.CalculateChecked(usage)
	if !errors.Is(err, ErrRateCardModelMismatch) {
		t.Errorf("want ErrRateCardModelMismatch, got %v", err)
	}

	if msg := err.Error(); !strings.Contains(msg, "claude-haiku-4-6") || !strings.Contains(msg, "claude-opus-4-6") {
		t.Errorf("error message missing model context: %v", err)
	}
}

func TestRateCardCalculateCheckedModelUsedMatchesRateCardModel(t *testing.T) {

	card := &RateCard{
		Provider: "gemini", Model: "gemini-flash",
		InputUSDPerMillion: 0.10, OutputUSDPerMillion: 0.40,
	}
	usage := TierResponse{
		ModelUsed:   "gemini-flash",
		InputTokens: 1_000_000, OutputTokens: 500_000,
	}
	got, err := card.CalculateChecked(usage)
	if err != nil {
		t.Fatalf("CalculateChecked should succeed when ModelUsed matches rate card: %v", err)
	}

	if got < 0.299 || got > 0.301 {
		t.Errorf("got %f, want 0.30", got)
	}
}

func TestRateCardCalculateCheckedZeroRatesRejected(t *testing.T) {

	card := &RateCard{
		Provider: "ghost-provider", Model: "ghost-model",
		InputUSDPerMillion: 0, OutputUSDPerMillion: 0,
	}
	usage := TierResponse{
		ModelUsed:   "ghost-model",
		InputTokens: 1_000_000, OutputTokens: 1_000_000,
	}
	_, err := card.CalculateChecked(usage)
	if !errors.Is(err, ErrRateCardZero) {
		t.Errorf("want ErrRateCardZero, got %v", err)
	}
}

func TestRateCardCalculateCheckedZeroOnlyOneRateOK(t *testing.T) {

	card := &RateCard{
		Provider: "promo", Model: "promo-model",
		InputUSDPerMillion: 0, OutputUSDPerMillion: 1.00,
	}
	usage := TierResponse{
		ModelUsed:   "promo-model",
		InputTokens: 1_000_000, OutputTokens: 1_000_000,
	}
	got, err := card.CalculateChecked(usage)
	if err != nil {
		t.Fatalf("CalculateChecked rejects asymmetric rate (should accept): %v", err)
	}

	if got < 0.999 || got > 1.001 {
		t.Errorf("got %f, want 1.00", got)
	}
}

func TestRateCardCalculateCheckedDoesNotChangeCalculateBehavior(t *testing.T) {

	card := &RateCard{
		Provider: "p", Model: "m",
		InputUSDPerMillion: 0, OutputUSDPerMillion: 0,
	}

	usage := TierResponse{InputTokens: 100, OutputTokens: 100}
	if got := card.Calculate(usage); got != 0 {
		t.Errorf("Calculate should return 0 silently for zero rates; got %f", got)
	}
}
