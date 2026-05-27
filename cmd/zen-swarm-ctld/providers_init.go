// SPDX-License-Identifier: MIT
// cmd/zen-swarm-ctld/providers_init.go
//
// BuildProviderRegistry constructs the providers.Registry from
// providers.toml — completing never-shipped registry
// build. It is the cross-package wiring point (orchestrator_wiring.go's
// sibling) that:
//
// 1. registers a BackendConstructor for every provider type, each a
// closure capturing the production keychain.SystemResolver{} so the
// providers.BackendConstructor signature stays Keychain-free
// (master contract C5);
// 2. loads providers.toml and registers each declared provider —
// a provider whose Keychain key is absent is registered as a
// disabled stub (the cascade skips the provider by name and
// `zen providers verify` surfaces it) rather than failing the
// whole daemon.
//
// invariant cascade-completeness is intentionally NOT enforced here:
// the "bypass" backend is registered by buildOrchestrator (master C5)
// AFTER BuildProviderRegistry returns, so a gate that runs at registry
// construction time would always reject any profile that names "bypass"
// (the default orchestrator cascade) on operators without bypass-config.
// The authoritative cascade-completeness gate lives in
// verifyCascadeCompleteness (orchestrator_wiring.go), invoked from
// main.go after buildOrchestrator wires bypass.
//
// invariant: cross-package wiring lives here, in package main — no
// internal/* package wires providers + config + keychain together.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type disabledBackend struct {
	name   string
	tier   providers.Tier
	reason string
}

func (d *disabledBackend) Name() string         { return d.name }
func (d *disabledBackend) Tier() providers.Tier { return d.tier }
func (d *disabledBackend) Capabilities() providers.TierCapabilities {
	return providers.TierCapabilities{}
}
func (d *disabledBackend) Close() error { return nil }
func (d *disabledBackend) Probe(_ context.Context) error {
	return fmt.Errorf("provider %q disabled: %s: %w", d.name, d.reason, providers.ErrTierUnavailable)
}
func (d *disabledBackend) Forward(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
	return nil, fmt.Errorf("provider %q disabled: %s: %w", d.name, d.reason, providers.ErrTierUnavailable)
}

func providersTierRequestStub() providers.TierRequest {
	return providers.TierRequest{Body: []byte(`{}`), Model: "stub"}
}

func tierForProviderType(t string) providers.Tier {
	switch t {
	case "anthropic-paygo":
		return providers.TierAnthropicPAYG
	case "gemini":
		return providers.TierGemini
	case "ollama":
		return providers.TierOllama
	case "openai-compat":
		return providers.TierGenericOpenAICompat
	default:
		return providers.TierPause
	}
}

func registerConstructors(reg *providers.Registry, kc keychain.Resolver) error {
	ctors := map[string]providers.BackendConstructor{
		"anthropic-paygo": func(cfg providers.ProviderConfig) (providers.TierBackend, error) {
			return providers.NewAnthropicPaygoBackend(cfg, kc)
		},
		"gemini": func(cfg providers.ProviderConfig) (providers.TierBackend, error) {
			return providers.NewGeminiBackend(cfg, kc)
		},
		"openai-compat": func(cfg providers.ProviderConfig) (providers.TierBackend, error) {
			return providers.NewOpenAICompatBackend(cfg, kc)
		},

		"ollama": func(cfg providers.ProviderConfig) (providers.TierBackend, error) {
			return providers.NewOllamaBackend(cfg)
		},
	}
	for typ, ctor := range ctors {
		if err := reg.RegisterConstructor(typ, ctor); err != nil {
			return fmt.Errorf("providers_init: register %q constructor: %w", typ, err)
		}
	}
	return nil
}

func BuildProviderRegistry(configDir string) (*providers.Registry, error) {
	reg := providers.NewRegistry()
	if err := registerConstructors(reg, keychain.SystemResolver{}); err != nil {
		return nil, err
	}

	providersPath := filepath.Join(configDir, "providers.toml")
	declared, err := config.LoadProviders(providersPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {

			return reg, nil
		}
		return nil, fmt.Errorf("providers_init: %w", err)
	}

	for _, cfg := range declared {
		if err := reg.RegisterFromConfig(cfg); err != nil {

			if errors.Is(err, keychain.ErrNotFound) {
				stub := &disabledBackend{
					name:   cfg.Name,
					tier:   tierForProviderType(cfg.Type),
					reason: "keychain entry " + cfg.APIKeyKeychain + " not found",
				}
				if rerr := reg.Register(cfg.Name, stub); rerr != nil {
					return nil, fmt.Errorf("providers_init: register disabled stub %q: %w", cfg.Name, rerr)
				}
				continue
			}
			return nil, fmt.Errorf("providers_init: register provider %q: %w", cfg.Name, err)
		}
	}

	return reg, nil
}

func logRegistrySummary(logger *slog.Logger, reg *providers.Registry) {
	logger.Info("provider registry built", "providers", reg.List())
}
