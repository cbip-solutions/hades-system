package cli_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

var updateGolden = flag.Bool("update", false, "regenerate golden help snapshots")

func helpForArgs(args []string) (string, error) {
	root := cli.NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append(args, "--help"))
	err := root.Execute()
	if err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String() + stderr.String(), nil
}

func TestHelp_Golden_Root(t *testing.T) {
	body, err := helpForArgs(nil)
	if err != nil {
		t.Fatalf("help root: %v", err)
	}
	checkOrUpdate(t, "help-root-direct.golden", body)
}

func TestHelp_Golden_AllLeaves(t *testing.T) {
	leaves := []string{
		"list", "show", "status", "history", "diff", "validate",
		"init", "migrate", "override", "reload", "reinforce",

		"propose-list", "ack", "deny", "revert", "propose",
	}
	for _, leaf := range leaves {
		t.Run(leaf, func(t *testing.T) {
			body, err := helpForArgs([]string{leaf})
			if err != nil {
				t.Fatalf("help %s: %v", leaf, err)
			}
			checkOrUpdate(t, "help-"+leaf+"-direct.golden", body)
		})
	}
}

func TestHelp_Golden_OverrideEdit(t *testing.T) {
	body, err := helpForArgs([]string{"override", "edit"})
	if err != nil {
		t.Fatalf("help override edit: %v", err)
	}
	checkOrUpdate(t, "help-override-edit-direct.golden", body)
}

func TestHelp_AllRequiredKeywordsPresent(t *testing.T) {
	body, err := helpForArgs(nil)
	if err != nil {
		t.Fatalf("help root: %v", err)
	}
	required := []string{
		"doctrina",
		"Lectura",
		"Escritura",
		"Enmienda",
		"Depuración",
		"--json",
		"--quiet",
		"--verbose",
		"--since",
		"--limit",
		"--filter",
		"--format",
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Errorf("required keyword %q missing from --help body", want)
		}
	}
}

func checkOrUpdate(t *testing.T, filename, actual string) {
	t.Helper()
	path := filepath.Join("testdata", filename)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", path, err)
	}
	if string(want) != actual {
		t.Errorf("golden mismatch for %s.\n--- want ---\n%s\n--- got ---\n%s",
			filename, want, actual)
	}
}
