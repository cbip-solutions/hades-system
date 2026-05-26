---
id: "ADR-0998"
title: [unclosed bracket
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
---

## Context

YAML title field is intentionally unterminated. Parse should return
ErrInvalidFrontmatter wrapping the yaml.v3 error.
