// go:build a7_full

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestPrefsRoundtripAcrossSchemaVersionUpgrade(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")

	major := strings.SplitN(onboard.CurrentPrefsSchemaVersion, ".", 2)[0]
	content := `schema_version = "` + major + `.99"
[global]
llm_provider = "anthropic-paygo"
doctrine_profile = "max-scope"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := onboard.LoadPrefs(path)
	if err != nil {
		t.Fatalf("LoadPrefs minor-bump: %v", err)
	}
	if p == nil {
		t.Fatal("LoadPrefs returned nil pointer + nil error")
	}

	if err := onboard.SavePrefs(path, p); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `schema_version = "` + onboard.CurrentPrefsSchemaVersion + `"`
	if !strings.Contains(string(got), want) {
		t.Errorf("SavePrefs did not rewrite schema_version to %q; content:\n%s", onboard.CurrentPrefsSchemaVersion, got)
	}
}

func TestPrefsXDGAndDefaultPath(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	t.Run("xdg-explicit", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/integ-xdg")
		t.Setenv("HOME", "/tmp/integ-home-ignored")
		want := filepath.Join("/tmp/integ-xdg", "zen-swarm", "onboard-prefs.toml")
		got := onboard.PrefsPath()
		if got != want {
			t.Errorf("XDG_CONFIG_HOME precedence: got %q, want %q", got, want)
		}
	})

	t.Run("home-fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/tmp/integ-home")
		want := filepath.Join("/tmp/integ-home", ".config", "zen-swarm", "onboard-prefs.toml")
		got := onboard.PrefsPath()
		if got != want {
			t.Errorf("HOME fallback: got %q, want %q", got, want)
		}
	})
}
