// SPDX-License-Identifier: MIT
// llm_fake.go is an in-process fake for the daemon's HTTP surface that
// internal/mcp/research/ depends on:
//
//	POST /v1/messages
//	GET  /v1/budget/cap_status
//	POST /v1/budget/record
//	GET  /v1/research/cache/get
//	POST /v1/research/cache/set
//	POST /v1/audit/emit
//
// Used downstream by:
//   - tests/compliance/inv_zen_075_test.go
//   - tests/compliance/inv_zen_076_test.go
//   - internal/mcp/research/synthesize_test.go
//   - internal/mcp/research/dispatch_test.go
//   - internal/mcp/research/agentic_test.go
//   - tests/adversarial/research_attack_test.go
//
// The fake is configured via LLMFakeOptions and observable via
// SeenRequests/SeenProfiles methods after the test exercises it.
package testharness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

type LLMFakeOptions struct {
	SynthesizeText string

	GapDetected   bool
	FollowupQuery string

	AllowBudget bool

	CacheSeed map[string][]byte

	FailEmitWithStatus int

	allowBudgetExplicit bool
}

type LLMFake struct {
	server *httptest.Server
	opts   LLMFakeOptions

	mu                sync.Mutex
	seenProfiles      []string
	seenMessages      []json.RawMessage
	seenAuditEmits    []json.RawMessage
	seenCacheSets     []json.RawMessage
	seenBudgetRecords []json.RawMessage
	cache             map[string][]byte
}

func NewLLMFake(opts LLMFakeOptions) *LLMFake {

	if !opts.AllowBudget {

		if opts.SynthesizeText == "" && opts.FollowupQuery == "" && len(opts.CacheSeed) == 0 && opts.FailEmitWithStatus == 0 && !opts.GapDetected {
			opts.AllowBudget = true
			opts.SynthesizeText = "ok"
		}
	}
	cache := make(map[string][]byte)
	for k, v := range opts.CacheSeed {
		cache[k] = v
	}
	f := &LLMFake{opts: opts, cache: cache}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", f.handleMessages)
	mux.HandleFunc("/v1/budget/cap_status", f.handleCapStatus)
	mux.HandleFunc("/v1/budget/record", f.handleBudgetRecord)
	mux.HandleFunc("/v1/research/cache/get", f.handleCacheGet)
	mux.HandleFunc("/v1/research/cache/set", f.handleCacheSet)
	mux.HandleFunc("/v1/audit/emit", f.handleAuditEmit)
	f.server = httptest.NewServer(mux)
	return f
}

func (f *LLMFake) URL() string { return f.server.URL }

func (f *LLMFake) Close() { f.server.Close() }

func (f *LLMFake) SeenProfiles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seenProfiles))
	copy(out, f.seenProfiles)
	return out
}

func (f *LLMFake) SeenMessages() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.seenMessages))
	copy(out, f.seenMessages)
	return out
}

func (f *LLMFake) SeenAuditEmits() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.seenAuditEmits))
	copy(out, f.seenAuditEmits)
	return out
}

func (f *LLMFake) SeenBudgetRecords() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.seenBudgetRecords))
	copy(out, f.seenBudgetRecords)
	return out
}

func (f *LLMFake) SeenCacheSets() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.seenCacheSets))
	copy(out, f.seenCacheSets)
	return out
}

func (f *LLMFake) handleMessages(w http.ResponseWriter, r *http.Request) {
	body, _ := llmReadBody(r)
	f.mu.Lock()
	f.seenProfiles = append(f.seenProfiles, r.Header.Get("X-Zen-Profile"))
	f.seenMessages = append(f.seenMessages, body)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	text := f.opts.SynthesizeText
	if f.opts.GapDetected {

		envelope := map[string]any{
			"gap_detected":   true,
			"followup_query": f.opts.FollowupQuery,
		}
		b, _ := json.Marshal(envelope)
		text = string(b)
	}

	resp := map[string]any{
		"id":   "msg_test_001",
		"type": "message",
		"role": "assistant",
		"content": []map[string]any{{
			"type": "text",
			"text": text,
		}},
		"model":         "opus-via-bypass",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  12,
			"output_tokens": 24,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *LLMFake) handleCapStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if f.opts.AllowBudget {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"axis":          "stage",
			"value":         "design",
			"allowed":       true,
			"remaining_usd": 4.20,
			"blocked_scope": "",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"axis":          "stage",
		"value":         "design",
		"allowed":       false,
		"remaining_usd": 0.0,
		"blocked_scope": "stage",
	})
}

func (f *LLMFake) handleBudgetRecord(w http.ResponseWriter, r *http.Request) {
	body, _ := llmReadBody(r)
	f.mu.Lock()
	f.seenBudgetRecords = append(f.seenBudgetRecords, body)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *LLMFake) handleCacheGet(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	f.mu.Lock()
	v, ok := f.cache[hash]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(v)
}

func (f *LLMFake) handleCacheSet(w http.ResponseWriter, r *http.Request) {
	body, _ := llmReadBody(r)
	var payload struct {
		Hash     string          `json:"hash"`
		Response json.RawMessage `json:"response"`
		TTLSecs  int64           `json:"ttl_secs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.cache[payload.Hash] = payload.Response
	f.seenCacheSets = append(f.seenCacheSets, body)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *LLMFake) handleAuditEmit(w http.ResponseWriter, r *http.Request) {
	body, _ := llmReadBody(r)
	f.mu.Lock()
	f.seenAuditEmits = append(f.seenAuditEmits, body)
	f.mu.Unlock()
	if f.opts.FailEmitWithStatus != 0 {
		w.WriteHeader(f.opts.FailEmitWithStatus)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func llmReadBody(r *http.Request) (json.RawMessage, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}

	return json.RawMessage(body), nil
}
