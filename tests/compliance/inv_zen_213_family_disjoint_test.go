package compliance_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen213_FamilyDisjointRuntime(t *testing.T) {

	regSrc := readSource(t, filepath.Join("internal", "providers", "registry.go"))
	if !strings.Contains(regSrc, "func (r *Registry) Families()") {
		t.Error("inv-zen-213: providers.Registry must expose Families()")
	}

	srvSrc := readSource(t, filepath.Join("internal", "mcp", "audit", "server.go"))
	if !strings.Contains(srvSrc, "ReviewerFamilyPoolFromRegistry") {
		t.Error("inv-zen-213: audit/server.go must define ReviewerFamilyPoolFromRegistry")
	}

	djSrc := readSource(t, filepath.Join("internal", "mcp", "audit", "disjoint.go"))
	if !strings.Contains(djSrc, "generator family excluded unconditionally") {
		t.Error("inv-zen-213: disjoint.go must contain marker phrase about unconditional generator exclusion")
	}
}
