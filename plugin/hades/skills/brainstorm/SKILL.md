---
name: brainstorm
description: |
  HADES research-first brainstorming: dispatch SOTA research before Q&A,
  apply project-doctrine override on top of superpowers:brainstorming, output
  design spec. Use when operator invokes /hades:brainstorm or before any design work.
license: Proprietary
agentskills_version: 1.0
keywords:
  - brainstorm
  - design
  - research-first
  - doctrine-override
  - hades
  - ADR-0006
---

# HADES — brainstorm skill (research-first brainstorming)

This skill provides the HADES project-doctrine override on top of the
generic `superpowers:brainstorming` skill. It is triggered by the `/hades:brainstorm`
slash command handler which explicitly calls `skill_load("superpowers:brainstorming")`.

## When to use

- Operator invokes `/hades:brainstorm [topic]`
- Before any design spec authoring for HADES plans
- When `superpowers:brainstorming` is loaded but project-doctrine context is missing

## Workflow

### 1. Load superpowers:brainstorming explicitly

```
skill_load("superpowers:brainstorming")
```

Per inv-zen-015, Hermes is discover-then-call. Explicit invocation required.

### 2. Research SOTA first (project-doctrine override)

Per ADR-0006 + memoria `feedback_research_first_brainstorm.md`:
- Identify 3-5 SOTA topics relevant to brainstorm scope
- Dispatch parallel research subagents (research-cheap profile)
- Each returns compact digest with citations
- Aggregate findings + present BEFORE Q1
- Acknowledge time-sensitivity per category

### 3. Q&A (one question per message)

Per `feedback_methodology_and_conventions.md` §2:
- ONE question per message
- Multiple choice A/B/C/D with explicit recommendation
- ~10-15 questions per brainstorm
- Probe all dimensions: architecture, data flow, errors, performance, security, operator UX

### 4. Design sections (after Q&A)

6 sections, one per message, operator approves each:
1. Architecture + components
2. Data flow
3. Error handling
4. Testing strategy
5. Operator UX
6. Security model + invariantes compile-checked

### 5. Output

Write spec to `docs/superpowers/specs/<date>-zen-swarm-<topic>-design.md`.
(filename convention preserved per spec §Q3 BORDERLINE)

## Cross-references

- docs/METHODOLOGY.md §2 brainstorm methodology
- ADR-0006 research-sota-always-integrated
- feedback_research_first_brainstorm.md
- /hades:brainstorm slash command handler
