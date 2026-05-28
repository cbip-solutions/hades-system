// SPDX-License-Identifier: MIT
package writer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/pyident"
)

func writeCommand(path string, e mapping.PlanEntry) error {
	if len(e.BodyBytes) == 0 {
		return fmt.Errorf("write_command: empty body for %s", e.SourcePath)
	}
	name := commandNameFromTargetPath(e.TargetPath)
	ident := pyIdentFromCommandName(name)

	fileBase := commandFileBasename(name)
	dir := filepath.Dir(path)
	pyPath := filepath.Join(dir, fileBase+".py")
	sidecarBasename := fileBase + ".md"
	sidecarPath := filepath.Join(dir, sidecarBasename)

	if err := atomicWriteFile(sidecarPath, e.BodyBytes, 0o644); err != nil {
		return fmt.Errorf("write_command: sidecar: %w", err)
	}

	body := renderCommandHandler(ident, sidecarBasename)
	return atomicWriteFile(pyPath, []byte(body), 0o644)
}

func commandFileBasename(slashName string) string {
	return pyident.FromName(slashName)
}

func commandNameFromTargetPath(p string) string {

	parts := strings.Split(p, "/")
	last := parts[len(parts)-1]
	return strings.TrimSuffix(last, ".py")
}

func pyIdentFromCommandName(s string) string {
	return pyident.FromName(s)
}

func renderCommandHandler(ident, sidecarBasename string) string {
	sb := strings.Builder{}
	sb.WriteString("# SPDX-License-Identifier: MIT\n")
	sb.WriteString("# Imported by `hades migrate claude-code`.\n")
	sb.WriteString("# Markdown body lives in `")
	sb.WriteString(sidecarBasename)
	sb.WriteString("` (raw, no escape).\n")
	sb.WriteString("# This wrapper is a fixed delegate — operator-supplied content NEVER\n")
	sb.WriteString("# enters Python source, eliminating the docstring-escape / RCE class.\n\n")
	sb.WriteString("from pathlib import Path\n\n")
	sb.WriteString("_SIDECAR = Path(__file__).parent / ")
	sb.WriteString(pyStringLiteral(sidecarBasename))
	sb.WriteString("\n\n")
	sb.WriteString("def ")
	sb.WriteString(ident)
	sb.WriteString("_handler(raw_args: str) -> str | None:\n")
	sb.WriteString(" # The slash command body is the markdown text in the sidecar.\n")
	sb.WriteString(" # Operator extends this handler to rewrite as native Python.\n")
	sb.WriteString("    _ = _SIDECAR.read_text(encoding=\"utf-8\")  # available to operator extension\n")
	sb.WriteString("    return None  # operator extends as needed\n")
	return sb.String()
}
