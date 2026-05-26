package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestZenMigrate_HelpGrouped(t *testing.T) {
	t.Parallel()
	cmd := NewMigrateCmd()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "DATABASE SCHEMA:") {
		t.Errorf("missing DATABASE SCHEMA group header: %s", out)
	}
	if !strings.Contains(out, "CONFIGURATION:") {
		t.Errorf("missing CONFIGURATION group header: %s", out)
	}
	if !strings.Contains(out, "claude-code") {
		t.Errorf("missing claude-code subcommand: %s", out)
	}
}

func TestZenMigrate_HasClaudeCodeSubcommand(t *testing.T) {
	t.Parallel()
	cmd := NewMigrateCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "claude-code" || strings.HasPrefix(sub.Use, "claude-code ") {
			found = true
		}
	}
	if !found {
		t.Errorf("claude-code subcommand not registered under zen migrate")
	}
}

func TestNewMigrateCommand_AliasOfNewMigrateCmd(t *testing.T) {
	t.Parallel()
	a := NewMigrateCmd()
	b := NewMigrateCommand()
	if a.Use != b.Use || a.Short != b.Short {
		t.Errorf("NewMigrateCommand should alias NewMigrateCmd: %q vs %q", a.Use, b.Use)
	}
}

func TestZenMigrate_HelpOrder_DatabaseSchemaBeforeConfiguration(t *testing.T) {
	t.Parallel()
	cmd := NewMigrateCmd()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	dsIdx := strings.Index(out, "DATABASE SCHEMA:")
	cfgIdx := strings.Index(out, "CONFIGURATION:")
	if dsIdx == -1 || cfgIdx == -1 || dsIdx > cfgIdx {
		t.Errorf("DATABASE SCHEMA must precede CONFIGURATION:\n%s", out)
	}
}
