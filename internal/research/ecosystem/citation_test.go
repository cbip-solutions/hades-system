package ecosystem

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestCitation_ValidTokens_Pass(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{
		{ChunkID: 1, SymbolPath: "crypto/sha256.Sum256", SourceURL: "https://pkg.go.dev/crypto/sha256#Sum256"},
		{ChunkID: 2, SymbolPath: "crypto/sha512.Sum512", SourceURL: "https://pkg.go.dev/crypto/sha512#Sum512"},
	}
	answer := "Use [doc_id:1] for SHA-256, or [doc_id:2] for SHA-512."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("valid tokens must accept; got %+v", res)
	}
	if len(res.Citations) != 2 {
		t.Errorf("want 2 citations; got %d", len(res.Citations))
	}
	if res.Citations[0].ID != "doc_1" || res.Citations[1].ID != "doc_2" {
		t.Errorf("ID format: %+v", res.Citations)
	}
	if res.Citations[0].ChunkID != 1 || res.Citations[1].ChunkID != 2 {
		t.Errorf("ChunkID: %+v", res.Citations)
	}
	if res.Citations[0].SymbolPath != "crypto/sha256.Sum256" {
		t.Errorf("SymbolPath: %+v", res.Citations[0])
	}
	if res.Citations[1].SourceURL != "https://pkg.go.dev/crypto/sha512#Sum512" {
		t.Errorf("SourceURL: %+v", res.Citations[1])
	}
	if res.AnswerText != answer {
		t.Errorf("AnswerText must round-trip; got %q", res.AnswerText)
	}
}

func TestCitation_Missing_Reject(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	answer := "Use Sum256 from crypto/sha256 (no citation token)."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Accepted {
		t.Errorf("missing citation must reject")
	}
	if !errors.Is(res.RejectErr, ErrCitationMissing) {
		t.Errorf("RejectErr must be ErrCitationMissing; got %v", res.RejectErr)
	}
}

func TestCitation_InvalidID_Reject(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}, {ChunkID: 2}}
	answer := "See [doc_id:99] which doesn't exist."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Accepted {
		t.Errorf("invalid citation must reject")
	}
	if !errors.Is(res.RejectErr, ErrCitationInvalidID) {
		t.Errorf("RejectErr must be ErrCitationInvalidID; got %v", res.RejectErr)
	}
	if !strings.Contains(res.RejectErr.Error(), "id=99") {
		t.Errorf("RejectErr should include offending id; got %v", res.RejectErr)
	}
}

func TestCitation_DuplicateTokens_Accepted(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	answer := "[doc_id:1] First mention. [doc_id:1] Second mention."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("duplicate same-ID accepted (single citation): %+v", res)
	}
	if len(res.Citations) != 1 {
		t.Errorf("dedup citations; got %d", len(res.Citations))
	}
}

func TestCitation_RetryUpTo3_PersistentFailure_Abstains(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	generator := &fakeAnswerGenerator{
		outputs: []string{
			"no citation here",
			"still nothing",
			"really nothing",
			"[doc_id:1] finally cited (but we're past the retry count)",
		},
	}
	result, err := v.ValidateWithRetry(context.Background(), generator, "query", chunks, 3)
	if err != nil {
		t.Fatalf("ValidateWithRetry: %v", err)
	}
	if result.Accepted {
		t.Errorf("persistent uncited generation across 3 retries must trigger abstain")
	}
	if !result.AbstainTriggered {
		t.Errorf("AbstainTriggered must be true")
	}
	if generator.calls() != 3 {
		t.Errorf("generator must be invoked exactly 3 times (initial + 2 retries == 3); got %d", generator.calls())
	}
	if result.Attempts != 3 {
		t.Errorf("Attempts must equal 3; got %d", result.Attempts)
	}
}

func TestCitation_FirstRetrySucceeds(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	generator := &fakeAnswerGenerator{
		outputs: []string{
			"no citation",
			"[doc_id:1] here",
		},
	}
	result, err := v.ValidateWithRetry(context.Background(), generator, "query", chunks, 3)
	if err != nil {
		t.Fatalf("ValidateWithRetry: %v", err)
	}
	if !result.Accepted {
		t.Errorf("retry success path must accept: %+v", result)
	}
	if generator.calls() != 2 {
		t.Errorf("generator must be invoked twice; got %d", generator.calls())
	}
	if result.Attempts != 2 {
		t.Errorf("Attempts must equal 2; got %d", result.Attempts)
	}
}

func TestCitation_OptionalMode_AcceptsUncited(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationOptional)
	chunks := []QueryChunk{{ChunkID: 1}}
	answer := "no citation here"
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("optional mode must accept uncited: %+v", res)
	}
	if len(res.Citations) != 0 {
		t.Errorf("no tokens → empty Citations; got %d", len(res.Citations))
	}
}

func TestCitation_OptionalMode_PopulatesValidCitations(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationOptional)
	chunks := []QueryChunk{{ChunkID: 3, SymbolPath: "foo.Bar"}}
	answer := "use [doc_id:3] please"
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("optional mode must accept: %+v", res)
	}
	if len(res.Citations) != 1 || res.Citations[0].ChunkID != 3 {
		t.Errorf("expected 1 citation chunk_id=3; got %+v", res.Citations)
	}
}

func TestCitation_NoneMode_AlwaysAccepts(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationNone)
	chunks := []QueryChunk{}
	res, err := v.Validate(context.Background(), "literally anything", chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("none mode must always accept")
	}
	if len(res.Citations) != 0 {
		t.Errorf("none mode must not emit citations; got %d", len(res.Citations))
	}
}

func TestCitation_PopulatesCitationRefFields(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 7, SymbolPath: "foo.Bar", SourceURL: "https://example.com/foo#Bar"}}
	answer := "[doc_id:7] usage example"
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted || len(res.Citations) != 1 {
		t.Fatalf("expected single citation; got %+v", res)
	}
	c := res.Citations[0]
	if c.ID != "doc_7" {
		t.Errorf("ID = %q; want doc_7", c.ID)
	}
	if c.ChunkID != 7 || c.SymbolPath != "foo.Bar" || !strings.HasPrefix(c.SourceURL, "https://example.com") {
		t.Errorf("CitationRef fields incorrect: %+v", c)
	}
}

func TestCitation_DeterministicOrder(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}, {ChunkID: 2}, {ChunkID: 3}}
	answer := "[doc_id:3] high. [doc_id:1] low. [doc_id:2] mid."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted || len(res.Citations) != 3 {
		t.Fatalf("expected 3 citations; got %+v", res)
	}
	for i, c := range res.Citations {
		if c.ChunkID != int64(i+1) {
			t.Errorf("citations[%d].ChunkID = %d; want %d", i, c.ChunkID, i+1)
		}
	}
}

func TestCitation_ValidateWithRetry_MaxAttemptsBelow1_Coerced(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	for _, n := range []int{0, -1, -100} {
		gen := &fakeAnswerGenerator{outputs: []string{"no citation"}}
		res, err := v.ValidateWithRetry(context.Background(), gen, "q", chunks, n)
		if err != nil {
			t.Fatalf("ValidateWithRetry(%d): %v", n, err)
		}
		if !res.AbstainTriggered {
			t.Errorf("n=%d expected AbstainTriggered=true; got %+v", n, res)
		}
		if gen.calls() != 1 {
			t.Errorf("n=%d expected exactly 1 generator call; got %d", n, gen.calls())
		}
	}
}

func TestCitation_GeneratorError_Bubbles(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	want := errors.New("backend down")
	gen := &fakeAnswerGenerator{forceErr: want}
	res, err := v.ValidateWithRetry(context.Background(), gen, "q", chunks, 3)
	if err == nil {
		t.Fatalf("expected error; got %+v", res)
	}
	if !errors.Is(err, want) {
		t.Errorf("error chain must contain backend error; got %v", err)
	}
}

func TestCitation_ContextCancelled_Validate(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := v.Validate(ctx, "[doc_id:1] x", []QueryChunk{{ChunkID: 1}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
}

func TestCitation_ContextCancelled_ValidateWithRetry(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gen := &fakeAnswerGenerator{outputs: []string{"no citation", "[doc_id:1] x"}}
	_, err := v.ValidateWithRetry(ctx, gen, "q", chunks, 3)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
	if gen.calls() != 0 {
		t.Errorf("generator must not be invoked; got %d calls", gen.calls())
	}
}

func TestCitation_RepromptUsesPriorFailure(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}

	t.Run("missing→reprompt mentions tokens", func(t *testing.T) {
		t.Parallel()
		gen := &capturingGenerator{outputs: []string{"no citation", "[doc_id:1] ok"}}
		_, err := v.ValidateWithRetry(context.Background(), gen, "q", chunks, 3)
		if err != nil {
			t.Fatalf("ValidateWithRetry: %v", err)
		}
		if len(gen.reprompts) < 2 {
			t.Fatalf("expected 2 reprompts captured; got %d", len(gen.reprompts))
		}
		if gen.reprompts[0] != "" {
			t.Errorf("first reprompt must be empty; got %q", gen.reprompts[0])
		}
		if !strings.Contains(gen.reprompts[1], "[doc_id:N]") {
			t.Errorf("reprompt for missing must mention grammar; got %q", gen.reprompts[1])
		}
	})

	t.Run("invalid→reprompt mentions unknown id", func(t *testing.T) {
		t.Parallel()
		gen := &capturingGenerator{outputs: []string{"[doc_id:99] wrong", "[doc_id:1] ok"}}
		_, err := v.ValidateWithRetry(context.Background(), gen, "q", chunks, 3)
		if err != nil {
			t.Fatalf("ValidateWithRetry: %v", err)
		}
		if len(gen.reprompts) < 2 {
			t.Fatalf("expected 2 reprompts captured; got %d", len(gen.reprompts))
		}
		if !strings.Contains(gen.reprompts[1], "unknown chunk ID") {
			t.Errorf("reprompt for invalid-id should mention unknown ID; got %q", gen.reprompts[1])
		}
	})
}

func TestCitation_NewValidator_DefaultsAppliedWhenZero(t *testing.T) {
	t.Parallel()
	v, err := NewCitationValidator(CitationConfig{})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	chunks := []QueryChunk{{ChunkID: 5}}
	res, err := v.Validate(context.Background(), "[doc_id:5] x", chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted || len(res.Citations) != 1 {
		t.Errorf("default config must accept valid token; got %+v", res)
	}
	res2, err := v.Validate(context.Background(), "no token", chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res2.Accepted {
		t.Errorf("default mode must be mandatory; got accepted=true")
	}
}

func TestCitation_CustomRegex(t *testing.T) {
	t.Parallel()
	rx := regexp.MustCompile(`<<docref:(\d+)>>`)
	v, err := NewCitationValidator(CitationConfig{Mode: CitationMandatoryGrammar, Regex: rx})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	chunks := []QueryChunk{{ChunkID: 4}}
	res, err := v.Validate(context.Background(), "use <<docref:4>> ok", chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted || len(res.Citations) != 1 || res.Citations[0].ChunkID != 4 {
		t.Errorf("custom regex must match; got %+v", res)
	}
}

func TestCitation_Concurrent_Validate(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1, SymbolPath: "a"}, {ChunkID: 2, SymbolPath: "b"}}
	answer := "[doc_id:1] then [doc_id:2]."
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				res, err := v.Validate(context.Background(), answer, chunks)
				if err != nil {
					t.Errorf("Validate: %v", err)
					return
				}
				if !res.Accepted || len(res.Citations) != 2 {
					t.Errorf("concurrent validate inconsistent: %+v", res)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestCitation_ValidateWithRetry_CancelMidLoop(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	ctx, cancel := context.WithCancel(context.Background())

	gen := &cancelOnNthGenerator{
		outputs: []string{"no citation", "still none", "really none"},
		cancel:  cancel,
		cancelN: 1,
	}
	_, err := v.ValidateWithRetry(ctx, gen, "q", chunks, 3)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from in-loop check; got %v", err)
	}
	if gen.callsN != 1 {
		t.Errorf("generator must be invoked exactly once before cancel; got %d", gen.callsN)
	}
}

func TestCitation_BuildReprompt_DefaultBranch(t *testing.T) {
	t.Parallel()

	custom := errors.New("some other reject")
	got := buildReprompt(custom, "prior answer")
	if !strings.Contains(got, "[doc_id:N]") {
		t.Errorf("default reprompt should mention grammar; got %q", got)
	}
}

func TestCitation_OnlyInvalidThenValid_StillAccepts(t *testing.T) {
	t.Parallel()
	v := newTestCitationValidator(t, CitationMandatoryGrammar)
	chunks := []QueryChunk{{ChunkID: 1}}
	answer := "[doc_id:99] bad. [doc_id:1] good."
	res, err := v.Validate(context.Background(), answer, chunks)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Accepted {
		t.Errorf("mixed valid+invalid with ≥1 valid must accept: %+v", res)
	}
	if len(res.Citations) != 1 || res.Citations[0].ChunkID != 1 {
		t.Errorf("only valid id retained; got %+v", res.Citations)
	}
}

func newTestCitationValidator(t *testing.T, mode CitationMode) *CitationValidator {
	t.Helper()
	v, err := NewCitationValidator(CitationConfig{Mode: mode})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	return v
}

type fakeAnswerGenerator struct {
	mu       sync.Mutex
	outputs  []string
	callsN   int
	forceErr error
}

func (g *fakeAnswerGenerator) Generate(ctx context.Context, query string, chunks []QueryChunk, reprompt string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.forceErr != nil {
		g.callsN++
		return "", g.forceErr
	}
	if g.callsN >= len(g.outputs) {
		return "", errors.New("exhausted")
	}
	out := g.outputs[g.callsN]
	g.callsN++
	return out, nil
}

func (g *fakeAnswerGenerator) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.callsN
}

type cancelOnNthGenerator struct {
	mu      sync.Mutex
	outputs []string
	callsN  int
	cancel  context.CancelFunc
	cancelN int
}

func (g *cancelOnNthGenerator) Generate(ctx context.Context, query string, chunks []QueryChunk, reprompt string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.callsN >= len(g.outputs) {
		return "", errors.New("exhausted")
	}
	out := g.outputs[g.callsN]
	g.callsN++
	if g.callsN == g.cancelN {
		g.cancel()
	}
	return out, nil
}

type capturingGenerator struct {
	mu        sync.Mutex
	outputs   []string
	callsN    int
	reprompts []string
}

func (g *capturingGenerator) Generate(ctx context.Context, query string, chunks []QueryChunk, reprompt string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reprompts = append(g.reprompts, reprompt)
	if g.callsN >= len(g.outputs) {
		return "", errors.New("exhausted")
	}
	out := g.outputs[g.callsN]
	g.callsN++
	return out, nil
}
