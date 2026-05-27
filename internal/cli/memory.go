// SPDX-License-Identifier: MIT
// Package cli — memory.go.
//
// `zen memory` is the operator-facing entry point for cross-corpus memory
// retrieval over the D aggregator (per-project + global pin index)
// and the ecosystem RAG (Go/Python/TypeScript/Rust docs corpus).
//
// Five leaves under one root:
//
// zen memory query <free-text> [--remote] [--limit N] [--format text|json]
// zen memory list [--limit N] [--offset M] [--format text|json]
// zen memory pin <note-id> --reason <text> [--operator <id>]
// zen memory unpin <note-id> [--reason <text>] [--operator <id>]
// zen memory promote <note-id> --reason <text> [--operator <id>]
//
// Semantics
//
// - query: cross-corpus search. With --remote, fans out to BOTH the
// aggregator AND the ecosystem RAG dispatcher in parallel, then
// fuses results via RRF k=60.
// Without --remote, queries the aggregator only. Soft-fails when ONE
// source errors (renders the other); hard-fails only when BOTH error.
//
// - list: enumerate pinned notes from the aggregator's global pin index.
//
// - pin / promote: alias pair (pin is the operator-ergonomics term, promote
// the D term). Both call MemoryPromote (POST /v1/knowledge/aggregator/promote).
// invariant: --reason MANDATORY (cobra MarkFlagRequired + RunE TrimSpace check).
//
// - unpin: reverse promote (POST /v1/knowledge/aggregator/unpromote).
//
// All subcommands lazily resolve a daemon HTTP client at RunE time via the
// memoryClientFactory function variable; tests override it directly to inject
// fakes (mirrors the KnowledgeClientFactory pattern but without the per-command
// factory threading because memory subcommands share one production constructor).
//
// Boundary this file imports internal/client + cobra + stdlib only. No
// internal/research/ecosystem import. Cross-corpus calls go
// through the daemon via the client; CLI never talks to dispatcher directly.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract):
// - 0 success
// - 1 operator-recoverable: empty free-text, empty --reason, daemon 422
// - 2 unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type MemoryClient interface {
	MemoryQuery(ctx context.Context, req client.AggQueryRequest) (*client.AggQueryResponse, error)
	MemoryList(ctx context.Context, limit, offset int) (*client.AggListResponse, error)
	MemoryPin(ctx context.Context, noteID string) error
	MemoryUnpin(ctx context.Context, noteID string) error
	MemoryPromote(ctx context.Context, req client.AggPromoteRequest) error

	EcosystemQuery(ctx context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error)
}

var memoryClientFactory = func(cmd *cobra.Command) MemoryClient {
	return &productionMemoryClient{c: newClientFromCmd(cmd)}
}

type productionMemoryClient struct {
	c *client.Client
}

func (p *productionMemoryClient) MemoryQuery(ctx context.Context, req client.AggQueryRequest) (*client.AggQueryResponse, error) {
	rows, err := p.c.AggQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	return &client.AggQueryResponse{Results: rows}, nil
}

func (p *productionMemoryClient) MemoryList(ctx context.Context, _, _ int) (*client.AggListResponse, error) {

	notes, err := p.c.AggList(ctx, "", false)
	if err != nil {
		return nil, err
	}
	return &client.AggListResponse{Notes: notes}, nil
}

func (p *productionMemoryClient) MemoryPin(ctx context.Context, noteID string) error {

	_, err := p.c.AggPromote(ctx, client.AggPromoteRequest{NoteID: noteID})
	return err
}

func (p *productionMemoryClient) MemoryUnpin(ctx context.Context, noteID string) error {
	_, err := p.c.AggUnpromote(ctx, client.AggUnpromoteRequest{NoteID: noteID})
	return err
}

func (p *productionMemoryClient) MemoryPromote(ctx context.Context, req client.AggPromoteRequest) error {
	_, err := p.c.AggPromote(ctx, req)
	return err
}

func (p *productionMemoryClient) EcosystemQuery(ctx context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error) {
	return p.c.EcosystemQuery(ctx, req)
}

const memoryQueryTimeout = 30 * time.Second

const memoryListTimeout = 10 * time.Second

const memoryMutateTimeout = 30 * time.Second

func NewMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Cross-corpus memory tooling (Plan 9 aggregator + Plan 14 ecosystem)",
		Long: `zen memory groups the operator-facing tools for cross-corpus memory
retrieval. The Plan 9 D aggregator (per-project + global pin index, FTS5
+ sqlite-vec + wikilink graph) joins with the Plan 14 ecosystem RAG
dispatcher (Go/Python/TypeScript/Rust docs corpus) when --remote is
active on the query subcommand. RRF k=60 fuses the two streams.

Five leaves:
  query     Cross-corpus search (aggregator + optional ecosystem)
  list      List pinned notes from the global pin index
  pin       Pin a note (alias for promote at the CLI surface)
  unpin     Reverse a prior pin/promote
  promote   Pin a note to the global pin index (Plan 9 term)

inv-zen-146: pin / promote / unpin all REQUIRE a non-empty --reason.
The reason is anchored on the Plan 9 audit chain and surfaces via
` + "`zen audit-chain history`" + `.`,
		Example: `  # Cross-corpus query (Plan 9 aggregator only)
  zen memory query "context cancellation"

  # Cross-corpus with Plan 14 ecosystem RAG fusion
  zen memory query "context cancellation" --remote

  # List pinned notes
  zen memory list

  # Pin a note (inv-zen-146: --reason required)
  zen memory pin internal-platform-x/M0-doctrine --reason "load-bearing for max-scope"

  # Unpin a note
  zen memory unpin internal-platform-x/M0-doctrine --reason "superseded by N0"`,
	}
	cmd.AddCommand(newMemoryQueryCmd())
	cmd.AddCommand(newMemoryListCmd())
	cmd.AddCommand(newMemoryPinCmd())
	cmd.AddCommand(newMemoryUnpinCmd())
	cmd.AddCommand(newMemoryPromoteCmd())
	return cmd
}
