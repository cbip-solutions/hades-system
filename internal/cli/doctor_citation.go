// SPDX-License-Identifier: MIT
// Package cli — doctor_citation.go
//
// - citation.envelope.serialize-roundtrip
// - citation.renderers (7 platforms tested)
// - citation.audit-chain.hades://audit-handler-functional
//
// ships the daemon-side citation envelope + hades://audit handler
// + markdown_fallback renderer. The "7 platforms tested" check counts
// the markdown_fallback renderer + 6 platform renderers
// — at HADES design ship time the count is 1; reading 1/7 is
// expected and rendered as warn until HADES design ships the remaining 6.
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const citationProbeTimeout = 3 * time.Second

type CitationProber interface {
	CitationProbe(ctx context.Context, check string) (*client.CitationProbeResp, error)
}

func NewDoctorCitationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "citation",
		Short: "Citation system checks (HADES design; 3 checks per design contract)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Citation system (HADES design)", runCitationChecks)
		},
	}
}

func runCitationChecks(ctx context.Context, c *client.Client) []CheckResult {
	return runCitationChecksWith(ctx, c)
}

func runCitationChecksWith(ctx context.Context, p CitationProber) []CheckResult {
	checks := []struct {
		probe string
		name  string
		hint  string
	}{
		{
			probe: "envelope-serialize-roundtrip",
			name:  "citation.envelope.serialize-roundtrip",
			hint:  "JSON round-trip property test failed; rerun: go test -run TestCitationEnvelopeRoundTrip ./internal/citation/...",
		},
		{
			probe: "renderers",
			name:  "citation.renderers",
			hint:  "renderer count <7 (HADES design ships markdown_fallback only; HADES design ships 6 platform renderers)",
		},
		{
			probe: "audit-handler-functional",
			name:  "citation.audit-chain.hades://audit-handler-functional",
			hint:  "/v1/audit/event/* unreachable; verify daemon health: hades daemon status",
		},
	}
	out := make([]CheckResult, 0, 3)
	for _, ch := range checks {
		cctx, cancel := context.WithTimeout(ctx, citationProbeTimeout)
		r, err := p.CitationProbe(cctx, ch.probe)
		cancel()
		out = append(out, citationResultFrom(ch.name, r, err, ch.hint))
	}
	return out
}

func citationResultFrom(name string, r *client.CitationProbeResp, err error, hint string) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}
