// SPDX-License-Identifier: MIT
// Package cli — memory_query.go.
//
// `hades memory query` cross-corpus retrieval with RRF k=60 fusion.
//
// Without --remote: only the release D aggregator is queried (single source).
// With --remote: both the aggregator + the release ecosystem RAG dispatcher
// are queried in parallel, then results are fused via Reciprocal Rank Fusion
// .
//
// Soft-fail semantics:
// - both sources succeed → fused output
// - one source errors → render the other (no error surfaced to operator)
// - both sources error → return an error mentioning both
//
// The RRF formula `1.0 / float64(k+rank+1)` matches the release D-10
// cross-ecosystem fusion implementation (internal/research/ecosystem/dispatcher.go);
// keeping the constant in sync across layers avoids re-ranking drift.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MemoryQueryFlags struct {
	FreeText string
	Remote   bool
	Limit    int
	Format   string
}

type MemoryHit struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Source   string  `json:"source"`
	URL      string  `json:"url,omitempty"`
	Snippet  string  `json:"snippet,omitempty"`
	RRFScore float64 `json:"rrf_score"`
}

var validMemoryQueryFormats = map[string]bool{"text": true, "json": true}

const rrfFusionK = 60

const memoryQueryOverFetch = 20

func newMemoryQueryCmd() *cobra.Command {
	flags := MemoryQueryFlags{}
	cmd := &cobra.Command{
		Use:   "query <free-text>",
		Short: "Cross-corpus memory search (aggregator + optional ecosystem fusion)",
		Long: `Search the Plan 9 D aggregator (per-project memory + global pin index).
With --remote, also query the Plan 14 ecosystem RAG dispatcher and fuse
results via RRF k=60 (cross-corpus reciprocal rank fusion).

Output formats:
  text  (default) — tabwriter table with SOURCE/SCORE/TITLE/URL/SNIPPET
  json            — array of MemoryHit objects (key, title, source, url, snippet, rrf_score)

Failure modes:
  - empty <free-text>      → exit 1 (operator-recoverable)
  - one source errors      → soft-fail: render what the other returned
  - both sources error     → exit 2 (unrecoverable transport / decode)`,
		Example: " # Aggregator-only (default)\n  hades memory query \"max-scope doctrine\"\n\n # Cross-corpus fusion with release ecosystem\n  hades memory query \"context cancellation\" --remote --limit 20\n\n # JSON for jq pipelines\n  hades memory query \"tessera\" --remote --format json | jq '.[].title'",

		Args: cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.FreeText = strings.Join(args, " ")
			c := memoryClientFactory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), memoryQueryTimeout)
			defer cancel()
			return RunMemoryQuery(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.Remote, "remote", false,
		"include Plan 14 ecosystem RAG (cross-corpus RRF fusion)")
	cmd.Flags().IntVar(&flags.Limit, "limit", 10, "result limit (default 10)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunMemoryQuery(ctx context.Context, c MemoryClient, flags MemoryQueryFlags, w io.Writer) error {
	if strings.TrimSpace(flags.FreeText) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory query: free-text argument is required"))
	}
	format := strings.TrimSpace(flags.Format)
	if format == "" {
		format = "text"
	}
	if !validMemoryQueryFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory query: --format %q must be one of text|json", format))
	}

	overFetch := memoryQueryOverFetch
	if flags.Limit > overFetch {
		overFetch = flags.Limit
	}

	type aggResult struct {
		rows []client.AggQueryResultRow
		err  error
	}
	type ecoResult struct {
		resp *client.EcosystemQueryResponse
		err  error
	}
	aggCh := make(chan aggResult, 1)
	ecoCh := make(chan ecoResult, 1)

	go func() {
		resp, err := c.MemoryQuery(ctx, client.AggQueryRequest{
			Text:  flags.FreeText,
			Limit: overFetch,
		})
		if err != nil {
			aggCh <- aggResult{err: err}
			return
		}
		if resp == nil {
			aggCh <- aggResult{rows: nil}
			return
		}
		aggCh <- aggResult{rows: resp.Results}
	}()

	go func() {
		if !flags.Remote {
			ecoCh <- ecoResult{}
			return
		}
		resp, err := c.EcosystemQuery(ctx, client.EcosystemQueryRequest{
			Query:      flags.FreeText,
			MaxResults: overFetch,
		})
		ecoCh <- ecoResult{resp: resp, err: err}
	}()

	aggRes := <-aggCh
	ecoRes := <-ecoCh

	if aggRes.err != nil && ecoRes.err != nil {
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("memory query: both sources failed: agg=%v eco=%v",
			aggRes.err, ecoRes.err))
	}
	if !flags.Remote && aggRes.err != nil {

		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("memory query: aggregator failed: %w", aggRes.err))
	}

	var ecoResp *client.EcosystemQueryResponse
	if ecoRes.err == nil {
		ecoResp = ecoRes.resp
	}
	fused := rrfFuseMemory(aggRes.rows, ecoResp, rrfFusionK)
	if flags.Limit > 0 && len(fused) > flags.Limit {
		fused = fused[:flags.Limit]
	}

	switch format {
	case "json":

		if fused == nil {
			fused = []MemoryHit{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(fused)
	default:
		return writeMemoryQueryText(w, fused)
	}
}

func rrfFuseMemory(aggRows []client.AggQueryResultRow, ecoResp *client.EcosystemQueryResponse, k int) []MemoryHit {
	type meta struct {
		title   string
		source  string
		url     string
		snippet string
	}
	scores := map[string]float64{}
	metas := map[string]meta{}

	for rank, r := range aggRows {
		key := "agg:" + r.NoteID
		scores[key] += 1.0 / float64(k+rank+1)
		metas[key] = meta{title: r.Title, source: "aggregator", snippet: r.Snippet}
	}
	if ecoResp != nil {
		for rank, chunk := range ecoResp.Chunks {
			key := "eco:" + chunk.SymbolPath + "@" + chunk.Version
			scores[key] += 1.0 / float64(k+rank+1)
			metas[key] = meta{
				title:   chunk.SymbolPath,
				source:  "ecosystem_docs",
				url:     chunk.SourceURL,
				snippet: truncateKnowledge(chunk.ContentText, 120),
			}
		}
	}

	hits := make([]MemoryHit, 0, len(scores))
	for key, score := range scores {
		m := metas[key]
		hits = append(hits, MemoryHit{
			Key:      key,
			Title:    m.title,
			Source:   m.source,
			URL:      m.url,
			Snippet:  m.snippet,
			RRFScore: score,
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].RRFScore != hits[j].RRFScore {
			return hits[i].RRFScore > hits[j].RRFScore
		}
		return hits[i].Key < hits[j].Key
	})
	return hits
}

func writeMemoryQueryText(w io.Writer, hits []MemoryHit) error {
	if len(hits) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tSCORE\tTITLE\tURL\tSNIPPET")
	for _, h := range hits {
		fmt.Fprintf(tw, "%s\t%.4f\t%s\t%s\t%s\n",
			h.Source,
			h.RRFScore,
			truncateKnowledge(h.Title, 50),
			truncateKnowledge(h.URL, 50),
			truncateKnowledge(h.Snippet, 60),
		)
	}
	return tw.Flush()
}
