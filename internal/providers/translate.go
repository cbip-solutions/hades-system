// SPDX-License-Identifier: MIT
// internal/providers/translate.go
//
// Canonical ↔ provider wire-format translation for the HADES design backends
// that do NOT speak the Anthropic Messages API natively.
//
// The CANONICAL format is the Anthropic /v1/messages JSON shape — it is
// what bypass.Client and AnthropicPaygoBackend already speak natively,
// so the dispatcher's TierRequest.Body always carries a canonical-encoded
// request and every backend must return a canonical-encoded response.
//
// - anthropic_paygo speaks canonical natively → no function here.
// - openai_compat: anthropicToOpenAIRequest / openAIToAnthropicResponse
// - gemini: anthropicToGeminiRequest / geminiToAnthropicResponse
//
// Scope: text-only request/response + token usage.
// Tool-use blocks, vision parts, and SSE streaming are NOT translated —
// a request carrying a non-text content block is rejected by the
// backend (Capabilities advertises SupportsToolUse=false /
// SupportsStreaming=false for these two backends, so the resolver never
// routes such traffic here). A future plan extends the translation.
package providers

import (
	"encoding/json"
	"fmt"
)

type canonicalRequest struct {
	Model       string             `json:"model"`
	System      json.RawMessage    `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	Messages    []canonicalMessage `json:"messages"`
	Tools       []json.RawMessage  `json:"tools,omitempty"`
	ToolChoice  json.RawMessage    `json:"tool_choice,omitempty"`
}

type canonicalMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type canonicalResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []canonicalContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      canonicalUsage          `json:"usage"`
}

type canonicalContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type canonicalUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

func parseCanonicalRequest(body []byte) (canonicalRequest, error) {
	var req canonicalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return canonicalRequest{}, fmt.Errorf("providers.translate: malformed canonical request: %w", err)
	}
	return req, nil
}

func resolveSystemText(raw json.RawMessage) (string, error) {
	return resolveTextBlocks(raw, "system")
}

func resolveContentText(raw json.RawMessage) (string, error) {
	return resolveTextBlocks(raw, "content")
}

func resolveTextBlocks(raw json.RawMessage, label string) (string, error) {

	trimmed := bytesTrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == `""` || string(trimmed) == "[]" {
		return "", nil
	}

	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", fmt.Errorf("providers.translate: %s: malformed string: %w", label, err)
		}
		return s, nil
	}

	if trimmed[0] != '[' {
		return "", fmt.Errorf("providers.translate: %s: expected string or content-block array, got %s", label, leadByte(trimmed))
	}
	var blocks []contentBlock
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return "", fmt.Errorf("providers.translate: %s: malformed content-block array: %w", label, err)
	}
	parts := make([]string, 0, len(blocks))
	for i, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "":
			return "", fmt.Errorf("providers.translate: %s[%d]: content block missing type field", label, i)
		default:

			return "", fmt.Errorf("providers.translate: %s[%d]: unsupported content-block type %q (v0.20.0 supports %q only)", label, i, b.Type, "text")
		}

		_ = b.CacheControl
	}
	return joinNewline(parts), nil
}

func hasToolsField(req canonicalRequest) bool {
	return req.Tools != nil
}

func bytesTrimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

func leadByte(b []byte) string {
	if len(b) == 0 {
		return "<empty>"
	}
	return fmt.Sprintf("%q", string(b[0]))
}

func joinNewline(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	n := len(parts) - 1
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for i, p := range parts {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, p...)
	}
	return string(out)
}

type openAIRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Messages    []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func anthropicToOpenAIRequest(body []byte, modelOverride string) ([]byte, error) {
	req, err := parseCanonicalRequest(body)
	if err != nil {
		return nil, err
	}
	systemText, err := resolveSystemText(req.System)
	if err != nil {
		return nil, fmt.Errorf("providers.translate: openai system: %w", err)
	}
	model := req.Model
	if modelOverride != "" {
		model = modelOverride
	}
	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if systemText != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: systemText})
	}
	for i, m := range req.Messages {
		content, err := resolveContentText(m.Content)
		if err != nil {
			return nil, fmt.Errorf("providers.translate: openai msg[%d] content: %w", i, err)
		}
		msgs = append(msgs, openAIMessage{Role: m.Role, Content: content})
	}
	out := openAIRequest{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("providers.translate: encode openai request: %w", err)
	}
	return encoded, nil
}

func openAIToAnthropicResponse(body []byte) ([]byte, TokenUsage, error) {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, TokenUsage{}, fmt.Errorf("providers.translate: malformed openai response: %w", err)
	}
	text := ""
	stop := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
		stop = mapOpenAIStopReason(resp.Choices[0].FinishReason)
	}
	canon := canonicalResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      resp.Model,
		Content:    []canonicalContentBlock{{Type: "text", Text: text}},
		StopReason: stop,
		Usage: canonicalUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	encoded, err := json.Marshal(canon)
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("providers.translate: encode canonical response: %w", err)
	}
	return encoded, TokenUsage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens}, nil
}

func mapOpenAIStopReason(r string) string {
	switch r {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "":
		return ""
	default:
		return r
	}
}

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

func anthropicToGeminiRequest(body []byte) ([]byte, error) {
	req, err := parseCanonicalRequest(body)
	if err != nil {
		return nil, err
	}
	systemText, err := resolveSystemText(req.System)
	if err != nil {
		return nil, fmt.Errorf("providers.translate: gemini system: %w", err)
	}
	contents := make([]geminiContent, 0, len(req.Messages))
	for i, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		content, err := resolveContentText(m.Content)
		if err != nil {
			return nil, fmt.Errorf("providers.translate: gemini msg[%d] content: %w", i, err)
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: content}},
		})
	}
	out := geminiRequest{
		Contents: contents,
		GenerationConfig: geminiGenConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}
	if systemText != "" {
		out.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: systemText}}}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("providers.translate: encode gemini request: %w", err)
	}
	return encoded, nil
}

// geminiToAnthropicResponse translates a Gemini generateContent response
// to the canonical shape and extracts token usage. usageMetadata's
// promptTokenCount/candidatesTokenCount map to input/output tokens. The
// Gemini finishReason "STOP" maps to the canonical "end_turn";
// "MAX_TOKENS" maps to "max_tokens". model is supplied by the backend
// (Gemini responses do not echo the model name).
func geminiToAnthropicResponse(body []byte, model string) ([]byte, TokenUsage, error) {
	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, TokenUsage{}, fmt.Errorf("providers.translate: malformed gemini response: %w", err)
	}
	text := ""
	stop := ""
	if len(resp.Candidates) > 0 {
		if len(resp.Candidates[0].Content.Parts) > 0 {
			text = resp.Candidates[0].Content.Parts[0].Text
		}
		stop = mapGeminiStopReason(resp.Candidates[0].FinishReason)
	}
	canon := canonicalResponse{
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    []canonicalContentBlock{{Type: "text", Text: text}},
		StopReason: stop,
		Usage: canonicalUsage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		},
	}
	encoded, err := json.Marshal(canon)
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("providers.translate: encode canonical response: %w", err)
	}
	return encoded, TokenUsage{
		InputTokens:  resp.UsageMetadata.PromptTokenCount,
		OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
	}, nil
}

func mapGeminiStopReason(r string) string {
	switch r {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "":
		return ""
	default:
		return r
	}
}

// keychainAccount is the account component of every hades-system Keychain
// entry — the form documented in HANDOFF
// (security add-generic-password -s "hades-system/<provider>" -a "hades-system").
// Backend constructors pass it to keychain.Resolver.Lookup.
const keychainAccount = "hades-system"

func capBody(body []byte) string {
	if len(body) > 512 {
		return string(append(body[:512:512], []byte("…[truncated]")...))
	}
	return string(body)
}
