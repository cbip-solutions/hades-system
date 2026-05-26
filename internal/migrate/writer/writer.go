// SPDX-License-Identifier: MIT
package writer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

var (
	ErrTargetNotEmpty = errors.New("writer: target exists and is non-empty; pass --force to overwrite")

	ErrBackupFailed = errors.New("writer: backup creation failed")

	ErrAtomicSwapFailed = errors.New("writer: atomic swap failed")

	ErrUnknownEntryKind = errors.New("writer: unknown PlanEntry.Kind")

	ErrUnsupportedTarget = errors.New("writer: target path outside configured roots")
)

type WriterConfig struct {
	HermesPluginRoot string
	HermesConfigPath string
	ZenConfigRoot    string
	BackupRoot       string
	ForceOverwrite   bool
}

type Writer struct {
	cfg           WriterConfig
	registerCalls []string
}

func New(cfg WriterConfig) *Writer {
	return &Writer{cfg: cfg}
}

func (w *Writer) Apply(plan *mapping.Plan) error {
	if plan == nil {
		return nil
	}
	if err := w.preflightTargets(plan); err != nil {
		return err
	}
	if err := w.backupIfNeeded(plan); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	for _, e := range plan.Entries {
		if err := w.applyEntry(e); err != nil {
			return fmt.Errorf("apply %s %s: %w", e.Kind, e.TargetPath, err)
		}
	}

	if w.cfg.HermesPluginRoot != "" {
		if err := w.emitPluginYAML(plan); err != nil {
			return fmt.Errorf("emit plugin.yaml: %w", err)
		}
		if err := w.emitInitPy(); err != nil {
			return fmt.Errorf("emit __init__.py: %w", err)
		}
	}
	return nil
}

func WritePlugin(targetDir string, manifest []byte) error {
	if targetDir == "" {
		return errors.New("migrate.writer.WritePlugin: empty targetDir")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("WritePlugin mkdir: %w", err)
	}
	manifestPath := filepath.Join(targetDir, "plugin.yaml")
	if err := atomicWriteFile(manifestPath, manifest, 0o644); err != nil {
		return fmt.Errorf("WritePlugin manifest: %w", err)
	}
	initPath := filepath.Join(targetDir, "__init__.py")
	initBody := []byte("def register():\n    \"\"\"Plugin register entrypoint (Hermes contract).\"\"\"\n    return None\n")
	if err := atomicWriteFile(initPath, initBody, 0o644); err != nil {
		return fmt.Errorf("WritePlugin __init__.py: %w", err)
	}
	for _, sub := range []string{"skills", "commands", "hooks"} {
		if err := os.MkdirAll(filepath.Join(targetDir, sub), 0o755); err != nil {
			return fmt.Errorf("WritePlugin mkdir %s: %w", sub, err)
		}
	}
	return nil
}

func (w *Writer) preflightTargets(plan *mapping.Plan) error {
	if w.cfg.ForceOverwrite {
		return nil
	}
	checkedDirs := map[string]bool{}
	for _, e := range plan.Entries {
		root, _, err := w.routeTarget(e)
		if err != nil {
			return err
		}
		if root == "" {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(stripPluginPrefix(e.TargetPath)))
		dir := filepath.Dir(full)
		if checkedDirs[dir] {
			continue
		}
		checkedDirs[dir] = true
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return ErrTargetNotEmpty
		}
	}
	return nil
}

func (w *Writer) routeTarget(e mapping.PlanEntry) (root string, joinAsIs bool, err error) {
	switch e.Kind {
	case mapping.EntryKindSkill, mapping.EntryKindCommand, mapping.EntryKindHook:
		return w.cfg.HermesPluginRoot, true, nil
	case mapping.EntryKindHermesConfig:
		return filepath.Dir(w.cfg.HermesConfigPath), false, nil
	case mapping.EntryKindDoctrine, mapping.EntryKindMemory:
		return w.cfg.ZenConfigRoot, true, nil
	case mapping.EntryKindMCPServer:

		return filepath.Dir(w.cfg.HermesConfigPath), false, nil
	default:
		return "", false, fmt.Errorf("%w: %s", ErrUnknownEntryKind, e.Kind)
	}
}

func (w *Writer) applyEntry(e mapping.PlanEntry) error {
	switch e.Kind {
	case mapping.EntryKindSkill:
		path, err := w.routeJoined(e)
		if err != nil {
			return err
		}
		if err := writeSkill(path, e); err != nil {
			return err
		}
		if e.RegisterCall != "" {
			w.registerCalls = append(w.registerCalls, e.RegisterCall)
		}
		return nil
	case mapping.EntryKindCommand:
		path, err := w.routeJoined(e)
		if err != nil {
			return err
		}
		if err := writeCommand(path, e); err != nil {
			return err
		}
		if e.RegisterCall != "" {
			w.registerCalls = append(w.registerCalls, e.RegisterCall)
		}
		return nil
	case mapping.EntryKindHook:
		path, err := w.routeJoined(e)
		if err != nil {
			return err
		}
		if err := writeHook(path, e); err != nil {
			return err
		}
		if e.RegisterCall != "" {
			w.registerCalls = append(w.registerCalls, e.RegisterCall)
		}
		return nil
	case mapping.EntryKindHermesConfig:
		if w.cfg.HermesConfigPath == "" {
			return nil
		}
		return writeHermesConfig(w.cfg.HermesConfigPath, e)
	case mapping.EntryKindDoctrine:
		if w.cfg.ZenConfigRoot == "" {
			return nil
		}
		path := filepath.Join(w.cfg.ZenConfigRoot, filepath.FromSlash(e.TargetPath))
		return writeDoctrineTOML(path, e)
	case mapping.EntryKindMemory:
		if w.cfg.ZenConfigRoot == "" {
			return nil
		}
		path := filepath.Join(w.cfg.ZenConfigRoot, filepath.FromSlash(e.TargetPath))
		return writeMemory(path, e)
	case mapping.EntryKindMCPServer:

		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnknownEntryKind, e.Kind)
	}
}

func (w *Writer) routeJoined(e mapping.PlanEntry) (string, error) {
	root, joinAsIs, err := w.routeTarget(e)
	if err != nil {
		return "", err
	}
	if !joinAsIs {
		return "", fmt.Errorf("%w: kind %s not directory-joinable", ErrUnsupportedTarget, e.Kind)
	}
	if root == "" {
		return "", fmt.Errorf("%w: configured root for kind %s is empty", ErrUnsupportedTarget, e.Kind)
	}
	return filepath.Join(root, filepath.FromSlash(stripPluginPrefix(e.TargetPath))), nil
}

func stripPluginPrefix(p string) string {
	if after, ok := strings.CutPrefix(p, "plugin/hades/"); ok {
		return after
	}
	return strings.TrimPrefix(p, "plugin/zen-swarm/")
}

func (w *Writer) emitInitPy() error {
	path := filepath.Join(w.cfg.HermesPluginRoot, "__init__.py")
	body := renderInitPy(w.registerCalls)
	return atomicWriteFile(path, body, 0o644)
}

func (w *Writer) emitPluginYAML(plan *mapping.Plan) error {
	path := filepath.Join(w.cfg.HermesPluginRoot, "plugin.yaml")
	body := renderPluginYAML(plan)
	return atomicWriteFile(path, body, 0o644)
}

func (w *Writer) registerCallsSorted() []string {
	out := append([]string{}, w.registerCalls...)
	sort.Strings(out)
	return out
}
