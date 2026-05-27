// SPDX-License-Identifier: MIT
// Package augment — DoctrineGate fast-path skip for forbidden doctrines.
//
// inv-hades-170: capa-firewall doctrine has [doctrine.augmentation] enable=false
// in the built-in TOML; DoctrineGate refuses augmentation for any session
// whose doctrine matches.
//
// Three-tier reason ordering (load-bearing):
// 1. doctrine name == "capa-firewall" → reason="capa-firewall-disabled"
// (canonical operator-visibility marker; even if TOML says Enable=true)
// 2. Augmentation.Enable == false → reason="doctrine-disabled"
// 3. Augmentation.MaxKGTokens == 0 → reason="max-tokens-zero"
//
// Order matters: (1) before (2) so canonical capa-firewall name wins over
// operator-override attempts; (2) before (3) because explicit operator
// disable is a stronger signal than zero-budget.
package augment

import (
	"context"
	"fmt"
)

const CapaFirewallDoctrineName = "capa-firewall"

func NewDoctrineGate(loader DoctrineLoader) *DoctrineGate {
	return &DoctrineGate{loader: loader}
}

func (g *DoctrineGate) Check(ctx context.Context, doctrineName string) (allowed bool, reason string, err error) {
	if g.loader == nil {
		return false, "", fmt.Errorf("doctrine_gate: loader nil (programmer bug)")
	}
	schema, err := g.loader.Load(ctx, doctrineName)
	if err != nil {
		return false, "", fmt.Errorf("doctrine_gate: load %q: %w", doctrineName, err)
	}
	if schema == nil {
		return false, "", fmt.Errorf("doctrine_gate: nil schema for %q", doctrineName)
	}

	if doctrineName == CapaFirewallDoctrineName {
		return false, "capa-firewall-disabled", nil
	}

	if !schema.Augmentation.Enable {
		return false, "doctrine-disabled", nil
	}

	if schema.Augmentation.MaxKGTokens <= 0 {
		return false, "max-tokens-zero", nil
	}

	return true, "", nil
}
