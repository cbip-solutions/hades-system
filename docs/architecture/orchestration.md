# Autonomous Orchestration

The orchestrator coordinates work without letting transient clients own durable
state. It manages queues, worktrees, dispatch, review, merge posture, cost
checks, and recovery loops through the daemon.

## Core Responsibilities

- Register projects and per-project routing policy.
- Lease isolated worktrees for tasks.
- Dispatch work through configured profiles and provider cascades.
- Track task state, attempts, retries, and attention items.
- Enforce review and confirmation boundaries.
- Evaluate merge readiness.
- Emit audit events for meaningful state transitions.
- Recover or pause when workers, providers, or local services fail.

The orchestrator is a control system. It should make state transitions explicit
and observable rather than hiding them inside a single long-running shell.

## Worktree Model

Worktrees isolate changes by scope. A worktree lease should identify:

- Project.
- Branch or task identifier.
- Files expected to change.
- Owning workflow.
- Expiration or release condition.
- Review and merge state.

This lets HADES clean up abandoned work, avoid overlapping writes, and compare
candidate branches without losing the context that produced them.

## Dispatch Model

Dispatch uses profiles. A profile maps a role to provider preferences, test
expectations, review depth, and safety constraints. Examples include coding,
research, review, and orchestration roles.

The dispatcher considers:

- Provider health.
- Cost and cap posture.
- Project allowlists.
- Local sidecar availability.
- Role requirements.
- Previous failures and circuit-breaker state.

Dispatch should fail with a reason when no provider is eligible.

## Review And Merge

Merge readiness is a composed decision. It should consider:

- Build, test, lint, and formatting status.
- HRA queue status.
- Caronte risk and impact.
- Contract Federation impact for API changes.
- Required confirmations.
- Audit evidence.
- Worktree ownership and branch freshness.

The merge layer should not infer that "green tests" means "safe to merge" when
review or contract evidence is still unresolved.

## Recovery

The orchestrator includes recovery hooks for common failure families:

- Provider failure or rate limiting.
- Provider class outage.
- Local service outage.
- Worker deadlock or repeated retry.
- Resource pressure.
- Sidecar failure.
- External dependency failure.

Recovery actions can include provider rotation, backoff, queue pause, health
probe, operator attention, or full stop. The action should be visible through
status, audit, and HRA surfaces.

## Operator Surfaces

Use:

```bash
bin/hades orchestrator status
bin/hades orchestrator state
bin/hades orchestrator pool
bin/hades doctor
```

The TUI exposes higher-density views for queues and review posture. Hermes
commands are useful when the workflow is conversation-driven.
