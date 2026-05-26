package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestBypassSubcommandsHelp(t *testing.T) {
	want := []string{
		"status", "probe", "audit", "update-config", "test",
		"extract-config", "cross-validate", "anomalies",
		"refresh-now", "pin", "unpin", "purge",
		"certs", "cf-range",
	}
	root := NewBypassCmd()
	have := map[string]bool{}
	for _, c := range root.Commands() {

		name := c.Name()
		have[name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: bypass %s", w)
		}
	}
}

func TestBypassAuditFlags(t *testing.T) {
	root := NewBypassCmd()
	for _, c := range root.Commands() {
		if c.Name() == "audit" {
			for _, want := range []string{"range", "inspect", "since"} {
				if c.Flags().Lookup(want) == nil {
					t.Errorf("audit flag missing: --%s", want)
				}
			}
			return
		}
	}
	t.Fatal("audit subcommand not found")
}

func TestBypassPurgeFlags(t *testing.T) {
	root := NewBypassCmd()
	for _, c := range root.Commands() {
		if c.Name() == "purge" {
			for _, want := range []string{"dry-run", "apply"} {
				if c.Flags().Lookup(want) == nil {
					t.Errorf("purge flag missing: --%s", want)
				}
			}
			return
		}
	}
	t.Fatal("purge subcommand not found")
}

func TestBypassPurgeMutuallyExclusiveFlags(t *testing.T) {
	root := NewBypassCmd()
	root.SetArgs([]string{"purge", "--dry-run", "--apply"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetOut(&stderr)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when both --dry-run and --apply are passed; got nil")
	}
	combined := strings.ToLower(err.Error() + stderr.String())

	if !strings.Contains(combined, "exclusive") &&
		!strings.Contains(combined, "none of the others") {
		t.Errorf("error must mention mutual exclusion, got: %q", combined)
	}
}

func TestBypassPurgeNoFlags(t *testing.T) {
	root := NewBypassCmd()
	root.SetArgs([]string{"purge"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetOut(&stderr)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when neither --dry-run nor --apply passed; got nil")
	}
	if !strings.Contains(err.Error()+stderr.String(), "--dry-run or --apply") {
		t.Errorf("error must mention required flag, got: %q / %q", err, stderr.String())
	}
}

func TestBypassCertsFlags(t *testing.T) {
	root := NewBypassCmd()
	for _, c := range root.Commands() {
		if c.Name() == "certs" {
			for _, want := range []string{"show", "rotate"} {
				if c.Flags().Lookup(want) == nil {
					t.Errorf("certs flag missing: --%s", want)
				}
			}
			return
		}
	}
	t.Fatal("certs subcommand not found")
}

func TestBypassHelpRendersAllSubcommands(t *testing.T) {
	root := NewBypassCmd()
	root.SilenceUsage = true
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	for _, want := range []string{"status", "probe", "audit", "update-config", "test", "anomalies", "refresh-now", "pin", "unpin", "purge", "certs", "cf-range"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("--help output missing subcommand %s", want)
		}
	}
}
