// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// cmd/zen-markdown-fallback-golden — Plan 12 Phase A fix-cycle I-2.
//
// Parity oracle for the Python wrapper-level markdown fallback in
// plugin/zen-swarm/renderers/__init__.py::_emit_markdown_fallback. The Go
// substrate (internal/citation/markdown_fallback.go::renderFootnote) is
// the cross-language source-of-truth; this binary invokes Go's renderer
// directly and prints the result so the Python parity test
// (plugin/zen-swarm/tests/renderers/test_markdown_fallback_go_parity.py)
// can compare byte-exact.
//
// Operator usage:
//
//	make bin/zen-markdown-fallback-golden
//	bin/zen-markdown-fallback-golden \
//	    -id c-test0001 \
//	    -payload "MergeEngine.Score()" \
//	    -audit-event-id evt-0001 \
//	    -project-id p \
//	    -doctrine default \
//	    -lane semantic \
//	    -confidence 0.5
//
// CI invocation: the Python parity test auto-discovers the binary at
// bin/zen-markdown-fallback-golden and falls back to an in-Python
// algorithmic replica when the binary is missing (e.g., before
// `make build`).
//
// Output the per-envelope CommonMark footnote (no trailing newline);
// matches what `MarkdownFallback.renderFootnote(env, sess)` returns.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func main() {
	id := flag.String("id", "", "Citation ID (e.g., c-test0001)")
	payload := flag.String("payload", "", "Payload string (verbatim, no escaping)")
	auditEventID := flag.String("audit-event-id", "", "Audit event ID (e.g., evt-0001)")
	projectID := flag.String("project-id", "", "Project ID (privacy boundary)")
	doctrine := flag.String("doctrine", "default", "Doctrine name (max-scope|default|capa-firewall)")
	laneStr := flag.String("lane", "semantic", "Retrieval lane (semantic|lexical|graph|rerank|temporal)")
	confidence := flag.Float64("confidence", 0.5, "Confidence in [0.0, 1.0]")
	expiration := flag.String("expiration", "", "Optional RFC3339 expiration (empty = no expiration)")

	flag.Parse()

	if *id == "" || *payload == "" || *auditEventID == "" || *projectID == "" {
		fmt.Fprintln(os.Stderr, "usage: zen-markdown-fallback-golden -id <X> -payload <Y> "+
			"-audit-event-id <Z> -project-id <P> [...]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	env := &citation.Envelope{
		ID:           citation.CitationID(*id),
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.RetrievalLane(*laneStr),
		AuditEventID: *auditEventID,
		Confidence:   *confidence,
		RRFScore:     0.01,
		RRFRank:      0,
		ProjectID:    *projectID,
		Payload:      *payload,
	}
	if *expiration != "" {
		t, err := time.Parse(time.RFC3339, *expiration)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid -expiration: %v\n", err)
			os.Exit(2)
		}
		env.Expiration = t
	}

	sess := citation.SessionContext{
		Doctrine: *doctrine,
		Platform: "markdown",
		Now:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	r := citation.NewMarkdownFallback(nil)
	out, err := r.Render(env, sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Render: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(out)
}
