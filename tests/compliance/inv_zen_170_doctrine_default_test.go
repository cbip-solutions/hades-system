package compliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestInvZen170_SentinelInvokedFromNewPipeline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "augment", "types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	if !strings.Contains(string(src), "capaFirewallAugmentDisabled()") {
		t.Error("inv-zen-170 sentinel capaFirewallAugmentDisabled() not invoked in NewPipeline")
	}
}

func TestInvZen170_BuiltinTOMLDisablesAugmentation(t *testing.T) {
	tomlPath := filepath.Join("..", "..", "internal", "doctrine", "builtin", "capa-firewall.toml")
	src, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read capa-firewall.toml: %v", err)
	}
	content := string(src)
	if !strings.Contains(content, "[augmentation]") {
		t.Error("inv-zen-170: [augmentation] section missing from capa-firewall.toml")
	}
	if !strings.Contains(content, "enable = false") {
		t.Error("inv-zen-170: capa-firewall.toml does not set augmentation.enable=false")
	}
	if !strings.Contains(content, "max_kg_tokens = 0") {
		t.Error("inv-zen-170: capa-firewall.toml does not set augmentation.max_kg_tokens=0")
	}
}

func TestInvZen170_CapaFirewallDoctrineGateRefuses(t *testing.T) {
	loader := &p170TestLoader{
		schemas: map[string]*augment.DoctrineSchema{
			"capa-firewall": {
				Augmentation: augment.AugmentationAxis{
					Enable:      true,
					MaxKGTokens: 99999999,
					TimeoutMs:   500,
				},
			},
		},
	}
	gate := augment.NewDoctrineGate(loader)
	allowed, reason, err := gate.Check(context.Background(), "capa-firewall")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if allowed {
		t.Error("inv-zen-170 violated: capa-firewall allowed even with Enable=true override")
	}
	if reason != "capa-firewall-disabled" {
		t.Errorf("inv-zen-170: expected canonical reason capa-firewall-disabled, got %q", reason)
	}
}

type p170TestLoader struct {
	schemas map[string]*augment.DoctrineSchema
}

func (l *p170TestLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if s, ok := l.schemas[name]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}
