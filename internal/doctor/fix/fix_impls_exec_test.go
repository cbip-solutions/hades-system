package fix_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

func fakeBinDir(t *testing.T, commands map[string]struct {
	exitCode int
	output   string
}) string {
	t.Helper()
	dir := t.TempDir()
	for name, spec := range commands {
		script := "#!/bin/sh\n"
		if spec.output != "" {

			script += "echo " + quoteForShell(spec.output) + "\n"
		}
		script += "exit " + itoa(spec.exitCode) + "\n"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile fake %s: %v", name, err)
		}
	}
	return dir
}

func quoteForShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+(n%10))) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

func TestHermesInstallFixDarwinBrewSucceeds(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("hermes install fix exec path only runs on darwin")
	}
	dir := fakeBinDir(t, map[string]struct {
		exitCode int
		output   string
	}{
		"brew": {exitCode: 0, output: "==> Installed hermes-agent"},
	})
	t.Setenv("PATH", dir)
	h := &fix.HermesInstallFix{}
	if err := h.Apply(context.Background(), check.FixModeYes); err != nil {
		t.Errorf("Apply with fake brew: %v", err)
	}
}

func TestHermesInstallFixDarwinBrewFails(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("hermes install fix exec path only runs on darwin")
	}
	dir := fakeBinDir(t, map[string]struct {
		exitCode int
		output   string
	}{
		"brew": {exitCode: 1, output: "Error: cannot find formula"},
	})
	t.Setenv("PATH", dir)
	h := &fix.HermesInstallFix{}
	err := h.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "brew install failed") {
		t.Errorf("err = %v, want 'brew install failed'", err)
	}
}

func TestHermesInstallFixDarwinBrewMissing(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only meaningful on darwin")
	}
	t.Setenv("PATH", t.TempDir())
	h := &fix.HermesInstallFix{}
	err := h.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "brew not found") {
		t.Errorf("err = %v, want 'brew not found'", err)
	}
}

func TestHermesInstallFixNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("only meaningful on non-darwin")
	}
	h := &fix.HermesInstallFix{}
	err := h.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "auto-install unsupported") {
		t.Errorf("err = %v, want 'auto-install unsupported'", err)
	}
}

func TestBypassConfigFixExecCmdFails(t *testing.T) {
	dir := fakeBinDir(t, map[string]struct {
		exitCode int
		output   string
	}{
		"zen": {exitCode: 7, output: "extract-config: not authorized"},
	})
	t.Setenv("PATH", dir)
	b := &fix.BypassConfigFix{}
	err := b.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "extract-config failed") {
		t.Errorf("err = %v, want substring 'extract-config failed'", err)
	}
}

func TestBypassConfigFixExecCmdSucceeds(t *testing.T) {
	dir := fakeBinDir(t, map[string]struct {
		exitCode int
		output   string
	}{
		"zen": {exitCode: 0, output: "config extracted"},
	})
	t.Setenv("PATH", dir)
	b := &fix.BypassConfigFix{}
	if err := b.Apply(context.Background(), check.FixModeYes); err != nil {
		t.Errorf("Apply success: %v", err)
	}
}

func TestDaemonRunningFixExec(t *testing.T) {
	for _, tc := range []struct {
		name       string
		exit       int
		wantSubstr string
	}{
		{"fail", 1, "daemon start failed"},
		{"succeed", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeBinDir(t, map[string]struct {
				exitCode int
				output   string
			}{
				"zen": {exitCode: tc.exit, output: "daemon: " + tc.name},
			})
			t.Setenv("PATH", dir)
			d := &fix.DaemonRunningFix{}
			err := d.Apply(context.Background(), check.FixModeYes)
			if tc.wantSubstr == "" {
				if err != nil {
					t.Errorf("Apply success: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestSchemaVersionFixExec(t *testing.T) {
	for _, tc := range []struct {
		name       string
		exit       int
		wantSubstr string
	}{
		{"fail", 1, "migrate up failed"},
		{"succeed", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeBinDir(t, map[string]struct {
				exitCode int
				output   string
			}{
				"zen": {exitCode: tc.exit, output: "migrate: " + tc.name},
			})
			t.Setenv("PATH", dir)
			s := &fix.SchemaVersionFix{}
			err := s.Apply(context.Background(), check.FixModeYes)
			if tc.wantSubstr == "" {
				if err != nil {
					t.Errorf("Apply success: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestCuratedMCPFixExecBranches(t *testing.T) {
	tests := []struct {
		name string
		pm   string
		exit int
		want string
	}{
		{"npm-success", "npm", 0, ""},
		{"npm-fail", "npm", 1, "install foo via npm failed"},
		{"brew-success", "brew", 0, ""},
		{"brew-fail", "brew", 1, "install foo via brew failed"},
		{"pip-success", "pip", 0, ""},
		{"pip-fail", "pip", 1, "install foo via pip failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeBinDir(t, map[string]struct {
				exitCode int
				output   string
			}{
				tc.pm: {exitCode: tc.exit, output: tc.name},
			})
			t.Setenv("PATH", dir)
			c := &fix.CuratedMCPFix{
				MissingMCPs: []fix.MCPInstallSpec{
					{Name: "foo", PackageManager: tc.pm, PackageName: "foo"},
				},
			}
			err := c.Apply(context.Background(), check.FixModeYes)
			if tc.want == "" {
				if err != nil {
					t.Errorf("Apply %s success: %v", tc.name, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
