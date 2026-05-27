// SPDX-License-Identifier: MIT
package cli

import (
	"os"
	"strings"
	"testing"
)

func TestADRProposeCommandForwardsPlanFlag(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("adr_propose.go")
	if err != nil {
		t.Fatalf("ReadFile(adr_propose.go): %v", err)
	}
	if !strings.Contains(string(raw), "c.ADRProposeWithPlan(ctx, topic, plan)") {
		t.Fatal("adr propose --plan is not forwarded to the daemon client")
	}
}
