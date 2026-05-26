package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicToOpenAIRequest(t *testing.T) {
	canonical := []byte(`{
		"model": "deepseek-chat",
		"system": "you are terse",
		"max_tokens": 256,
		"temperature": 0.2,
		"messages": [{"role": "user", "content": "hi"}]
	}`)
	out, err := anthropicToOpenAIRequest(canonical, "deepseek-chat")
	if err != nil {
		t.Fatalf("anthropicToOpenAIRequest: %v", err)
	}
	var got struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal translated request: %v", err)
	}
	if got.Model != "deepseek-chat" || got.MaxTokens != 256 {
		t.Errorf("model/max_tokens wrong: %+v", got)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2 (system folded in)", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "you are terse" {
		t.Errorf("system message wrong: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "hi" {
		t.Errorf("user message wrong: %+v", got.Messages[1])
	}
}

func TestOpenAIToAnthropicResponse(t *testing.T) {
	openaiResp := []byte(`{
		"id": "chatcmpl-1",
		"model": "deepseek-chat",
		"choices": [{"message": {"role": "assistant", "content": "hello there"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 7}
	}`)
	body, usage, err := openAIToAnthropicResponse(openaiResp)
	if err != nil {
		t.Fatalf("openAIToAnthropicResponse: %v", err)
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 7 {
		t.Errorf("usage = (%d,%d), want (12,7)", usage.InputTokens, usage.OutputTokens)
	}
	var got struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal translated response: %v", err)
	}
	if got.Model != "deepseek-chat" {
		t.Errorf("model = %q, want deepseek-chat", got.Model)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "text" || got.Content[0].Text != "hello there" {
		t.Errorf("content block wrong: %+v", got.Content)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn (mapped from stop)", got.StopReason)
	}
}

func TestAnthropicToGeminiRequest(t *testing.T) {
	canonical := []byte(`{
		"model": "gemini-2.0-flash",
		"system": "be brief",
		"max_tokens": 128,
		"messages": [
			{"role": "user", "content": "q1"},
			{"role": "assistant", "content": "a1"},
			{"role": "user", "content": "q2"}
		]
	}`)
	out, err := anthropicToGeminiRequest(canonical)
	if err != nil {
		t.Fatalf("anthropicToGeminiRequest: %v", err)
	}
	var got struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		GenerationConfig struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal translated gemini request: %v", err)
	}
	if len(got.Contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(got.Contents))
	}
	if got.Contents[1].Role != "model" {
		t.Errorf("assistant role = %q, want model", got.Contents[1].Role)
	}
	if got.Contents[0].Parts[0].Text != "q1" {
		t.Errorf("first part text wrong: %+v", got.Contents[0])
	}
	if got.SystemInstruction.Parts[0].Text != "be brief" {
		t.Errorf("systemInstruction wrong: %+v", got.SystemInstruction)
	}
	if got.GenerationConfig.MaxOutputTokens != 128 {
		t.Errorf("maxOutputTokens = %d, want 128", got.GenerationConfig.MaxOutputTokens)
	}
}

func TestGeminiToAnthropicResponse(t *testing.T) {
	geminiResp := []byte(`{
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "gemini says hi"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 20, "candidatesTokenCount": 9}
	}`)
	body, usage, err := geminiToAnthropicResponse(geminiResp, "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("geminiToAnthropicResponse: %v", err)
	}
	if usage.InputTokens != 20 || usage.OutputTokens != 9 {
		t.Errorf("usage = (%d,%d), want (20,9)", usage.InputTokens, usage.OutputTokens)
	}
	if !strings.Contains(string(body), "gemini says hi") {
		t.Errorf("translated body missing text: %s", string(body))
	}
	var got struct {
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Model != "gemini-2.0-flash" {
		t.Errorf("model = %q, want gemini-2.0-flash", got.Model)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn (mapped from STOP)", got.StopReason)
	}
}

func TestTranslateRejectsMalformedRequest(t *testing.T) {
	if _, err := anthropicToOpenAIRequest([]byte("not json"), "m"); err == nil {
		t.Error("anthropicToOpenAIRequest accepted non-JSON body")
	}
	if _, err := anthropicToGeminiRequest([]byte("not json")); err == nil {
		t.Error("anthropicToGeminiRequest accepted non-JSON body")
	}
}

func TestResolveSystemTextString(t *testing.T) {
	got, err := resolveSystemText(json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatalf("resolveSystemText(string): %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestResolveSystemTextArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}},{"type":"text","text":"bye"}]`)
	got, err := resolveSystemText(raw)
	if err != nil {
		t.Fatalf("resolveSystemText(array): %v", err)
	}
	if got != "hi\nbye" {
		t.Errorf("got %q, want %q (newline-joined)", got, "hi\nbye")
	}
}

func TestResolveSystemTextEmpty(t *testing.T) {
	cases := []json.RawMessage{nil, json.RawMessage(``), json.RawMessage(`""`), json.RawMessage(`null`), json.RawMessage(`[]`)}
	for i, c := range cases {
		got, err := resolveSystemText(c)
		if err != nil {
			t.Errorf("case %d (%q): unexpected error %v", i, string(c), err)
		}
		if got != "" {
			t.Errorf("case %d (%q): got %q, want empty", i, string(c), got)
		}
	}
}

func TestResolveSystemTextUnsupportedBlock(t *testing.T) {
	cases := []json.RawMessage{
		json.RawMessage(`[{"type":"image","source":{"type":"base64"}}]`),
		json.RawMessage(`[{"type":"tool_use","id":"x"}]`),
		json.RawMessage(`[{"type":"tool_result","tool_use_id":"x"}]`),
	}
	for i, c := range cases {
		_, err := resolveSystemText(c)
		if err == nil {
			t.Errorf("case %d (%q): expected error for unsupported block type", i, string(c))
		}
	}
}

func TestResolveSystemTextMalformed(t *testing.T) {
	if _, err := resolveSystemText(json.RawMessage(`{"type":"text","text":"hi"}`)); err == nil {
		t.Error("resolveSystemText accepted a non-array object")
	}
	if _, err := resolveSystemText(json.RawMessage(`12345`)); err == nil {
		t.Error("resolveSystemText accepted a number")
	}
}

func TestResolveContentTextString(t *testing.T) {
	got, err := resolveContentText(json.RawMessage(`"hi"`))
	if err != nil {
		t.Fatalf("resolveContentText(string): %v", err)
	}
	if got != "hi" {
		t.Errorf("got %q, want %q", got, "hi")
	}
}

func TestResolveContentTextArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"part1"},{"type":"text","text":"part2","cache_control":{"type":"ephemeral"}}]`)
	got, err := resolveContentText(raw)
	if err != nil {
		t.Fatalf("resolveContentText(array): %v", err)
	}
	if got != "part1\npart2" {
		t.Errorf("got %q, want %q", got, "part1\npart2")
	}
}

func TestResolveContentTextEmptyAndUnsupported(t *testing.T) {
	for _, c := range []json.RawMessage{nil, json.RawMessage(``), json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`""`)} {
		got, err := resolveContentText(c)
		if err != nil {
			t.Errorf("empty case %q: %v", string(c), err)
		}
		if got != "" {
			t.Errorf("empty case %q: got %q", string(c), got)
		}
	}
	for _, c := range []json.RawMessage{
		json.RawMessage(`[{"type":"image","source":{}}]`),
		json.RawMessage(`[{"type":"tool_use","id":"x","name":"f"}]`),
		json.RawMessage(`[{"type":"tool_result","tool_use_id":"x"}]`),
	} {
		if _, err := resolveContentText(c); err == nil {
			t.Errorf("unsupported case %q: expected error", string(c))
		}
	}
}

func TestAnthropicToOpenAIRichBody(t *testing.T) {
	hermesBody := []byte(`{
		"model":"claude-opus-4-7",
		"max_tokens":4096,
		"system":[
			{"type":"text","text":"You are Claude."},
			{"type":"text","text":"Respond in JSON.","cache_control":{"type":"ephemeral"}}
		],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"context preamble","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"actual question"}
			]},
			{"role":"assistant","content":"prior answer"}
		]
	}`)
	out, err := anthropicToOpenAIRequest(hermesBody, "deepseek-chat")
	if err != nil {
		t.Fatalf("anthropicToOpenAIRequest(rich body): %v", err)
	}
	if strings.Contains(string(out), "cache_control") {
		t.Errorf("cache_control leaked to OpenAI body: %s", string(out))
	}
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal openai body: %v", err)
	}
	if got.Model != "deepseek-chat" {
		t.Errorf("model = %q, want deepseek-chat (override)", got.Model)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages len = %d, want 3 (system + user + assistant)", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "You are Claude.\nRespond in JSON." {
		t.Errorf("system message wrong: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "context preamble\nactual question" {
		t.Errorf("user message wrong: %+v", got.Messages[1])
	}
	if got.Messages[2].Role != "assistant" || got.Messages[2].Content != "prior answer" {
		t.Errorf("assistant message wrong: %+v", got.Messages[2])
	}
}

func TestAnthropicToGeminiRichBody(t *testing.T) {
	hermesBody := []byte(`{
		"model":"claude-opus-4-7",
		"max_tokens":2048,
		"system":[
			{"type":"text","text":"Be terse."},
			{"type":"text","text":"Avoid emojis.","cache_control":{"type":"ephemeral"}}
		],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"first part","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"second part"}
			]},
			{"role":"assistant","content":"reply"}
		]
	}`)
	out, err := anthropicToGeminiRequest(hermesBody)
	if err != nil {
		t.Fatalf("anthropicToGeminiRequest(rich body): %v", err)
	}
	if strings.Contains(string(out), "cache_control") {
		t.Errorf("cache_control leaked to Gemini body: %s", string(out))
	}
	var got struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal gemini body: %v", err)
	}
	if got.SystemInstruction.Parts[0].Text != "Be terse.\nAvoid emojis." {
		t.Errorf("systemInstruction wrong: %+v", got.SystemInstruction)
	}
	if len(got.Contents) != 2 {
		t.Fatalf("contents len = %d, want 2 (user + assistant)", len(got.Contents))
	}
	if got.Contents[0].Role != "user" || got.Contents[0].Parts[0].Text != "first part\nsecond part" {
		t.Errorf("user content wrong: %+v", got.Contents[0])
	}
	if got.Contents[1].Role != "model" || got.Contents[1].Parts[0].Text != "reply" {
		t.Errorf("assistant→model content wrong: %+v", got.Contents[1])
	}
}

func TestAnthropicToOpenAIRejectsUnsupportedBlocks(t *testing.T) {
	body := []byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"..."}}]}]
	}`)
	if _, err := anthropicToOpenAIRequest(body, "m"); err == nil {
		t.Error("anthropicToOpenAIRequest accepted image block")
	}
}

func TestAnthropicToGeminiRejectsUnsupportedBlocks(t *testing.T) {
	body := []byte(`{
		"model":"m","max_tokens":1,
		"system":[{"type":"tool_use","id":"x","name":"f"}],
		"messages":[{"role":"user","content":"hi"}]
	}`)
	if _, err := anthropicToGeminiRequest(body); err == nil {
		t.Error("anthropicToGeminiRequest accepted tool_use block in system")
	}
}

func TestResolveTextBlocksMissingTypeField(t *testing.T) {
	raw := json.RawMessage(`[{"text":"no type field"}]`)
	if _, err := resolveSystemText(raw); err == nil {
		t.Error("expected error for content block missing type field")
	}
	if _, err := resolveContentText(raw); err == nil {
		t.Error("expected error for content block missing type field (content path)")
	}
}

func TestResolveTextBlocksWhitespacePadded(t *testing.T) {
	cases := []json.RawMessage{
		json.RawMessage("  \t\n\r\"hi\"  \n"),
		json.RawMessage("\n[{\"type\":\"text\",\"text\":\"hi\"}]\t"),
	}
	for _, c := range cases {
		got, err := resolveSystemText(c)
		if err != nil {
			t.Errorf("case %q: %v", string(c), err)
		}
		if got != "hi" {
			t.Errorf("case %q: got %q, want %q", string(c), got, "hi")
		}
	}
}

func TestResolveTextBlocksSingleBlock(t *testing.T) {
	got, err := resolveSystemText(json.RawMessage(`[{"type":"text","text":"only"}]`))
	if err != nil {
		t.Fatalf("resolveSystemText(single block): %v", err)
	}
	if got != "only" {
		t.Errorf("got %q, want %q (no newline appended for single-block)", got, "only")
	}
}

func TestResolveTextBlocksMalformedArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":42}]`)
	if _, err := resolveSystemText(raw); err == nil {
		t.Error("expected error for malformed content-block array")
	}
}

func TestHasToolsField(t *testing.T) {

	body := []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
	req, err := parseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("parseCanonicalRequest: %v", err)
	}
	if hasToolsField(req) {
		t.Error("hasToolsField returned true for body without tools")
	}

	body = []byte(`{"model":"m","max_tokens":1,"tools":[{"name":"foo","input_schema":{}}],"messages":[{"role":"user","content":"hi"}]}`)
	req, err = parseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("parseCanonicalRequest(tools): %v", err)
	}
	if !hasToolsField(req) {
		t.Error("hasToolsField returned false for body with tools array")
	}

	body = []byte(`{"model":"m","max_tokens":1,"tools":[],"messages":[{"role":"user","content":"hi"}]}`)
	req, err = parseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("parseCanonicalRequest(empty tools): %v", err)
	}
	if !hasToolsField(req) {
		t.Error("hasToolsField returned false for body with empty tools array (caller still declared intent)")
	}
}
