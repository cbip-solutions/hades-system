// cmd/zen-swarm-ctld/main_alias_resolver_test.go
//
// Task A-4 — daemon-side wiring witness. The mcpgateway.Server
// constructed at main.go:609 MUST have a non-nil ProjectsAliasResolver
// after Task A-4 wires the projectsaliasadapter. This test isolates the
// wiring step (constructing the adapter from *store.Store + calling
// SetAliasResolver) so a future refactor that drops the wire-call
// breaks this test BEFORE breaking production.
//
// Why not a full main() integration test: main() is a long boot path
// with many side effects (sockets, signals, launchd). The wiring step
// itself is the load-bearing claim: given a real *store.Store, the
// adapter constructed via projectsaliasadapter.New(s) satisfies the
// mcpgateway.ProjectsAliasResolver interface, and SetAliasResolver +
// AliasResolver round-trip correctly. This isolation also avoids
// pulling daemon-spawn keychain blockers into the unit-test surface
// (feedback_macos_keychain_ci_blocker).
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectsaliasadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestMCPGatewayWiringInjectsAliasResolver(t *testing.T) {

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	defer d.Close()

	srv := mcpgateway.NewServer(d)
	aliasResolver := projectsaliasadapter.New(s)
	srv.SetAliasResolver(aliasResolver)

	got := srv.AliasResolver()
	if got == nil {
		t.Fatal("Server.AliasResolver() = nil after SetAliasResolver(non-nil); wiring step skipped or broken")
	}

	if got != aliasResolver {
		t.Errorf("Server.AliasResolver() = %v; want %v (the instance we set)", got, aliasResolver)
	}
}

func TestProjectsAliasAdapterSatisfiesResolverInterface(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	a := projectsaliasadapter.New(s)
	var _ mcpgateway.ProjectsAliasResolver = a
	if a == nil {
		t.Fatal("projectsaliasadapter.New returned nil for valid store")
	}
}

// TestMainWiresAliasResolverAtNewServerCallSite is the load-bearing
// source-regex witness: main.go MUST construct a projectsaliasadapter
// and inject it via SetAliasResolver immediately after constructing
// mcpgateway.NewServer (per Task A-4 + spec §3.2). A future refactor
// that drops the wire call breaks this test BEFORE breaking the daemon
// at boot.
//
// The match is intentionally loose on whitespace/positioning so the
// regex stays robust against routine reformatting; it asserts the two
// claims that matter:
//
// 1. projectsaliasadapter.New(...) is called somewhere in main.go
// 2. *.SetAliasResolver(...) is called somewhere in main.go
//
// Sister test pattern: revert either call site and this test fires.
func TestMainWiresAliasResolverAtNewServerCallSite(t *testing.T) {

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate main.go for source-regex scan")
	}
	mainPath := filepath.Join(filepath.Dir(thisFile), "main.go")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(data)

	if !regexp.MustCompile(`projectsaliasadapter\.New\(`).MatchString(src) {
		t.Errorf("main.go is missing projectsaliasadapter.New(...) call; alias resolver wiring was not added at main.go:609")
	}

	if !regexp.MustCompile(`\.SetAliasResolver\(`).MatchString(src) {
		t.Errorf("main.go is missing .SetAliasResolver(...) call; alias resolver wiring was not added at main.go:609")
	}

	if !regexp.MustCompile(`"[^"]*/internal/daemon/projectsaliasadapter"`).MatchString(src) {
		t.Errorf("main.go is missing projectsaliasadapter import; wiring incomplete")
	}
}
