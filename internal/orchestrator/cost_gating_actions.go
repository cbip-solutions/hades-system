// SPDX-License-Identifier: MIT
// Package orchestrator — cost-gating action dispatcher (Plan 5 Phase G-3).
//
// G-3 ships:
//   - CostActionContext value type (Reason, DecisionID, Snapshot, Threshold)
//     carrying context for action dispatch.
//   - ApplyAction(ctx, actuator, action, context) pure dispatch function
//     routing each closed-vocabulary CostAction to its corresponding
//     OrchestratorActuator method (idempotent under repeated invocation).
//   - Engine.Apply wholesale-replacement: dispatch-based implementation
//     replacing G-2's passthrough stub.
//   - OrchestratorActuator interface widening to 9-method surface
//     (G-1 declared 5 methods; G-3 expands to 9; forward-declaration
//     discipline preserved in cost_gating.go).
//
// G-2 applyOverrideForTest seam: G-2's TestRun_ApplyError_EmitsEventAndContinues
// used SetApplyOverrideForTest to inject a hook consumed by the G-2 stub.
// G-3 wholesale-replaces Apply; the seam + hook field disappear.
// Failed-path test adapted: per-method actuator error-injection via
// costG3FakeActuator fields (act.hardPauseErr = errors.New(...);
// set snapshot to trigger HardPause; verify EvtBudgetDegradationFailed).
package orchestrator

import (
	"context"
	"fmt"
)

type CostActionContext struct {
	Reason     string
	DecisionID string
	Snapshot   BudgetSnapshot
	Threshold  ThresholdRow
}

// ApplyAction routes a CostAction to the corresponding OrchestratorActuator
// method. Pure dispatch logic; concurrency safety + idempotency enforced
// at the actuator level (Plan 4 worker infrastructure + Phase D core).
//
// All actions are idempotent under repeated invocation (e.g., HardPause
// already-paused is a no-op + warn event emitted by the actuator, not here).
//
// Nil context is safe: c is defaulted to &CostActionContext{} (empty fields)
// before dispatch. Methods that need reason/ID (HardPause, Waiting,
// WaitForConfirmation) receive empty strings if context is nil; callers
// MUST ensure the engine's Apply populates the context before dispatch.
//
// Unknown action returns wrapped ErrUnknownCostAction; no method is called.
func ApplyAction(ctx context.Context, act OrchestratorActuator, a CostAction, c *CostActionContext) error {
	if c == nil {
		c = &CostActionContext{}
	}
	switch a {
	case CostActionContinue:
		return act.RestoreDefaults(ctx)
	case CostActionDropL3Strategic:
		return act.DropAtDepth(ctx, 3)
	case CostActionTierDegradeL2:
		return act.SetTier(ctx, 2)
	case CostActionTierDegradeL1L2:
		return act.SetTier(ctx, 1)
	case CostActionReduceParallelism:
		return act.SetParallelism(ctx, 1, 2)
	case CostActionHardPause:
		return act.HardPause(ctx, c.Reason)
	case CostActionEmergencyOnlyTier:
		return act.EmergencyOnlyTier(ctx)
	case CostActionEscalateL4:
		return act.EscalateL4(ctx, map[string]any{
			"reason":   c.Reason,
			"snapshot": c.Snapshot,
		})
	case CostActionWaitingForConfirmation:
		return act.WaitForConfirmation(ctx, c.DecisionID)
	case CostActionWaiting:
		return act.Waiting(ctx, c.Reason)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownCostAction, a)
	}
}
