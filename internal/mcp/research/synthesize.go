// SPDX-License-Identifier: MIT
// synthesize.go — calls the daemon HTTP /v1/messages endpoint.
//
// Posts an Anthropic-shaped messages request to the daemon's
// /v1/messages endpoint with X-Zen-Profile=research-synthesize.
// Today (Plan 4 timeline) /v1/messages is served by the Plan 2
// anthropic-bypass route — the X-Zen-Profile header is recorded for
// audit but does NOT route requests differently per profile. Plan 3
// (orchestrator) is the future extension point that will introduce
// per-profile multi-backend routing; this file's request shape is
// already compatible with that future change. Doc updated post-review
// I-2 to reflect actual current behaviour (Plan 3 not yet merged on
// main).
//
// Citation extraction: best-effort scan of fenced JSON code block in
// the response (`json {"citations":[...]}`) — captures synthesizer-
// emitted citations for inv-zen-075 verification downstream. Found
// citations are returned as RawCitation; the cite verifier (Task I-9)
// converts them to VerifiedCitation.
package research

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type SynthesizerOptions struct {
	DaemonURL string

	AuthToken string

	HTTPClient *http.Client

	Profile string

	Model string

	MaxTokens int

	Timeout time.Duration

	SystemPrompt string
}

type SynthesizerImpl struct {
	opts SynthesizerOptions
}

func NewSynthesizer(opts SynthesizerOptions) *SynthesizerImpl {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.Profile == "" {
		opts.Profile = "research-synthesize"
	}
	if opts.Model == "" {
		opts.Model = "opus-via-bypass"
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 4096
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.SystemPrompt == "" {
		opts.SystemPrompt = "You are a research synthesizer. Summarize the findings as a coherent report. Cite each source URL. Return citations as a fenced JSON block: ```json {\"citations\":[{\"source_id\":\"...\",\"url\":\"...\",\"title\":\"...\"}]} ```"
	}
	return &SynthesizerImpl{opts: opts}
}

var _ Synthesizer = (*SynthesizerImpl)(nil)

func (s *SynthesizerImpl) Synthesize(ctx context.Context, in SynthesizeInput) (SynthesizeOutput, error) {
	if s.opts.DaemonURL == "" {
		return SynthesizeOutput{}, errors.New("synthesize: DaemonURL not set")
	}

	if len(in.RawFindings) == 0 {
		return SynthesizeOutput{}, errors.New("synthesize: empty findings")
	}

	prompt := in.Prompt
	if prompt == "" {
		prompt = s.opts.SystemPrompt
	}
	findingsJSON, _ := json.Marshal(in.RawFindings)
	userText := "Findings (JSON):\n" + string(findingsJSON) + "\n\nProduce the synthesis."
	body, err := json.Marshal(map[string]any{
		"model":      s.opts.Model,
		"max_tokens": s.opts.MaxTokens,
		"system":     prompt,
		"messages": []map[string]any{
			{"role": "user", "content": userText},
		},
	})
	if err != nil {
		return SynthesizeOutput{}, fmt.Errorf("synthesize: marshal: %w", err)
	}

	if s.opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
	}

	url := strings.TrimRight(s.opts.DaemonURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SynthesizeOutput{}, fmt.Errorf("synthesize: new req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Zen-Profile", s.opts.Profile)
	if s.opts.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.opts.AuthToken)
	}

	resp, err := s.opts.HTTPClient.Do(req)
	if err != nil {
		return SynthesizeOutput{}, fmt.Errorf("synthesize: do: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return SynthesizeOutput{}, fmt.Errorf("synthesize: status %d: %s", resp.StatusCode, snippet(respBody))
	}

	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return SynthesizeOutput{}, fmt.Errorf("synthesize: parse: %w", err)
	}
	if len(anthropicResp.Content) == 0 {
		return SynthesizeOutput{Report: ""}, nil
	}
	report := anthropicResp.Content[0].Text
	citations := extractCitationsFromText(report)

	return SynthesizeOutput{
		Report:     report,
		Citations:  citations,
		Structured: respBody,
	}, nil
}

var citationsBlockRegex = regexp.MustCompile("(?s)```json\\s*(\\{[^`]*\"citations\"[^`]*\\})\\s*```")

func extractCitationsFromText(text string) []RawCitation {
	m := citationsBlockRegex.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	var envelope struct {
		Citations []RawCitation `json:"citations"`
	}
	if err := json.Unmarshal([]byte(m[1]), &envelope); err != nil {
		return nil
	}
	return envelope.Citations
}
