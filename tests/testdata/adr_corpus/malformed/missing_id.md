---
title: "Missing ID"
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
---

## Context

This ADR omits the required `id` field. The validator should surface
ErrSchemaViolation when this file is parsed via ValidateFile.
