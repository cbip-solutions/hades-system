package compliance_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/plugin"
)

func TestInvZen186XDGCanonicalWithXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got := plugin.XDGConfigDir("zen-swarm")
	want := filepath.Join("/custom/xdg", "zen-swarm")
	if got != want {
		t.Errorf("XDGConfigDir with XDG_CONFIG_HOME: got %q, want %q", got, want)
	}
}

func TestInvZen186XDGCanonicalFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := plugin.XDGConfigDir("zen-swarm")
	want := filepath.Join("/home/test", ".config", "zen-swarm")
	if got != want {
		t.Errorf("XDGConfigDir fallback: got %q, want %q", got, want)
	}
}

func TestInvZen186XDGStateDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got := plugin.XDGStateDir("zen-swarm")
	want := filepath.Join("/custom/state", "zen-swarm")
	if got != want {
		t.Errorf("XDGStateDir: got %q, want %q", got, want)
	}
}

func TestInvZen186XDGCacheDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	got := plugin.XDGCacheDir("zen-swarm")
	want := filepath.Join("/custom/cache", "zen-swarm")
	if got != want {
		t.Errorf("XDGCacheDir: got %q, want %q", got, want)
	}
}

func TestInvZen186XDGPathsCrossplatform(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/cross/platform/home")
	got := plugin.XDGConfigDir("zen-swarm")
	if !strings.HasPrefix(got, "/cross/platform/home") {
		t.Errorf("XDGConfigDir cross-platform: got %q, expected prefix /cross/platform/home", got)
	}
}
