// SPDX-License-Identifier: MIT
package worktree

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Bootstrap struct {
	Language string
	Commands []string
}

func SetupEnv(worktreePath string, bs Bootstrap) error {
	return zerrors.ErrNotImplementedPlan5
}

func LoadBootstrap(projectPath string) (Bootstrap, error) {
	return Bootstrap{}, zerrors.ErrNotImplementedPlan5
}
