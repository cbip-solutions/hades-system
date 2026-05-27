// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

type generator interface {
	Generate(ctx context.Context, model, prompt string, numPredict int) (string, error)
}

type HTTPPOSTer interface {
	FetchPOST(ctx context.Context, url string, body []byte, headers map[string]string) (*ollamaFetchResult, error)
}

type ollamaFetchResult struct {
	Body           []byte
	HTTPStatusCode int
}

type OllamaClient struct {
	BaseURL string

	HTTPPOSTer HTTPPOSTer

	Timeout time.Duration

	generator generator
}

type ollamaRequest struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	Stream  bool          `json:"stream"`
	Options ollamaOptions `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict  int     `json:"num_predict,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ollamaResponse struct {
	Response        string `json:"response"`
	EvalCount       int    `json:"eval_count,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	Done            bool   `json:"done,omitempty"`
}

func (c *OllamaClient) Generate(ctx context.Context, model, prompt string, numPredict int) (string, error) {

	if c.generator != nil {
		return c.generator.Generate(ctx, model, prompt, numPredict)
	}

	if c.BaseURL == "" {
		c.BaseURL = "http://127.0.0.1:11434"
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.HTTPPOSTer == nil {
		return "", errors.New("ollama: HTTPPOSTer not configured")
	}

	req := ollamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
		Options: ollamaOptions{
			NumPredict: numPredict,
		},
	}
	body, err := json.Marshal(req)
	if err != nil {

		return "", fmt.Errorf("ollama: marshal request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	result, err := c.HTTPPOSTer.FetchPOST(callCtx, c.BaseURL+"/api/generate", body, map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return "", fmt.Errorf("ollama: fetch: %w", err)
	}
	if result.HTTPStatusCode >= 400 {
		return "", fmt.Errorf("ollama: HTTP %d: %s", result.HTTPStatusCode, string(result.Body))
	}
	var resp ollamaResponse
	if err := json.Unmarshal(result.Body, &resp); err != nil {
		return "", fmt.Errorf("ollama: parse response (json): %w", err)
	}
	return resp.Response, nil
}

type ollamaPOSTerAdapter struct {
	rv *cache.Revalidator
}

func NewOllamaPOSTerAdapter(rv *cache.Revalidator) (HTTPPOSTer, error) {
	if rv == nil {
		return nil, errors.New("ollama: NewOllamaPOSTerAdapter: *cache.Revalidator must be non-nil")
	}
	return &ollamaPOSTerAdapter{rv: rv}, nil
}

func (a *ollamaPOSTerAdapter) FetchPOST(ctx context.Context, url string, body []byte, headers map[string]string) (*ollamaFetchResult, error) {
	res, err := a.rv.FetchPOST(ctx, url, cache.FetchPOSTOptions{
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	return &ollamaFetchResult{
		Body:           res.Body,
		HTTPStatusCode: res.HTTPStatusCode,
	}, nil
}

var _ HTTPPOSTer = (*ollamaPOSTerAdapter)(nil)
