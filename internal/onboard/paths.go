// SPDX-License-Identifier: MIT
package onboard

import (
	"os"
	"path/filepath"
	"runtime"
)

func GlobalConfigPath() string {
	return filepath.Join(xdgConfigHome(), "hades-system", "config.toml")
}

func GlobalDoctrinesDir() string {
	return filepath.Join(xdgConfigHome(), "hades-system", "doctrines")
}

func GlobalProvidersDir() string {
	return filepath.Join(xdgConfigHome(), "hades-system", "providers")
}

// OnboardPrefsPath returns the onboard prefs path, mirroring
// `internal/onboard/prefs.Path()` so callers that already import the
// parent `onboard` package can resolve the path without taking a
// transitive `prefs` dependency. C3 reconciliation 2026-05-14: the two
// resolvers MUST agree byte-for-byte under any XDG_CONFIG_HOME +
// HOME / USERPROFILE configuration.
func OnboardPrefsPath() string {
	return filepath.Join(xdgConfigHome(), "hades-system", "onboard-prefs.toml")
}

func xdgConfigHome() string {
	return resolveXDGConfigHome(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func resolveXDGConfigHome(goos string, getenv func(string) string, homeFn func() (string, error)) string {
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	if goos == "windows" {
		if appdata := getenv("APPDATA"); appdata != "" {
			return appdata
		}
	}
	home, err := homeFn()
	if err != nil || home == "" {

		return "."
	}
	return filepath.Join(home, ".config")
}
