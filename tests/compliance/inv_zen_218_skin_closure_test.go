// tests/compliance/inv_zen_218_skin_closure_test.go
//
// inv-zen-218 (Plan 18a Phase B B-10) — HADES skin module import closure.
//
// Doctrine: per spec §Q2 + master §B "Critical invariants" + plan-18a
// Phase B B-1 amendment, the HADES skin module
// (plugin/hades/skins/hades.py) MUST be a closed system:
//
//  1. Imports ONLY from Python stdlib (os, pathlib, logging, typing,
//     tomllib, etc.).
//  2. Imports ONLY from hermes_cli (the Hermes plugin contract surface).
//  3. Reads ONLY from its own assets directory
//     (plugin/hades/skins/assets/) + palette.toml.
//
// Forbidden imports (would violate single-egress + privacy + closure):
//   - requests, httpx, urllib, urllib.request, urllib3, aiohttp
//   - socket, ssl
//   - http.client, http.server
//   - subprocess (no shell-out from a UX-styling module)
//   - any internal/ package (Go-side daemon-client imports leaking
//     through Python boundary — would violate inv-zen-031 separation)
//   - any plugin/hades/{commands,hooks,providers,renderers,
//     transports} import (skin must NOT couple to operational modules)
//
// Test strategy: read hades.py + grep against a forbidden-prefix list.
// Companion ADR: docs/decisions/0094-hades-skin-closure.md.
package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const skinModulePath = "plugin/hades/skins/hades.py"

var inv218ForbiddenImportPrefixes = []string{

	"urllib.request",
	"urllib3",
	"urllib",
	"requests",
	"httpx",
	"aiohttp",
	"http.client",
	"http.server",
	"socket",
	"ssl",

	"subprocess",

	"internal.",
	"github.com/zen-swarm/",

	"hermes_plugins.hades.commands",
	"hermes_plugins.hades.hooks",
	"hermes_plugins.hades.providers",
	"hermes_plugins.hades.renderers",
	"hermes_plugins.hades.transports",

	"..commands",
	"..hooks",
	"..providers",
	"..renderers",
	"..transports",
}

var inv218ImportLineRe = regexp.MustCompile(
	`^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`,
)

func TestInvZen218SkinModuleImportClosure(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, skinModulePath)
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("cannot read %s: %v", skinModulePath, err)
	}

	violations := []string{}
	for lineno, line := range strings.Split(string(body), "\n") {
		m := inv218ImportLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var mod string
		if m[1] != "" {
			mod = m[1]
		} else {
			mod = m[2]
		}
		if mod == "" {
			continue
		}
		for _, bad := range inv218ForbiddenImportPrefixes {
			if strings.HasPrefix(mod, bad) {
				violations = append(violations,
					fmtInv218Violation(lineno+1, mod, bad, line))
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("inv-zen-218 (skin module closure) violated. "+
			"%d offending import(s) in %s:\n%s",
			len(violations), skinModulePath, strings.Join(violations, "\n"))
	}
}

func TestInvZen218SkinModuleNoFsEscape(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, skinModulePath)
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("cannot read %s: %v", skinModulePath, err)
	}

	forbidden := []string{
		`"/etc/`,
		`"/var/`,
		`"/tmp/`,
		`"/usr/`,
		`"/opt/`,
		`"~/.ssh`,
		`"~/.aws`,
		`"~/.config/zen-swarm/credentials`,
	}

	violations := []string{}
	for lineno, line := range strings.Split(string(body), "\n") {
		stripped := strings.TrimSpace(line)

		if strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, `"""`) {
			continue
		}
		for _, bad := range forbidden {
			if strings.Contains(line, bad) {
				violations = append(violations,
					fmtInv218Violation(lineno+1, bad, "fs-escape", line))
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("inv-zen-218 (skin module fs-closure) violated. "+
			"%d offending path(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func fmtInv218Violation(lineno int, mod, why, line string) string {
	return strings.TrimRight(
		"  - line "+inv218Itoa(lineno)+": offending="+mod+
			" (rule: "+why+") | source: "+strings.TrimSpace(line),
		" ",
	)
}

func inv218Itoa(i int) string {
	switch {
	case i == 0:
		return "0"
	case i < 0:
		return "-" + inv218Itoa(-i)
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+(i%10))) + digits
		i /= 10
	}
	return digits
}
