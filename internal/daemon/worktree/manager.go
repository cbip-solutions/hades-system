// SPDX-License-Identifier: MIT
// Package worktree owns git worktree lifecycle for swarm tasks
// (spec §2.5). Each task gets a worktree under
// ~/.local/share/zen-swarm/worktrees/<project>/<feature>/<task-id>/
// on a unique branch zen/<feature>/<task-id>.
package worktree

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Spec struct {
	ProjectPath string
	Project     string
	Feature     string
	TaskID      string
}

type Worktree struct {
	Path   string
	Branch string
	Spec   Spec
}

type Manager struct {
}

func NewManager(rootDir string) *Manager { return &Manager{} }

func (m *Manager) Create(s Spec) (*Worktree, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}

func (m *Manager) Remove(w *Worktree) error {
	return zerrors.ErrNotImplementedPlan5
}

func (m *Manager) ListActive() ([]*Worktree, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}

func (m *Manager) CleanOlderThan(days int) (int, error) {
	return 0, zerrors.ErrNotImplementedPlan5
}
