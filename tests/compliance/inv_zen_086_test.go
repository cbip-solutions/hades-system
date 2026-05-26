package compliance_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/budget"
)

func TestInvZen086BudgetMCPStdioCanonical(t *testing.T) {

	if !budget.AssertStdioCanonical() {
		t.Error("AssertStdioCanonical returned false")
	}
}

func TestInvZen086BoundaryPreserved(t *testing.T) {
	if !budget.AssertBoundaryPreserved() {
		t.Error("AssertBoundaryPreserved returned false")
	}
}
