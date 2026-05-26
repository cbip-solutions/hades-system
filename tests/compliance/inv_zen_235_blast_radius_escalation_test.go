package compliance

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestInvZen235HighBlastRadiusEscalatesHRA(t *testing.T) {
	log := &inv235CapturingEventLog{}
	r := hra.NewEscalationRules(log, clock.Real{}, "sess", "proj")
	before, _, _ := r.L3Counters()
	r.HighBlastRadius(context.Background(), "high", 0.8, []string{"pkg.Hub"})
	after, _, _ := r.L3Counters()
	if after != before+1 {
		t.Errorf("inv-zen-235: high blast-radius did not bump L3 escalations (%d → %d)", before, after)
	}
	if len(log.escalations()) != 1 {
		t.Errorf("inv-zen-235: expected 1 EvtEscalationDecision, got %d", len(log.escalations()))
	}
}

func TestInvZen235LowBlastRadiusDoesNotEscalateHRA(t *testing.T) {
	log := &inv235CapturingEventLog{}
	r := hra.NewEscalationRules(log, clock.Real{}, "sess", "proj")
	before, _, _ := r.L3Counters()
	r.HighBlastRadius(context.Background(), "medium", 0.4, nil)
	r.HighBlastRadius(context.Background(), "low", 0.1, nil)
	after, _, _ := r.L3Counters()
	if after != before {
		t.Errorf("inv-zen-235: non-high level bumped L3 escalations (%d → %d); only 'high' must fire", before, after)
	}
	if len(log.escalations()) != 0 {
		t.Errorf("inv-zen-235: non-high level emitted EvtEscalationDecision (got %d); must be 0", len(log.escalations()))
	}
}

func TestInvZen235HighBlastRadiusPausesAutonomy(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionHighBlastRadius: orch.ThresholdHigh,
	}, false)
	if got := pol.Evaluate(orch.DecisionHighBlastRadius, orch.DecisionEvent{Class: orch.DecisionHighBlastRadius}); got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("inv-zen-235: high_blast_radius@high = %v; want MandatoryPause", got)
	}

	empty := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{}, false)
	if got := empty.Evaluate(orch.DecisionHighBlastRadius, orch.DecisionEvent{Class: orch.DecisionHighBlastRadius}); got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("inv-zen-235: unmapped high_blast_radius = %v; want MandatoryPause", got)
	}
}

func TestInvZen235HighBlastRadiusForcesThoroughMode(t *testing.T) {
	got := merge.EscalateForBlastRadius(merge.ModeEmergencyOnly, "high")
	if got != merge.ModeHighRisk {
		t.Errorf("inv-zen-235: high verdict did not override EmergencyOnly → HighRisk (got %v)", got)
	}
	if merge.ModeFor(got).TestTier != merge.TestTierFull {
		t.Errorf("inv-zen-235: ModeHighRisk tier = %v; want TestTierFull", merge.ModeFor(got).TestTier)
	}
}

func TestInvZen235OrchestratorDoesNotImportCaronte(t *testing.T) {

	allowedSeamFiles := map[string]bool{
		"seam_contractfix.go":      true,
		"seam_contractfix_test.go": true,
	}
	root := filepath.Join(repoRoot(t), "internal", "orchestrator")
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		base := filepath.Base(path)
		if allowedSeamFiles[base] {

			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "internal/caronte") {
				t.Errorf("inv-zen-235 boundary: %s imports %s (internal/orchestrator must NOT import internal/caronte)", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if scanned == 0 {
		t.Fatal("inv-zen-235: sentinel failure — 0 Go files scanned under internal/orchestrator; layout changed")
	}
}

type inv235CapturingEventLog struct {
	mu     sync.Mutex
	events []eventlog.Event
}

func (c *inv235CapturingEventLog) Subscribe(_ eventlog.Filter, bufferSize int) eventlog.Subscription {

	if bufferSize < 1 {
		bufferSize = 1
	}
	sub := &inv235NoopSubscription{
		events: make(chan eventlog.Record, bufferSize),
		done:   make(chan struct{}),
	}
	close(sub.done)
	return sub
}

func (c *inv235CapturingEventLog) Append(_ context.Context, e eventlog.Event) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return int64(len(c.events)), nil
}

func (c *inv235CapturingEventLog) escalations() []eventlog.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []eventlog.Event
	for _, e := range c.events {
		if e.Type == eventlog.EvtEscalationDecision {
			out = append(out, e)
		}
	}
	return out
}

type inv235NoopSubscription struct {
	events chan eventlog.Record
	done   chan struct{}
}

func (s *inv235NoopSubscription) Events() <-chan eventlog.Record { return s.events }
func (s *inv235NoopSubscription) Done() <-chan struct{}          { return s.done }
func (s *inv235NoopSubscription) Close()                         {}
