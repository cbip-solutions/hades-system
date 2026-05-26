//go:build chaos

package chaos

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type mutableLoader struct {
	schema atomic.Pointer[augment.DoctrineSchema]
}

func newMutableLoader(initial augment.DoctrineSchema) *mutableLoader {
	m := &mutableLoader{}
	m.schema.Store(&initial)
	return m
}

func (m *mutableLoader) Load(_ context.Context, _ string) (*augment.DoctrineSchema, error) {
	p := m.schema.Load()
	if p == nil {
		return &augment.DoctrineSchema{}, nil
	}

	s := *p
	return &s, nil
}

func (m *mutableLoader) swap(next augment.DoctrineSchema) {
	m.schema.Store(&next)
}

func TestDoctrineReload_NoTornReadsUnderConcurrency(t *testing.T) {
	enabled := augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{Enable: true, MaxKGTokens: 10000},
	}
	disabled := augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{Enable: false, MaxKGTokens: 0},
	}

	loader := newMutableLoader(enabled)
	gate := augment.NewDoctrineGate(loader)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		toggle := false
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				toggle = !toggle
				if toggle {
					loader.swap(disabled)
				} else {
					loader.swap(enabled)
				}
			}
		}
	}()

	const readers = 30
	const iterations = 300
	var torn atomic.Int32

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < iterations; j++ {
				allowed, reason, err := gate.Check(ctx, "default")
				if err != nil {
					torn.Add(1)
					continue
				}

				if allowed && reason != "" {
					torn.Add(1)
				}
				if !allowed && reason == "" {
					torn.Add(1)
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	if torn.Load() > 0 {
		t.Errorf("torn reads detected: %d (allowed/reason coherence broken under concurrent reload)", torn.Load())
	}
}

func TestDoctrineReload_GateObservesPostSwap(t *testing.T) {
	enabled := augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{Enable: true, MaxKGTokens: 10000},
	}
	disabled := augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{Enable: false, MaxKGTokens: 0},
	}

	loader := newMutableLoader(enabled)
	gate := augment.NewDoctrineGate(loader)

	allowed1, _, err1 := gate.Check(context.Background(), "default")
	if err1 != nil {
		t.Fatalf("Check pre-swap: %v", err1)
	}
	if !allowed1 {
		t.Errorf("pre-swap: allowed=false, want true (Enable=true)")
	}

	loader.swap(disabled)

	allowed2, reason2, err2 := gate.Check(context.Background(), "default")
	if err2 != nil {
		t.Fatalf("Check post-swap: %v", err2)
	}
	if allowed2 {
		t.Errorf("post-swap: allowed=true, want false (Enable=false)")
	}
	if reason2 != "doctrine-disabled" {
		t.Errorf("post-swap reason = %q, want %q", reason2, "doctrine-disabled")
	}
}

// TestDoctrineReload_CapaFirewallNameWins verifies inv-zen-170: the
// canonical capa-firewall name short-circuits regardless of TOML state.
// A "capa-firewall" doctrine with Enable=true (operator override
// attempt) MUST still be refused.
func TestDoctrineReload_CapaFirewallNameWins(t *testing.T) {
	override := augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{Enable: true, MaxKGTokens: 99999},
	}
	loader := newMutableLoader(override)
	gate := augment.NewDoctrineGate(loader)

	allowed, reason, err := gate.Check(context.Background(), augment.CapaFirewallDoctrineName)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Error("capa-firewall doctrine: allowed=true, want false (inv-zen-170)")
	}
	if reason != "capa-firewall-disabled" {
		t.Errorf("reason = %q, want capa-firewall-disabled", reason)
	}
}
