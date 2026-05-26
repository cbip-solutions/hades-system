---
id: "ADR-0001"
title: "Runtime substrate boundary"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "architecture", "runtime"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0001: Runtime substrate boundary

## Context

The project needs a stable runtime boundary between the orchestration daemon
and the interactive agent shell it supervises. Keeping that boundary explicit
lets the daemon own audit, budgeting, health checks, and project isolation
without depending on private implementation details of the shell.

## Decision

The daemon treats the agent shell as a replaceable subprocess reached through a
small adapter. The adapter owns process launch, capability discovery, health
probes, and event forwarding. Product logic lives above that boundary.

## Consequences

- The daemon can evolve independently from the shell implementation.
- Tests can exercise the adapter contract with fixtures instead of real user
  configuration.
- Runtime-specific quirks stay behind a single package boundary.
