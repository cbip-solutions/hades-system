// SPDX-License-Identifier: MIT
package cli

const migrateHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const migrateUsageTemplate = `Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}DATABASE SCHEMA:
  (reserved for future zen-swarm schema migration subcommands)

CONFIGURATION:
  claude-code        Import from ~/.claude/ (skills/commands/hooks/memory/permissions)
  plan-18            Migrate legacy /zen-swarm:* slash references to /hades:*
  hermes-config      Re-emit Hermes config from current zen-swarm state (Phase F)
  doctrine           Migrate doctrine TOML schema between zen-swarm versions (Phase F)
  config             Migrate global config TOML (~/.config/zen-swarm/config.toml) (Phase F)

Use "{{.CommandPath}} <subcommand> --help" for details.
{{end}}{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}`
