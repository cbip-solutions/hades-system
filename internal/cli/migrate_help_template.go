// SPDX-License-Identifier: MIT
package cli

const migrateHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const migrateUsageTemplate = "Usage:\n  {{.UseLine}}\n\n{{if .HasAvailableSubCommands}}DATABASE SCHEMA:\n  (reserved for future hades-system schema migration subcommands)\n\nCONFIGURATION:\n  claude-code        Import from ~/local agent config/ (skills/commands/hooks/memory/permissions)\n  HADES design            Migrate legacy /hades-system:* slash references to /hades:*\n  hermes-config      Re-emit Hermes config from current hades-system state (stage)\n  doctrine           Migrate doctrine TOML schema between hades-system versions (stage)\n  config             Migrate global config TOML (~/.config/hades-system/config.toml) (stage)\n\nUse \"{{.CommandPath}} <subcommand> --help\" for details.\n{{end}}{{if .HasAvailableLocalFlags}}\nFlags:\n{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}\n{{end}}"
