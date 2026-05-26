package mcpgateway_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func TestKnownSubsystemsHasCaronteNotGitnexus(t *testing.T) {
	subs := mcpgateway.KnownSubsystems()
	hasCaronte := false
	for _, s := range subs {
		if s == "gitnexus" {
			t.Errorf("KnownSubsystems() still contains %q (Plan 19 sovereignty: gitnexus segment removed)", s)
		}
		if s == "caronte" {
			hasCaronte = true
		}
	}
	if !hasCaronte {
		t.Errorf("KnownSubsystems() missing %q (Plan 19: code-graph segment renamed gitnexus->caronte); got %v", "caronte", subs)
	}

	for _, tool := range []string{"query", "context", "impact", "wiki", "get_risk", "get_why", "get_health", "trace_call_path", "get_cochange", "get_implementations", "get_architecture"} {
		tn := mcpgateway.MustToolName("caronte", tool)
		if tn.Subsystem() != "caronte" {
			t.Errorf("MustToolName(caronte, %q).Subsystem() = %q; want caronte", tool, tn.Subsystem())
		}
	}
}

func TestGitnexusProxyFileRemoved(t *testing.T) {
	if _, err := os.Stat(gitnexusProxyPath(t)); !os.IsNotExist(err) {
		t.Errorf("internal/daemon/mcpgateway/gitnexus_proxy.go still exists (err=%v); Plan 19 Phase L deletes it", err)
	}
}

func gitnexusProxyPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Join(filepath.Dir(thisFile), "gitnexus_proxy.go")
}

func TestToolNameCanonicalForm(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	want := "mcp_zen-swarm_research_agentic"
	if tn.String() != want {
		t.Errorf("ToolName.String() = %q, want %q", tn.String(), want)
	}
	if tn.Subsystem() != "research" {
		t.Errorf("Subsystem() = %q, want %q", tn.Subsystem(), "research")
	}
	if tn.Tool() != "agentic" {
		t.Errorf("Tool() = %q, want %q", tn.Tool(), "agentic")
	}
}

func TestToolNameValidation(t *testing.T) {
	cases := []struct {
		subsystem, tool string
		wantErr         bool
	}{
		{"research", "agentic", false},
		{"", "agentic", true},
		{"research", "", true},
		{"Research", "agentic", true},
		{"research_x", "tool", true},
		{"research", "tool-x", false},
		{"research/x", "tool", true},
		{"research", "tool with space", true},
		{"research", "tool", false},
	}
	for _, c := range cases {
		_, err := mcpgateway.NewToolName(c.subsystem, c.tool)
		got := err != nil
		if got != c.wantErr {
			t.Errorf("NewToolName(%q, %q) err=%v wantErr=%v",
				c.subsystem, c.tool, err, c.wantErr)
		}
	}
}

func TestSubsystemKnownClosedSet(t *testing.T) {
	known := mcpgateway.KnownSubsystems()
	want := []string{"research", "budget", "audit", "sshexec", "codegen", "caronte"}
	if len(known) != len(want) {
		t.Fatalf("KnownSubsystems len = %d, want %d", len(known), len(want))
	}
	seen := make(map[string]bool)
	for _, s := range known {
		seen[s] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("KnownSubsystems missing %q", w)
		}
	}
}

func TestToolNameParse(t *testing.T) {
	tn, err := mcpgateway.ParseToolName("mcp_zen-swarm_audit_emit")
	if err != nil {
		t.Fatalf("ParseToolName: %v", err)
	}
	if tn.Subsystem() != "audit" {
		t.Errorf("Subsystem = %q want %q", tn.Subsystem(), "audit")
	}
	if tn.Tool() != "emit" {
		t.Errorf("Tool = %q want %q", tn.Tool(), "emit")
	}
}

func TestToolNameParseRejectsMalformed(t *testing.T) {
	cases := []string{
		"audit_emit",
		"mcp_zen-swarm",
		"mcp_zen-swarm_",
		"mcp_zen-swarm_research",
		"mcp_other_research_agentic",
		"",
	}
	for _, c := range cases {
		if _, err := mcpgateway.ParseToolName(c); err == nil {
			t.Errorf("ParseToolName(%q) returned nil error; expected error", c)
		}
	}
}

func TestSentinelAnchorsExist(t *testing.T) {

	if !mcpgateway.AssertToolRegistryDedup() {
		t.Error("AssertToolRegistryDedup returned false")
	}

	if !mcpgateway.AssertBoundaryPreserved() {
		t.Error("AssertBoundaryPreserved returned false")
	}
}

func TestToolNameParseWithUnderscoreInTool(t *testing.T) {

	tn, err := mcpgateway.ParseToolName("mcp_zen-swarm_budget_cap_status")
	if err != nil {
		t.Fatalf("ParseToolName: %v", err)
	}
	if tn.Subsystem() != "budget" {
		t.Errorf("Subsystem = %q want budget", tn.Subsystem())
	}
	if tn.Tool() != "cap_status" {
		t.Errorf("Tool = %q want cap_status", tn.Tool())
	}
	if tn.String() != "mcp_zen-swarm_budget_cap_status" {
		t.Errorf("String round-trip mismatch: %q", tn.String())
	}
}

func TestToolNameZero(t *testing.T) {
	var z mcpgateway.ToolName
	if !z.IsZero() {
		t.Error("zero ToolName not IsZero")
	}
	tn := mcpgateway.MustToolName("audit", "emit")
	if tn.IsZero() {
		t.Error("non-zero ToolName reports IsZero")
	}
}

func TestModeString(t *testing.T) {
	cases := []struct {
		mode mcpgateway.Mode
		want string
	}{
		{mcpgateway.ModeInteractive, "interactive"},
		{mcpgateway.ModeAutonomy, "autonomy"},
		{mcpgateway.ModeAFK, "afk"},
		{mcpgateway.ModeUnspecified, "unspecified"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q want %q", c.mode, got, c.want)
		}
	}
}

func TestDoctrineResolved(t *testing.T) {
	cases := []struct {
		in   mcpgateway.Doctrine
		want mcpgateway.Doctrine
	}{
		{"", mcpgateway.DoctrineDefault},
		{mcpgateway.DoctrineMaxScope, mcpgateway.DoctrineMaxScope},
		{mcpgateway.DoctrineCapaFirewall, mcpgateway.DoctrineCapaFirewall},
		{mcpgateway.DoctrineDefault, mcpgateway.DoctrineDefault},
	}
	for _, c := range cases {
		if got := c.in.Resolved(); got != c.want {
			t.Errorf("Doctrine(%q).Resolved() = %q want %q", c.in, got, c.want)
		}
	}
}

func TestDoctrineMaxConcurrent(t *testing.T) {
	cases := []struct {
		d    mcpgateway.Doctrine
		want int
	}{
		{mcpgateway.DoctrineMaxScope, 20},
		{mcpgateway.DoctrineDefault, 10},
		{mcpgateway.DoctrineCapaFirewall, 5},
		{"", 10},
		{"unknown", 10},
	}
	for _, c := range cases {
		if got := c.d.MaxConcurrent(); got != c.want {
			t.Errorf("Doctrine(%q).MaxConcurrent() = %d want %d", c.d, got, c.want)
		}
	}
}

func TestNopAuditEmitter(t *testing.T) {
	e := mcpgateway.NopAuditEmitter()
	if e == nil {
		t.Fatal("NopAuditEmitter returned nil")
	}

	e.Emit("test", []byte(`{}`))
}

func TestMustToolNamePanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustToolName on invalid input did not panic")
		}
	}()
	_ = mcpgateway.MustToolName("Research", "agentic")
}

func TestSubsystemKnownEmpty(t *testing.T) {

	if _, err := mcpgateway.NewToolName("", "tool"); err == nil {
		t.Error("NewToolName empty subsystem returned nil err")
	}
}

func TestToolNameDigitsAllowed(t *testing.T) {

	tn, err := mcpgateway.NewToolName("research", "agentic2")
	if err != nil {
		t.Fatalf("NewToolName with digits: %v", err)
	}
	if tn.Tool() != "agentic2" {
		t.Errorf("Tool = %q want agentic2", tn.Tool())
	}
}

func TestParseToolNameEmptySubsystem(t *testing.T) {

	if _, err := mcpgateway.ParseToolName("mcp_zen-swarm__tool"); err == nil {
		t.Error("ParseToolName empty subsystem returned nil err")
	}
}

type staticResolver struct {
	id  string
	err error
}

func (s staticResolver) Resolve(_ context.Context, _ string) (string, error) {
	return s.id, s.err
}

func TestProjectsAliasResolverContract(t *testing.T) {
	var _ mcpgateway.ProjectsAliasResolver = staticResolver{}
	r := staticResolver{id: "abc", err: nil}
	got, err := r.Resolve(context.Background(), "in")
	if err != nil || got != "abc" {
		t.Fatalf("resolver contract broken: got %q, %v; want %q, nil", got, err, "abc")
	}
}

func TestErrAliasNotFoundIsSentinel(t *testing.T) {
	if mcpgateway.ErrAliasNotFound == nil {
		t.Fatal("ErrAliasNotFound is nil; expected non-nil sentinel")
	}
	if !errors.Is(mcpgateway.ErrAliasNotFound, mcpgateway.ErrAliasNotFound) {
		t.Fatal("ErrAliasNotFound is not errors.Is itself; sentinel contract broken")
	}
}
