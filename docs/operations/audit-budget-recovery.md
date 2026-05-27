# Audit, Budget, And Recovery

HADES treats evidence, cost, and recovery as runtime surfaces. These systems
answer different questions:

- Audit: what happened, when, and with what evidence?
- Budget: which provider or project scope is allowed to spend?
- Recovery: what should the daemon do when a dependency fails?

## Audit

Audit events are append-oriented records for meaningful state transitions. They
are useful for debugging, accountability, and replaying a workflow after a
session ends.

Common event families include:

- Daemon startup and subsystem readiness.
- Provider registration, failure, and fallback.
- Budget cap checks, pauses, and resumes.
- Worktree lease, release, review, and merge decisions.
- HRA attention and confirmation transitions.
- Federation workspace and policy changes.
- Artifact verification and release checks.
- Recovery attempts and outcomes.

Inspect recent events:

```bash
bin/hades audit events --limit 20
bin/hades audit types
```

When available, chain verification and cold-archive commands provide stronger
evidence for long-lived audit records.

## Budget

Budget controls prevent provider calls from silently exceeding configured
limits. HADES attributes spend by axes such as project, caller, agent, and
augmentation scope.

Useful commands:

```bash
bin/hades budget events --limit 20
bin/hades budget cap-status --axis project --value example-service --estimate-usd 0.25
bin/hades budget pause --axis project --value example-service --reason "release freeze" --yes
bin/hades budget resume --axis project --value example-service --yes
```

Use explicit caps for pay-as-you-go providers. A missing cap should be visible
configuration state, not an assumption that spend is bounded.

## Recovery

Recovery hooks classify failures and choose a bounded response. Common response
types include:

- Retry with backoff.
- Provider rotation.
- Provider-class pause.
- Local health probe.
- Queue pause.
- HRA escalation.
- Operator confirmation.
- Stop and preserve state for inspection.

Recovery should not erase evidence. A failed worker, paused queue, or provider
outage should leave enough state for the next session to understand what
happened.

## Confirmation Boundaries

Some operations should pause rather than continue automatically:

- Breaking API changes with known consumers.
- High-risk merge candidates.
- Destructive workspace lifecycle operations.
- Budget override or resume after cap.
- Untrusted remote execution path.
- Missing audit storage for an evidence-required operation.

When a confirmation is requested, the daemon should capture reason, scope,
evidence, decision, and resulting transition.

## Practical Workflow

When work pauses unexpectedly:

1. Run `bin/hades status`.
2. Run `bin/hades doctor`.
3. Inspect HRA or confirmation state.
4. Check budget cap status for the affected project/provider.
5. Inspect recent audit events.
6. Retry only after the degraded reason is understood.

This keeps "resume" decisions evidence-based instead of relying on memory.
