package onboard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGlobalConfigPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := GlobalConfigPath()
	want := filepath.Join("/tmp/xdg-test", "zen-swarm", "config.toml")
	if got != want {
		t.Errorf("GlobalConfigPath: got %q want %q", got, want)
	}
}

func TestGlobalConfigPathDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME semantics differ on Windows; covered by TestGlobalConfigPathWindowsAPPDATA")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got := GlobalConfigPath()
	want := filepath.Join("/tmp/home", ".config", "zen-swarm", "config.toml")
	if got != want {
		t.Errorf("GlobalConfigPath default: got %q want %q", got, want)
	}
}

func TestGlobalDoctrinesDirDefaultPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME semantics differ on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got := GlobalDoctrinesDir()
	want := filepath.Join("/tmp/home", ".config", "zen-swarm", "doctrines")
	if got != want {
		t.Errorf("GlobalDoctrinesDir: got %q want %q", got, want)
	}
}

func TestGlobalDoctrinesDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := GlobalDoctrinesDir()
	want := filepath.Join("/tmp/xdg-test", "zen-swarm", "doctrines")
	if got != want {
		t.Errorf("GlobalDoctrinesDir XDG: got %q want %q", got, want)
	}
}

func TestGlobalProvidersDirDefaultPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME semantics differ on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got := GlobalProvidersDir()
	want := filepath.Join("/tmp/home", ".config", "zen-swarm", "providers")
	if got != want {
		t.Errorf("GlobalProvidersDir: got %q want %q", got, want)
	}
}

func TestGlobalProvidersDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := GlobalProvidersDir()
	want := filepath.Join("/tmp/xdg-test", "zen-swarm", "providers")
	if got != want {
		t.Errorf("GlobalProvidersDir XDG: got %q want %q", got, want)
	}
}

func TestOnboardPrefsPathMirrorsPrefsPackage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := OnboardPrefsPath()
	want := filepath.Join("/tmp/xdg-test", "zen-swarm", "onboard-prefs.toml")
	if got != want {
		t.Errorf("OnboardPrefsPath: got %q want %q", got, want)
	}
}

func TestOnboardPrefsPathDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME semantics differ on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got := OnboardPrefsPath()
	want := filepath.Join("/tmp/home", ".config", "zen-swarm", "onboard-prefs.toml")
	if got != want {
		t.Errorf("OnboardPrefsPath default: got %q want %q", got, want)
	}
}

func TestPathsHomeUnreadableFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses %APPDATA% not $HOME")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	got := GlobalConfigPath()

	suffix := filepath.Join("zen-swarm", "config.toml")
	if filepath.Base(got) != "config.toml" {
		t.Errorf("GlobalConfigPath fallback: got %q; expected suffix %q", got, suffix)
	}
}

func TestPathsAllResolversShareRoot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/root-test")
	wantPrefix := filepath.Join("/tmp/root-test", "zen-swarm")
	cases := map[string]string{
		"GlobalConfigPath":   GlobalConfigPath(),
		"GlobalDoctrinesDir": GlobalDoctrinesDir(),
		"GlobalProvidersDir": GlobalProvidersDir(),
		"OnboardPrefsPath":   OnboardPrefsPath(),
	}
	for name, got := range cases {
		if !filepathHasPrefix(got, wantPrefix) {
			t.Errorf("%s = %q; missing prefix %q", name, got, wantPrefix)
		}
	}
}

func filepathHasPrefix(path, prefix string) bool {
	cleanPath := filepath.Clean(path)
	cleanPrefix := filepath.Clean(prefix)
	if len(cleanPath) < len(cleanPrefix) {
		return false
	}
	return cleanPath[:len(cleanPrefix)] == cleanPrefix
}

func TestPathsAreAbsolute(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/abs-test")
	for _, p := range []string{
		GlobalConfigPath(),
		GlobalDoctrinesDir(),
		GlobalProvidersDir(),
		OnboardPrefsPath(),
	} {
		if !filepath.IsAbs(p) {
			t.Errorf("path %q is not absolute", p)
		}
	}
}

func TestResolveXDGConfigHomeWindowsAPPDATA(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "APPDATA":
			return `C:\Users\Test\AppData\Roaming`
		default:
			return ""
		}
	}
	homeFn := func() (string, error) { return "C:/Users/Test", nil }
	got := resolveXDGConfigHome("windows", getenv, homeFn)
	want := `C:\Users\Test\AppData\Roaming`
	if got != want {
		t.Errorf("resolveXDGConfigHome windows+APPDATA: got %q want %q", got, want)
	}
}

func TestResolveXDGConfigHomeWindowsNoAPPDATAFallsBackToHome(t *testing.T) {
	getenv := func(string) string { return "" }
	homeFn := func() (string, error) { return "C:/Users/Test", nil }
	got := resolveXDGConfigHome("windows", getenv, homeFn)
	want := filepath.Join("C:/Users/Test", ".config")
	if got != want {
		t.Errorf("resolveXDGConfigHome windows-no-APPDATA: got %q want %q", got, want)
	}
}

func TestResolveXDGConfigHomeXDGAlwaysWins(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return "/xdg/win"
		case "APPDATA":
			return `C:\AppData`
		default:
			return ""
		}
	}
	homeFn := func() (string, error) { return "/home/test", nil }
	got := resolveXDGConfigHome("windows", getenv, homeFn)
	want := "/xdg/win"
	if got != want {
		t.Errorf("resolveXDGConfigHome XDG wins: got %q want %q", got, want)
	}
}

func TestResolveXDGConfigHomeLastResort(t *testing.T) {
	getenv := func(string) string { return "" }
	homeFn := func() (string, error) { return "", os.ErrNotExist }
	got := resolveXDGConfigHome("linux", getenv, homeFn)
	if got != "." {
		t.Errorf("resolveXDGConfigHome last-resort: got %q want '.'", got)
	}
}

func TestResolveXDGConfigHomeEmptyHomeWithoutError(t *testing.T) {
	getenv := func(string) string { return "" }
	homeFn := func() (string, error) { return "", nil }
	got := resolveXDGConfigHome("linux", getenv, homeFn)
	if got != "." {
		t.Errorf("resolveXDGConfigHome empty-home: got %q want '.'", got)
	}
}

func TestResolveXDGConfigHomeLinuxDefault(t *testing.T) {
	getenv := func(string) string { return "" }
	homeFn := func() (string, error) { return "/home/op", nil }
	got := resolveXDGConfigHome("linux", getenv, homeFn)
	want := filepath.Join("/home/op", ".config")
	if got != want {
		t.Errorf("resolveXDGConfigHome linux: got %q want %q", got, want)
	}
}
