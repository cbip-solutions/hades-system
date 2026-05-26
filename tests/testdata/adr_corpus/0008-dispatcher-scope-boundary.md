---
id: "ADR-0008"
title: "Dispatcher scope boundary"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "dispatcher", "budgeting"]
deciders: ["maintainer"]
risk-level: "medium"
---

# ADR 0008: Dispatcher scope boundary

## Context

The dispatcher is responsible for routing model traffic, enforcing budget
controls, and emitting audit records. It should not duplicate provider-specific
logic that belongs behind the runtime adapter.

## Decision

The dispatcher owns the single egress point, request metadata, cost accounting,
and circuit-breaker policy. Provider-specific routing stays behind the adapter
contract.

## Consequences

- Cost and audit behavior stay consistent across providers.
- Provider integrations can change without rewriting dispatcher policy.
- Tests can focus on the dispatcher contract instead of provider internals.
