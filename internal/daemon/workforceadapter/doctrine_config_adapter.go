// SPDX-License-Identifier: MIT
package workforceadapter

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

type DoctrineConfigAdapter struct {
	resolved doctrine.Resolved

	repoRoot string

	readFile func(path string) ([]byte, error)
}

type DoctrineConfigAdapterOptions struct {
	Resolved doctrine.Resolved

	RepoRoot string

	ReadFile func(path string) ([]byte, error)
}

func NewDoctrineConfigAdapter(opts DoctrineConfigAdapterOptions) (*DoctrineConfigAdapter, error) {
	if opts.Resolved.Schema.Name == "" && len(opts.Resolved.Provenance) == 0 {
		return nil, errors.New("workforceadapter: NewDoctrineConfigAdapter: Resolved is empty")
	}
	rf := opts.ReadFile
	if rf == nil {
		rf = os.ReadFile
	}
	return &DoctrineConfigAdapter{
		resolved: opts.Resolved,
		repoRoot: opts.RepoRoot,
		readFile: rf,
	}, nil
}

var _ worker.DoctrineConfig = (*DoctrineConfigAdapter)(nil)

func (a *DoctrineConfigAdapter) ReinforcementTemplate(name string) string {
	pointer := a.resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer
	if pointer == "" || a.repoRoot == "" {
		return placeholderMarker(name)
	}
	abs := pointer
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.repoRoot, pointer)
	}
	body, err := a.readFile(abs)
	if err != nil {
		return placeholderMarker(name)
	}
	if len(body) == 0 {
		return placeholderMarker(name)
	}
	return string(body)
}

func (a *DoctrineConfigAdapter) CheckpointDeadline(name string) time.Duration {
	d := time.Duration(a.resolved.Schema.Subprocess.PersistentTTLSliding)
	if d <= 0 {
		return worker.DefaultCheckpointDeadline
	}

	const maxTacticalWindow = 5 * time.Minute
	if d > maxTacticalWindow {
		return maxTacticalWindow
	}
	return d
}

func placeholderMarker(name string) string {
	return "[doctrine: " + name + "]"
}
