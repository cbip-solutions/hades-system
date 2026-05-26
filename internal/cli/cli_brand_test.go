package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type brandFixtureRow struct {
	name                        string
	constructor                 func() *cobra.Command
	subcmdPath                  []string
	mustContainHADESShort       string
	mustNotContainZenSwarmShort bool
	longMustNotContainZenSwarm  bool
}

var brandFixtureRows = []brandFixtureRow{

	{
		name:                        "root",
		constructor:                 NewRootCmd,
		mustContainHADESShort:       "HADES",
		mustNotContainZenSwarmShort: true,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "audit-chain",
		constructor:                 func() *cobra.Command { return NewAuditChainCmd() },
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "daemon",
		constructor:                 NewDaemonCmd,
		mustContainHADESShort:       "HADES",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "doctor",
		constructor:                 NewDoctorCmd,
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "docs",
		constructor:                 NewDocsCmd,
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "migrate-claude-code",
		constructor:                 NewMigrateCmd,
		subcmdPath:                  []string{"claude-code"},
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "schedule",
		constructor:                 NewScheduleCmdProd,
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "init",
		constructor:                 NewInitCmd,
		mustContainHADESShort:       "HADES",
		mustNotContainZenSwarmShort: true,
		longMustNotContainZenSwarm:  true,
	},

	{
		name:                        "sessions",
		constructor:                 NewSessionsCmdProd,
		mustContainHADESShort:       "HADES",
		mustNotContainZenSwarmShort: true,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "providers",
		constructor:                 func() *cobra.Command { return NewProvidersCmd() },
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "specs",
		constructor:                 func() *cobra.Command { return NewSpecsCmdProd() },
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},

	{
		name:                        "knowledge",
		constructor:                 func() *cobra.Command { return NewKnowledgeCmdProd() },
		mustContainHADESShort:       "",
		mustNotContainZenSwarmShort: false,
		longMustNotContainZenSwarm:  false,
	},
}

func TestPhase18bG_BrandFixtures(t *testing.T) {
	for _, row := range brandFixtureRows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			cmd := row.constructor()
			if cmd == nil {
				t.Fatalf("constructor returned nil for %s", row.name)
			}

			for _, name := range row.subcmdPath {
				var found *cobra.Command
				for _, sub := range cmd.Commands() {
					if sub.Use == name || strings.HasPrefix(sub.Use, name+" ") {
						found = sub
						break
					}
				}
				if found == nil {
					t.Fatalf("subcommand %q not found under %s", name, cmd.Use)
				}
				cmd = found
			}

			if row.mustContainHADESShort != "" {
				if !strings.Contains(cmd.Short, row.mustContainHADESShort) {
					t.Errorf("cobra %q Short=%q does not contain %q (Phase G rebrand expected)",
						row.name, cmd.Short, row.mustContainHADESShort)
				}
			}

			if row.mustNotContainZenSwarmShort {
				if strings.Contains(cmd.Short, "zen-swarm") {
					t.Errorf("cobra %q Short=%q still contains 'zen-swarm' (Phase G rebrand expected)",
						row.name, cmd.Short)
				}
			}

			if row.longMustNotContainZenSwarm {
				if strings.Contains(cmd.Long, "zen-swarm") {
					t.Errorf("cobra %q Long=%q still contains 'zen-swarm' (Phase G rebrand expected)",
						row.name, cmd.Long)
				}
			}
		})
	}
}

func TestPhase18bG_RootVersionBranded(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--version"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("zen --version returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "HADES") {
		t.Errorf("zen --version output = %q; expected HADES brand (Phase G-6 rebrand expected)", output)
	}
	if !strings.HasPrefix(output, "HADES system v") {
		t.Errorf("zen --version output = %q; expected prefix 'HADES system v' (Phase G-6 template)", output)
	}
	if !strings.Contains(output, "(binary: zen)") {
		t.Errorf("zen --version output = %q; expected '(binary: zen)' suffix for binary name preservation", output)
	}
}

func TestPhase18bG_NoZenSwarmInCobraShort(t *testing.T) {
	root := NewRootCmd()

	borderstayAllowlist := map[string]string{

		"daemon": "zen-swarm-ctld (binary name, spec §Q3 BORDERLINE, inv-zen-219)",

		"plan-18": "/zen-swarm:* slash syntax (source-path ref, spec §Q3 BORDERLINE-STAYS, Phase F)",
	}

	var violations []string
	var walk func(*cobra.Command, string)
	walk = func(c *cobra.Command, path string) {
		fullPath := path
		if fullPath != "" {
			fullPath += " "
		}
		fullPath += c.Use

		if strings.Contains(c.Short, "zen-swarm") {

			if reason, ok := borderstayAllowlist[c.Use]; ok {

				t.Logf("BORDERLINE-STAYS allowlisted: %s Short=%q (%s)", fullPath, c.Short, reason)
			} else {
				violations = append(violations, fullPath+" Short: "+c.Short)
			}
		}

		for _, sub := range c.Commands() {
			walk(sub, fullPath)
		}
	}
	walk(root, "")

	if len(violations) > 0 {
		t.Errorf("cobra Short fields still contain 'zen-swarm' (Phase G expected to rebrand all):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func TestPhase18bH_Plan14CLIGapFill(t *testing.T) {
	migrateCmd := NewMigrateCmd()
	if migrateCmd == nil {
		t.Fatal("NewMigrateCmd() returned nil")
	}

	var ccCmd *cobra.Command
	for _, sub := range migrateCmd.Commands() {
		if sub.Use == "claude-code" || strings.HasPrefix(sub.Use, "claude-code ") {
			ccCmd = sub
			break
		}
	}
	if ccCmd == nil {
		t.Fatal("migrate claude-code subcommand not found")
	}

	flag := ccCmd.Flags().Lookup("target-zen-config")
	if flag == nil {
		t.Fatal("--target-zen-config flag not found on migrate claude-code")
	}

	desc := flag.Usage

	if !strings.HasPrefix(desc, "HADES doctrine") {
		t.Errorf("--target-zen-config Usage = %q; want prefix \"HADES doctrine\" per Phase H H-5 (spec §Q3 IN)",
			desc)
	}
	if strings.HasPrefix(desc, "zen-swarm doctrine") {
		t.Errorf("--target-zen-config Usage = %q starts with legacy brand subject; Phase H H-5 rebrand expected",
			desc)
	}

	if !strings.Contains(desc, "~/.config/zen-swarm") {
		t.Errorf("--target-zen-config Usage = %q does not preserve BORDERLINE config path ~/.config/zen-swarm; spec §Q3 BORDERLINE carve-out",
			desc)
	}
}
