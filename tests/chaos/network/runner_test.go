// go:build chaos

package network

import (
	"context"
	"testing"
	"time"
)

func TestToxiproxy80ScenarioMatrix(t *testing.T) {
	reg, err := LoadRegistryForTest()
	if err != nil {
		t.Skipf("Toxiproxy not available (run scripts/setup_toxiproxy_dev.sh); skipping: %v", err)
	}
	scenarios := GenerateScenarios(reg)
	if got := len(scenarios); got != 80 {
		t.Fatalf("scenario count = %d, want 80 (10 toxics × 8 edges)", got)
	}
	runner := NewRunner(reg)
	for _, s := range scenarios {
		s := s
		t.Run(s.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := runner.Run(ctx, s); err != nil {
				t.Errorf("scenario %s: %v", s, err)
			}
		})
	}
}
