// SPDX-License-Identifier: MIT
package cli

const migrateHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const migrateUsageTemplate = `Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}DATABASE SCHEMA:
  (reserved for future hades-system schema migration subcommands)

CONFIGURATION:
  claude-code        Import from ~/.claude/ (skills/commands/hooks/memory/permissions)
  plan-18            Migrate legacy /hades-system:* slash references to /hades:*
  hermes-config      Re-emit Hermes config from current hades-system state (Phase F)
  doctrine           Migrate doctrine TOML schema between hades-system versions (Phase F)
  config             Migrate global config TOML (~/.config/hades-system/config.toml) (Phase F)

Use "{{.CommandPath}} <subcommand> --help" for details.
{{end}}{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}`
