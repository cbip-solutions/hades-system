//go:build integration

package plan19

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

var _ research.GitnexusClient = (*caronte.Engine)(nil)

const (
	toolQuery           = caronteWirePrefix + "query"
	toolContext         = caronteWirePrefix + "context"
	toolImpact          = caronteWirePrefix + "impact"
	toolWiki            = caronteWirePrefix + "wiki"
	toolGetRisk         = caronteWirePrefix + "get_risk"
	toolGetWhy          = caronteWirePrefix + "get_why"
	toolGetHealth       = caronteWirePrefix + "get_health"
	toolTraceCallPath   = caronteWirePrefix + "trace_call_path"
	toolGetCoChange     = caronteWirePrefix + "get_cochange"
	toolGetImpls        = caronteWirePrefix + "get_implementations"
	toolGetArchitecture = caronteWirePrefix + "get_architecture"
)

func TestCaronteDropInAndElevenToolsEndToEnd(t *testing.T) {
	fixtureDir := writeGoFixtureProject(t, t.TempDir())
	h := startDaemonWithProject(t, fixtureDir)

	const (
		symF0      = "fixproj.F0"
		symF1      = "fixproj.F1"
		ifaceGreet = "fixproj.Greeter"
		fileMain   = "fixproj.go"
	)

	t.Run("query returns the drop-in CodeGraphResult {hits} shape", func(t *testing.T) {

		res := callTool(t, h, toolQuery, map[string]any{"query": "F0"})
		if _, ok := res["hits"]; !ok {
			t.Errorf("query result missing 'hits' (drop-in CodeGraphResult contract); got keys %v", keysOf(res))
		}
		if _, ok := res["project_id"]; !ok {
			t.Errorf("query result missing 'project_id'; got keys %v", keysOf(res))
		}
	})

	t.Run("impact is DISTINCT from query (RiskScore, not a code_graph alias)", func(t *testing.T) {
		res := callTool(t, h, toolImpact, map[string]any{"changed_symbols": []string{symF1}})

		if _, ok := res["Level"]; !ok {
			t.Errorf("impact result missing 'Level' (distinct RiskScore op); got keys %v — regressed to a code_graph alias?", keysOf(res))
		}
		if _, ok := res["hits"]; ok {
			t.Errorf("impact result has 'hits' — it must NOT be a query alias (spec §1.2 distinct ops)")
		}
	})

	t.Run("context is DISTINCT (ContextResult callers/community shape)", func(t *testing.T) {
		res := callTool(t, h, toolContext, map[string]any{"symbol": symF1})

		if _, ok := res["Symbol"]; !ok {
			t.Errorf("context result missing 'Symbol' (distinct ContextResult op); got keys %v", keysOf(res))
		}
		if _, ok := res["hits"]; ok {
			t.Errorf("context result has 'hits' — it must NOT be a query alias (spec §1.2)")
		}
		if _, ok := res["Level"]; ok {
			t.Errorf("context result has 'Level' — it must NOT be an impact alias (spec §1.2)")
		}
	})

	t.Run("get_implementations returns the {implementations} fan-out shape", func(t *testing.T) {
		res := callTool(t, h, toolGetImpls, map[string]any{"interface": ifaceGreet})
		// proxy wraps as {"implementations":[…]}. Empty (no resolved EdgeImplements
		// rows without indexing) but the key MUST be present + non-hits/non-Level.
		if _, ok := res["implementations"]; !ok {
			t.Errorf("get_implementations missing 'implementations'; got keys %v", keysOf(res))
		}
	})

	t.Run("trace_call_path returns the {hops} shape", func(t *testing.T) {
		res := callTool(t, h, toolTraceCallPath, map[string]any{"symbol": symF0, "depth": 5})
		if _, ok := res["hops"]; !ok {
			t.Errorf("trace_call_path missing 'hops'; got keys %v", keysOf(res))
		}
	})

	t.Run("get_architecture returns the package/SCC layout shape", func(t *testing.T) {
		res := callTool(t, h, toolGetArchitecture, nil)

		if _, okP := res["Packages"]; !okP {
			if _, okC := res["Cycles"]; !okC {
				t.Errorf("get_architecture missing both 'Packages' and 'Cycles'; got keys %v", keysOf(res))
			}
		}
	})

	t.Run("get_health reports engine + index status", func(t *testing.T) {
		res := callTool(t, h, toolGetHealth, nil)

		if _, ok := res["ProjectID"]; !ok {
			t.Errorf("get_health missing 'ProjectID'; got keys %v", keysOf(res))
		}
		if _, ok := res["NodeCount"]; !ok {
			t.Errorf("get_health missing 'NodeCount'; got keys %v", keysOf(res))
		}
	})

	t.Run("wiki returns {module, markdown} (Caronte implements it; not 503)", func(t *testing.T) {

		res := callTool(t, h, toolWiki, nil)
		if _, ok := res["markdown"]; !ok {
			t.Errorf("wiki missing 'markdown' (Caronte implements wiki — gitnexus 503'd, spec §1.2); got keys %v", keysOf(res))
		}
	})

	t.Run("get_cochange returns the {peers} shape", func(t *testing.T) {

		res := callTool(t, h, toolGetCoChange, map[string]any{"file": fileMain})
		if _, ok := res["peers"]; !ok {
			t.Errorf("get_cochange missing 'peers'; got keys %v", keysOf(res))
		}
	})

	t.Run("get_why responds well-formed (intent: ADR+semantic+lore)", func(t *testing.T) {

		res := callToolRaw(t, h, toolGetWhy, map[string]any{"subject": symF0})
		if res.rpcErr != "" {
			t.Errorf("get_why returned JSON-RPC error (expected graceful degrade): %s", res.rpcErr)
		}
		if res.payload != nil {
			if _, ok := res.payload["Subject"]; !ok {
				t.Errorf("get_why payload missing 'Subject'; got keys %v", keysOf(res.payload))
			}
		}
	})

	t.Run("get_risk responds well-formed (full RiskScore over change set)", func(t *testing.T) {

		res := callTool(t, h, toolGetRisk, map[string]any{"changed_files": []string{fileMain}})
		if _, ok := res["Level"]; !ok {
			t.Errorf("get_risk missing 'Level' (RiskScore op); got keys %v", keysOf(res))
		}
	})
}
