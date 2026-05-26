---
id: "ADR-0007"
title: "Code graph engine integration"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "code-graph", "tooling"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0007: Code graph engine integration

## Context

Agents need code context that is richer than filename search. Impact analysis,
implementation lookup, and design-intent queries all benefit from a local code
graph.

## Decision

The daemon exposes a code-graph interface as an internal service. Runtime
callers use stable query methods and do not depend on the storage or indexing
implementation.

## Consequences

- Tooling can answer impact and context questions through one interface.
- The engine can be replaced or reindexed without changing callers.
- High-risk code graph results must surface uncertainty instead of pretending
  to be complete.
