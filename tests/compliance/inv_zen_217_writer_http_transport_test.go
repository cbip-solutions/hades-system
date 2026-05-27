// Compliance test for invariant: the migrate writer's mcp_servers block
// MUST emit HTTP transport (NOT stdio) for the gateway-default zen-swarm
// entry pointing at the zen-swarm-ctld daemon.
//
// Methodology: dual-layer.
// 1. SOURCE-LEVEL (AST): parse internal/migrate/writer/write_hermes_config.go
// and walk every basic-literal string. Assert NONE matches the stdio
// forbidden patterns ("command: zen-swarm-ctld" + the stdio args literal).
// This catches a future edit that copies the old stdio form back in.
// Comments are excluded — a doc comment explaining the bug history is
// acceptable; only EMITTED string literals are dangerous.
// 2. RUNTIME (rendered output): call RenderMCPServersBlockForCompliance(nil)
// via the writer package import and walk the returned lines. Assert the
// zen-swarm: section contains "transport: http" and NOT
// "command: zen-swarm-ctld".
//
// C-10. Replaces the emission contract; preserved
// across all subsequent plans ( rename of plugin/zen-swarm/ →
// plugin/hades/ does NOT touch the daemon's MCP form; this invariant survives).
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

const writeHermesConfigFile_invZen217 = "internal/migrate/writer/write_hermes_config.go"

func resolveRepoFile_invZen217(t *testing.T, rel string) string {
	t.Helper()
	cwd, _ := filepath.Abs(".")
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		candidate := filepath.Join(d, rel)
		if _, err := parser.ParseFile(token.NewFileSet(), candidate, nil, parser.PackageClauseOnly); err == nil {
			return candidate
		}
	}
	t.Fatalf("could not locate %s from %s upward", rel, cwd)
	return ""
}

func TestInvZen217_NoStdioCommandInWriterSource(t *testing.T) {
	path := resolveRepoFile_invZen217(t, writeHermesConfigFile_invZen217)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	// Forbidden string fragments. Each fragment MUST NOT appear in any basic
	// literal in the file. Comments are excluded from this assertion (a
	// comment explaining the bug history is acceptable; only EMITTED strings
	// are dangerous).
	forbidden := []string{
		"command: zen-swarm-ctld",

		`"--socket", "/tmp/zen-swarm.sock"`,
	}
	ast.Inspect(f, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}

		for _, frag := range forbidden {
			if strings.Contains(bl.Value, frag) {
				pos := fset.Position(bl.Pos())
				t.Errorf("inv-zen-217 violation: forbidden stdio form %q found at %s:%d",
					frag, pos.Filename, pos.Line)
			}
		}
		return true
	})
}

func TestInvZen217_GatewayEntryUsesHTTPTransport(t *testing.T) {
	lines := writer.RenderMCPServersBlockForCompliance(nil)
	var inZenSwarm bool
	var sawHTTPTransport bool
	for _, line := range lines {
		if strings.TrimSpace(line) == "zen-swarm:" {
			inZenSwarm = true
			continue
		}

		if inZenSwarm {
			trimmed := strings.TrimSpace(line)
			isChildLine := strings.HasPrefix(line, "    ")
			isNewSiblingKey := !isChildLine && strings.HasSuffix(trimmed, ":") && trimmed != ""
			if isNewSiblingKey {
				inZenSwarm = false
				continue
			}
			if strings.Contains(line, "transport: http") {
				sawHTTPTransport = true
			}
			if strings.Contains(line, "command: zen-swarm-ctld") {
				t.Errorf("inv-zen-217 runtime violation: stdio `command: zen-swarm-ctld` emitted under zen-swarm entry: %s", line)
			}
		}
	}
	if !sawHTTPTransport {
		t.Errorf("inv-zen-217: zen-swarm gateway entry does not emit `transport: http` line\nfull lines:\n%s",
			strings.Join(lines, "\n"))
	}
}

func TestInvZen217_DocLinkInSpec(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	rel := "docs/superpowers/specs/2026-05-20-zen-swarm-plan-18-hades-system-unified-ux-design.md"
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		candidate := filepath.Join(d, rel)
		if _, err := os.Stat(candidate); err == nil {
			return
		}
	}
	t.Fatalf("could not locate spec file %s from %s upward", rel, cwd)
}
