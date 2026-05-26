---
id: "ADR-0004"
title: "Hierarchical workforce bounds"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "orchestration", "workforce"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0004: Hierarchical workforce bounds

## Context

Large changes can be split across many independent workers, but a flat pool is
hard to coordinate and review. The orchestrator needs a bounded shape that can
scale without losing accountability.

## Decision

The workforce is hierarchical. The orchestrator assigns work to team leads, team
leads assign work to workers, and each layer has a bounded fan-out. The exact
depth is selected from the task graph and project doctrine.

## Consequences

- Parallel work has a clear owner at each level.
- Review load scales with the number of teams instead of raw workers.
- The orchestrator records the chosen depth and width for auditability.
