package integration_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
)

func TestXDGPathPatternIsSeparatorAware(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		subdirs []string
	}{
		{"linux-style", "/home/operator/.config", []string{"zen-swarm", "config.toml"}},
		{"macos-style", "/Users/operator/Library/Application Support", []string{"zen-swarm", "onboard-prefs.toml"}},
		{"windows-style", `C:\Users\operator\AppData\Roaming`, []string{"zen-swarm", "config.toml"}},
		{"xdg-explicit", "/tmp/xdg-override", []string{"zen-swarm"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{tc.base}, tc.subdirs...)
			got := filepath.Join(args...)

			if !strings.ContainsRune(got, filepath.Separator) {
				t.Errorf("filepath.Join(%q, %v) did not emit OS separator %q: got %q",
					tc.base, tc.subdirs, string(filepath.Separator), got)
			}

			tail := tc.subdirs[len(tc.subdirs)-1]
			if !strings.HasSuffix(got, tail) {
				t.Errorf("filepath.Join did not preserve tail %q in %q", tail, got)
			}
		})
	}
}

func TestXDGEnvPrecedencePattern(t *testing.T) {

	resolveBase := func(xdg, home string) string {
		if xdg != "" {
			return xdg
		}
		return filepath.Join(home, ".config")
	}

	t.Run("xdg-explicit-wins", func(t *testing.T) {
		got := resolveBase("/explicit/xdg", "/home/u")
		if got != "/explicit/xdg" {
			t.Errorf("XDG explicit precedence: got %q, want /explicit/xdg", got)
		}
	})

	t.Run("home-fallback", func(t *testing.T) {
		got := resolveBase("", "/home/u")
		want := filepath.Join("/home/u", ".config")
		if got != want {
			t.Errorf("HOME fallback: got %q, want %q", got, want)
		}
	})

	t.Run("env-swap-deterministic", func(t *testing.T) {

		a := resolveBase("/swap-1", "")
		b := resolveBase("/swap-1", "")
		if a != b {
			t.Errorf("non-deterministic resolver: a=%q b=%q", a, b)
		}
		c := resolveBase("/swap-2", "")
		if a == c {
			t.Errorf("resolver insensitive to XDG swap: a=%q c=%q", a, c)
		}
	})
}

func TestXDGPathConstructionIsRuntimeAware(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin", "windows":

	default:
		t.Errorf("unknown GOOS %q: Plan 13 spec §6.3 declares CI matrix {linux,darwin,windows}; add coverage if extending", runtime.GOOS)
	}
}

func TestOnboardTypesCompileAcrossPlatforms(t *testing.T) {

	_ = onboard.WizardKindGlobal
	_ = onboard.WizardKindGreenfield
	_ = onboard.WizardKindBrownfield
	_ = onboard.ModeRecommended
	_ = onboard.ModeReuse
	_ = onboard.ModeCustomize
	if !onboard.WizardKindGlobal.IsKnown() {
		t.Error("WizardKindGlobal.IsKnown() = false; expected true")
	}
	if !onboard.ModeRecommended.IsKnown() {
		t.Error("ModeRecommended.IsKnown() = false; expected true")
	}
}
