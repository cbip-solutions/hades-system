// SPDX-License-Identifier: MIT
// Package cli — new.go.
//
// Surface greenfield scaffold from embedded or pluggable template. Consumes
// Phase A onboard.Wizard with WizardKindGreenfield discriminator + Phase A
// preflight + Plan 9 audit emit (via daemon HTTP audit client per
// inv-zen-031 — internal/cli MUST NOT import internal/store) + Phase D
// D1/D2/D3 templates+hooks.
//
// Stage 0 drift adjustments vs the master plan §"Tech Stack" assumption:
//
//   - Plan cited `qna.WizardKind/Mode/Defaults/Answers`. Actual canonical
//     types live in package `onboard` (per C1 reconciliation 2026-05-14).
//     This file imports both: `onboard` for the types + Wizard interface,
//     `qna` for the bubbletea + non-interactive concrete constructors.
//   - Plan cited `internal/audit/chain.Emit(ctx, type, payload)`. Actual
//     pattern (Phase C config_init.go) uses
//     `newClientFromCmd(cmd).AuditEmit(ctx, client.AuditEmitReq{...})`
//     against the daemon HTTP /v1/audit/emit endpoint. Audit emit failures
//     are surfaced as warnings, not blockers.
//   - Plan invented `errExit(code, msg)` typed errors. Actual exit-code
//     dispatch uses `ErrRecoverable` (exit 1) + `ErrPreflightFailure`
//     (exit 3); unknown errors default to exit 2 (cmd/zen/main.go). Plan's
//     conflict (exit 4) and SIGINT (exit 130) collapse into:
//   - non-interactive missing required flag → ErrRecoverable (exit 1)
//   - target exists + non-empty → ErrRecoverable (exit 1)
//   - Hermes preflight fail → ErrPreflightFailure (exit 3)
//   - ctx.Canceled → propagate as-is (exit 2 generic)
//     The plan's exit-code-4 conflict is operator-fixable ("add --force"),
//     so the recoverable category is the doctrinally-correct mapping.
//   - Plan's `WizardDefaults.RecognizeResult` field does NOT exist on
//     onboard.WizardDefaults. Brownfield (D5) threads recognize values
//     through existing fields (ProjectKind, etc.) rather than a separate
//     RecognizeResult pointer.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
	"github.com/cbip-solutions/hades-system/internal/templates"
	"github.com/cbip-solutions/hades-system/internal/templates/embedded"
	"github.com/cbip-solutions/hades-system/internal/templates/hooks"
	"github.com/cbip-solutions/hades-system/internal/templates/pluggable"
)

func NewNewCmd() *cobra.Command {
	var (
		template        string
		templateVersion string
		path            string
		projectName     string
		nonInteractive  bool
		yes             bool
		resetPrefs      bool
		listTemplates   bool
		force           bool
		forceGit        bool
		hermesScope     string
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Scaffold a new project (greenfield)",
		Long: `Scaffold a new project from an embedded or pluggable template.

zen new is the greenfield entry point: it creates a project from scratch.
For brownfield (existing code), use ` + "`zen init`" + `. To import an
existing claude-code install, use ` + "`zen migrate claude-code`" + `.

Templates:
  Embedded (no network):  hermes-plugin-only, hermes-plugin+daemon,
                          go-cli, python-cli, ts-saas, ml-pipeline
  Pluggable (git URL):    --template gh:user/repo
                          --template https://github.com/user/repo.git
                          --template git@github.com:user/repo.git
  Pin a version:          --template-version v1.2.3   (tag)
                          --template-version abc1234  (sha)

EXIT CODES:
  0  success
  1  operator-recoverable (invalid flag, target exists without --force)
  2  unrecoverable (transport, daemon 5xx, generic I/O)
  3  preflight failure (Hermes missing, plugin format remnant)
`,
		Example: `  # Recommended defaults, embedded plugin template:
  zen new --template hermes-plugin-only --project-name my-plugin

  # Pluggable from GitHub:
  zen new --template gh:foo/bar --template-version v1.0.0 --project-name my-app

  # Non-interactive in CI:
  zen new --non-interactive --template go-cli --project-name svc --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if listTemplates {
				return runListTemplates(cmd.OutOrStdout())
			}
			return runNew(ctx, cmd, newArgs{
				template:        template,
				templateVersion: templateVersion,
				path:            path,
				projectName:     projectName,
				nonInteractive:  nonInteractive,
				yes:             yes,
				resetPrefs:      resetPrefs,
				force:           force,
				forceGit:        forceGit,
				hermesScope:     hermesScope,
			})
		},
	}
	cmd.Flags().StringVar(&template, "template", "", "Template name (embedded) or git URL (pluggable)")
	cmd.Flags().StringVar(&templateVersion, "template-version", "", "Pin pluggable template to tag or SHA")
	cmd.Flags().StringVar(&path, "path", "", "Target directory (default: ./<project-name>)")
	cmd.Flags().StringVar(&projectName, "project-name", "", "Project name (^[a-z][a-z0-9-]{0,63}$)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail loudly on missing required answer")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip Y/N confirmations (assume yes for safe defaults)")
	cmd.Flags().BoolVar(&resetPrefs, "reset-preferences", false, "Force customize path; ignore persisted prefs")
	cmd.Flags().BoolVar(&listTemplates, "list-templates", false, "Print embedded template names + descriptions and exit")
	cmd.Flags().BoolVar(&force, "force", false, "Allow scaffolding into a non-empty target")
	cmd.Flags().BoolVar(&forceGit, "force-git", false, "Run git init even when cwd is already inside a repo")
	cmd.Flags().StringVar(&hermesScope, "hermes-scope", "user", "Hermes plugin install scope: user (~/.hermes/plugins/) | project (.hermes/plugins/)")
	return cmd
}

type newArgs struct {
	template        string
	templateVersion string
	path            string
	projectName     string
	nonInteractive  bool
	yes             bool
	resetPrefs      bool
	force           bool
	forceGit        bool

	hermesScope string
}

func runListTemplates(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTITLE\tDESCRIPTION")
	for _, m := range embedded.EmbeddedTemplates {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Name, m.Title, m.ShortDescription)
	}
	return tw.Flush()
}

func validateNewHermesScope(s string) (string, error) {
	switch s {
	case "", "user":
		return "user", nil
	case "project":
		return "project", nil
	default:
		return "", fmt.Errorf("--hermes-scope %q invalid; must be \"user\" or \"project\"", s)
	}
}

func runNew(ctx context.Context, cmd *cobra.Command, args newArgs) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	resolvedScope, err := validateNewHermesScope(args.hermesScope)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("%v", err))
	}
	args.hermesScope = resolvedScope

	if err := preflight.CheckHermesInstalled(ctx); err != nil {
		fmt.Fprintln(stderr, "error: Hermes >=0.13.0 not detected")
		fmt.Fprintln(stderr, "       install from https://github.com/hermes-agent/hermes-agent")
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}

	if err := preflight.CheckBashInstalled(ctx); err != nil {
		fmt.Fprintln(stderr, "error: bash not found on PATH (required for template hooks)")
		fmt.Fprintln(stderr, "       Debian/Ubuntu: bash ships by default; Alpine: apk add bash")
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}

	if present, _, _ := preflight.CCDetect(); present {
		fmt.Fprintln(stdout, "Detected ~/.claude/ — consider running `zen migrate claude-code` first")
		fmt.Fprintln(stdout, "to import your existing config + skills + commands into Hermes plugin format.")
		fmt.Fprintln(stdout, "(Continuing with greenfield scaffold; pass --yes to suppress this hint.)")
	}

	if args.nonInteractive {
		if args.template == "" {
			fmt.Fprintln(stderr, "error: --non-interactive requires --template")
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("non-interactive: missing --template"))
		}
		if args.projectName == "" {
			fmt.Fprintln(stderr, "error: --non-interactive requires --project-name")
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("non-interactive: missing --project-name"))
		}
	}

	tmpl, err := resolveTemplate(ctx, args.template, args.templateVersion)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("template resolve: %v", err))
	}

	if err := hooks.RunPreflight(ctx, tmpl); err != nil {
		fmt.Fprintf(stderr, "error: template pre_prompt: %v\n", err)
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: pre_prompt: %v", ErrPreflightFailure, err))
	}

	mode := onboard.ModeCustomize
	if args.yes || args.nonInteractive {
		mode = onboard.ModeRecommended
	}
	if args.resetPrefs && !args.nonInteractive {
		mode = onboard.ModeCustomize
	}
	defaults := onboard.WizardDefaults{
		ProjectName:       args.projectName,
		ProjectKind:       inferKindFromTemplate(args.template),
		ProjectRoot:       args.path,
		TemplateName:      args.template,
		TemplateVersion:   args.templateVersion,
		Doctrine:          "max-scope",
		HermesPluginScope: args.hermesScope,
	}

	var wizard onboard.Wizard
	if args.nonInteractive {
		wizard = qna.NewNonInteractiveWizard()
	} else {
		wizard = qna.NewBubbleteaWizard()
	}
	answers, err := wizard.Run(ctx, onboard.WizardKindGreenfield, mode, defaults)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		fmt.Fprintf(stderr, "error: wizard: %v\n", err)
		return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("wizard: %w", err))
	}

	target := args.path
	if target == "" {
		if answers.ProjectName != "" {
			target = answers.ProjectName
		} else {
			target = args.projectName
		}
	}
	if target == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("no project path could be determined; pass --path or --project-name"))
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintf(stderr, "error: resolve target: %v\n", err)
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("target abs: %w", err))
	}

	exists, nonEmpty, _ := dirState(abs)
	switch {
	case exists && nonEmpty && !args.force:
		fmt.Fprintf(stderr, "error: target %q exists and is not empty; pass --force to overwrite\n", abs)
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("target exists: %s", abs))
	case exists && args.force:

		if err := os.RemoveAll(abs); err != nil {
			fmt.Fprintf(stderr, "error: force remove existing %q: %v\n", abs, err)
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("force remove existing %q: %w", abs, err))
		}
	case exists && !nonEmpty:

		if err := os.Remove(abs); err != nil {
			fmt.Fprintf(stderr, "error: remove empty target %q: %v\n", abs, err)
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("remove empty target %q: %w", abs, err))
		}
	}

	if answers.ProjectName == "" {
		answers.ProjectName = args.projectName
	}
	answers.InitGit = computeInitGit(abs, args.forceGit)

	tplAnswers := asTemplateAnswers(answers, abs, args.hermesScope)
	if err := hooks.Run(ctx, tmpl, abs, tplAnswers); err != nil {
		fmt.Fprintf(stderr, "error: scaffold: %v\n", err)

		if errors.Is(err, hooks.ErrHookFailed) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("scaffold: %w: %v", ErrRecoverable, err))
		}
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("scaffold: %w", err))
	}

	emitOnboardNew(ctx, cmd, stderr, answers, abs, args.template)

	printNextSteps(stdout, abs, answers)
	return nil
}

// emitOnboardNew posts evt.onboard.new.run to the daemon audit endpoint.
// Best-effort: failure prints a warning but never blocks scaffold success.
// Daemon may be down (zen new is operable without daemon); audit-pending
// queue (Plan 9) catches up later.
func emitOnboardNew(ctx context.Context, cmd *cobra.Command, stderr io.Writer, a onboard.WizardAnswers, target, template string) {
	auditClient := newClientFromCmd(cmd)
	payload := map[string]any{
		"template":     template,
		"project_name": a.ProjectName,
		"target":       target,
		"doctrine":     a.Doctrine,
		"kind":         "greenfield",
	}
	if _, err := auditClient.AuditEmit(ctx, client.AuditEmitReq{
		Type:    "evt.onboard.new.run",
		Payload: payload,
	}); err != nil {
		fmt.Fprintf(stderr, "warning: audit emit: %v\n", err)
	}
}

func resolveTemplate(ctx context.Context, name, version string) (templates.Template, error) {
	if name == "" {
		return nil, errors.New("--template required (run `zen new --list-templates` for embedded options)")
	}

	if t, err := embedded.Template(name); err == nil {
		return t, nil
	}

	u, parseErr := pluggable.ParseURL(name)
	if parseErr != nil {
		return nil, fmt.Errorf("template %q: %w", name, parseErr)
	}
	cache, err := pluggable.DefaultCache()
	if err != nil {
		return nil, err
	}
	ref := version
	if ref == "" {
		ref = "main"
	}
	return pluggable.Fetch(ctx, u.CloneURL, ref, cache)
}

func inferKindFromTemplate(name string) string {
	switch name {
	case "hermes-plugin-only", "hermes-plugin+daemon":
		return "plugin"
	case "go-cli":
		return "cli-go"
	case "python-cli":
		return "cli-python"
	case "ts-saas":
		return "saas"
	case "ml-pipeline":
		return "ml-pipeline"
	}
	return "plugin"
}

func dirState(path string) (exists, nonEmpty bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return false, false, nil
		}
		return false, false, statErr
	}
	if !info.IsDir() {

		return true, true, nil
	}
	entries, readErr := os.ReadDir(path)
	if readErr != nil {
		return true, false, readErr
	}
	return true, len(entries) > 0, nil
}

func computeInitGit(target string, forceGit bool) bool {
	if forceGit {
		return true
	}
	dir := filepath.Dir(target)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return true
}

func asTemplateAnswers(a onboard.WizardAnswers, absTarget, scopeOverride string) templates.Answers {
	doctrine := a.Doctrine
	if doctrine == "" {
		doctrine = "max-scope"
	}
	scope := scopeOverride
	if scope == "" {
		scope = a.HermesPluginScope
	}
	if scope == "" {
		scope = "user"
	}
	return templates.Answers{
		ProjectName:       a.ProjectName,
		ProjectKind:       a.ProjectKind,
		ProjectPath:       absTarget,
		Doctrine:          doctrine,
		MCPSelections:     a.MCPSelections,
		InitGit:           a.InitGit,
		LinkHermesPlugin:  a.LinkHermesPlugin,
		PingDaemon:        a.PingDaemon,
		HermesPluginScope: scope,
	}
}

func printNextSteps(w io.Writer, abs string, a onboard.WizardAnswers) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Scaffold complete.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. cd %s\n", abs)
	fmt.Fprintln(w, "  2. Review the README and edit plugin.yaml metadata")
	fmt.Fprintln(w, "  3. Implement skills/ + commands/ + hooks/")
	if a.LinkHermesPlugin {
		fmt.Fprintln(w, "  4. Hermes plugin link already installed (~/.hermes/plugins/)")
	} else {
		fmt.Fprintln(w, "  4. Run `hermes plugins list` to confirm registration")
	}
	fmt.Fprintln(w)
}
