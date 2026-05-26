package plugin

import (
	"path/filepath"
	"testing"
)

func TestXDGConfigDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got := XDGConfigDir("zen-swarm")
	want := filepath.Join("/custom/xdg", "zen-swarm")
	if got != want {
		t.Errorf("XDGConfigDir = %q, want %q", got, want)
	}
}

func TestXDGConfigDirFallbackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := XDGConfigDir("zen-swarm")
	want := filepath.Join("/home/test", ".config", "zen-swarm")
	if got != want {
		t.Errorf("XDGConfigDir = %q, want %q", got, want)
	}
}

func TestXDGStateDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got := XDGStateDir("zen-swarm")
	want := filepath.Join("/custom/state", "zen-swarm")
	if got != want {
		t.Errorf("XDGStateDir = %q, want %q", got, want)
	}
}

func TestXDGStateDirFallbackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := XDGStateDir("zen-swarm")
	want := filepath.Join("/home/test", ".local", "state", "zen-swarm")
	if got != want {
		t.Errorf("XDGStateDir = %q, want %q", got, want)
	}
}

func TestXDGCacheDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	got := XDGCacheDir("zen-swarm")
	want := filepath.Join("/custom/cache", "zen-swarm")
	if got != want {
		t.Errorf("XDGCacheDir = %q, want %q", got, want)
	}
}

func TestXDGCacheDirFallbackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := XDGCacheDir("zen-swarm")
	want := filepath.Join("/home/test", ".cache", "zen-swarm")
	if got != want {
		t.Errorf("XDGCacheDir = %q, want %q", got, want)
	}
}

func TestXDGDataDirRespectsEnv(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got := XDGDataDir("zen-swarm")
	want := filepath.Join("/custom/data", "zen-swarm")
	if got != want {
		t.Errorf("XDGDataDir = %q, want %q", got, want)
	}
}

func TestXDGDataDirFallbackToHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := XDGDataDir("zen-swarm")
	want := filepath.Join("/home/test", ".local", "share", "zen-swarm")
	if got != want {
		t.Errorf("XDGDataDir = %q, want %q", got, want)
	}
}

func TestXDGHomeFallbackToUserHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	got := XDGConfigDir("zen-swarm")
	if got == "" {
		t.Error("XDGConfigDir returned empty string with HOME unset")
	}
}
