// tests/compliance/inv_zen_102_cost_ledger_isolation_test.go
//
// Compliance test for inv-zen-102 (cost-ledger write isolation):
// internal/orchestrator/* and internal/daemon/orchestratoradapter/
// MUST NOT write the cost_ledger table directly. All cost writes flow
// via Plan 3 dispatcher → internal/daemon/dispatcheradapter.
//
// This test parses every .go file under the orchestrator + adapter
// trees and rejects any literal SQL fragment "INSERT INTO cost_ledger",
// "UPDATE cost_ledger", or "DELETE FROM cost_ledger" (case-insensitive).
//
// Boundary inv-zen-089: same trees MUST NOT import internal/store
// (only orchestratoradapter is allowed; that package lives at
// internal/daemon/orchestratoradapter, NOT internal/orchestrator).
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen102_CostLedgerIsolation(t *testing.T) {
	roots := []string{
		"../../internal/orchestrator",
		"../../internal/daemon/orchestratoradapter",
	}
	bannedVerbs := []string{
		"insert into cost_ledger",
		"update cost_ledger",
		"delete from cost_ledger",
	}

	fset := token.NewFileSet()
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			t.Errorf("inv-zen-102: expected %s to exist", root)
			continue
		}
		var files []string
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".go") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}

			src, err := os.ReadFile(f)
			if err != nil {
				t.Errorf("read %s: %v", f, err)
				continue
			}
			lower := strings.ToLower(string(src))
			for _, verb := range bannedVerbs {
				if strings.Contains(lower, verb) {
					t.Errorf("inv-zen-102 violated: %s contains %q (orchestrator must route cost writes through dispatcheradapter)", f, verb)
				}
			}

			if !strings.Contains(f, "/internal/orchestrator/") {
				continue
			}
			file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
			if err != nil {
				t.Errorf("parse %s: %v", f, err)
				continue
			}
			for _, imp := range file.Imports {
				if strings.Contains(imp.Path.Value, "internal/store") {
					t.Errorf("inv-zen-089 violated: %s imports internal/store", f)
				}
			}
		}
	}
}

func TestInvZen102_AdapterIsTheOnlyBridge(t *testing.T) {
	const adapterFile = "../../internal/daemon/orchestratoradapter/adapter.go"
	src, err := os.ReadFile(adapterFile)
	if err != nil {
		t.Fatalf("read adapter: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		`"github.com/cbip-solutions/hades-system/internal/store"`,
		`"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"`,
		`"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"`,
		`"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("orchestratoradapter MUST import %s (boundary bridge contract)", want)
		}
	}
}
