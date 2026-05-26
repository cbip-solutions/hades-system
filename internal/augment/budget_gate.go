// SPDX-License-Identifier: MIT
// Package augment — BudgetGate enforces Plan 4 budget MCP cap_status pre-call
// check + writes cost_ledger entry post-call.
//
// inv-zen-167: every augmentation request MUST pass through BudgetGate.Check
// before any LLM/MCP cost is incurred.
//
// Two-method contract:
//   - Check(ctx, BudgetCheckInput) — reads RolledUSDByAxis; returns
//     (proceed bool, blockedScope string, err error).
//   - Commit(ctx, BudgetCommitInput) — writes cost_ledger entry with the
//     augmentation axis attribution; idempotent on RequestID.
package augment

import (
	"context"
	"errors"
	"fmt"
)

const PerTokenUSDDefault = 0.000003

const BudgetAxisName = "augmentation"

type BudgetCheckInput struct {
	ProjectID       string
	Doctrine        string
	RequestedTokens int
	CapUSD          float64
}

type BudgetCommitInput struct {
	RequestID string
	ProjectID string
	Doctrine  string
	Tokens    int
}

func NewBudgetGate(store BudgetStore, clock Clock) *BudgetGate {
	if clock == nil {
		clock = SystemClock{}
	}
	return &BudgetGate{store: store, clock: clock}
}

func (g *BudgetGate) Check(ctx context.Context, in BudgetCheckInput) (proceed bool, blockedScope string, err error) {
	if in.RequestedTokens <= 0 {
		return false, "", errors.New("budget_gate: RequestedTokens must be > 0")
	}
	if in.CapUSD < 0 {
		return false, "", errors.New("budget_gate: CapUSD must be >= 0")
	}
	rolled, err := g.store.RolledUSDByAxis(ctx, BudgetAxisName, in.ProjectID, 0)
	if err != nil {
		return false, "", fmt.Errorf("budget_gate: rolled %s/%s: %w", BudgetAxisName, in.ProjectID, err)
	}
	estimated := float64(in.RequestedTokens) * PerTokenUSDDefault
	if rolled+estimated > in.CapUSD {
		return false, fmt.Sprintf("%s:%s", BudgetAxisName, in.ProjectID), nil
	}
	return true, "", nil
}

func (g *BudgetGate) Commit(ctx context.Context, in BudgetCommitInput) error {
	if in.RequestID == "" {
		return errors.New("budget_gate: RequestID required (idempotency)")
	}
	if in.Tokens <= 0 {
		return errors.New("budget_gate: Tokens must be > 0")
	}
	entry := CostLedgerEntry{
		RequestID: in.RequestID,
		ProjectID: in.ProjectID,
		Doctrine:  in.Doctrine,
		USD:       float64(in.Tokens) * PerTokenUSDDefault,
		Tokens:    in.Tokens,
		EmittedAt: g.clock.Now().UnixMilli(),
	}
	if err := g.store.InsertCostLedgerEntry(ctx, entry); err != nil {
		return fmt.Errorf("budget_gate: insert cost_ledger entry %s: %w", in.RequestID, err)
	}
	return nil
}
