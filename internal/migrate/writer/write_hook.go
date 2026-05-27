// SPDX-License-Identifier: MIT
package writer

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

// ErrPythonHookManualMigration is returned for native Python hook bodies
// (lang == "python"). Per C-4: a stub callback returning None would silently
// drop the operator's hook decisions (hook returning {"action": "block",
// "message": "unsafe"} would become no-op, disabling protections like an
// rm -rf blocker). Rather than ship a stub (feedback_no_stubs_complete_code.md),
// we reject native Python hooks with an actionable operator-facing message;
// the migrate flow surfaces this as a strict-mode halt or lenient warning
// (caller decides).
//
// Operator remediation: re-implement the hook as bash (preferred — full
// portable path via the sidecar pattern) OR manually port to a Hermes
// Python callback signature `fn(**kwargs) -> dict | None` and drop it
// into the generated plugin/hooks/ directory by hand.
var ErrPythonHookManualMigration = errors.New(
	"writer: native Python hooks must be migrated manually; this version only auto-migrates bash hooks. " +
		"Hermes plugin format requires Python callbacks of signature fn(**kwargs) -> dict | None; " +
		"the migration tool cannot safely transform arbitrary Python hook bodies (mismatched control flow, " +
		"missing entry-point detection). Operator options: (a) re-implement the hook as bash and re-run migrate, " +
		"or (b) port to the Hermes callback shape manually and drop the resulting .py into plugin/hades-system/hooks/")

func writeHook(path string, e mapping.PlanEntry) error {
	if len(e.BodyBytes) == 0 {
		return fmt.Errorf("write_hook: empty body for %s", e.SourcePath)
	}
	lang := "bash"
	for _, n := range e.Notes {
		if strings.HasPrefix(n, "source-lang=") {
			lang = strings.TrimPrefix(n, "source-lang=")
		}
	}
	if lang != "bash" {

		return ErrPythonHookManualMigration
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	sidecarPath := filepath.Join(dir, stem+".sh")

	if err := atomicWriteFile(sidecarPath, e.BodyBytes, 0o755); err != nil {
		return fmt.Errorf("write_hook: sidecar: %w", err)
	}

	wrapper := renderHookWrapper(e.HookEvent, stem+".sh")
	return atomicWriteFile(path, []byte(wrapper), 0o644)
}

func renderHookWrapper(event, sidecarBasename string) string {
	ident := pyIdentFromCommandName(event)
	sb := strings.Builder{}
	sb.WriteString("# SPDX-License-Identifier: MIT\n")
	sb.WriteString("# Imported by `hades migrate claude-code`.\n")
	sb.WriteString("# Event: ")
	sb.WriteString(event)
	sb.WriteString(" (Hermes VALID_HOOKS canonical, post-SOTA-5).\n")
	sb.WriteString("#\n")
	sb.WriteString("# Sidecar file pattern: the bash body lives in `")
	sb.WriteString(sidecarBasename)
	sb.WriteString("` (raw, no escape).\n")
	sb.WriteString("# This wrapper is a fixed delegate — operator-supplied content NEVER\n")
	sb.WriteString("# enters Python source, eliminating the docstring-escape / RCE class.\n\n")
	sb.WriteString("import subprocess\n")
	sb.WriteString("from pathlib import Path\n\n")
	sb.WriteString("_SIDECAR = Path(__file__).parent / ")

	sb.WriteString(pyStringLiteral(sidecarBasename))
	sb.WriteString("\n\n")
	sb.WriteString("def ")
	sb.WriteString(ident)
	sb.WriteString("_callback(**kwargs):\n")
	sb.WriteString("    body = _SIDECAR.read_text(encoding=\"utf-8\")\n")
	sb.WriteString("    result = subprocess.run(\n")
	sb.WriteString("        [\"/bin/bash\", \"-c\", body],\n")
	sb.WriteString("        capture_output=True, text=True, env=kwargs.get(\"env\", None),\n")
	sb.WriteString("    )\n")
	sb.WriteString("    if result.returncode != 0:\n")
	sb.WriteString("        return {\"action\": \"block\", \"message\": result.stderr}\n")
	sb.WriteString("    return None\n")
	return sb.String()
}

func pyStringLiteral(s string) string {
	sb := strings.Builder{}
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&sb, "\\x%02x", r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
