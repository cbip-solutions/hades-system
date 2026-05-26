package sshexec

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestAllowlistDoctrineOnly(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "max-scope",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *", "pytest *"},
				Hosts:    []string{"vps"},
			},
		},
	}
	a, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantPatterns := []string{"alembic *", "pytest *"}
	if !reflect.DeepEqual(a.Patterns, wantPatterns) {
		t.Errorf("Patterns = %v, want %v", a.Patterns, wantPatterns)
	}
	if a.Source != "doctrine" {
		t.Errorf("Source = %q, want %q", a.Source, "doctrine")
	}
	if a.Project != "internal-platform-x" {
		t.Errorf("Project = %q", a.Project)
	}
}

func TestAllowlistProjectNarrowsDoctrine(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "max-scope",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *", "pytest *", "psql *"},
				Hosts:    []string{"vps", "vps-staging"},
			},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *", "pytest *"]
hosts = ["vps"]
`)
	a, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	sort.Strings(a.Patterns)
	want := []string{"alembic *", "pytest *"}
	if !reflect.DeepEqual(a.Patterns, want) {
		t.Errorf("Patterns = %v, want %v (project narrows doctrine)", a.Patterns, want)
	}
	if !strings.Contains(a.Source, "merge") {
		t.Errorf("Source = %q, want contains 'merge'", a.Source)
	}
}

func TestAllowlistProjectCannotWidenDoctrine(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *", "psql *"]
hosts = ["vps"]
`)
	a, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatalf("Resolve unexpectedly succeeded with widened allowlist: %+v", a)
	}
	if !strings.Contains(err.Error(), "exceeds doctrine ceiling") {
		t.Errorf("err = %v, want contains 'exceeds doctrine ceiling'", err)
	}
}

func TestAllowlistEmptyEverywhereIsFailClosed(t *testing.T) {
	doc := &doctrine.Schema{Name: "capa-firewall"}
	a, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(a.Patterns) != 0 {
		t.Errorf("Patterns non-empty in fail-closed mode: %v", a.Patterns)
	}
	if len(a.Hosts) != 0 {
		t.Errorf("Hosts non-empty in fail-closed mode: %v", a.Hosts)
	}
}

func TestAllowlistRejectsInvalidPatterns(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"*", " ", "alembic *"},
				Hosts:    []string{"vps"},
			},
		},
	}
	_, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err == nil {
		t.Fatal("Resolve accepted lone '*' pattern; want rejected")
	}
}

func TestAllowlistHostNotInDoctrineRejected(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps", "rogue-host"]
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("Resolve accepted unauthorized host; want rejected")
	}
}

func TestAllowlistAcceptsSlashTrailingStarPattern(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{
					"pytest tests/integration/*",
					"alembic *",
					"git status",
				},
				Hosts: []string{"vps"},
			},
		},
	}
	a, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve rejected slash-trailing-star pattern: %v", err)
	}
	want := []string{"alembic *", "git status", "pytest tests/integration/*"}
	if !reflect.DeepEqual(a.Patterns, want) {
		t.Errorf("Patterns = %v, want %v", a.Patterns, want)
	}
}

func TestAllowlistRejectsMidStringStarPattern(t *testing.T) {
	cases := [][]string{
		{"pytest *.py"},
		{"alembic*"},
		{"docker run --name=*-foo"},
		{"some/*/middle"},
	}
	for _, pats := range cases {
		err := validatePatterns(pats)
		if err == nil {
			t.Errorf("validatePatterns(%v) accepted; want rejected", pats)
		}
	}
}

func TestAllowlistRejectsForbiddenCharInPattern(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic; rm"},
				Hosts:    []string{"vps"},
			},
		},
	}
	_, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err == nil {
		t.Fatal("Resolve accepted forbidden-char pattern; want rejected")
	}
}

func TestAllowlistMissingProjectFile(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{Patterns: []string{"alembic *"}, Hosts: []string{"vps"}},
		},
	}
	_, err := ResolveAllowlist(doc, "/nonexistent-path-abc/zenswarm.toml", "internal-platform-x")
	if err == nil {
		t.Fatal("Resolve unexpectedly succeeded with missing project file")
	}
	if !strings.Contains(err.Error(), "read project toml") {
		t.Errorf("err = %v, want contains 'read project toml'", err)
	}
}

func TestAllowlistInvalidProjectTOML(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{Patterns: []string{"alembic *"}, Hosts: []string{"vps"}},
		},
	}
	tomlPath := writeTempZenswarm(t, "this is not = valid TOML !! [")
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("Resolve accepted malformed TOML")
	}
	if !strings.Contains(err.Error(), "decode project toml") {
		t.Errorf("err = %v, want contains 'decode project toml'", err)
	}
}

func TestAllowlistNilDoctrineSchemaRejected(t *testing.T) {
	_, err := ResolveAllowlist(nil, "", "internal-platform-x")
	if err == nil {
		t.Fatal("nil doctrine schema accepted")
	}
}

func TestAllowlistProjectInvalidPatternRejected(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{Patterns: []string{"alembic *"}, Hosts: []string{"vps"}},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["*"]
hosts = ["vps"]
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("project lone '*' pattern accepted")
	}
}

func TestAllowlistDoctrineDefaultsCarried(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "max-scope",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
			Defaults: doctrine.SSHExecDefaults{
				Timeout:   doctrine.Duration(30 * time.Minute),
				MaxStdout: 64 * 1024 * 1024,
				MaxStderr: 8 * 1024 * 1024,
			},
		},
	}
	a, err := ResolveAllowlist(doc, "", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if a.Defaults.Timeout != 30*time.Minute {
		t.Errorf("Defaults.Timeout = %v, want 30m", a.Defaults.Timeout)
	}
	if a.Defaults.MaxStdout != 64*1024*1024 {
		t.Errorf("Defaults.MaxStdout = %d, want 67108864", a.Defaults.MaxStdout)
	}
	if a.Defaults.MaxStderr != 8*1024*1024 {
		t.Errorf("Defaults.MaxStderr = %d, want 8388608", a.Defaults.MaxStderr)
	}
}

func TestAllowlistProjectNarrowsDefaults(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "max-scope",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
			Defaults: doctrine.SSHExecDefaults{
				Timeout:   doctrine.Duration(30 * time.Minute),
				MaxStdout: 64 * 1024 * 1024,
				MaxStderr: 8 * 1024 * 1024,
			},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps"]

[ssh_exec.defaults]
timeout = "5m"
max_stdout = 16777216
max_stderr = 2097152
`)
	a, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if a.Defaults.Timeout != 5*time.Minute {
		t.Errorf("Defaults.Timeout = %v, want 5m (project narrowed)", a.Defaults.Timeout)
	}
	if a.Defaults.MaxStdout != 16777216 {
		t.Errorf("Defaults.MaxStdout = %d, want 16777216", a.Defaults.MaxStdout)
	}
}

func TestAllowlistProjectWidensDefaultsRejected(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
			Defaults: doctrine.SSHExecDefaults{Timeout: doctrine.Duration(10 * time.Minute)},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps"]

[ssh_exec.defaults]
timeout = "1h"
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("project widened timeout was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds doctrine ceiling") {
		t.Errorf("err = %v, want 'exceeds doctrine ceiling'", err)
	}
}

func TestAllowlistProjectWidensMaxStdoutRejected(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
			Defaults: doctrine.SSHExecDefaults{MaxStdout: 16 * 1024 * 1024},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps"]

[ssh_exec.defaults]
max_stdout = 67108864
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("project widened max_stdout was accepted")
	}
	if !strings.Contains(err.Error(), "max_stdout") {
		t.Errorf("err = %v, want contains 'max_stdout'", err)
	}
}

func TestAllowlistProjectWidensMaxStderrRejected(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
			Defaults: doctrine.SSHExecDefaults{MaxStderr: 1024 * 1024},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps"]

[ssh_exec.defaults]
max_stderr = 16777216
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("project widened max_stderr was accepted")
	}
	if !strings.Contains(err.Error(), "max_stderr") {
		t.Errorf("err = %v, want contains 'max_stderr'", err)
	}
}

func TestAllowlistProjectInvalidTimeoutFormat(t *testing.T) {
	doc := &doctrine.Schema{
		Name: "default",
		SSHExec: doctrine.SSHExecAxis{
			Allowlist: doctrine.SSHExecAllowlist{
				Patterns: []string{"alembic *"},
				Hosts:    []string{"vps"},
			},
		},
	}
	tomlPath := writeTempZenswarm(t, `
[ssh_exec.allowlist]
patterns = ["alembic *"]
hosts = ["vps"]

[ssh_exec.defaults]
timeout = "potato"
`)
	_, err := ResolveAllowlist(doc, tomlPath, "internal-platform-x")
	if err == nil {
		t.Fatal("invalid timeout was accepted")
	}
}

func TestAllowlistHostAllowed(t *testing.T) {
	a := &Allowlist{Hosts: []string{"vps", "vps-staging"}}
	if !a.HostAllowed("vps") {
		t.Errorf("HostAllowed(vps) = false")
	}
	if a.HostAllowed("rogue") {
		t.Errorf("HostAllowed(rogue) = true")
	}
}

func writeTempZenswarm(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	return p
}
