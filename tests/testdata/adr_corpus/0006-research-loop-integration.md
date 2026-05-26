---
id: "ADR-0006"
title: "Research loop integration"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "research", "design"]
deciders: ["maintainer"]
risk-level: "low"
---

# ADR 0006: Research loop integration

## Context

Design decisions can become stale when they rely only on memory. The project
needs a repeatable way to check current ecosystem practice before committing to
major architecture.

## Decision

Major design and orchestration decisions include a research loop. The loop
collects current references, records alternatives, and links the decision to the
evidence used at the time.

## Consequences

- Decisions are easier to revisit when the ecosystem changes.
- The project can deliberately deviate from current practice with a recorded
  rationale.
- Trivial edits can skip the loop to avoid ceremony.
