# Hierarchical Review Architecture

HRA is the review and attention model used by HADES to keep autonomous work
bounded by human-readable evidence. It classifies work into levels, raises
attention when risk increases, and provides a consistent path from routine
execution to explicit confirmation.

HRA is not a reviewer replacement. It is the system that decides which review
depth is appropriate, what evidence should be visible, and when a workflow must
pause.

## Review Levels

HADES uses four practical attention levels:

| Level | Meaning | Typical Action |
| --- | --- | --- |
| L1 | Routine | Continue with standard checks and compact audit events. |
| L2 | Attention | Increase review detail and surface evidence to the operator. |
| L3 | Confirmation | Pause until an explicit decision is recorded. |
| L4 | Stop | Stop or quarantine the workflow until the underlying issue is resolved. |

Exact thresholds can vary by doctrine, project policy, provider posture, and
runtime configuration. The important property is monotonicity: evidence that
raises risk should not silently lower review depth.

## Inputs

HRA consumes signals from several daemon-owned subsystems:

- Caronte risk, intent, impact, and co-change results.
- Contract Federation breaking-change records.
- Provider budget and cost-cap posture.
- Worktree isolation and merge status.
- Test, lint, build, and artifact-verification status.
- Audit events, recovery attempts, and repeated failure counters.
- Confirmation policy and operator decisions.
- Doctor and health probes for daemon dependencies.

Because signals come from the daemon, frontends can render the same review
state through CLI, TUI, Hermes, or MCP surfaces.

## Queue Semantics

The HRA queue is an attention queue. Items are expected to include:

- Stable identifier.
- Scope: project, worktree, branch, task, endpoint, or provider.
- Level and reason.
- Evidence summary.
- Suggested next action.
- Whether a confirmation is required.
- Links to audit events or graph queries when available.

The TUI HRA view is optimized for scanning. CLI and HTTP surfaces are better
for automation and structured export.

## Confirmation Flow

When work reaches a confirmation boundary, the daemon should record:

1. What operation requested confirmation.
2. Why the operation crossed the boundary.
3. Which evidence was visible at the time.
4. Who or what acknowledged, denied, or timed out the request.
5. What state transition followed the decision.

Confirmations are part of the runtime state. They should not be implemented as
ephemeral prompts that disappear without an audit trail.

## Relationship To Caronte

Caronte feeds HRA with code understanding. For example:

- High blast radius can raise review level.
- Missing design intent can require additional explanation.
- A breaking API change with known consumers can require confirmation.
- Dense co-change signals can expand the review scope.

This relationship is deliberately asymmetric: Caronte informs review, but HRA
decides attention and confirmation posture.

## Relationship To Merge

Merge decisions should consume HRA state. A clean build is not sufficient when
the HRA queue contains unresolved L3 or L4 items for the same scope. A typical
merge path should require:

- Tests and static checks.
- Caronte impact review for broad changes.
- Contract Federation review for API changes.
- HRA queue clear or explicitly acknowledged.
- Audit evidence attached to the final decision.

## Degraded Modes

HRA should degrade toward visibility:

- If Caronte is stale, show graph freshness as a risk input.
- If audit storage is unavailable, fail operations that require durable
  evidence.
- If a provider is over budget, pause the affected scope.
- If confirmation transport is unavailable, do not silently proceed through an
  L3 boundary.

The desired failure mode is "paused with reason", not "continued without
evidence."
