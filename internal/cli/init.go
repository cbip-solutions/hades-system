// SPDX-License-Identifier: MIT
// Package cli — init.go.
//
// Surface brownfield attach to an existing repo. Delegates Phase B
// recognize for inference + Phase A onboard.Wizard with WizardKindBrownfield
// + writes .zen/{config,scaffold}.toml + optional .hermes/plugins/ symlink.
//
// brownfield is ADDITIVE ONLY: never overwrites operator source files
// (only writes inside .zen/ + .hermes/).
//
// Stage 0 drift adjustments vs plan §"Tech Stack":
//   - Plan §3416 sets `defaults.RecognizeResult = &result`. Field does
//     not exist on onboard.WizardDefaults. Workaround: thread recognize
//     fields into existing defaults (ProjectKind, Doctrine).
//   - Plan cites `recognize.Result.Framework` (singular) — actual struct
//     has `Frameworks []FrameworkEvidence`. Helper firstFramework() reads
//     the head of the slice or returns "" when empty.
//   - Plan cites `recognize.Result.WorkspaceRoot` — actual is
//     `Monorepo *MonorepoInfo` carrying the root. Adapted to read
//     Monorepo.WorkspaceRoot when populated.
//   - Plan cites `recognize.Result.Confidence map[string]float64` —
//     actual `PrimaryConfidence float64` + per-language evidence
//     confidences in `Languages []LanguageEvidence`.
//   - Exit codes: ErrRecoverable / ErrPreflightFailure sentinels (NOT
//     plan's errExit; same adaptation as new.go).
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/config"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
	"github.com/cbip-solutions/hades-system/internal/recognize"
)

const sidecarsExampleAsset = `# Example sidecars.toml — copy to ~/.config/hades/sidecars.toml to enable.
#
# [tier1.bypass] declares the optional Tier 1 sidecar for advanced
# Anthropic Max-subscription integration. Per decisión 17-i, the default
# install path uses the Plan 16 provider cascade (Anthropic paygo +
# Gemini + OpenRouter direct backends); this section is only needed for
# operators who run the private bypass sidecar binary (zen-bypass-tier1).
#
# Path resolution:
#   $XDG_CONFIG_HOME/hades/sidecars.toml     (when XDG_CONFIG_HOME is set)
#   $HOME/.config/hades/sidecars.toml        (default fallback)
#
# Validation rules enforced at daemon startup:
#   * url MUST be http://127.0.0.1:PORT or http://localhost:PORT
#     (loopback only; the sidecar binds to loopback, never publicly).
#   * tier MUST equal 1 (the table-name encodes the tier).
#   * health_probe_interval_s MUST be in [5, 3600].
#   * request_timeout_s MUST be in [1, 600].
#   * required is optional (default false; graceful-degrade lets the
#     dispatcher fall through to the Plan 16 cascade when the sidecar
#     is absent or unhealthy).
#
# A missing sidecars.toml is a NORMAL state — the daemon falls through
# to the Plan 16 cascade automatically.

[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
required = false
`

func NewInitCmd() *cobra.Command {
	var (
		acceptInference     bool
		nonInteractive      bool
		yes                 bool
		resetPrefs          bool
		forceMerge          bool
		noPluginLink        bool
		withSidecarsExample bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Attach HADES to an existing project (brownfield)",
		Long: `Attach HADES + Hermes plugin scaffolding to an existing project.

zen init runs inside an existing project's cwd. It:
  1. Walks UP to the workspace root (monorepo-aware: pnpm-workspace.yaml,
     go.work, turbo.json, nx.json, Cargo.toml [workspace], etc.).
  2. Infers language + framework + monorepo + maturity via three-tier
     recognize (manifest > config > glob).
  3. Confirms the inference with the operator (Y/n).
  4. Writes .zen/config.toml + .zen/scaffold.toml (additive only; never
     overwrites operator source).
  5. Optionally symlinks .hermes/plugins/<project-name>/ for project-
     scope Hermes plugin discovery (Q13=D opt-in).

For greenfield projects (no existing code), use ` + "`zen new`" + `.
To import an existing claude-code install, use ` + "`zen migrate claude-code`" + `.

EXIT CODES:
  0  success
  1  operator-recoverable (invalid flag, .zen/config.toml exists, non-interactive
     without --accept-inference)
  2  unrecoverable (generic I/O, recognize failure)
  3  preflight failure (Hermes missing)
`,
		Example: `  # Interactive: walk-up + recognize + confirm:
  zen init

  # Accept inference and proceed:
  zen init --accept-inference

  # CI-friendly:
  zen init --non-interactive --accept-inference --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runInit(ctx, cmd, initArgs{
				acceptInference:     acceptInference,
				nonInteractive:      nonInteractive,
				yes:                 yes,
				resetPrefs:          resetPrefs,
				forceMerge:          forceMerge,
				noPluginLink:        noPluginLink,
				withSidecarsExample: withSidecarsExample,
			})
		},
	}
	cmd.Flags().BoolVar(&acceptInference, "accept-inference", false, "Accept recognize results without confirmation")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail loudly on missing required answer")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip Y/N confirmations (assume yes)")
	cmd.Flags().BoolVar(&resetPrefs, "reset-preferences", false, "Force customize path; ignore persisted prefs")
	cmd.Flags().BoolVar(&forceMerge, "force-merge", false, "Merge into existing .zen/config.toml (default: error)")
	cmd.Flags().BoolVar(&noPluginLink, "no-plugin-link", false, "Skip .hermes/plugins/ symlink (advanced)")
	cmd.Flags().BoolVar(&withSidecarsExample, "with-sidecars-example", false, "Seed ~/.config/hades/sidecars.toml from the bundled example when absent (Plan 15 Phase B Tier 1 sidecar opt-in)")
	return cmd
}

type initArgs struct {
	acceptInference     bool
	nonInteractive      bool
	yes                 bool
	resetPrefs          bool
	forceMerge          bool
	noPluginLink        bool
	withSidecarsExample bool
}

func runInit(ctx context.Context, cmd *cobra.Command, args initArgs) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	if err := preflight.CheckHermesInstalled(ctx); err != nil {
		fmt.Fprintln(stderr, "error: Hermes >=0.13.0 not detected")
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}

	if err := preflight.CheckBashInstalled(ctx); err != nil {
		fmt.Fprintln(stderr, "error: bash not found on PATH (required for template hooks)")
		fmt.Fprintln(stderr, "       Debian/Ubuntu: bash ships by default; Alpine: apk add bash")
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("%w: %v", ErrPreflightFailure, err))
	}

	if args.nonInteractive && !args.acceptInference {
		fmt.Fprintln(stderr, "error: --non-interactive requires --accept-inference")
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("non-interactive: missing --accept-inference"))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("getwd: %w", err))
	}
	workspaceRoot, err := findWorkspaceRoot(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "error: workspace root: %v\n", err)
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("workspace root: %w", err))
	}

	rootFS := os.DirFS(workspaceRoot)
	result, err := recognize.Recognize(ctx, rootFS)
	if err != nil {
		fmt.Fprintf(stderr, "error: recognize: %v\n", err)
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("recognize: %w", err))
	}
	printRecognizeResult(stdout, workspaceRoot, result)

	if !args.acceptInference && !args.yes {
		if !promptInitInferenceYN(cmd.InOrStdin(), stdout, "Accept this inference?") {
			fmt.Fprintln(stdout, "Inference declined. Re-run with --accept-inference or fix manually.")
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("inference declined by operator"))
		}
	}

	cfgPath := filepath.Join(workspaceRoot, ".zen", "config.toml")
	if _, statErr := os.Stat(cfgPath); statErr == nil && !args.forceMerge {
		fmt.Fprintf(stderr, "error: %s exists; pass --force-merge to merge, or remove first\n", cfgPath)
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable(".zen/config.toml exists: %s", cfgPath))
	}

	mode := onboard.ModeCustomize
	if args.yes || args.acceptInference {
		mode = onboard.ModeRecommended
	}
	defaults := onboard.WizardDefaults{
		ProjectName: filepath.Base(workspaceRoot),
		ProjectKind: "brownfield",
		ProjectRoot: workspaceRoot,
		Doctrine:    defaultDoctrineFor(result),
	}

	var wizard onboard.Wizard
	if args.nonInteractive {
		wizard = qna.NewNonInteractiveWizard()
	} else {
		wizard = qna.NewBubbleteaWizard()
	}
	answers, err := wizard.Run(ctx, onboard.WizardKindBrownfield, mode, defaults)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("wizard: %w", err))
	}

	if err := writeBrownfieldScaffold(workspaceRoot, answers, result); err != nil {
		fmt.Fprintf(stderr, "error: write scaffold: %v\n", err)
		return ierrors.Wrap(ierrors.Code("wizard.migrate-incomplete"), fmt.Errorf("write scaffold: %w", err))
	}

	if !args.noPluginLink && answers.LinkHermesPlugin {
		if err := linkHermesPlugin(workspaceRoot, answers.ProjectName); err != nil {
			fmt.Fprintf(stderr, "warn: plugin link skipped: %v\n", err)
		}
	}

	emitOnboardInit(ctx, cmd, stderr, workspaceRoot, result, args.acceptInference)

	if args.withSidecarsExample {
		if err := seedSidecarsExample(stdout, stderr); err != nil {

			fmt.Fprintf(stderr, "warning: sidecars.toml seed: %v\n", err)
		}
	}

	printInitNextSteps(stdout, workspaceRoot, answers)
	return nil
}

// seedSidecarsExample writes the bundled sidecars.toml.example contents to
// ~/.config/hades/sidecars.toml IF the file is absent. Idempotent: when
// the file exists, the function logs a skip message and returns nil
// (operator-edited tuning MUST never be silently clobbered — that would
// be the worst possible side effect of an "init" command).
//
// Tier 1 sidecar opt-in scaffolding. The default install path uses the
// providers.toml cascade (no sidecars.toml needed); operators who run the
// private zen-bypass-tier1 sidecar pass --with-sidecars-example to seed a
// starter file they can hand-edit.
func seedSidecarsExample(stdout, stderr io.Writer) error {
	path := config.SidecarsPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(stdout, "  sidecars.toml already exists at %s — skipped (re-run with operator-edited content preserved)\n", path)
		return nil
	} else if !os.IsNotExist(err) {

		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(sidecarsExampleAsset), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(stdout, "  sidecars.toml example seeded at %s — edit to enable Tier 1 sidecar\n", path)
	return nil
}

func emitOnboardInit(ctx context.Context, cmd *cobra.Command, stderr io.Writer, root string, r recognize.Result, accepted bool) {
	auditClient := newClientFromCmd(cmd)
	payload := map[string]any{
		"workspace_root":     root,
		"primary_language":   r.PrimaryLanguage,
		"primary_confidence": r.PrimaryConfidence,
		"framework":          firstFramework(r),
		"monorepo":           r.Monorepo != nil,
		"maturity":           maturityStr(r),
		"accepted_inference": accepted,
	}
	if _, err := auditClient.AuditEmit(ctx, client.AuditEmitReq{
		Type:    "evt.onboard.init.run",
		Payload: payload,
	}); err != nil {
		fmt.Fprintf(stderr, "warning: audit emit: %v\n", err)
	}
}

func findWorkspaceRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	markers := []string{
		"pnpm-workspace.yaml",
		"go.work",
		"turbo.json",
		"nx.json",
		"rush.json",
		"lerna.json",
		"BUILD.bazel",
		"pants.toml",
	}
	cur := abs
	gitRoot := ""
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(cur, m)); err == nil {
				return cur, nil
			}
		}
		if gitRoot == "" {
			if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
				gitRoot = cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if gitRoot != "" {
		return gitRoot, nil
	}
	return abs, nil
}

func printRecognizeResult(w io.Writer, root string, r recognize.Result) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workspace root: %s\n", root)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Inferred from manifest + config + byte-ranking:")
	if r.PrimaryLanguage != "" {
		fmt.Fprintf(w, "  Primary language: %s (confidence %.2f)\n", r.PrimaryLanguage, r.PrimaryConfidence)
	}
	if fw := firstFramework(r); fw != "" {
		fmt.Fprintf(w, "  Framework:        %s\n", fw)
	}
	if r.Monorepo != nil {
		fmt.Fprintf(w, "  Monorepo:         yes (root: %s, tool: %s)\n", r.Monorepo.Root, r.Monorepo.Tool)
	}
	if m := maturityStr(r); m != "" {
		fmt.Fprintf(w, "  Maturity:         %s\n", m)
	}
	fmt.Fprintln(w)
}

func firstFramework(r recognize.Result) string {
	if len(r.Frameworks) == 0 {
		return ""
	}
	return r.Frameworks[0].Framework
}

func maturityStr(r recognize.Result) string {
	m := r.Maturity
	if m.CommitCount == 0 && m.LastCommitISO8601 == "" && !m.HasCI {
		return ""
	}
	if m.CommitCount >= 50 || m.HasCI {
		return "mature"
	}
	if m.CommitCount > 0 {
		return "early"
	}
	return "empty"
}

func defaultDoctrineFor(r recognize.Result) string {
	if r.Doctrine != "" {
		return r.Doctrine
	}
	switch maturityStr(r) {
	case "mature":
		return "capa-firewall"
	case "early":
		return "default"
	default:
		return "max-scope"
	}
}

func writeBrownfieldScaffold(root string, a onboard.WizardAnswers, r recognize.Result) error {
	zenDir := filepath.Join(root, ".zen")
	if err := os.MkdirAll(zenDir, 0o755); err != nil {
		return err
	}
	cfg := strings.Join([]string{
		fmt.Sprintf(`schema_version = %q`, onboard.CurrentConfigSchemaVersion),
		fmt.Sprintf(`project_name = %q`, a.ProjectName),
		fmt.Sprintf(`doctrine = %q`, a.Doctrine),
		fmt.Sprintf(`primary_language = %q`, r.PrimaryLanguage),
		fmt.Sprintf(`framework = %q`, firstFramework(r)),
		fmt.Sprintf(`monorepo = %v`, r.Monorepo != nil),
		fmt.Sprintf(`maturity = %q`, maturityStr(r)),
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(zenDir, "config.toml"), []byte(cfg), 0o644); err != nil {
		return err
	}
	scaffold := strings.Join([]string{
		fmt.Sprintf(`schema_version = %q`, onboard.CurrentConfigSchemaVersion),
		`wizard_kind = "brownfield"`,
		`template = "brownfield-additive"`,
		``,
	}, "\n")
	return os.WriteFile(filepath.Join(zenDir, "scaffold.toml"), []byte(scaffold), 0o644)
}

// linkHermesPlugin creates the .hermes/plugins/<project-name>/ symlink.
// Best-effort: returns error but caller treats as warning.
func linkHermesPlugin(root, projectName string) error {
	pluginDir := filepath.Join(root, ".hermes", "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	link := filepath.Join(pluginDir, projectName)
	if _, err := os.Lstat(link); err == nil {
		return nil
	}
	return os.Symlink(root, link)
}

func promptInitInferenceYN(in io.Reader, out io.Writer, q string) bool {
	fmt.Fprintf(out, "%s [Y/n] ", q)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if s == "" || s == "y" || s == "yes" {
		return true
	}
	return false
}

func printInitNextSteps(w io.Writer, root string, a onboard.WizardAnswers) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "zen init complete.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. Review .zen/config.toml at %s\n", root)
	fmt.Fprintln(w, "  2. Run `zen doctor full` to verify the installation")
	fmt.Fprintln(w, "  3. Run `hermes plugins list` to confirm registration")
	if a.LinkHermesPlugin {
		fmt.Fprintln(w, "     (project-scope plugin link installed at .hermes/plugins/)")
	}
	fmt.Fprintln(w)
}
