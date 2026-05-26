// SPDX-License-Identifier: MIT
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

const MigrateClaudeCodeAuditEventType = "evt.migrate.claude_code.run"

const MigrateClaudeCodePermissionUnmappedEventType = "evt.migrate.claude_code.permission.unmapped"

type claudeCodeFlags struct {
	source       string
	targetHermes string
	targetConfig string
	targetZenCfg string
	preset       string
	include      string
	exclude      string
	dryRun       bool
	planOutput   string
	applyPlan    string
	force        bool
	backupTarget bool
	verify       bool
	jsonOutput   bool
}

func newMigrateClaudeCodeCommand() *cobra.Command {
	f := &claudeCodeFlags{}
	cmd := &cobra.Command{
		Use:   "claude-code",
		Short: "Import a Claude Code installation (~/.claude/) into Hermes plugin format + zen doctrine",
		Long: `Imports an existing Claude Code installation (~/.claude/ + project-local
.claude/) into HADES's Hermes substrate format. Spec §2.4 mapping table.

Modes:
  --dry-run                  Print plan only; no filesystem changes.
  --plan-output PATH         Write JSON plan to PATH (deterministic).
  --apply-plan PATH          Apply a previously generated plan.
  --preset {strict|lenient}  Strict halts on unmapped surfaces; lenient skips + warns.
  --force                    Overwrite existing target files.
  --backup-target            Create tar.gz backup before mutating (inv-zen-177).
  --json                     Emit structured JSON summary.

  (--verify lands in Phase F when "zen doctor hermes" integrates.)

Surfaces imported (spec §2.4 mapping table):
  ~/.claude/skills/<name>/SKILL.md           → plugin/zen-swarm/skills/<name>/SKILL.md
  ~/.claude/commands/<name>.md               → plugin/zen-swarm/commands/<name>.py
  ~/.claude/hooks/<event>.{sh,py}            → plugin/zen-swarm/hooks/<hermes-event>.py
  ~/.claude/settings.json#permissions        → doctrines/imported-from-claude-code.toml
  ~/.claude/settings.json#model+mcpServers   → config.yaml
  ~/.claude/projects/<slug>/memory/*.md      → projects/<slug>/memory/*.md
  ~/.claude/.mcp.json#mcpServers             → config.yaml#mcp_servers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateClaudeCode(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.source, "source", defaultSource(), "Source directory to read from")
	cmd.Flags().StringVar(&f.targetHermes, "target-hermes", "", "Hermes plugin destination (default: $CWD/plugin/zen-swarm)")
	cmd.Flags().StringVar(&f.targetConfig, "target-config", "", "Hermes config destination (default: ~/.hermes/config.yaml)")
	cmd.Flags().StringVar(&f.targetZenCfg, "target-zen-config", "", "HADES doctrine + project TOML destination (default: ~/.config/zen-swarm)")
	cmd.Flags().StringVar(&f.preset, "preset", "lenient", "Mapping preset: strict | lenient | preview")
	cmd.Flags().StringVar(&f.include, "include", "", "Restrict surfaces (CSV: skills,commands,hooks,settings,memory,mcp-servers)")
	cmd.Flags().StringVar(&f.exclude, "exclude", "", "Skip surfaces (CSV)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print what would be written; no filesystem changes")
	cmd.Flags().StringVar(&f.planOutput, "plan-output", "", "Write JSON migration plan to PATH")
	cmd.Flags().StringVar(&f.applyPlan, "apply-plan", "", "Apply previously generated plan from PATH")
	cmd.Flags().BoolVar(&f.force, "force", false, "Overwrite existing target files")
	cmd.Flags().BoolVar(&f.backupTarget, "backup-target", false, "Tar target dirs before write (inv-zen-177)")

	cmd.Flags().BoolVar(&f.verify, "verify", false, "(reserved for Phase F)")
	if err := cmd.Flags().MarkHidden("verify"); err != nil {

		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to hide --verify: %v\n", err)
	}
	cmd.Flags().BoolVar(&f.jsonOutput, "json", false, "Emit structured JSON summary")
	return cmd
}

func defaultSource() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

func defaultTargetHermes() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "plugin", "zen-swarm")
}

func defaultTargetConfig() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "config.yaml")
}

func defaultTargetZenConfig() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zen-swarm")
}

func defaultBackupRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "zen-swarm", "migrate-backups")
}

func runMigrateClaudeCode(cmd *cobra.Command, f *claudeCodeFlags) error {
	preset := mapping.Preset(f.preset)
	if f.preset == "preview" {
		f.dryRun = true
		preset = mapping.PresetLenient
	}
	if !preset.IsValid() {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("invalid --preset %q (want strict|lenient|preview)", f.preset))
	}
	if f.targetHermes == "" {
		f.targetHermes = defaultTargetHermes()
	}
	if f.targetConfig == "" {
		f.targetConfig = defaultTargetConfig()
	}
	if f.targetZenCfg == "" {
		f.targetZenCfg = defaultTargetZenConfig()
	}

	var plan *mapping.Plan
	if f.applyPlan != "" {

		body, err := os.ReadFile(f.applyPlan)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read --apply-plan %s: %w", f.applyPlan, err))
		}
		plan = &mapping.Plan{}
		if err := json.Unmarshal(body, plan); err != nil {
			return ierrors.Wrap(ierrors.Code("wizard.config-corrupt"), fmt.Errorf("parse --apply-plan: %w", err))
		}

		if plan.Source != "" {
			inv, srcErr := source.ReadAll(plan.Source)
			if srcErr != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("re-read source for --apply-plan: %w", srcErr))
			}
			if err := rehydrateBodyBytes(plan, inv); err != nil {
				return err
			}
		}
	} else {

		inv, err := source.ReadAll(f.source)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read source %s: %w", f.source, err))
		}
		applyInclude(inv, f.include, f.exclude)
		plan, err = mapping.Map(inv, preset)
		if err != nil {
			return err
		}
		plan.Source = f.source
		plan.CreatedAt = time.Now().UTC()
	}

	if plan.Preset != mapping.PresetStrict {
		for _, perm := range unmappedPermissionsFromPlan(plan) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"settings.json: permission %q has no risk-tier classifier (lenient: classified as medium; emit evt.migrate.claude_code.permission.unmapped)", perm))
		}
	}

	if f.dryRun || f.planOutput != "" {
		return emitPlan(cmd, plan, f)
	}

	backupRoot := ""
	if f.backupTarget || f.force {
		backupRoot = defaultBackupRoot()
	}
	w := writer.New(writer.WriterConfig{
		HermesPluginRoot: f.targetHermes,
		HermesConfigPath: f.targetConfig,
		ZenConfigRoot:    f.targetZenCfg,
		BackupRoot:       backupRoot,
		ForceOverwrite:   f.force,
	})
	if err := w.Apply(plan); err != nil {
		return err
	}
	// evt.migrate.claude_code.permission.unmapped per spec §3.7 + Phase E
	// §5979-5982 + CHANGELOG.md:38. Best-effort: daemon-down does not block
	// the apply itself (the writer already succeeded). Surface warning so
	// operators know forensic trace failed.
	emitMigrateClaudeCodeAudit(cmd, plan, f, backupRoot)
	if f.jsonOutput {
		summary := map[string]interface{}{
			"applied":  true,
			"entries":  len(plan.Entries),
			"warnings": plan.Warnings,
			"target": map[string]string{
				"hermes":      f.targetHermes,
				"hermes_cfg":  f.targetConfig,
				"zen_config":  f.targetZenCfg,
				"backup_root": backupRoot,
			},
		}
		body, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Migration applied: %d entries, %d warnings\n", len(plan.Entries), len(plan.Warnings))
	}
	if f.verify {

		fmt.Fprintln(cmd.OutOrStdout(), "Verify: deferred to Phase F (run `zen doctor hermes` after Phase F lands).")
	}
	return nil
}

func rehydrateBodyBytes(plan *mapping.Plan, inv *source.Inventory) error {
	pathBody := map[string][]byte{}
	for _, s := range inv.Skills {
		pathBody[s.Path] = s.Body
	}
	for _, c := range inv.Commands {
		pathBody[c.Path] = c.Body
	}
	for _, h := range inv.Hooks {
		pathBody[h.Path] = h.Body
	}
	for _, m := range inv.MemoryFiles {
		pathBody[m.Path] = m.Body
	}

	for i := range plan.Entries {
		e := &plan.Entries[i]
		if e.Kind == mapping.EntryKindDoctrine || e.Kind == mapping.EntryKindHermesConfig {

			fresh, err := mapping.Map(inv, plan.Preset)
			if err != nil {
				return err
			}
			for _, fe := range fresh.Entries {
				if fe.Kind == e.Kind {
					e.BodyBytes = fe.BodyBytes
					break
				}
			}
			continue
		}
		if body, ok := pathBody[e.SourcePath]; ok {
			e.BodyBytes = body
		}
	}
	// Verify hashes (I-1 TOCTOU). Skip entries with no recorded SHA256 (back-
	// compat: an old plan file may predate the hash schema; we emit a
	// warning at the CLI layer in that case).
	if err := verifyPlanHashes(plan); err != nil {
		return err
	}
	return nil
}

var ErrPlanHashMismatch = errors.New("migrate: plan hash mismatch (source file tampered between plan and apply; TOCTOU guard inv-zen-183)")

func verifyPlanHashes(plan *mapping.Plan) error {
	for _, e := range plan.Entries {
		if e.SHA256 == "" {

			continue
		}
		actual := sha256Hex(e.BodyBytes)
		if actual != e.SHA256 {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("%w: %s/%s (planned=%s, actual=%s)",
				ErrPlanHashMismatch, e.Kind, e.SourcePath, e.SHA256, actual))
		}
	}
	if plan.MerkleRoot == "" {
		return nil
	}

	tmp := *plan
	tmp.ComputeHashes()
	if tmp.MerkleRoot != plan.MerkleRoot {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("%w: merkle root mismatch (planned=%s, actual=%s)",
			ErrPlanHashMismatch, plan.MerkleRoot, tmp.MerkleRoot))
	}
	return nil
}

func sha256Hex(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func emitPlan(cmd *cobra.Command, plan *mapping.Plan, f *claudeCodeFlags) error {
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if f.planOutput != "" {
		if err := os.MkdirAll(filepath.Dir(f.planOutput), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.planOutput, body, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Plan written: %s\n", f.planOutput)
		return nil
	}
	if f.jsonOutput {
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Dry-run plan (%d entries, %d warnings):\n", len(plan.Entries), len(plan.Warnings))
	for _, e := range plan.Entries {
		fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s -> %s\n", e.Kind, e.SourcePath, e.TargetPath)
	}
	for _, warn := range plan.Warnings {
		fmt.Fprintf(cmd.OutOrStdout(), "  WARN: %s\n", warn)
	}
	return nil
}

func applyInclude(inv *source.Inventory, include, exclude string) {
	includeSet := parseCSV(include)
	excludeSet := parseCSV(exclude)
	keep := func(kind string) bool {
		if len(includeSet) > 0 && !includeSet[kind] {
			return false
		}
		if excludeSet[kind] {
			return false
		}
		return true
	}
	if !keep("skills") {
		inv.Skills = nil
	}
	if !keep("commands") {
		inv.Commands = nil
	}
	if !keep("hooks") {
		inv.Hooks = nil
	}
	if !keep("settings") {
		inv.Settings = nil
	}
	if !keep("memory") {
		inv.MemoryFiles = nil
	}
	if !keep("mcp-servers") {
		inv.MCPServers = nil
	}
}

func parseCSV(s string) map[string]bool {
	out := map[string]bool{}
	if s == "" {
		return out
	}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

// emitMigrateClaudeCodeAudit fires the canonical Plan 13 audit events for
// a successful `zen migrate claude-code` apply: one
// evt.migrate.claude_code.run summarising the migration + one
// evt.migrate.claude_code.permission.unmapped per unrecognised permission
// under lenient preset (none under strict — strict halts Apply before this
// point via writer.ImportDoctrineStrict).
//
// Best-effort: any HTTP / encoding failure surfaces as a stderr warning so
// the operator knows forensic trace dropped, but the apply itself is
// already complete and the CLI continues to success-exit.
//
// Spec §3.7 line 629 + Phase E plan §5979-5982 + CHANGELOG.md:38.
func emitMigrateClaudeCodeAudit(cmd *cobra.Command, plan *mapping.Plan, f *claudeCodeFlags, backupRoot string) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	c := newClientFromCmd(cmd)
	if c == nil {
		return
	}

	for _, perm := range unmappedPermissionsFromPlan(plan) {
		_, err := c.AuditEmit(ctx, client.AuditEmitReq{
			Type: MigrateClaudeCodePermissionUnmappedEventType,
			Payload: map[string]any{
				"permission": perm,
				"preset":     string(plan.Preset),
				"source":     plan.Source,
				"reason":     "no_tier_match",
			},
		})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: audit emit (permission.unmapped %q) failed: %v\n", perm, err)

		}
	}

	// 2. Aggregate run event. Payload carries enough for forensic
	// reconstruction without inlining bodies (BodyBytes not serialised):
	// source/target paths, preset, mode, per-kind entry counts, warning
	// count, MerkleRoot binding the source-side hashes.
	mode := "apply"
	if f.applyPlan != "" {
		mode = "apply_plan"
	}
	kinds := map[string]int{}
	for _, e := range plan.Entries {
		kinds[string(e.Kind)]++
	}
	if _, err := c.AuditEmit(ctx, client.AuditEmitReq{
		Type: MigrateClaudeCodeAuditEventType,
		Payload: map[string]any{
			"source":              plan.Source,
			"target_hermes":       f.targetHermes,
			"target_hermes_cfg":   f.targetConfig,
			"target_zen_config":   f.targetZenCfg,
			"backup_root":         backupRoot,
			"preset":              string(plan.Preset),
			"mode":                mode,
			"entry_count":         len(plan.Entries),
			"entry_count_by_kind": kinds,
			"warning_count":       len(plan.Warnings),
			"merkle_root":         plan.MerkleRoot,
			"force":               f.force,
			"backup_target":       f.backupTarget,
		},
	}); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: audit emit (migrate.run) failed: %v\n", err)
	}
}

func unmappedPermissionsFromPlan(plan *mapping.Plan) []string {
	for _, e := range plan.Entries {
		if e.Kind != mapping.EntryKindDoctrine {
			continue
		}
		var src struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		}
		if err := json.Unmarshal(e.BodyBytes, &src); err != nil {
			return nil
		}
		return writer.UnmappedPermissions(src.Allow, src.Deny)
	}
	return nil
}
