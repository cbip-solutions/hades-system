---
id: "ADR-0003"
title: "Single multi-tenant daemon"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "daemon", "multi-tenant"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0003: Single multi-tenant daemon

## Context

The system manages several projects from one developer workstation. A separate
daemon per project would improve process isolation but would also multiply
configuration, logs, and lifecycle operations.

## Decision

The system runs one daemon with logical per-project isolation. Requests carry a
project identifier, state is partitioned by project, and cross-project access is
guarded at the adapter boundary.

## Consequences

- The operator starts and observes one service.
- Shared caches and process state reduce resource overhead.
- Isolation must be tested at the project-context boundary.
