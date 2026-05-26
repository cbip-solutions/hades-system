package compliance

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
)

// TestInvZen185_MigrateHelpGrouped asserts `zen migrate --help` emits the
// canonical two-group structure per Q11=C (spec §7.2). inv-zen-185.
//
// Doctrine: any future Phase F subcommand additions to `zen migrate` MUST
// land in the CONFIGURATION group (claude-code/hermes-config/doctrine/config).
// Future schema-migration subcommands (up/down/status) MUST land in DATABASE
// SCHEMA. The order DATABASE SCHEMA before CONFIGURATION is load-bearing UX.
func TestInvZen185_MigrateHelpGrouped(t *testing.T) {
	t.Parallel()
	cmd := cli.NewMigrateCmd()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	requiredGroups := []string{"DATABASE SCHEMA:", "CONFIGURATION:"}
	for _, g := range requiredGroups {
		if !strings.Contains(out, g) {
			t.Errorf("inv-zen-185 violation: group %q missing from --help output:\n%s", g, out)
		}
	}

	dsIdx := strings.Index(out, "DATABASE SCHEMA:")
	cfgIdx := strings.Index(out, "CONFIGURATION:")
	if dsIdx == -1 || cfgIdx == -1 || dsIdx > cfgIdx {
		t.Errorf("group order: DATABASE SCHEMA must precede CONFIGURATION:\n%s", out)
	}
}

func TestInvZen185_NewMigrateCommandSameGrouping(t *testing.T) {
	t.Parallel()
	cmd := cli.NewMigrateCommand()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "DATABASE SCHEMA:") || !strings.Contains(out, "CONFIGURATION:") {
		t.Errorf("NewMigrateCommand alias diverged from NewMigrateCmd grouping:\n%s", out)
	}
}
