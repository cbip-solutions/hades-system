// SPDX-License-Identifier: MIT
package mapping

// allTableEntries returns the canonical mapping-rule set from amendment §2.4.
// Used by TestMappingTableCoversAllSurfaces to assert every source kind has
// at least one mapping rule. Mutation here MUST be accompanied by a golden
// fixture pair under internal/migrate/golden/fixtures/ (regression guard).
//
// The returned slice is for shape-coverage testing only; SourcePath +
// TargetPath are illustrative placeholders. Real mapping produces concrete
// paths at runtime in mapper.go's mapSkills / mapCommands / etc.
func allTableEntries() []PlanEntry {
	return []PlanEntry{
		{Kind: EntryKindSkill, SourcePath: "~/local agent config/skills/<name>/SKILL.md", TargetPath: "plugin/hades-system/skills/<name>/SKILL.md"},
		{Kind: EntryKindCommand, SourcePath: "~/local agent config/commands/<name>.md", TargetPath: "plugin/hades-system/commands/<name>.py"},
		{Kind: EntryKindHook, SourcePath: "local agent memory/hooks/<event>.{sh,py}", TargetPath: "plugin/hades-system/hooks/<hermes-event>.py"},
		{Kind: EntryKindDoctrine, SourcePath: "local agent memory/settings.json#permissions", TargetPath: "doctrines/imported-from-claude-code.toml"},
		{Kind: EntryKindMemory, SourcePath: "~/local agent config/projects/<slug>/memory/*.md", TargetPath: "projects/<slug>/memory/*.md"},
		{Kind: EntryKindMCPServer, SourcePath: "~/local agent config/.mcp.json#mcpServers", TargetPath: "config.yaml#mcp_servers"},
		{Kind: EntryKindHermesConfig, SourcePath: "local agent memory/settings.json#model+mcpServers", TargetPath: "config.yaml"},
	}
}
