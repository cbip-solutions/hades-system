package federation

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkspaceDBPath_ZenStateDirOverridesEverything(t *testing.T) {
	got, err := WorkspaceDBPath(map[string]string{
		"ZEN_STATE_DIR":  "/srv/zen",
		"XDG_STATE_HOME": "/should/not/win",
		"HOME":           "/should/not/win/either",
	})
	if err != nil {
		t.Fatalf("WorkspaceDBPath: %v", err)
	}
	want := filepath.Join("/srv/zen", "zen-swarm", "workspace.db")
	if got != want {
		t.Errorf("WorkspaceDBPath = %q; want %q (ZEN_STATE_DIR overrides)", got, want)
	}
}

func TestWorkspaceDBPath_XDGStateHome(t *testing.T) {
	got, err := WorkspaceDBPath(map[string]string{
		"XDG_STATE_HOME": "/home/user/.local/state",
		"HOME":           "/home/user",
	})
	if err != nil {
		t.Fatalf("WorkspaceDBPath: %v", err)
	}
	want := filepath.Join("/home/user/.local/state", "zen-swarm", "workspace.db")
	if got != want {
		t.Errorf("WorkspaceDBPath = %q; want %q (XDG_STATE_HOME)", got, want)
	}
}

func TestWorkspaceDBPath_MacOSFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only fallback")
	}
	got, err := WorkspaceDBPath(map[string]string{"HOME": "/Users/operator"})
	if err != nil {
		t.Fatalf("WorkspaceDBPath: %v", err)
	}
	want := filepath.Join("/Users/operator/Library/Application Support", "zen-swarm", "workspace.db")
	if got != want {
		t.Errorf("WorkspaceDBPath = %q; want %q (macOS fallback)", got, want)
	}
}

func TestWorkspaceDBPath_POSIXFallback(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin uses Library fallback, not .local/state")
	}
	got, err := WorkspaceDBPath(map[string]string{"HOME": "/home/u"})
	if err != nil {
		t.Fatalf("WorkspaceDBPath: %v", err)
	}
	want := filepath.Join("/home/u/.local/state", "zen-swarm", "workspace.db")
	if got != want {
		t.Errorf("WorkspaceDBPath = %q; want %q (POSIX fallback)", got, want)
	}
}

func TestWorkspaceDBPath_EmptyEnv_NoHome(t *testing.T) {
	_, err := WorkspaceDBPath(map[string]string{})
	if err == nil {
		t.Fatal("WorkspaceDBPath({}) returned nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "HOME") && !strings.Contains(err.Error(), "ZEN_STATE_DIR") {
		t.Errorf("err %q does not mention HOME or ZEN_STATE_DIR", err)
	}
}

func TestWorkspaceDBPath_NilEnvSafe(t *testing.T) {
	_, err := WorkspaceDBPath(nil)
	if err == nil {
		t.Fatal("WorkspaceDBPath(nil) returned nil err; want non-nil")
	}

	if err.Error() == "" {
		t.Errorf("err has empty message; want a usable diagnostic")
	}
}
