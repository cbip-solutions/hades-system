// SPDX-License-Identifier: MIT
// Package ecosystem — citation.go
//
// Grammar-constrained citation enforcement per design contract=A Layer 1 +
// §4.2 step 10 + invariant (citation grammar tokens validated before
// answer emission).
//
// # Citation grammar
//
// token: '[doc_id:' integer ']'
//
// Each token references a QueryChunk.ChunkID present in the retrieval
// result set. Tokens MAY repeat in the answer; duplicates dedupe to a
// single CitationRef in the validated output. Mixed valid+invalid
// citations accept (one valid is enough); pure-invalid or pure-missing
// reject.
//
// # Modes (DoctrineProfile.CitationMode)
//
// CitationMandatoryGrammar — at least one valid token required.
// missing → reject + (optionally) reprompt + (persistent) abstain.
// all invalid → reject as ErrCitationInvalidID (first offending id).
// ≥1 valid → accept, dropping the invalid ones silently.
// CitationOptional — tokens optional; valid ones populate
// Citations; absent tokens → Accepted=true with empty Citations.
// CitationNone — citations off entirely; Validate always
// accepts.
//
// # Retry policy
//
// ValidateWithRetry attempts up to maxAttempts (≥1; values <1 coerced
// to 1) generator calls, feeding the prior validation failure into the
// next reprompt. Persistent failure → returns
// ValidationResult{AbstainTriggered: true, Attempts: maxAttempts}. The
// dispatcher consumes the abstain flag to emit EvtRAGAbstain.
//
// # Why both grammar generation AND validate-at-receive
//
// Some backends (Anthropic Haiku, DeepSeek-V3) support grammar-
// constrained decoding (token-level mask). Others (Ollama qwen2.5) do
// not. Validate-at-receive is the universal floor; grammar-decode is a
// quality-of-life upgrade. We never trust the generator's claim of
// compliance — FACTUM (arXiv 2601.05866) finds 57% of RAG-optimized
// citations unfaithful, BUT absent citation is a hard-fail signal we
// trust as evidence of generator drift.
//
// # Concurrency
//
// CitationValidator is immutable after construction; Validate and
// ValidateWithRetry are safe under concurrent use (the only mutation is
// to the AnswerGenerator, whose concurrency is the caller's problem).
//
// # invariant
//
// "Citation grammar tokens [doc_id:N] validated before answer emission"
// — this file is the validator. Any code path emitting QueryResult must
// route through Validate (D-9 dispatcher integration enforces this).
package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

var (
	ErrCitationMissing = errors.New("citation: no valid [doc_id:N] token in answer")

	ErrCitationInvalidID = errors.New("citation: [doc_id:N] references unknown chunk")
)

var defaultCitationRegex = regexp.MustCompile(`\[doc_id:(\d+)\]`)

type CitationConfig struct {
	Mode CitationMode

	// Regex overrides the default `\[doc_id:(\d+)\]` token grammar.
	// Group 1 MUST capture the integer ChunkID. nil → defaultCitationRegex.
	Regex *regexp.Regexp
}

// AnswerGenerator is the abstraction over the LLM call that produces an
// answer. The production implementation wraps the HADES design dispatcher
// (Haiku / DeepSeek / Ollama backend with grammar-mask where supported).
// Tests inject a fake returning canned outputs. Generate MUST honor
// ctx cancellation; the reprompt string is "" on the first attempt and
// a human-readable instruction on subsequent attempts (see
// buildReprompt).
type AnswerGenerator interface {
	Generate(ctx context.Context, query string, chunks []QueryChunk, reprompt string) (string, error)
}

type ValidationResult struct {
	Accepted         bool
	AnswerText       string
	Citations        []CitationRef
	Attempts         int
	AbstainTriggered bool
	RejectErr        error
}

type CitationValidator struct {
	mode CitationMode
	rx   *regexp.Regexp
}

func NewCitationValidator(cfg CitationConfig) (*CitationValidator, error) {
	if cfg.Mode == "" {
		cfg.Mode = CitationMandatoryGrammar
	}
	if cfg.Regex == nil {
		cfg.Regex = defaultCitationRegex
	}
	return &CitationValidator{mode: cfg.Mode, rx: cfg.Regex}, nil
}

func (v *CitationValidator) Validate(ctx context.Context, answer string, chunks []QueryChunk) (*ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v.mode == CitationNone {
		return &ValidationResult{Accepted: true, AnswerText: answer}, nil
	}

	matches := v.rx.FindAllStringSubmatch(answer, -1)
	chunkByID := indexQueryChunks(chunks)

	seen := make(map[int64]struct{}, len(matches))
	citations := make([]CitationRef, 0, len(matches))
	var firstInvalidID int64
	var sawInvalid bool

	for _, m := range matches {
		idInt, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {

			sawInvalid = true
			continue
		}
		if _, dup := seen[idInt]; dup {
			continue
		}
		seen[idInt] = struct{}{}
		c, ok := chunkByID[idInt]
		if !ok {
			sawInvalid = true
			if firstInvalidID == 0 {
				firstInvalidID = idInt
			}
			continue
		}
		citations = append(citations, CitationRef{
			ID:         fmt.Sprintf("doc_%d", idInt),
			ChunkID:    idInt,
			SymbolPath: c.SymbolPath,
			SourceURL:  c.SourceURL,
		})
	}
	sortCitations(citations)

	switch v.mode {
	case CitationOptional:

		return &ValidationResult{Accepted: true, AnswerText: answer, Citations: citations}, nil
	case CitationMandatoryGrammar:
		if len(citations) > 0 {

			return &ValidationResult{Accepted: true, AnswerText: answer, Citations: citations}, nil
		}

		if sawInvalid {
			return &ValidationResult{
				Accepted:   false,
				AnswerText: answer,
				RejectErr:  fmt.Errorf("%w (id=%d)", ErrCitationInvalidID, firstInvalidID),
			}, nil
		}
		return &ValidationResult{
			Accepted:   false,
			AnswerText: answer,
			RejectErr:  ErrCitationMissing,
		}, nil
	}

	return &ValidationResult{Accepted: true, AnswerText: answer, Citations: citations}, nil
}

func (v *CitationValidator) ValidateWithRetry(
	ctx context.Context,
	gen AnswerGenerator,
	query string,
	chunks []QueryChunk,
	maxAttempts int,
) (*ValidationResult, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	reprompt := ""
	var lastReject error = ErrCitationMissing
	var lastAnswer string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		answer, err := gen.Generate(ctx, query, chunks, reprompt)
		if err != nil {
			return nil, fmt.Errorf("citation: generator attempt %d: %w", attempt, err)
		}
		res, err := v.Validate(ctx, answer, chunks)
		if err != nil {
			return nil, err
		}
		res.Attempts = attempt
		if res.Accepted {
			return res, nil
		}

		lastReject = res.RejectErr
		lastAnswer = answer
		reprompt = buildReprompt(res.RejectErr, answer)
	}
	return &ValidationResult{
		Accepted:         false,
		AnswerText:       lastAnswer,
		AbstainTriggered: true,
		Attempts:         maxAttempts,
		RejectErr:        lastReject,
	}, nil
}

func indexQueryChunks(cs []QueryChunk) map[int64]QueryChunk {
	out := make(map[int64]QueryChunk, len(cs))
	for _, c := range cs {
		out[c.ChunkID] = c
	}
	return out
}

func sortCitations(cs []CitationRef) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].ChunkID < cs[j].ChunkID })
}

func buildReprompt(rejectErr error, _ string) string {
	switch {
	case errors.Is(rejectErr, ErrCitationMissing):
		return "Your previous answer omitted required citation tokens. Re-answer using [doc_id:N] for every claim referencing the supplied chunks."
	case errors.Is(rejectErr, ErrCitationInvalidID):
		return "Your previous answer cited an unknown chunk ID. Only cite IDs present in the provided chunks list."
	default:
		return "Re-answer with required [doc_id:N] citations referencing only the supplied chunks."
	}
}
