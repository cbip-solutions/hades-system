# Caronte Code Graph

Caronte is the in-process code-graph engine embedded in HADES. It gives the
daemon and operator surfaces a shared way to answer questions such as:

- What symbols and call paths are affected by this change?
- Why does this file or endpoint exist?
- Which files tend to change together?
- Which implementation satisfies this interface?
- Which downstream clients consume this API endpoint?

Caronte runs inside the daemon process. It does not require a separate graph
service. State is stored under the HADES state directory and surfaced through
CLI, HTTP, TUI, and MCP routes.

## Data Model

Caronte indexes a repository into several related views:

- **Files**: source paths, languages, hashes, and freshness metadata.
- **Symbols**: functions, methods, types, constants, interfaces, handlers, and
  exported package members.
- **Edges**: call edges, containment edges, implementation edges, references,
  and endpoint-to-handler edges.
- **Structure**: package communities, strongly connected components, k-core
  scores, and blast-radius hints.
- **Evolution**: churn, co-change, recent activity, and historical coupling.
- **Intent**: design-intent notes, ownership hints, ADR-like records when
  present, and semantic retrieval payloads.
- **Contracts**: API endpoints, clients, calls, manifests, links, unresolved
  references, and breaking-change records.

The graph is intentionally multi-lane. Lexical search, parser output, semantic
search, structural metrics, and evolution metrics answer different questions.
High-confidence answers often combine more than one lane.

## Indexing Pipeline

A typical indexing pass performs:

1. Discover files that match supported languages.
2. Parse files into language-aware symbol records.
3. Resolve local references and obvious call edges.
4. Extract handlers, routes, and API-like endpoints.
5. Compute structural metrics for risk and blast radius.
6. Refresh semantic payloads when an embedder is configured.
7. Persist index metadata so stale graphs are visible.

Use:

```bash
bin/hades caronte reindex
bin/hades doctor caronte
```

`doctor caronte` should be the first stop when output looks stale, incomplete,
or unexpectedly empty.

## Main CLI Queries

```bash
bin/hades impact internal/daemon/server.go
bin/hades why internal/daemon/server.go
bin/hades risk internal/daemon/server.go internal/client/status.go
bin/hades cochange internal/daemon/server.go
bin/hades impl internal/providers.Backend
bin/hades context internal/daemon/server.go --format json
bin/hades codegraph query "handler status"
```

Use `--format json` when another tool will consume the output. Human-readable
output is optimized for scanning and triage.

## MCP Tools

The Caronte MCP surface is designed for agents. The important tools are:

- `query`: search graph records.
- `context`: gather local context around a file or symbol.
- `impact`: estimate affected symbols and paths.
- `wiki`: summarize graph-backed knowledge.
- `get_risk`: return blast-radius and risk signals.
- `get_why`: return intent and rationale signals.
- `get_health`: inspect graph freshness and index posture.
- `trace_call_path`: follow call paths.
- `get_cochange`: inspect historical coupling.
- `get_implementations`: resolve interface implementations.
- `get_architecture`: summarize architectural structure.

For high-risk code, run `get_why` and `get_risk` before editing. The goal is
to understand intent and blast radius before generating changes.

## Risk Scoring

Caronte risk is not a single magic number. It combines signals such as:

- Centrality and k-core membership.
- Callers and callees.
- Number of files affected by likely call paths.
- Churn and co-change density.
- Contract endpoints or consumers attached to the symbol.
- Known intent records or safety comments.

The risk result should guide review depth. A high-risk result does not mean
"do not change"; it means "increase evidence, review, and confirmation."

## Intent Lookup

`hades why <target>` asks Caronte for design-intent evidence. A good response
can include:

- The nearest symbol or file-level rationale.
- Related architecture notes.
- Linked endpoints or contracts.
- Relevant co-changing files.
- Confidence and freshness metadata.

When intent is missing, treat that as useful information. The change may still
be correct, but the review should compensate with tests and explicit rationale.

## Degraded Modes

Caronte should fail visibly:

- No index: doctor reports missing or stale index state.
- Unsupported language: lexical and file-level views may still work.
- Missing embedder: semantic lanes degrade while parser and lexical lanes
  remain available.
- Stale contract data: federation health reports indexing currency.
- Empty result: commands should distinguish "not found" from daemon or storage
  errors.

When in doubt, reindex and rerun the focused query before escalating.
