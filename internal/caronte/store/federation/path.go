// SPDX-License-Identifier: MIT
package federation

import (
	"fmt"
	"path/filepath"
	"runtime"
)

func WorkspaceDBPath(env map[string]string) (string, error) {
	if zenState := env["ZEN_STATE_DIR"]; zenState != "" {
		return filepath.Join(zenState, "zen-swarm", "workspace.db"), nil
	}
	if xdg := env["XDG_STATE_HOME"]; xdg != "" {
		return filepath.Join(xdg, "zen-swarm", "workspace.db"), nil
	}
	home := env["HOME"]
	if home == "" {
		return "", fmt.Errorf("caronte/store/federation: cannot resolve workspace.db path — neither ZEN_STATE_DIR nor XDG_STATE_HOME nor HOME is set in env")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "zen-swarm", "workspace.db"), nil
	}
	return filepath.Join(home, ".local", "state", "zen-swarm", "workspace.db"), nil
}
