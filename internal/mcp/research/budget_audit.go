// SPDX-License-Identifier: MIT
// budget_audit.go — adapters from typed clients to the
// BudgetClient + AuditClient interfaces declared in types.go.
//
// The internal/mcp/client package exposes:
// - *client.BudgetClient with CapStatus + Record methods
// - *client.EmitClient with Emit method
//
// This file narrows those typed clients into the
// research-package-local BudgetClient + AuditClient interfaces.
// The type system enforces the boundary (inv-hades-031).
package research

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

type BudgetAdapter struct {
	bc *client.BudgetClient
}

func NewBudgetAdapter(bc *client.BudgetClient) *BudgetAdapter {
	return &BudgetAdapter{bc: bc}
}

var _ BudgetClient = (*BudgetAdapter)(nil)

func (a *BudgetAdapter) PreCall(ctx context.Context, scope, value string, _ float64) (bool, string, error) {
	if a.bc == nil {
		return true, "", nil
	}
	resp, err := a.bc.CapStatus(ctx, scope, value)
	if err != nil {
		return false, "", err
	}
	return resp.Allowed, resp.BlockedScope, nil
}

func (a *BudgetAdapter) Record(ctx context.Context, costID string, axes map[string]string) error {
	if a.bc == nil {
		return nil
	}
	tags := make([]client.AxisTag, 0, len(axes))
	for k, v := range axes {
		tags = append(tags, client.AxisTag{
			CostID: costID,
			Axis:   k,
			Value:  v,
		})
	}
	return a.bc.Record(ctx, client.RecordRequest{
		CostID:   costID,
		AxisTags: tags,
	})
}

type AuditAdapter struct {
	ec *client.EmitClient
}

func NewAuditAdapter(ec *client.EmitClient) *AuditAdapter {
	return &AuditAdapter{ec: ec}
}

var _ AuditClient = (*AuditAdapter)(nil)

func (a *AuditAdapter) Emit(ctx context.Context, eventType string, payload []byte) error {
	if a.ec == nil {
		return nil
	}
	return a.ec.Emit(ctx, client.AuditEvent{
		Type:    eventType,
		Payload: string(payload),
	})
}
