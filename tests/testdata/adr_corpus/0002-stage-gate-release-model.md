---
id: "ADR-0002"
title: "Stage-gate release model"
status: "accepted"
date: "2026-04-30"
plan: "fixture"
tags: ["fixture", "release", "governance"]
deciders: ["maintainer"]
risk-level: "low"
---

# ADR 0002: Stage-gate release model

## Context

The project combines design, planning, implementation, release, and maintenance
work. Without explicit gates, release criteria drift into informal memory and
become hard to audit.

## Decision

The project uses a stage-gate model. Each stage has a named artifact, a clear
entry condition, a clear exit condition, and a verification command that proves
the stage can advance.

## Consequences

- Release readiness is visible to contributors and reviewers.
- Gate failures point to the stage that owns the missing evidence.
- Smaller projects can use the same model with fewer checks.
