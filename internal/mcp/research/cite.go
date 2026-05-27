// SPDX-License-Identifier: MIT
// cite.go — citation verification + formatter (invariant).
//
// Type system:
// - RawCitation: producer-side type (synthesizer LLM output, raw
// hit list). Cannot flow into the formatter or downstream
// consumer; the type system rejects this at compile time.
// - VerifiedCitation: consumer-side type. Only Cite.Verify can
// produce a VerifiedCitation, and only after a HEAD probe + DNS
// check passes.
//
// Verification (invariant):
// - HEAD probe: outbound HTTP HEAD; must return 2xx or 3xx (4xx/5xx
// drops the citation).
// - DNS NXDOMAIN: any net.*Error with "no such host" drops the
// citation.
// - Non-HTTP URLs (file://, caronte:// code-graph node schemes) are passed
// through without probing — they are by-construction local.
// - Concurrent verification with bounded fan-out (default 8) for
// latency control on large citation sets.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type CiteVerifierOptions struct {
	HTTPClient *http.Client

	MaxConcurrent int

	Timeout time.Duration

	LocalSchemes []string
}

type CiteVerifier struct {
	opts         CiteVerifierOptions
	localSchemes map[string]struct{}
}

func NewCiteVerifier(opts CiteVerifierOptions) *CiteVerifier {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.MaxConcurrent == 0 {
		opts.MaxConcurrent = 8
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.LocalSchemes == nil {
		opts.LocalSchemes = []string{"file", "caronte"}
	}
	local := make(map[string]struct{}, len(opts.LocalSchemes))
	for _, s := range opts.LocalSchemes {
		local[strings.ToLower(s)] = struct{}{}
	}
	return &CiteVerifier{opts: opts, localSchemes: local}
}

var _ CiteService = (*CiteVerifier)(nil)

func (v *CiteVerifier) Verify(ctx context.Context, raw []RawCitation) ([]VerifiedCitation, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]VerifiedCitation, 0, len(raw))
	var mu sync.Mutex

	sem := make(chan struct{}, v.opts.MaxConcurrent)
	var wg sync.WaitGroup
	for _, r := range raw {
		r := r
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			status, ok := v.probeAndJudge(ctx, r.URL)
			if !ok {
				return
			}
			vc := VerifiedCitation{
				SourceID:   r.SourceID,
				URL:        r.URL,
				Title:      r.Title,
				VerifiedAt: time.Now().Unix(),
				HTTPStatus: status,
			}
			mu.Lock()
			out = append(out, vc)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

func (v *CiteVerifier) probeAndJudge(ctx context.Context, raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return 0, false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return 0, false
	}
	if _, isLocal := v.localSchemes[scheme]; isLocal {

		return 0, true
	}
	if scheme != "http" && scheme != "https" {
		return 0, false
	}
	status := v.probeStatus(ctx, raw)
	if status >= 200 && status < 400 {
		return status, true
	}
	return status, false
}

func (v *CiteVerifier) probeStatus(ctx context.Context, raw string) int {
	probeCtx, cancel := context.WithTimeout(ctx, v.opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, raw, nil)
	if err != nil {
		return 0
	}
	resp, err := v.opts.HTTPClient.Do(req)
	if err != nil {

		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func (v *CiteVerifier) Format(verified []VerifiedCitation) (string, []byte) {
	if len(verified) == 0 {
		return "", []byte("[]")
	}
	var md strings.Builder
	for i, c := range verified {
		title := c.Title
		if title == "" {
			title = c.URL
		}
		md.WriteString(fmt.Sprintf("[%d] %s — %s\n", i+1, title, c.URL))
	}
	structured := buildOTelEnvelope(verified)
	return md.String(), structured
}

func buildOTelEnvelope(verified []VerifiedCitation) []byte {
	type entry struct {
		SourceID   string `json:"source_id"`
		URL        string `json:"url"`
		Title      string `json:"title"`
		VerifiedAt int64  `json:"verified_at"`
		HTTPStatus int    `json:"http_status"`
	}
	entries := make([]entry, 0, len(verified))
	for _, c := range verified {
		entries = append(entries, entry{
			SourceID:   c.SourceID,
			URL:        c.URL,
			Title:      c.Title,
			VerifiedAt: c.VerifiedAt,
			HTTPStatus: c.HTTPStatus,
		})
	}

	b, err := json.Marshal(map[string]any{
		"gen_ai.citations": entries,
	})
	if err != nil {
		return []byte("[]")
	}
	return b
}
