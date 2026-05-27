// SPDX-License-Identifier: MIT
package mapping

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func Map(inv *source.Inventory, preset Preset) (*Plan, error) {
	if !preset.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidPreset, preset)
	}
	plan := &Plan{
		SchemaVersion: "1.0",
		Preset:        preset,
		Entries:       []PlanEntry{},
		Warnings:      []string{},
	}
	if inv == nil {
		return plan, nil
	}
	if err := mapSkills(inv, plan); err != nil {
		return nil, err
	}
	if err := mapCommands(inv, plan); err != nil {
		return nil, err
	}
	if err := mapHooks(inv, plan, preset); err != nil {
		return nil, err
	}
	if err := mapSettings(inv, plan, preset); err != nil {
		return nil, err
	}
	if err := mapMemory(inv, plan); err != nil {
		return nil, err
	}
	if err := mapMCP(inv, plan); err != nil {
		return nil, err
	}

	if len(inv.Warnings) > 0 {
		plan.Warnings = append(plan.Warnings, inv.Warnings...)
	}

	plan.ComputeHashes()
	return plan, nil
}

func mapSkills(inv *source.Inventory, plan *Plan) error {
	for _, s := range inv.Skills {
		fm := generateFrontmatter(s.Name, s.Body)
		plan.Entries = append(plan.Entries, PlanEntry{
			Kind:        EntryKindSkill,
			SourcePath:  s.Path,
			TargetPath:  filepath.ToSlash(filepath.Join("plugin", "zen-swarm", "skills", s.Name, "SKILL.md")),
			Frontmatter: fm,
			BodyBytes:   s.Body,
			RegisterCall: fmt.Sprintf(
				"ctx.register_skill(%q, %q, description=%q)",
				s.Name,
				"skills/"+s.Name+"/SKILL.md",
				fm["description"],
			),
		})
	}
	return nil
}

func mapCommands(inv *source.Inventory, plan *Plan) error {
	for _, c := range inv.Commands {

		desc := firstLine(c.Body)
		plan.Entries = append(plan.Entries, PlanEntry{
			Kind:       EntryKindCommand,
			SourcePath: c.Path,
			TargetPath: filepath.ToSlash(filepath.Join("plugin", "zen-swarm", "commands", c.Name+".py")),
			BodyBytes:  c.Body,
			RegisterCall: fmt.Sprintf(
				"ctx.register_command(%q, handler=%s_handler, description=%q, args_hint=%q)",
				c.Name, sanitizePyIdent(c.Name), desc, "",
			),
		})
	}
	return nil
}

func mapHooks(inv *source.Inventory, plan *Plan, preset Preset) error {
	for _, h := range inv.Hooks {
		hermes, risk, ok := remapHookEvent(h.EventName)
		if !ok {
			if preset == PresetStrict {
				return fmt.Errorf("%w: hook event %q (no Hermes canonical mapping)", ErrUnmappedSurface, h.EventName)
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("hooks/%s: no Hermes canonical mapping (skipped)", h.EventName))
			continue
		}
		if risk && preset == PresetStrict {
			return fmt.Errorf("%w: hook event %q → %s (Hermes spec evolved post-spike; run `zen doctor hermes --check-hook-contract` to re-verify)",
				ErrHookRiskFlagged, h.EventName, hermes)
		}
		if risk {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("hooks/%s → %s: risk-flagged in Hermes head; verify support manually", h.EventName, hermes))
		}

		if h.Lang == "python" {
			if preset == PresetStrict {
				return fmt.Errorf("%w: native Python hook %q requires manual migration (re-implement as bash, or hand-port to Hermes callback shape and drop into plugin/zen-swarm/hooks/)",
					ErrPythonHookUnsupported, h.EventName)
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"hooks/%s → %s: native Python hook skipped (auto-migration unsafe; re-implement as bash, or hand-port the file to Hermes callback shape and drop into plugin/zen-swarm/hooks/)",
				h.EventName, hermes))
			continue
		}
		plan.Entries = append(plan.Entries, PlanEntry{
			Kind:       EntryKindHook,
			SourcePath: h.Path,
			TargetPath: filepath.ToSlash(filepath.Join("plugin", "zen-swarm", "hooks", hermes+".py")),
			HookEvent:  hermes,
			BodyBytes:  h.Body,
			Notes:      []string{fmt.Sprintf("source-lang=%s", h.Lang)},
			RegisterCall: fmt.Sprintf("ctx.register_hook(%q, %s_callback)",
				hermes, sanitizePyIdent(hermes)),
		})
	}
	return nil
}

// knownSettingsKeys is the set of top-level settings.json fields the mapper
// understands. Any other field encountered triggers strict-mode halt or
// lenient warning per invariant "no silent drop".
var knownSettingsKeys = map[string]bool{
	"permissions": true,
	"env":         true,
	"model":       true,
	"hooks":       true,
	"mcpServers":  true,
}

func mapSettings(inv *source.Inventory, plan *Plan, preset Preset) error {
	if inv.Settings == nil {
		return nil
	}

	if len(inv.Settings.Raw) > 0 {

		unknown := make([]string, 0)
		for k := range inv.Settings.Raw {
			if !knownSettingsKeys[k] {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		for _, k := range unknown {
			if preset == PresetStrict {
				return fmt.Errorf("%w: settings.json field %q", ErrUnmappedSurface, k)
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("settings.json: unknown field %q (skipped)", k))
		}
	}

	plan.Entries = append(plan.Entries, PlanEntry{
		Kind:       EntryKindDoctrine,
		SourcePath: inv.Settings.Path,
		TargetPath: filepath.ToSlash(filepath.Join("doctrines", "imported-from-claude-code.toml")),
		BodyBytes:  serializePermissions(inv.Settings),
	})

	merged := mergeMCPServers(inv.Settings.MCPServers, inv)
	plan.Entries = append(plan.Entries, PlanEntry{
		Kind:       EntryKindHermesConfig,
		SourcePath: inv.Settings.Path,
		TargetPath: "config.yaml",
		BodyBytes:  serializeHermesConfigPayload(inv.Settings.Model, merged),
	})
	return nil
}

func mergeMCPServers(settings map[string]source.MCPServer, inv *source.Inventory) map[string]source.MCPServer {
	out := map[string]source.MCPServer{}
	if inv.MCPServers != nil {
		for k, v := range inv.MCPServers.MCPServers {
			out[k] = v
		}
	}
	for k, v := range settings {
		out[k] = v
	}
	return out
}

func mapMemory(inv *source.Inventory, plan *Plan) error {
	for _, m := range inv.MemoryFiles {
		base := filepath.Base(m.Path)
		plan.Entries = append(plan.Entries, PlanEntry{
			Kind:       EntryKindMemory,
			SourcePath: m.Path,
			TargetPath: filepath.ToSlash(filepath.Join("projects", m.ProjectSlug, "memory", base)),
			BodyBytes:  m.Body,
		})
	}
	return nil
}

func mapMCP(inv *source.Inventory, plan *Plan) error {
	if inv.MCPServers == nil {
		return nil
	}

	names := make([]string, 0, len(inv.MCPServers.MCPServers))
	for n := range inv.MCPServers.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		plan.Entries = append(plan.Entries, PlanEntry{
			Kind:       EntryKindMCPServer,
			SourcePath: inv.MCPServers.Path,
			TargetPath: filepath.ToSlash(filepath.Join("config.yaml#mcp_servers", name)),
			Notes:      []string{"emit-into-hermes-config"},
		})
	}
	return nil
}

func serializePermissions(s *source.SettingsSource) []byte {
	return []byte(fmt.Sprintf(`{"allow":%s,"deny":%s,"env":%s}`,
		jsonStrArr(s.Permissions.Allow),
		jsonStrArr(s.Permissions.Deny),
		jsonStrMap(s.Env)))
}

func serializeHermesConfig(s *source.SettingsSource) []byte {
	return serializeHermesConfigPayload(s.Model, s.MCPServers)
}

func serializeHermesConfigPayload(model string, servers map[string]source.MCPServer) []byte {
	return []byte(fmt.Sprintf(`{"model":%q,"mcpServers":%s}`,
		model,
		jsonMCPMap(servers)))
}

func jsonStrArr(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	parts := make([]string, len(s))
	for i, v := range s {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func jsonStrMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%q:%q", k, m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func jsonMCPMap(m map[string]source.MCPServer) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		v := m[k]
		parts[i] = fmt.Sprintf("%q:{\"command\":%q,\"args\":%s,\"env\":%s}",
			k, v.Command, jsonStrArr(v.Args), jsonStrMap(v.Env))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
