// SPDX-License-Identifier: MIT
// Package cli — knowledge_remote.go.
//
// RunKnowledgeQueryRemote implements `hades knowledge query --remote`. Called
// from RunKnowledgeQuery when flags.Remote=true. Dispatches to the daemon's
// POST /v1/knowledge/ecosystem/query endpoint, which routes through the
//
// invariant amendment: --remote is NOW OPERATIONAL.
// The sentinel short-circuit previously rendered a deferred-message
// pointer; it is replaced by a live round-trip to the daemon. The
// invariant boundary is preserved by routing distinction:
// - --remote=true → ecosystem RAG over ingested corpus
// (daemon-side Dispatcher; no open-web queries from the daemon).
// - --remote=false → release aggregator (FTS5 over local docs;
// internal/knowledge package returns ErrRemoteNotShipped if a
// caller bypasses the CLI and passes Query{Remote: true}).
//
// Flags added by this task (only meaningful with --remote):
//
// --ecosystem go|python|typescript|rust filter to one ecosystem
// (empty = router decides)
// --version <semver> version context
// (empty = 5-layer cascade)
// --doctrine max-scope|default|capa-firewall
// --max-results N default 10
// --remote-format json|human output format (default human)
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

var validEcosystems = map[string]bool{
	"go": true, "python": true, "typescript": true, "rust": true,
}

var validDoctrines = map[string]bool{
	"max-scope": true, "default": true, "capa-firewall": true,
}

var validRemoteFormats = map[string]bool{"json": true, "human": true}

func RunKnowledgeQueryRemote(ctx context.Context, c KnowledgeClient, flags KnowledgeQueryFlags, w io.Writer) error {
	eco := strings.TrimSpace(flags.Ecosystem)
	if eco != "" && !validEcosystems[eco] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--ecosystem %q must be one of go|python|typescript|rust", eco))
	}

	doctrine := strings.TrimSpace(flags.Doctrine)
	if doctrine != "" && !validDoctrines[doctrine] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--doctrine %q must be one of max-scope|default|capa-firewall", doctrine))
	}

	format := strings.TrimSpace(flags.RemoteFormat)
	if format == "" {
		format = "human"
	}
	if !validRemoteFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--remote-format %q must be one of json|human", format))
	}

	maxResults := flags.RemoteMaxResults
	if maxResults < 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--max-results must be non-negative; got %d", maxResults))
	}
	if maxResults == 0 {
		maxResults = 10
	}

	req := client.EcosystemQueryRequest{
		Query:      flags.FreeText,
		Ecosystem:  eco,
		Version:    flags.Version,
		Doctrine:   doctrine,
		MaxResults: maxResults,
	}

	resp, err := c.EcosystemQuery(ctx, req)
	if err != nil {
		return classifyEcosystemError(err, "remote-query")
	}

	if resp.Abstained {
		_, ferr := fmt.Fprintf(w, "abstained: %s\n", resp.AbstainReason)
		return ferr
	}

	switch format {
	case "json":
		return writeEcosystemJSON(w, resp)
	default:
		return writeEcosystemHuman(w, resp)
	}
}

func writeEcosystemJSON(w io.Writer, resp *client.EcosystemQueryResponse) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func writeEcosystemHuman(w io.Writer, resp *client.EcosystemQueryResponse) error {
	if len(resp.Chunks) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SYMBOL\tPACKAGE\tVERSION\tKIND\tSCORE\tURL")
	for _, c := range resp.Chunks {
		score := c.RerankerScore
		if score == 0 {
			score = c.SimilarityScore
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.3f\t%s\n",
			truncateKnowledge(c.SymbolPath, 40),
			truncateKnowledge(c.PackageName, 20),
			c.Version,
			c.Kind,
			score,
			truncateKnowledge(c.SourceURL, 50),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if resp.Provenance.DetectedVersion != "" {
		fmt.Fprintf(w, "\ndetected version: %s (layer %d, method: %s)\n",
			resp.Provenance.DetectedVersion,
			resp.Provenance.DetectionLayer,
			resp.Provenance.RoutingMethod)
	}
	return nil
}

func classifyEcosystemError(err error, op string) error {
	if err == nil {
		return nil
	}
	if IsRecoverable(err) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("ecosystem: %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("ecosystem: %s: %w", op, err))
}
