package augment_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type fakeDoctrineLoader struct {
	schemas map[string]*augment.DoctrineSchema
	errOn   map[string]error
	nilOn   map[string]bool
}

func (f *fakeDoctrineLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if e, ok := f.errOn[name]; ok {
		return nil, e
	}
	if f.nilOn != nil && f.nilOn[name] {
		return nil, nil
	}
	if s, ok := f.schemas[name]; ok {
		return s, nil
	}
	return nil, errors.New("doctrine not found: " + name)
}

func newGate(t *testing.T, schemas map[string]*augment.DoctrineSchema) *augment.DoctrineGate {
	t.Helper()
	return augment.NewDoctrineGate(&fakeDoctrineLoader{schemas: schemas})
}

func TestDoctrineGate_CapaFirewallBlocks(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"capa-firewall": {
			Augmentation: augment.AugmentationAxis{
				Enable:      false,
				MaxKGTokens: 0,
				TimeoutMs:   500,
			},
		},
	})
	allowed, reason, err := gate.Check(context.Background(), "capa-firewall")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Fatal("expected capa-firewall to be blocked")
	}
	if reason != "capa-firewall-disabled" {
		t.Fatalf("reason: want capa-firewall-disabled, got %q", reason)
	}
}

func TestDoctrineGate_DefaultDoctrineAllows(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"default": {
			Augmentation: augment.AugmentationAxis{
				Enable:      true,
				MaxKGTokens: 10000,
				TimeoutMs:   1000,
			},
		},
	})
	allowed, reason, err := gate.Check(context.Background(), "default")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !allowed {
		t.Fatalf("expected default to be allowed; reason=%q", reason)
	}
	if reason != "" {
		t.Fatalf("reason: want empty string for allowed, got %q", reason)
	}
}

func TestDoctrineGate_MaxScopeAllows(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"max-scope": {
			Augmentation: augment.AugmentationAxis{
				Enable:      true,
				MaxKGTokens: 25000,
				TimeoutMs:   2000,
			},
		},
	})
	allowed, _, err := gate.Check(context.Background(), "max-scope")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !allowed {
		t.Fatal("expected max-scope to be allowed")
	}
}

func TestDoctrineGate_NonCapaFirewallExplicitDisable(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"custom": {
			Augmentation: augment.AugmentationAxis{
				Enable:      false,
				MaxKGTokens: 5000,
				TimeoutMs:   1000,
			},
		},
	})
	allowed, reason, err := gate.Check(context.Background(), "custom")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Fatal("expected custom doctrine to be blocked")
	}
	if reason != "doctrine-disabled" {
		t.Fatalf("reason: want doctrine-disabled, got %q", reason)
	}
}

func TestDoctrineGate_MaxTokensZeroBlocks(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"zero-budget": {
			Augmentation: augment.AugmentationAxis{
				Enable:      true,
				MaxKGTokens: 0,
				TimeoutMs:   1000,
			},
		},
	})
	allowed, reason, err := gate.Check(context.Background(), "zero-budget")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Fatal("expected zero-budget doctrine to be blocked")
	}
	if reason != "max-tokens-zero" {
		t.Fatalf("reason: want max-tokens-zero, got %q", reason)
	}
}

func TestDoctrineGate_MissingDoctrineErrors(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"default": {
			Augmentation: augment.AugmentationAxis{Enable: true, MaxKGTokens: 10000, TimeoutMs: 1000},
		},
	})
	_, _, err := gate.Check(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown doctrine")
	}
}

func TestDoctrineGate_LoaderErrorPropagates(t *testing.T) {
	gate := augment.NewDoctrineGate(&fakeDoctrineLoader{
		errOn: map[string]error{"max-scope": errors.New("loader exploded")},
	})
	_, _, err := gate.Check(context.Background(), "max-scope")
	if err == nil || !contains(err.Error(), "loader exploded") {
		t.Fatalf("expected loader error to propagate, got: %v", err)
	}
}

func TestDoctrineGate_OrderingCapaFirewallTakesPrecedence(t *testing.T) {
	gate := newGate(t, map[string]*augment.DoctrineSchema{
		"capa-firewall": {
			Augmentation: augment.AugmentationAxis{
				Enable:      true,
				MaxKGTokens: 10000,
				TimeoutMs:   500,
			},
		},
	})
	allowed, reason, err := gate.Check(context.Background(), "capa-firewall")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Fatal("capa-firewall must remain blocked even if Enable=true")
	}
	if reason != "capa-firewall-disabled" {
		t.Fatalf("reason: want capa-firewall-disabled, got %q", reason)
	}
}

func TestDoctrineGate_NilLoaderErrors(t *testing.T) {
	gate := augment.NewDoctrineGate(nil)
	_, _, err := gate.Check(context.Background(), "anything")
	if err == nil || !contains(err.Error(), "loader nil") {
		t.Fatalf("expected nil-loader error, got %v", err)
	}
}

func TestDoctrineGate_NilSchemaErrors(t *testing.T) {
	gate := augment.NewDoctrineGate(&fakeDoctrineLoader{
		nilOn: map[string]bool{"weird": true},
	})
	_, _, err := gate.Check(context.Background(), "weird")
	if err == nil || !contains(err.Error(), "nil schema") {
		t.Fatalf("expected nil-schema error, got %v", err)
	}
}
