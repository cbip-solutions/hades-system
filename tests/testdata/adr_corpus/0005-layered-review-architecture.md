---
id: "ADR-0005"
title: "Layered review architecture"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "review", "quality"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0005: Layered review architecture

## Context

Generation throughput is useful only when review keeps pace. Serial review makes
workers wait, while no review lets defects accumulate until integration.

## Decision

Review runs in layers. Tactical review checks individual changes, strategic
review looks for cross-change patterns, and architectural review checks system
invariants before stage transitions.

## Consequences

- Feedback can arrive while workers continue on independent tasks.
- High-risk findings escalate to broader review.
- The audit trail records findings, fixes, and unresolved risks.
