// SPDX-License-Identifier: MIT
// config_init.go — HADES design — `hades config init` global wizard.
//
// Thin Cobra subcommand that calls internal/onboard/qna/Wizard.Run with
// WizardKindGlobal, persists outputs (config.toml + doctrine clone + plugin
// install), and emits evt.onboard.config_init.run via daemon HTTP audit endpoint.
//
// invariant: every TOML written here includes schema_version = "1.0".
// invariant: plugin install path resolved by internal/onboard/plugin/.
// invariant: this file NEVER imports internal/store.

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/plugin"
	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
	"github.com/cbip-solutions/hades-system/internal/onboard/prefs"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
)

type configInitFlags struct {
	yes            bool
	resetPrefs     bool
	nonInteractive bool
	doctrine       string
	provider       string
	customURL      string
	noPlugin       bool
	scope          string
}

func NewConfigInitCmd() *cobra.Command {
	var flags configInitFlags

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Run the global setup wizard (LLM provider + doctrine + Hermes plugin)",
		Long:  "hades config init walks through the global setup wizard.\n\nIt configures:\n  - LLM provider (anthropic-bypass, anthropic-paygo, local-ollama, custom)\n  - Active doctrine (~/.config/hades-system/doctrines/)\n  - Hermes plugin install location (per design choice project-scope resolution; skip via --no-plugin)\n\nOn success, writes:\n  ~/.config/hades-system/config.toml   (schema_version=\"1.0\"; invariant)\n  ~/.config/hades-system/doctrines/<selected>.toml\n  ~/.hermes/plugins/hades-system/      (or project-scope path if spike confirmed)\n\nEXIT CODES:\n  0  success (incl. operator CTRL-C during wizard)\n  1  operator-recoverable error (e.g., --provider=custom requires --custom-url)\n  2  unrecoverable error (transport, JSON, daemon 5xx, --non-interactive gate)\n  3  preflight failure (Hermes not installed, plugin format remnant detected)\n\nReviewed MCP selection (design choice) lands in stage (hades mcp reviewed apply).",

		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConfigInitFlags(flags); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigInit(cmd, flags)
		},
	}

	cmd.Flags().BoolVar(&flags.yes, "yes", false, "Use recommended defaults (skip wizard Q&A)")
	cmd.Flags().BoolVar(&flags.resetPrefs, "reset-preferences", false, "Overwrite existing config and preferences")
	cmd.Flags().BoolVar(&flags.nonInteractive, "non-interactive", false, "Exit 2 if interactive prompt would be required")
	cmd.Flags().StringVar(&flags.doctrine, "doctrine", "", "Pre-select doctrine name (skip wizard question)")
	cmd.Flags().StringVar(&flags.provider, "provider", "", "Pre-select LLM provider: anthropic-bypass|anthropic-paygo|local-ollama|custom")
	cmd.Flags().StringVar(&flags.customURL, "custom-url", "", "Custom provider base URL (required with --provider=custom --yes)")
	cmd.Flags().BoolVar(&flags.noPlugin, "no-plugin", false, "Skip Hermes plugin install step")
	cmd.Flags().StringVar(&flags.scope, "scope", "user", "Plugin install scope: user|project")

	return cmd
}

func validateConfigInitFlags(flags configInitFlags) error {
	switch flags.scope {
	case "user", "project":
	default:
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--scope %q invalid; must be \"user\" or \"project\"", flags.scope))
	}
	if flags.provider == "custom" && flags.yes && flags.customURL == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--provider=custom --yes requires --custom-url to produce a usable providers/custom.toml"))
	}
	return nil
}

func runConfigInit(cmd *cobra.Command, flags configInitFlags) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := preflight.CheckHermesInstalled(ctx); err != nil {
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}
	if err := preflight.CheckPluginFormatRemnants(ctx); err != nil {
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}

	configPath := onboard.GlobalConfigPath()
	if _, statErr := os.Stat(configPath); statErr == nil && !flags.resetPrefs {
		cmd.Printf("Global config already initialized at %s\n", configPath)
		cmd.Println("Run with --reset-preferences to overwrite.")
		return nil
	}

	if !flags.yes {
		present, _, ccErr := preflight.CCDetect()
		if ccErr != nil {
			cmd.Printf("warning: cc detection: %v; proceeding without gate\n", ccErr)
		} else if present {
			if flags.nonInteractive {
				return errors.New("detected local agent memory/; run 'hades migrate claude-code' first or pass --yes to skip this check")
			}
			confirmed, promptErr := configInitPromptYesNo(
				cmd.OutOrStdout(),
				cmd.InOrStdin(),
				"Detected ~/local agent config/. Run `hades migrate claude-code` first to import your existing install. Continue anyway? [y/N]",
				false,
			)
			if promptErr != nil {
				return promptErr
			}
			if !confirmed {
				cmd.Println("Aborted. Run `hades migrate claude-code --help` for import options.")
				return nil
			}
		}
	}

	savedPrefs, prefsErr := prefs.Load(onboard.OnboardPrefsPath())
	if prefsErr != nil && !errors.Is(prefsErr, os.ErrNotExist) {
		cmd.Printf("warning: prefs load: %v; using built-in defaults\n", prefsErr)

		savedPrefs = nil
	}

	mode := resolveConfigInitWizardMode(flags, savedPrefs)
	if mode == onboard.ModeCustomize && flags.nonInteractive {
		return errors.New("wizard requires interactive input; use --yes for defaults or supply --doctrine and --provider flags")
	}

	flagsMap := map[string]string{}
	if flags.doctrine != "" {
		flagsMap["doctrine"] = flags.doctrine
	}
	if flags.provider != "" {
		flagsMap["llm-provider"] = flags.provider
	}
	defaults := onboard.DefaultsFromFlagsAndPrefs(flagsMap, savedPrefs)

	if flags.customURL != "" {
		defaults.CustomProviderURL = flags.customURL
	}

	var wizard onboard.Wizard
	if flags.nonInteractive {
		wizard = qna.NewNonInteractiveWizard()
	} else {
		wizard = qna.NewBubbleteaWizard()
	}
	answers, err := wizard.Run(ctx, onboard.WizardKindGlobal, mode, defaults)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("wizard: %w", err))
	}

	cfg := &onboard.GlobalConfig{
		SchemaVersion: onboard.CurrentConfigSchemaVersion,
		LLMProvider:   answers.LLMProvider,
		Doctrine:      answers.Doctrine,
		HermesScope:   flags.scope,
	}
	if err := onboard.WriteGlobalConfig(cfg); err != nil {
		return ierrors.Wrap(ierrors.Code("wizard.migrate-incomplete"), fmt.Errorf("write config: %w", err))
	}

	if err := onboard.CloneBuiltinDoctrine(answers.Doctrine); err != nil {
		return ierrors.Wrap(ierrors.Code("wizard.migrate-incomplete"), fmt.Errorf("clone doctrine: %w", err))
	}

	if answers.LLMProvider != "" && answers.LLMProvider != "anthropic-bypass" {
		provCfg := &onboard.ProviderConfig{
			SchemaVersion:     onboard.CurrentConfigSchemaVersion,
			Name:              answers.LLMProvider,
			BypassConfigPath:  answers.BypassConfigPath,
			OllamaEndpoint:    answers.OllamaEndpoint,
			CustomProviderURL: answers.CustomProviderURL,
		}
		if err := onboard.WriteProviderTOML(answers.LLMProvider, provCfg); err != nil {
			return ierrors.Wrap(ierrors.Code("wizard.migrate-incomplete"), fmt.Errorf("write provider: %w", err))
		}
	}

	if !flags.noPlugin {

		loc, locErr := plugin.ResolveLocation(flags.scope == "project")
		if locErr != nil {
			cmd.Printf("warning: plugin location resolve: %v\n", locErr)
		} else {
			installOpts := plugin.InstallOptions{
				Location:   loc,
				Scope:      flags.scope,
				ProjectDir: configInitCurrentDir(),
				Manifest:   []byte("schema_version = \"" + onboard.CurrentConfigSchemaVersion + "\"\n"),
			}
			if _, pluginErr := plugin.Install(ctx, installOpts); pluginErr != nil {
				// Non-fatal: surface as warning; operator can re-run `hades config init`.
				cmd.Printf("warning: plugin install: %v\n", pluginErr)
			}
		}
	}

	if mode == onboard.ModeCustomize {
		if saveErr := prefs.Save(prefs.Path(), onboard.PrefsFromAnswers(answers)); saveErr != nil {
			cmd.Printf("warning: prefs save: %v; future runs will not see these preferences\n", saveErr)
		}
	}

	auditClient := newClientFromCmd(cmd)
	if _, emitErr := auditClient.AuditEmit(ctx, client.AuditEmitReq{
		Type: "evt.onboard.config_init.run",
		Payload: map[string]any{
			"provider": answers.LLMProvider,
			"doctrine": answers.Doctrine,
			"scope":    flags.scope,
			"mode":     mode.String(),
		},
	}); emitErr != nil {

		cmd.Printf("warning: audit emit: %v\n", emitErr)
	}

	cmd.Println()
	cmd.Printf("Global config initialized at %s\n", configPath)
	cmd.Println("Next steps:")
	cmd.Println("  hades new          — scaffold a new project")
	cmd.Println("  hades doctor full  — verify your installation")

	return nil
}

func resolveConfigInitWizardMode(flags configInitFlags, savedPrefs *prefs.Prefs) onboard.WizardMode {

	effectivePrefs := savedPrefs
	if flags.resetPrefs {
		effectivePrefs = nil
	}
	if flags.yes {
		if hasPersistedPrefs(effectivePrefs) {
			return onboard.ModeReuse
		}
		return onboard.ModeRecommended
	}
	return onboard.ModeCustomize
}

func hasPersistedPrefs(p *prefs.Prefs) bool {
	if p == nil {
		return false
	}

	if p.LLMProvider != "" || p.Doctrine != "" || p.TemplateName != "" {
		return true
	}
	if p.BypassConfigPath != "" || p.OllamaEndpoint != "" || p.CustomProviderURL != "" {
		return true
	}
	if p.GitConfigName != "" || p.GitConfigEmail != "" {
		return true
	}
	if p.ProjectKind != "" || p.TemplateVersion != "" || p.DoctrineSource != "" {
		return true
	}
	if p.AuditRetentionDays != 0 {
		return true
	}
	if p.InstallHermes || p.EnableAuditChain || p.InitGit || p.LinkHermesPlugin || p.PingDaemon {
		return true
	}
	return len(p.MCPs) > 0
}

func configInitHomeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func configInitCurrentDir() string {
	dir, _ := os.Getwd()
	return dir
}

func configInitPromptYesNo(out io.Writer, in io.Reader, prompt string, defaultVal bool) (bool, error) {
	fmt.Fprintln(out, prompt)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return defaultVal, fmt.Errorf("read prompt: %w", err)
		}
		return defaultVal, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return defaultVal, nil
	}
	return strings.ToLower(line) == "y", nil
}
