// SPDX-License-Identifier: MIT
// internal/providers/ratecard.go
package providers

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

type RateCard struct {
	Provider                   string
	Model                      string
	InputUSDPerMillion         float64
	OutputUSDPerMillion        float64
	CacheReadUSDPerMillion     float64
	CacheCreationUSDPerMillion float64
	Source                     string
	UpdatedAt                  time.Time
}

func (c *RateCard) Validate() error {
	if c.Provider == "" {
		return errors.New("providers.RateCard: provider is empty")
	}
	if c.Model == "" {
		return fmt.Errorf("providers.RateCard(%s): model is empty", c.Provider)
	}
	if c.InputUSDPerMillion < 0 {
		return fmt.Errorf("providers.RateCard(%s/%s): input rate %v < 0",
			c.Provider, c.Model, c.InputUSDPerMillion)
	}
	if c.OutputUSDPerMillion < 0 {
		return fmt.Errorf("providers.RateCard(%s/%s): output rate %v < 0",
			c.Provider, c.Model, c.OutputUSDPerMillion)
	}
	if c.CacheReadUSDPerMillion < 0 {
		return fmt.Errorf("providers.RateCard(%s/%s): cache read rate %v < 0",
			c.Provider, c.Model, c.CacheReadUSDPerMillion)
	}
	if c.CacheCreationUSDPerMillion < 0 {
		return fmt.Errorf("providers.RateCard(%s/%s): cache creation rate %v < 0",
			c.Provider, c.Model, c.CacheCreationUSDPerMillion)
	}
	return nil
}

var ErrRateCardModelMismatch = errors.New("providers.RateCard: model mismatch (usage.ModelUsed != rate.Model)")

var ErrRateCardZero = errors.New("providers.RateCard: both input and output rates are zero (provider not configured)")

func (c *RateCard) Calculate(usage TierResponse) float64 {
	const million = 1_000_000.0
	cost := float64(usage.InputTokens) / million * c.InputUSDPerMillion
	cost += float64(usage.OutputTokens) / million * c.OutputUSDPerMillion
	cost += float64(usage.CacheReadTokens) / million * c.CacheReadUSDPerMillion
	cost += float64(usage.CacheCreationTokens) / million * c.CacheCreationUSDPerMillion
	return cost
}

func (c *RateCard) CalculateChecked(usage TierResponse) (float64, error) {

	if usage.ModelUsed != "" && usage.ModelUsed != c.Model {
		return 0, fmt.Errorf("%w: usage=%q rate=%q", ErrRateCardModelMismatch, usage.ModelUsed, c.Model)
	}
	if c.InputUSDPerMillion == 0 && c.OutputUSDPerMillion == 0 {
		return 0, ErrRateCardZero
	}
	return c.Calculate(usage), nil
}

func rateCardKey(provider, model string) string {
	return provider + ":" + model
}

type RateCardRegistry struct {
	mu    sync.RWMutex
	cards map[string]*RateCard
}

func NewRateCardRegistry() *RateCardRegistry {
	return &RateCardRegistry{cards: make(map[string]*RateCard)}
}

// Register adds a rate card. Returns an error if the card is invalid
// (Validate fails) or if a card already exists for (provider, model)
// — silent overwrite would mask provider-config typos.
//
// CONTRACT Register stores the *RateCard pointer directly. Callers
// MUST NOT mutate the pointed-to struct after Register — Lookup
// returns the same pointer, and any post-Register mutation would
// silently corrupt the registry's view ( cost ledger reads
// rates concurrently). To replace a rate, construct a new *RateCard
// and Register again under a fresh key (or via a future Update method
// that does the swap atomically).
func (r *RateCardRegistry) Register(c *RateCard) error {
	if c == nil {
		return errors.New("providers.RateCardRegistry.Register: card is nil")
	}
	if err := c.Validate(); err != nil {
		return fmt.Errorf("providers.RateCardRegistry.Register: %w", err)
	}
	key := rateCardKey(c.Provider, c.Model)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cards[key]; ok {
		return fmt.Errorf("providers.RateCardRegistry.Register: %s already registered", key)
	}
	r.cards[key] = c
	return nil
}

// Lookup returns the rate card for (provider, model). Returns an
// error wrapping ErrRateMissing if not found — dispatcher
// short-circuits with a 503 when the rate is missing rather than
// silently treating cost as zero.
//
// Composite-key analogue of Registry.Get: where TierBackend lookup is
// by single name, RateCard lookup needs both provider and model. The
// returned pointer is the exact value stored at Register/LoadFromConfig
// time; do not mutate it (see Register's CONTRACT).
func (r *RateCardRegistry) Lookup(provider, model string) (*RateCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := rateCardKey(provider, model)
	c, ok := r.cards[key]
	if !ok {
		return nil, fmt.Errorf("providers.RateCardRegistry.Lookup(%s): %w", key, ErrRateMissing)
	}
	return c, nil
}

func (r *RateCardRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.cards))
	for k := range r.cards {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type rateCardConfigFile struct {
	RateCards []rateCardConfigEntry `toml:"rate_cards"`
}

type rateCardConfigEntry struct {
	Provider                   string  `toml:"provider"`
	Model                      string  `toml:"model"`
	InputUSDPerMillion         float64 `toml:"input_usd_per_million"`
	OutputUSDPerMillion        float64 `toml:"output_usd_per_million"`
	CacheReadUSDPerMillion     float64 `toml:"cache_read_usd_per_million"`
	CacheCreationUSDPerMillion float64 `toml:"cache_creation_usd_per_million"`
}

func (r *RateCardRegistry) LoadFromConfig(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("providers.RateCardRegistry.LoadFromConfig(%s): %w", path, err)
	}
	var doc rateCardConfigFile
	if err := toml.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("providers.RateCardRegistry.LoadFromConfig(%s): toml: %w", path, err)
	}
	now := time.Now().UTC()
	staged := make([]*RateCard, 0, len(doc.RateCards))
	for _, e := range doc.RateCards {
		c := &RateCard{
			Provider:                   e.Provider,
			Model:                      e.Model,
			InputUSDPerMillion:         e.InputUSDPerMillion,
			OutputUSDPerMillion:        e.OutputUSDPerMillion,
			CacheReadUSDPerMillion:     e.CacheReadUSDPerMillion,
			CacheCreationUSDPerMillion: e.CacheCreationUSDPerMillion,
			Source:                     "config",
			UpdatedAt:                  now,
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("providers.RateCardRegistry.LoadFromConfig(%s): %w", path, err)
		}
		staged = append(staged, c)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range staged {
		key := rateCardKey(c.Provider, c.Model)
		if _, ok := r.cards[key]; ok {
			return fmt.Errorf("providers.RateCardRegistry.LoadFromConfig(%s): %s already registered (call before Register)",
				path, key)
		}
	}
	for _, c := range staged {
		r.cards[rateCardKey(c.Provider, c.Model)] = c
	}
	return nil
}
