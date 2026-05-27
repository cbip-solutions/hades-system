// SPDX-License-Identifier: MIT
// Package hooks executes lifecycle hooks + transactional materialization
// templates.
//
// Lifecycle (per spec §3.5):
//
// 1. pre_prompt.{sh,py} — validate host env (BEFORE the wizard runs).
// In the entry-point flow, this hook is invoked BY THE CLI
// (hades new / hades init) BEFORE calling onboard.Wizard.Run via a
// separate hooks.RunPreflight() function so the wizard never sees a
// host that fails preflight.
// 2. pre_gen.{sh,py} — validate wizard answers (BEFORE materialize).
// Receives templates.Answers as JSON on stdin. Non-zero exit halts.
// 3. Materialize — write rendered files to a temp dir (NOT dst).
// 4. post_gen.{sh,py} — run init scripts (git init, plugin link, etc.).
// Receives templates.Answers as JSON on stdin AND cwd is the temp
// dir so `git init` inits the staged tree.
// 5. Swap — atomic rename temp → dst. Failure removes temp.
//
// All errors cause rollback: temp dir removed, dst untouched.
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	t "github.com/cbip-solutions/hades-system/internal/templates"
)

var ErrHookFailed = errors.New("hook failed")

func RunPreflight(ctx context.Context, tmpl t.Template) error {
	script, ok := pickHook(tmpl, "pre_prompt")
	if !ok {
		return nil
	}
	return runHook(ctx, tmpl, script, t.Answers{}, "")
}

func Run(ctx context.Context, tmpl t.Template, dst string, answers t.Answers) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "hades-scaffold-"+sanitizeID(tmpl.Name())+"-*")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}

	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(tmp)
		}
	}()

	if script, ok := pickHook(tmpl, "pre_gen"); ok {
		if err := runHook(ctx, tmpl, script, answers, ""); err != nil {
			return fmt.Errorf("pre_gen hook: %w", err)
		}
	}

	if err := tmpl.Materialize(ctx, tmp, answers); err != nil {
		return fmt.Errorf("materialize: %w", err)
	}

	if script, ok := pickHook(tmpl, "post_gen"); ok {
		if err := runHook(ctx, tmpl, script, answers, tmp); err != nil {
			return fmt.Errorf("post_gen hook: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent of dst: %w", err)
	}
	if err := swapInto(tmp, dst); err != nil {
		return fmt.Errorf("rename temp -> dst: %w", err)
	}
	renamed = true
	return nil
}

func pickHook(tmpl t.Template, base string) (string, bool) {
	for _, ext := range []string{".sh", ".py"} {
		name := base + ext
		if _, err := fs.Stat(tmpl.FS(), name); err == nil {
			return name, true
		}
	}
	return "", false
}

func runHook(ctx context.Context, tmpl t.Template, scriptPath string, answers t.Answers, cwd string) error {
	data, err := fs.ReadFile(tmpl.FS(), scriptPath)
	if err != nil {
		return fmt.Errorf("read hook %q: %w", scriptPath, err)
	}

	tmpFile, err := os.CreateTemp("", "hades-hook-*"+filepath.Ext(scriptPath))
	if err != nil {
		return fmt.Errorf("create hook tmp: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write hook tmp: %w", err)
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod hook tmp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close hook tmp: %w", err)
	}
	// Pick the executor. The 6 canonical embedded templates ship
	// pre_prompt.sh + pre_gen.sh + post_gen.sh files that all start with
	// `#!/usr/bin/env bash` and use bash-only constructs:
	//
	// - `set -euo pipefail` (the `-o pipefail` flag is bash/ksh-only;
	// POSIX sh / Debian dash / Alpine busybox-ash do NOT support it)
	// - `[[... =~... ]]` regex matching (bash builtin; POSIX sh has
	// no `[[` and no `=~`)
	//
	// On Debian (where /bin/sh is `dash`) or Alpine (busybox-ash), invoking
	// these scripts via `sh` errors out with `[[: not found` or
	// `pipefail: bad option` BEFORE any operator-visible logic runs.
	//
	// Resolution (CQ-1 fix 2026-05-17): always invoke `.sh` hooks via
	// `bash`. Cross-platform Linux now means "bash is on PATH" — that
	// constraint is enforced by the preflight gate (CheckBashInstalled
	// below) called by the CLI driver BEFORE we reach this point. macOS
	// + Linux desktop distros + Alpine (with `apk add bash`) + Debian
	// (which ships bash by default even when sh is dash) all satisfy it.
	// Windows uses WSL bash (operator-installed precondition).
	var cmd *exec.Cmd
	switch filepath.Ext(scriptPath) {
	case ".sh":
		cmd = exec.CommandContext(ctx, "bash", tmpFile.Name())
	case ".py":
		cmd = exec.CommandContext(ctx, "python3", tmpFile.Name())
	default:
		return fmt.Errorf("hook %q: unsupported extension", scriptPath)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	jsonBytes, err := json.Marshal(answers)
	if err != nil {
		return fmt.Errorf("marshal answers: %w", err)
	}
	cmd.Stdin = strings.NewReader(string(jsonBytes))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {

		return fmt.Errorf("hook %q exit: %w: %s", scriptPath, errors.Join(ErrHookFailed, err), strings.TrimSpace(stderr.String()))
	}
	return nil
}

func sanitizeID(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
