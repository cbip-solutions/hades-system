package worker_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func TestStaticDoctrineConfigReinforcementHit(t *testing.T) {
	cfg := worker.StaticDoctrineConfig{
		Templates: map[string]string{"max-scope": "MAX-SCOPE-FULL-TEMPLATE"},
	}
	got := cfg.ReinforcementTemplate("max-scope")
	if got != "MAX-SCOPE-FULL-TEMPLATE" {
		t.Errorf("ReinforcementTemplate(max-scope) = %q, want MAX-SCOPE-FULL-TEMPLATE", got)
	}
}

func TestStaticDoctrineConfigReinforcementPlaceholder(t *testing.T) {
	cfg := worker.StaticDoctrineConfig{}
	got := cfg.ReinforcementTemplate("max-scope")
	if !strings.Contains(got, "[doctrine: max-scope]") {
		t.Errorf("placeholder = %q, want substring '[doctrine: max-scope]'", got)
	}
}

func TestStaticDoctrineConfigReinforcementEmptyEntry(t *testing.T) {
	cfg := worker.StaticDoctrineConfig{
		Templates: map[string]string{"capa-firewall": ""},
	}
	got := cfg.ReinforcementTemplate("capa-firewall")
	if !strings.Contains(got, "[doctrine: capa-firewall]") {
		t.Errorf("empty-entry fallback = %q, want substring '[doctrine: capa-firewall]'", got)
	}
}

func TestStaticDoctrineConfigDeadlineHit(t *testing.T) {
	cfg := worker.StaticDoctrineConfig{
		Deadlines: map[string]time.Duration{"max-scope": 30 * time.Second},
	}
	got := cfg.CheckpointDeadline("max-scope")
	if got != 30*time.Second {
		t.Errorf("CheckpointDeadline = %v, want 30s", got)
	}
}

func TestStaticDoctrineConfigDeadlineMissing(t *testing.T) {
	cfg := worker.StaticDoctrineConfig{}
	got := cfg.CheckpointDeadline("max-scope")
	if got != worker.DefaultCheckpointDeadline {
		t.Errorf("CheckpointDeadline = %v, want DefaultCheckpointDeadline (%v)",
			got, worker.DefaultCheckpointDeadline)
	}
}

func TestStaticDoctrineConfigDeadlineNonPositive(t *testing.T) {
	cases := map[string]time.Duration{
		"zero":     0,
		"negative": -10 * time.Second,
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := worker.StaticDoctrineConfig{
				Deadlines: map[string]time.Duration{"max-scope": d},
			}
			got := cfg.CheckpointDeadline("max-scope")
			if got != worker.DefaultCheckpointDeadline {
				t.Errorf("CheckpointDeadline = %v, want DefaultCheckpointDeadline", got)
			}
		})
	}
}

func TestDefaultCheckpointDeadlineValue(t *testing.T) {
	if worker.DefaultCheckpointDeadline != 30*time.Second {
		t.Errorf("DefaultCheckpointDeadline = %v, want 30s", worker.DefaultCheckpointDeadline)
	}
}
