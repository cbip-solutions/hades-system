// SPDX-License-Identifier: MIT
// Package fix — plugin_format_fix.go ships the DESTRUCTIVE Fix impl for
// the hermes.plugin-format check (inv-hades-176 + 190).
//
// Delegates to:
// - internal/doctor/backup (F4) for backup-before-modify (inv-hades-177)
// - internal/migrate/writer for fresh plugin scaffold
//
// inv-hades-178 enforcement: declared via interface guard
// `_ Destructive = (*PluginFormatFix)(nil)` AND IsDestructive() returns true.
// The GuardDestructive gate rejects FixModeAutoSafe/FixModeInteractive
// without TTY; only FixModeYes (explicit) and FixModeInteractive in TTY
// proceed.
//
// Failure halts:
// - Backup fails → halt; plugin untouched
// - Backup succeeds + delete fails → halt; manifest references backup
// for `hades doctor restore <ID>` reverse op
// - Delete succeeds + scaffold fails → halt; manifest references backup
// for reverse op
//
// Operator UX: post-failure, the error string carries the BackupID so the
// operator can run `hades doctor restore <ID>` immediately.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type PluginScaffolder interface {
	ScaffoldFreshPlugin(ctx context.Context, targetPath string) error
}

type Backuper interface {
	BackupTarget(ctx context.Context, checkName, path string) (manifest backup.Manifest, err error)
	RemoveAfterBackup(ctx context.Context, path string) error
}

type PluginFormatFix struct {
	pluginPath string
	scaffolder PluginScaffolder
	backuper   Backuper
}

type PluginFormatFixConfig struct {
	PluginPath string
	Scaffolder PluginScaffolder
	Backuper   Backuper
}

func NewPluginFormatFix(cfg PluginFormatFixConfig) *PluginFormatFix {
	return &PluginFormatFix{
		pluginPath: cfg.PluginPath,
		scaffolder: cfg.Scaffolder,
		backuper:   cfg.Backuper,
	}
}

func (p *PluginFormatFix) Name() string { return "hermes.plugin-format" }

func (p *PluginFormatFix) IsDestructive() bool { return true }

func (p *PluginFormatFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		return fmt.Errorf("fix: read-only mode; run `hades doctor full --fix --yes` (destructive: backup + delete + scaffold)")
	}

	if p.backuper == nil {
		return errors.New("fix: backuper not configured (F4 dependency missing)")
	}
	if p.pluginPath == "" {
		return errors.New("fix: pluginPath not configured (caller must resolve via hermes.PluginPathResolver)")
	}
	manifest, err := p.backuper.BackupTarget(ctx, p.Name(), p.pluginPath)
	if err != nil {
		return fmt.Errorf("fix: backup failed; refusing to destroy plugin: %w", err)
	}

	if _, sterr := os.Stat(manifest.Path); sterr != nil {
		return fmt.Errorf("fix: backup manifest stat failed at %s: %w; refusing to proceed", manifest.Path, sterr)
	}

	if err := p.backuper.RemoveAfterBackup(ctx, p.pluginPath); err != nil {
		return fmt.Errorf("fix: delete plugin failed (backup at %s for manual restore via `hades doctor restore %s`): %w",
			manifest.Path, manifest.BackupID, err)
	}

	if p.scaffolder == nil {
		return fmt.Errorf("fix: scaffolder not configured; plugin deleted (backup at %s; restore via `hades doctor restore %s`)",
			manifest.Path, manifest.BackupID)
	}
	if err := p.scaffolder.ScaffoldFreshPlugin(ctx, p.pluginPath); err != nil {
		return fmt.Errorf("fix: scaffold failed (backup at %s; restore via `hades doctor restore %s`): %w",
			manifest.Path, manifest.BackupID, err)
	}

	return nil
}

var (
	_ Destructive = (*PluginFormatFix)(nil)
	_ Applier     = (*PluginFormatFix)(nil)
)
