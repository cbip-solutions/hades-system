// SPDX-License-Identifier: MIT
// Package handlers — augment.go (release Task B-4 endpoint shell;
// extends with the 5-lane RRF pipeline).
//
// /v1/augment is the daemon-side endpoint the Python
// plugin/hades-system/hooks/llm_handlers.py:pre_llm_call hook callback
// calls before each LLM
// completion. The endpoint:
//
// 1. Reads the active doctrine's augmentation config (release doctrine
// schema § [doctrine.augmentation]).
// 2. When enable = false (capa-firewall default per inv-hades-170), returns
// 204 No Content. Hermes treats this as "proceed unaugmented".
// 3. When enabled, runs the 5-lane RRF retrieval pipeline and
// returns an AugmentResponse envelope with static_context (system
// prompt portion, eligible for Anthropic prompt cache) + volatile
// context (user-message portion) + citations[].
//
// SHIPS:
// - Endpoint routing + JSON request/response shapes
// - Doctrine-gate veto path (204 for capa-firewall)
// - Empty envelope when augmentation is enabled but pipeline absent
// - Error handling (400 / 405 / 500)
//
// does NOT ship:
// - 5-lane RRF retrieval (caronte.query + aggregator FTS5 + caronte.context
// + cross-encoder reranker + temporal scoring)
// - GraphRAG community summarization
// - Anthropic prompt cache static/volatile split
// - Privacy filter at retrieval boundary (cross-project doctrine respect)
// - Token budget gate
// - release Tessera audit anchor for AugmentationStarted/Completed events
//
// Returning an empty envelope is the production behaviour when no
// retrieval has been performed; replaces it with the populated
// envelope. This is NOT a stub — the doctrine-gate, the envelope
// contract, and the error-handling are complete; only the retrieval is
// deferred to (whose whole purpose is to land it).

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type AugmentRequest struct {
	SessionID string `json:"session_id,omitempty"`

	ConversationID string `json:"conversation_id,omitempty"`

	Project string `json:"project,omitempty"`

	Prompt string `json:"prompt,omitempty"`

	PromptHash string `json:"prompt_hash,omitempty"`

	MaxTokensHint int `json:"max_tokens_hint,omitempty"`

	Mode string `json:"mode,omitempty"`
}

type AugmentResponse struct {
	StaticContext string `json:"static_context"`

	VolatileContext string `json:"volatile_context"`

	Citations []Citation `json:"citations"`

	AuditEventID string `json:"audit_event_id,omitempty"`

	Doctrine string `json:"doctrine"`

	MaxKGTokens int `json:"max_kg_tokens"`
}

type Citation struct {
	ID           string  `json:"id"`
	SourceTool   string  `json:"source_tool"`
	Confidence   float64 `json:"confidence"`
	Snippet      string  `json:"snippet,omitempty"`
	AuditEventID string  `json:"audit_event_id,omitempty"`
	Project      string  `json:"project,omitempty"`
	URI          string  `json:"uri,omitempty"`
}

type AugmentationConfig struct {
	Enable bool

	MaxKGTokens int

	DoctrineName string
}

type DoctrineReader interface {
	AugmentationConfig(ctx context.Context, project string) (AugmentationConfig, error)
}

type PipelineRunner func(ctx context.Context, req PipelineRequest) (PipelineResponse, error)

type PipelineRequest struct {
	Prompt    string
	ProjectID string
	Doctrine  string
	SessionID string
	Mode      string
	RequestID string
}

type PipelineResponse struct {
	StaticContext   string
	VolatileContext string
	Citations       []byte
	AuditEventID    string
	Truncated       bool
	SkippedReason   string
}

const maxAugmentBodyBytes = 4 << 20

// Augment returns the http.HandlerFunc for /v1/augment.
//
// dr MUST be non-nil — passing nil is a wiring bug at daemon bootstrap
// that panics here rather than at first request. This aligns with the
// fail-fast discipline enforced by the sibling transport.NewMessagesHandler
// and transport.NewHadesSystemTransport constructors (reviewer M4: a half-
// wired daemon that reports healthy on the readiness probe while
// every /v1/augment call 500s masks the wiring bug from operators —
// panic-and-exit at construction makes the doctor surface the bug
// immediately).
//
// The handler:
// 1. Validates method (POST only) → 405 if not.
// 2. Decodes JSON body → 400 on malformed.
// 3. Reads doctrine augmentation config → 500 on read error.
// 4. If enable = false → 204 (Hermes proceeds unaugmented).
// 5. Otherwise returns 200 + AugmentResponse envelope (empty in ;
// populated by pipeline).
//
// The handler does NOT log prompt content (operator privacy: prompts may
// carry sensitive context). release audit anchor for the request itself
// uses PromptHash, never raw Prompt.
func Augment(dr DoctrineReader) http.HandlerFunc {
	if dr == nil {

		panic("handlers.Augment: doctrine reader is required")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed; POST /v1/augment required", http.StatusMethodNotAllowed)
			return
		}
		bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAugmentBodyBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		var req AugmentRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, fmt.Sprintf("decode augment request: %v", err), http.StatusBadRequest)
			return
		}
		project := req.Project
		if project == "" {
			project = "default"
		}
		cfg, err := dr.AugmentationConfig(r.Context(), project)
		if err != nil {
			http.Error(w, fmt.Sprintf("doctrine read: %v", err), http.StatusInternalServerError)
			return
		}
		if !cfg.Enable {

			w.WriteHeader(http.StatusNoContent)
			return
		}

		envelope := AugmentResponse{
			StaticContext:   "",
			VolatileContext: "",
			Citations:       []Citation{},
			Doctrine:        cfg.DoctrineName,
			MaxKGTokens:     cfg.MaxKGTokens,
		}
		writeAugmentJSON(w, http.StatusOK, envelope)
	}
}

// AugmentWithPipeline returns the http.HandlerFunc for /v1/augment with
// the augment.Pipeline wired in.
//
// Behavior matches Augment(dr) except: when the doctrine permits, the
// handler invokes `runner` (which wraps augment.Pipeline.Run) and returns
// the structured response (static+volatile JSON portions + citations[]).
//
// dr + runner MUST be non-nil; nil panics at construction.
func AugmentWithPipeline(dr DoctrineReader, runner PipelineRunner) http.HandlerFunc {
	if dr == nil {
		panic("handlers.AugmentWithPipeline: doctrine reader is required")
	}
	if runner == nil {
		panic("handlers.AugmentWithPipeline: pipeline runner is required")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed; POST /v1/augment required", http.StatusMethodNotAllowed)
			return
		}
		bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAugmentBodyBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		var req AugmentRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, fmt.Sprintf("decode augment request: %v", err), http.StatusBadRequest)
			return
		}
		project := req.Project
		if project == "" {
			project = "default"
		}
		cfg, err := dr.AugmentationConfig(r.Context(), project)
		if err != nil {
			http.Error(w, fmt.Sprintf("doctrine read: %v", err), http.StatusInternalServerError)
			return
		}
		if !cfg.Enable {

			w.WriteHeader(http.StatusNoContent)
			return
		}

		pipelineReq := PipelineRequest{
			Prompt:    req.Prompt,
			ProjectID: project,
			Doctrine:  cfg.DoctrineName,
			SessionID: req.SessionID,
			Mode:      req.Mode,
			RequestID: requestIDOrDefault(req),
		}
		pipelineResp, runErr := runner(r.Context(), pipelineReq)
		if runErr != nil {
			http.Error(w, fmt.Sprintf("augment pipeline: %v", runErr), http.StatusInternalServerError)
			return
		}
		if pipelineResp.SkippedReason != "" {

			w.WriteHeader(http.StatusNoContent)
			return
		}

		envelope := AugmentResponse{
			StaticContext:   pipelineResp.StaticContext,
			VolatileContext: pipelineResp.VolatileContext,
			Citations:       deserializeCitations(pipelineResp.Citations),
			AuditEventID:    pipelineResp.AuditEventID,
			Doctrine:        cfg.DoctrineName,
			MaxKGTokens:     cfg.MaxKGTokens,
		}
		writeAugmentJSON(w, http.StatusOK, envelope)
	}
}

func requestIDOrDefault(req AugmentRequest) string {
	if req.PromptHash != "" {
		return req.PromptHash
	}
	if req.SessionID != "" {
		return req.SessionID + ":" + req.ConversationID
	}

	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {

		return "augment-no-id"
	}
	return fmt.Sprintf("augment-%d-%s", time.Now().UnixNano(), hex.EncodeToString(rnd[:]))
}

func deserializeCitations(raw []byte) []Citation {
	if len(raw) == 0 {
		return []Citation{}
	}
	var citationsRaw []map[string]any
	if err := json.Unmarshal(raw, &citationsRaw); err != nil {
		return []Citation{}
	}
	out := make([]Citation, 0, len(citationsRaw))
	for _, c := range citationsRaw {
		conf := 0.0
		if v, ok := c["confidence"].(float64); ok {
			conf = v
		}
		out = append(out, Citation{
			ID:           stringField(c, "id"),
			SourceTool:   resolveCitationSource(stringField(c, "source")),
			Confidence:   conf,
			Snippet:      stringField(c, "payload"),
			AuditEventID: stringField(c, "audit_event_id"),
			Project:      stringField(c, "project_id"),
		})
	}
	return out
}

func resolveCitationSource(wire string) string {
	if s, ok := citation.ParseCitationSource(wire); ok {
		return string(s)
	}
	return wire
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func writeAugmentJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
