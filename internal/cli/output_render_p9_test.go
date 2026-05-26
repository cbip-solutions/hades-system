package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestAuditChainHistory_YAMLFormatParses(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"history", "--format", "yaml"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v", err)
	}
	var got any
	if err := yaml.Unmarshal([]byte(stdout), &got); err != nil {
		t.Errorf("--format yaml output is not valid YAML: %v\noutput=%s", err, stdout)
	}
}

func TestStateHistory_JSONFormatParses(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"history", "--json"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI error: %v", err)
	}
	var got []client.StateChange
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Errorf("--json output is not valid JSON array: %v\noutput=%s", err, stdout)
	}
}

func TestAdrGraph_LastChildGlyph(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"graph", "--from", "ADR-0001"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI error: %v", err)
	}

	if !strings.Contains(stdout, "└─") {
		t.Errorf("└─ glyph not found in ASCII tree output (last-child rendering broken):\n%s", stdout)
	}
}

func TestStateShow_TableIncludesManualFieldCount(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"show"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI error: %v", err)
	}

	if !strings.Contains(stdout, "manual_field_count") {
		t.Errorf("manual_field_count missing from state show output:\n%s", stdout)
	}
}

func TestResearchHistory_QuietSuppressesColumnHeaders(t *testing.T) {
	srv := mockResearchP9Server(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewResearchCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"history", "--quiet"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("CLI error: %v", err)
	}
	got := stdout.String()

	for _, header := range []string{"QUERY", "SOURCE", "FINDINGS", "DISPATCHED"} {
		if strings.Contains(got, header) {
			t.Errorf("--quiet: column header %q should be suppressed, got:\n%s", header, got)
		}
	}
}
