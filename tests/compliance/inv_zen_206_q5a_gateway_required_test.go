// tests/compliance/inv_zen_206_q5a_gateway_required_test.go
//
// invariant — Q5=A: gateway boot is unconditional + the code-graph
// backend is hard-required. zen-swarm-ctld bootstrap MUST refuse to start when
// the code-graph engine fails to construct; there is NO env-var bypass.
//
// As of the backend is the in-process Caronte engine
// (buildCaronteEngine / caronte.NewEngine); the gitnexus subprocess child has
// been removed entirely. The Q5=A POLICY is unchanged — bootstrap-required,
// no env-var bypass — only the backend changed. invariant owns the
// drop-in specifics (compile anchor, inverse witness, AST-level bootstrap
// assertions); this file owns the Q5=A unconditional-boot angle.
//
// Triple-anchor:
// 1. compile internal/daemon/mcpgateway/sentinel.go AssertBoundaryPreserved
// (boundary anchor; bypass paths cannot import internal/store)
// 2. runtime cmd/zen-swarm-ctld/mcpgateway_wiring_test.go
// TestBuildDispatcherRefusesNilCaronte — nil caronte engine → err
// 3. compliance THIS FILE — source-level grep over cmd/zen-swarm-ctld/main.go:
// (a) no ZEN_MCPGATEWAY env-var bypass guard
// (b) mcpgateway.NewServer + srv.SetMCPGateway called at top-level
// (c) os.Exit(1) reachable on caronte bootstrap failure + no
// env-var bypass wraps the caronte engine construction
//
// Per spec §1 Q5=A:
// "zen-swarm-ctld bootstrap refuses without the code-graph backend" —
// unconditional. Pre-fix had this wrapped in
// `if os.Getenv("ZEN_MCPGATEWAY")` which silently let the daemon boot
// without the gateway. This test prevents the regression.
//
// Per spec §1 Q1=B + §1 Q5=A, the daemon ships a single mcpgateway
// endpoint backed by the code-graph engine; both surfaces are load-bearing
// on boot.

package compliance_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readMainGo(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (no go.mod found in ancestors)")
		}
		dir = parent
	}
	path := filepath.Join(dir, "cmd", "zen-swarm-ctld", "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInvZen206NoEnvVarBypassGuard(t *testing.T) {
	src := readMainGo(t)
	if strings.Contains(strings.ToLower(src), "zen_mcpgateway") {
		t.Errorf("inv-zen-206 violated: cmd/zen-swarm-ctld/main.go references ZEN_MCPGATEWAY "+
			"(env-var bypass forbidden per Q5=A). Excerpt:\n%s",
			snippetAroundCaseInsensitive(src, "zen_mcpgateway"))
	}
}

func TestInvZen206GatewayWiredUnconditionally(t *testing.T) {
	src := readMainGo(t)

	if !strings.Contains(src, "mcpgateway.NewServer(") {
		t.Error("inv-zen-206 violated: main.go does not call mcpgateway.NewServer(...) — " +
			"gateway is not wired into daemon boot")
	}
	if !strings.Contains(src, "srv.SetMCPGateway(") {
		t.Error("inv-zen-206 violated: main.go does not call srv.SetMCPGateway(...) — " +
			"gateway is not published to daemon.Server")
	}

	guardPattern := regexp.MustCompile(`(?s)if\s+[^{]*os\.Getenv\([^)]+\)[^{]*\{[^}]*mcpgateway\.NewServer\(`)
	if guardPattern.MatchString(src) {
		t.Error("inv-zen-206 violated: mcpgateway.NewServer call is wrapped in an " +
			"`if os.Getenv(...)` guard. Per Q5=A the gateway must boot unconditionally.")
	}
}

// TestInvZen206CodeGraphFailureAbortsBoot asserts that code-graph engine
// bootstrap failure is followed by os.Exit(1) (or equivalent hard-stop). The
// load-bearing claim per spec §1 Q5=A: "daemon refuses to start" — not
// "daemon logs a warning + continues".
//
// As of the backend is Caronte (buildCaronteEngine /
// caronte.NewEngine). invariant owns the AST-level bootstrap assertions;
// this test focuses on the Q5=A no-bypass angle: the caronte construction
// MUST NOT be guarded by an os.Getenv() conditional, and the error path
// MUST reach os.Exit(1).
//
// The test would fail if a future change:
// - reverts to a gitnexus subprocess (NewGitnexusChildClient) — buildCaronteEngine
// call would disappear, violating the first assertion;
// - wraps buildCaronteEngine in an env-var optional guard — the guard-pattern
// check would fire;
// - changes the error path from os.Exit(1) to a log+continue — the exit
// pattern would not match.
func TestInvZen206CodeGraphFailureAbortsBoot(t *testing.T) {
	src := readMainGo(t)

	if !strings.Contains(src, "buildCaronteEngine(") {
		t.Fatal("inv-zen-206 violated: main.go does not construct the code-graph engine " +
			"via buildCaronteEngine(...) — Q5=A code-graph backend required (caronte era)")
	}

	// 2. The construction MUST NOT be wrapped in an os.Getenv() guard
	// (unconditional per Q5=A). Catches any regression that re-introduces
	// an optional-boot env var around the engine construction.
	guardPattern := regexp.MustCompile(`(?s)if\s+[^{]*os\.Getenv\([^)]+\)[^{]*\{[^}]*buildCaronteEngine\(`)
	if guardPattern.MatchString(src) {
		t.Error("inv-zen-206 violated: buildCaronteEngine call is wrapped in an " +
			"`if os.Getenv(...)` guard. Per Q5=A the code-graph backend must boot unconditionally.")
	}

	exitPattern := regexp.MustCompile(`(?s)caronteErr\s*!=\s*nil\s*\{[^}]*os\.Exit\(1\)`)
	if !exitPattern.MatchString(src) {
		t.Error("inv-zen-206 violated: caronte bootstrap-failure path does NOT call " +
			"os.Exit(1). Per Q5=A the daemon MUST refuse to start when the code-graph engine fails.")
	}
}

func snippetAroundCaseInsensitive(hay, needle string) string {
	idx := strings.Index(strings.ToLower(hay), strings.ToLower(needle))
	if idx < 0 {
		return ""
	}
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + len(needle) + 60
	if end > len(hay) {
		end = len(hay)
	}
	return "..." + hay[start:end] + "..."
}
