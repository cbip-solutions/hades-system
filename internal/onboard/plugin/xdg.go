// SPDX-License-Identifier: MIT
package plugin

import (
	"os"
	"path/filepath"
)

func XDGConfigDir(app string) string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, app)
	}
	home, _ := homeDirOrDot()
	return filepath.Join(home, ".config", app)
}

func XDGStateDir(app string) string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, app)
	}
	home, _ := homeDirOrDot()
	return filepath.Join(home, ".local", "state", app)
}

func XDGCacheDir(app string) string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, app)
	}
	home, _ := homeDirOrDot()
	return filepath.Join(home, ".cache", app)
}

func XDGDataDir(app string) string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, app)
	}
	home, _ := homeDirOrDot()
	return filepath.Join(home, ".local", "share", app)
}

func homeDirOrDot() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".", err
	}
	return home, nil
}
