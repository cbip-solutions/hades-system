// SPDX-License-Identifier: MIT
// Package cli — providers_extra.go
//
// The Keychain-touching `zen providers` subcommands: verify, rotate,
// add, setup. Split from providers.go (list + init) to keep each file
// focused. All four resolve their config dir via the configDirFunc seam
// so tests point at a t.TempDir().
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/config"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func findProviderConfig(dir, name string) (providers.ProviderConfig, error) {
	declared, err := config.LoadProviders(filepath.Join(dir, "providers.toml"))
	if err != nil {
		return providers.ProviderConfig{}, fmt.Errorf("providers: %w", err)
	}
	for _, p := range declared {
		if p.Name == name {
			return p, nil
		}
	}
	return providers.ProviderConfig{}, fmt.Errorf("providers: no provider %q in providers.toml", name)
}

func buildBackend(cfg providers.ProviderConfig) (providers.TierBackend, error) {
	kc := keychain.SystemResolver{}
	switch cfg.Type {
	case "anthropic-paygo":
		return providers.NewAnthropicPaygoBackend(cfg, kc)
	case "gemini":
		return providers.NewGeminiBackend(cfg, kc)
	case "openai-compat":
		return providers.NewOpenAICompatBackend(cfg, kc)
	case "ollama":

		return providers.NewOllamaBackend(cfg)
	default:
		return nil, fmt.Errorf("providers: unknown type %q", cfg.Type)
	}
}

func newProvidersVerifyCmd(dir configDirFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "verify <name>",
		Short: "Validate auth + reachability for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := findProviderConfig(dir(), args[0])
			if err != nil {
				return err
			}
			backend, err := buildBackend(cfg)
			if err != nil {
				if errors.Is(err, keychain.ErrNotFound) {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("providers verify %q: Keychain entry %q not found — provision it with `security add-generic-password -U -s %q -a \"zen-swarm\" -w \"<key>\"`",
						args[0], cfg.APIKeyKeychain, cfg.APIKeyKeychain))
				}
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("providers verify %q: %w", args[0], err))
			}
			defer backend.Close()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			if err := backend.Probe(ctx); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("providers verify %q: probe failed: %w", args[0], err))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "provider %q OK — Keychain key resolves, endpoint reachable\n", args[0])
			return nil
		},
	}
}

func newProvidersRotateCmd(dir configDirFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <name>",
		Short: "Rotate the Keychain credential for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := findProviderConfig(dir(), args[0])
			if err != nil {
				return err
			}
			if cfg.APIKeyKeychain == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("providers rotate %q: provider has no api_key_keychain (type %q needs no key)", args[0], cfg.Type))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "paste the new key for %q (service %q), then EOF (Ctrl-D):\n", args[0], cfg.APIKeyKeychain)
			reader := bufio.NewReader(cmd.InOrStdin())
			line, _ := reader.ReadString('\n')
			key := strings.TrimSpace(line)
			if key == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("providers rotate %q: empty key (refusing to store)", args[0]))
			}
			if err := storeKeychainKey(cfg.APIKeyKeychain, key); err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("providers rotate %q: %w", args[0], err))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rotated Keychain key for %q\n", args[0])
			return nil
		},
	}
}

func newProvidersAddCmd(dir configDirFunc) *cobra.Command {
	var typ, endpoint, model, family, kcRef string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Append a [[providers]] entry to providers.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			entry := providers.ProviderConfig{
				Name: name, Type: typ, Endpoint: endpoint, Model: model,
				Family: family, APIKeyKeychain: kcRef,
			}
			if err := entry.Validate(); err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("providers add %q: %w", name, err))
			}
			path := filepath.Join(dir(), "providers.toml")

			if existing, lerr := config.LoadProviders(path); lerr == nil {
				for _, p := range existing {
					if p.Name == name {
						return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("providers add %q: provider already declared", name))
					}
				}
			} else if !errors.Is(lerr, os.ErrNotExist) {
				return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("providers add %q: %w", name, lerr))
			}
			if err := appendProviderEntry(path, entry); err != nil {
				return ierrors.Wrap(ierrors.Code("wizard.migrate-incomplete"), fmt.Errorf("providers add %q: %w", name, err))
			}

			if _, err := config.LoadProviders(path); err != nil {
				return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("providers add %q: providers.toml invalid after append: %w", name, err))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added provider %q to %s\n", name, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "provider type (anthropic-paygo|gemini|ollama|openai-compat)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "http(s) base URL")
	cmd.Flags().StringVar(&model, "model", "", "model name")
	cmd.Flags().StringVar(&family, "family", "", "family key (inv-zen-080)")
	cmd.Flags().StringVar(&kcRef, "keychain", "", "Keychain service ref (zen-swarm/<provider>)")
	return cmd
}

func newProvidersSetupCmd(dir configDirFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive provider configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := dir()
			providersPath := filepath.Join(d, "providers.toml")
			out := cmd.OutOrStdout()
			if _, err := os.Stat(providersPath); errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(out, "no providers.toml found.")
				fmt.Fprintln(out, "step 1: run `zen providers init` to materialize the roster template")
			} else {
				fmt.Fprintf(out, "providers.toml present at %s\n", providersPath)
			}
			fmt.Fprintln(out, "step 2: edit each [[providers]] endpoint to the provider's real base URL")
			fmt.Fprintln(out, "step 3: provision each Keychain key:")
			fmt.Fprintln(out, "        security add-generic-password -U -s \"zen-swarm/<provider>\" -a \"zen-swarm\" -w \"<key>\"")
			fmt.Fprintln(out, "step 4: run `zen providers verify <name>` per provider to confirm")
			return nil
		},
	}
}

func appendProviderEntry(path string, e providers.ProviderConfig) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("open providers.toml: %w", err))
	}
	defer f.Close()
	var b strings.Builder
	b.WriteString("\n[[providers]]\n")
	fmt.Fprintf(&b, "name             = %q\n", e.Name)
	fmt.Fprintf(&b, "type             = %q\n", e.Type)
	fmt.Fprintf(&b, "endpoint         = %q\n", e.Endpoint)
	fmt.Fprintf(&b, "model            = %q\n", e.Model)
	fmt.Fprintf(&b, "family           = %q\n", e.Family)
	if e.APIKeyKeychain != "" {
		fmt.Fprintf(&b, "api_key_keychain = %q\n", e.APIKeyKeychain)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("append providers.toml: %w", err))
	}
	return nil
}
