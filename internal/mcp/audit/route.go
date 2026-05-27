// SPDX-License-Identifier: MIT
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RouteRequest struct {
	Diff string

	CriteriaPrompt string

	ReviewerFamily string

	ReviewerModel string
}

type dispatchRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Messages  []dispatchMessage `json:"messages"`
}

type dispatchMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dispatchResponse struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Content []responseContent `json:"content"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type rawVerdict struct {
	Classification string   `json:"classification"`
	Concerns       []string `json:"concerns"`
	Suggestions    []string `json:"suggestions"`
}

type Router struct {
	daemonBaseURL string
	authToken     string
	defaultModel  string
	httpClient    *http.Client
}

const DefaultRouterTimeout = 120 * time.Second

type RouterOption func(*Router)

func WithTimeout(d time.Duration) RouterOption {
	return func(r *Router) {
		if d <= 0 {
			panic(fmt.Sprintf("audit: WithTimeout requires d > 0, got %v", d))
		}
		r.httpClient.Timeout = d
	}
}

func NewRouter(daemonBaseURL, authToken, defaultModel string, opts ...RouterOption) *Router {
	r := &Router{
		daemonBaseURL: strings.TrimRight(daemonBaseURL, "/"),
		authToken:     authToken,
		defaultModel:  defaultModel,
		httpClient:    &http.Client{Timeout: DefaultRouterTimeout},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Router) RouteCall(ctx context.Context, req RouteRequest) (Verdict, error) {
	if req.Diff == "" || req.CriteriaPrompt == "" || req.ReviewerFamily == "" || req.ReviewerModel == "" {
		return Verdict{}, fmt.Errorf("%w (Diff/CriteriaPrompt/ReviewerFamily/ReviewerModel)", ErrEmptyRouteRequest)
	}

	userContent := req.CriteriaPrompt + "\n" + req.Diff

	body := dispatchRequest{
		Model:     req.ReviewerModel,
		MaxTokens: 1024,
		Messages: []dispatchMessage{
			{Role: "user", Content: userContent},
		},
	}

	bodyBytes, _ := json.Marshal(body)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		r.daemonBaseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.authToken)
	httpReq.Header.Set("X-HADES-Profile", "audit-reviewer")
	httpReq.Header.Set("X-HADES-Family-Constraint", req.ReviewerFamily)

	httpReq.Header.Set("X-HADES-Model-Hint", req.ReviewerModel)

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return Verdict{}, fmt.Errorf("audit: dispatch HTTP call: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Verdict{}, fmt.Errorf("audit: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("audit: dispatcher returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var dispResp dispatchResponse
	if err := json.Unmarshal(respBytes, &dispResp); err != nil {
		return Verdict{}, fmt.Errorf("audit: parse dispatcher response: %w", err)
	}

	text := ""
	for _, c := range dispResp.Content {
		if c.Type == "text" && c.Text != "" {
			text = c.Text
			break
		}
	}
	if text == "" {
		return Verdict{}, ErrEmptyContentBlock
	}

	verdict, err := parseVerdictFromText(text, req.ReviewerFamily, dispResp.Model)
	if err != nil {
		return Verdict{}, fmt.Errorf("audit: parse verdict from LLM output: %w", err)
	}
	return verdict, nil
}

var ErrEmptyContentBlock = errors.New("audit: dispatcher returned no text content")

var ErrEmptyRouteRequest = errors.New("audit: RouteRequest has empty required field")

// errNoJSON is returned by extractJSONObject when the input contains no
// '{' character. Unexported because it surfaces only via parseVerdictFromText's
// wrapped error message; callers do not branch on it (review I-1).
var errNoJSON = errors.New("audit: LLM output contains no JSON object")

var errUnbalancedJSON = errors.New("audit: LLM output has unbalanced braces")

func extractJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", errNoJSON
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if escape {

			escape = false
			continue
		}
		if inStr {
			switch c {
			case '\\':
				escape = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], nil
			}
		}
	}
	return "", errUnbalancedJSON
}

func parseVerdictFromText(text, reviewerProvider, reviewerModel string) (Verdict, error) {
	jsonText, err := extractJSONObject(text)
	if err != nil {
		return Verdict{}, fmt.Errorf("LLM output is not valid verdict JSON: %w; text=%q", err, truncate(text, 200))
	}

	var raw rawVerdict
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return Verdict{}, fmt.Errorf("LLM output is not valid verdict JSON: %w; text=%q", err, truncate(text, 200))
	}

	cls, err := ParseClassification(raw.Classification)
	if err != nil {
		return Verdict{}, fmt.Errorf("LLM returned invalid classification: %w", err)
	}

	concerns := raw.Concerns
	if concerns == nil {
		concerns = []string{}
	}
	suggestions := raw.Suggestions
	if suggestions == nil {
		suggestions = []string{}
	}

	return Verdict{
		Classification:   cls,
		Concerns:         concerns,
		Suggestions:      suggestions,
		ReviewerProvider: reviewerProvider,
		ReviewerModel:    reviewerModel,
	}, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
