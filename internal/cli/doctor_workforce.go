// SPDX-License-Identifier: MIT
// Package cli — doctor_workforce.go.
//
// `hades doctor workforce` checks workforce primitives (gate, queues, specs).
//
// Threshold rationale (review M-5): the warning thresholds for
// checkpoint queue depth and unconsumed fix-prompts are CLI-default
// health-check values, intentionally NOT part of the doctrine schema.
// Promoting them to doctrine-resolved values would require a doctrine
// schema addition, which is out of release scope per invariant
// (additive-only schema; schema bumps belong to a future Plan that
// owns the doctrine surface). The constants below document the rationale
// and act as the sole source of truth for the namespace; a future Plan
// can migrate them to doctrine.workforce.{checkpoint_warn,
// fix_prompt_warn} when that schema slot opens.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

const (
	// workforceCheckpointWarn is the queue depth above which
	// checkWorkforceQueueDepths reports a warning. Empirically chosen
	// such that healthy daemons stay well below; sustained breaches
	// indicate a stalled aggregator. See M-5 above for migration path.
	workforceCheckpointWarn = 1000
	// workforceFixPromptWarn is the unconsumed-fix-prompt count above
	// which checkWorkforceFixPromptsBacklog reports a warning. Reflects
	// the size beyond which operator triage becomes the bottleneck;
	// per-doctrine tuning is a future-Plan concern (review M-5).
	workforceFixPromptWarn = 50
)

func doctorWorkforceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "workforce",
		Short: "Workforce primitives health (queue depths, gate, specs)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Workforce (Plan 4)", runWorkforceChecks)
		},
	}
}

func runWorkforceChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkWorkforceGateReachable,
		checkWorkforceQueueDepths,
		checkWorkforceSpecsLoaded,
		checkWorkforceFixPromptsBacklog,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkWorkforceGateReachable(ctx context.Context, c *client.Client) CheckResult {
	st, err := c.GateState(ctx)
	if err != nil {
		return CheckResult{Name: "workforce.gate.reachable", Status: "fail", Detail: err.Error(),
			Hint: "daemon /v1/workforce/gate/state unreachable; restart daemon"}
	}
	return CheckResult{Name: "workforce.gate.reachable", Status: "ok",
		Detail: fmt.Sprintf("state=%s can_pause=%t", st.State, st.CanPause)}
}

func checkWorkforceQueueDepths(ctx context.Context, c *client.Client) CheckResult {
	cps, err := c.WorkforceCheckpoints(ctx, "", 500, 0)
	if err != nil {
		return CheckResult{Name: "workforce.queue.depths", Status: "fail", Detail: err.Error()}
	}
	if len(cps) > workforceCheckpointWarn {
		return CheckResult{Name: "workforce.queue.depths", Status: "warn",
			Detail: fmt.Sprintf("checkpoints=%d > %d", len(cps), workforceCheckpointWarn),
			Hint:   "checkpoint queue excessive; investigate stalled aggregator"}
	}
	return CheckResult{Name: "workforce.queue.depths", Status: "ok",
		Detail: fmt.Sprintf("checkpoints=%d", len(cps))}
}

func checkWorkforceSpecsLoaded(ctx context.Context, c *client.Client) CheckResult {
	specs, err := c.WorkforceSpecs(ctx, "", 500, 0)
	if err != nil {
		return CheckResult{Name: "workforce.specs.loaded", Status: "fail", Detail: err.Error(),
			Hint: "GET /v1/workforce/specs failed"}
	}
	return CheckResult{Name: "workforce.specs.loaded", Status: "ok",
		Detail: fmt.Sprintf("%d specs loaded", len(specs))}
}

func checkWorkforceFixPromptsBacklog(ctx context.Context, c *client.Client) CheckResult {
	fps, err := c.WorkforceFixPrompts(ctx, "", 500, 0)
	if err != nil {
		return CheckResult{Name: "workforce.fix_prompts.backlog", Status: "fail", Detail: err.Error()}
	}
	pending := 0
	for _, fp := range fps {
		if !fp.Consumed {
			pending++
		}
	}
	if pending > workforceFixPromptWarn {
		return CheckResult{Name: "workforce.fix_prompts.backlog", Status: "warn",
			Detail: fmt.Sprintf("%d unconsumed fix-prompts (> %d)", pending, workforceFixPromptWarn),
			Hint:   "review backlog: hades workforce fix-prompts"}
	}
	return CheckResult{Name: "workforce.fix_prompts.backlog", Status: "ok",
		Detail: fmt.Sprintf("%d unconsumed", pending)}
}
