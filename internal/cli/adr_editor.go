// SPDX-License-Identifier: MIT
// Package cli — adr_editor.go.
//
// editorRunner runs the operator's preferred editor against the path
// passed in. Test code substitutes a fake editor by replacing this
// package-level variable (same pattern as TestOnlyClientFactory in
// workforce.go).
//
// Editor precedence (git convention): $VISUAL > $EDITOR > vi.
package cli

import (
	"fmt"
	"os"
	"os/exec"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

var editorRunner = realEditorRun

func resolveEditorName() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

func realEditorRun(path string) error {
	editor := resolveEditorName()
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("editor %s exited with error: %w", editor, err))
	}
	return nil
}
